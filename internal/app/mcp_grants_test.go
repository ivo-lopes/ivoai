package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workers"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestWorkerRequiresExactToolsAndExplicitWritePlan(t *testing.T) {
	a := autoTestApp(t, t.TempDir(), "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	s := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	for _, name := range []string{"read_tool", "write_tool", "unknown_tool", "delete_tool"} {
		var annotation *mcp.ToolAnnotations
		if name == "read_tool" {
			annotation = &mcp.ToolAnnotations{ReadOnlyHint: true}
		}
		s.AddTool(&mcp.Tool{Name: name, Annotations: annotation, InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})
	}
	upstream := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, &mcp.StreamableHTTPOptions{JSONResponse: true}))
	defer upstream.Close()
	if err := a.MCPAdd("fixture", upstream.URL); err != nil {
		t.Fatal(err)
	}
	cfg, _ := a.Store.Load()
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	id, _ := session.NewID()
	now := time.Now().UTC()
	root := t.TempDir()
	v := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeDirect, PrimaryExecutor: "codex", PrimaryModel: session.UnknownModel(), WorkingDirectory: root, MaxWorkers: 2, State: session.StateRunning, MemoryStatus: "disabled", ContextStatus: "disabled", ServerStatus: "not-connected"}
	if err := store.Create(v); err != nil {
		t.Fatal(err)
	}
	request := workers.Request{Executor: "codex", Directory: root, Runtime: root, Task: "fixture"}
	for _, tool := range []string{"read_tool", "write_tool", "unknown_tool"} {
		required, err := a.checkMCPPlan(context.Background(), cfg, []routing.TaskInput{{AllowedMCPTools: map[string][]string{"fixture": {tool}}}})
		if err != nil || required != (tool != "read_tool") {
			t.Fatalf("tool approval requirement: %s %v %v", tool, required, err)
		}
	}
	for _, tc := range []struct {
		name             string
		tools            []string
		approved, denied bool
	}{
		{"server-only", nil, false, true}, {"exact read", []string{"read_tool"}, false, false},
		{"unapproved write", []string{"write_tool"}, false, true}, {"unknown", []string{"unknown_tool"}, false, true},
		{"approved write", []string{"write_tool"}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.approved {
				plan := "plan_" + strings.Repeat("a", 32)
				if _, err := store.Update(id, func(v *session.Session) error { v.PlanID = plan; return nil }); err != nil {
					t.Fatal(err)
				}
				if err := store.RequestDecision(id, plan, "plan"); err != nil {
					t.Fatal(err)
				}
				if err := store.ResolveDecision(id, plan, true); err != nil {
					t.Fatal(err)
				}
			}
			task := routing.Task{TaskInput: routing.TaskInput{ID: "fixture", Role: "research", AllowedMCPs: []string{"fixture"}, AllowedMCPTools: map[string][]string{"fixture": tc.tools}}}
			got, err := a.prepareWorkerAccess(context.Background(), cfg, store, id, task, request)
			if (err != nil) != tc.denied {
				t.Fatalf("denied=%v err=%v", tc.denied, err)
			}
			if err != nil {
				return
			}
			defer got.Release()
			_, grants := got.Access.Metadata()
			if len(grants["fixture"]) != len(tc.tools) {
				t.Fatal("scope expanded")
			}
		})
	}
}

func TestDirectRegistryProjectionDeniesWriteAndUnknownEvenInFull(t *testing.T) {
	a := autoTestApp(t, t.TempDir(), "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	var calls atomic.Int32
	s := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	for _, name := range []string{"read_tool", "write_tool", "unknown_tool", "delete_tool"} {
		var annotation *mcp.ToolAnnotations
		if name == "read_tool" {
			annotation = &mcp.ToolAnnotations{ReadOnlyHint: true}
		}
		s.AddTool(&mcp.Tool{Name: name, Annotations: annotation, InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			calls.Add(1)
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "fixture"}}}, nil
		})
	}
	upstream := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	defer upstream.Close()
	if err := a.MCPAdd("fixture", upstream.URL); err != nil {
		t.Fatal(err)
	}
	cfg, _ := a.Store.Load()
	cfg.OpenCode.PermissionMode = "full"
	for _, executor := range []string{"codex", "claude", "opencode"} {
		k, err := a.prepareSessionKnowledge(context.Background(), cfg, nil, executor, t.TempDir(), t.TempDir(), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"read_tool", "write_tool", "unknown_tool", "delete_tool"} {
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `","arguments":{}}}`
			r, _ := http.NewRequest("POST", k.external.URL(0), strings.NewReader(body))
			r.Header.Set("Authorization", "Bearer "+k.external.Token())
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Accept", "application/json, text/event-stream")
			response, err := http.DefaultClient.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			payload, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if name != "read_tool" && !strings.Contains(string(payload), "MCP_DENIED") {
				t.Fatalf("%s unapproved tool allowed", executor)
			}
		}
		k.close()
	}
	if calls.Load() != 3 {
		t.Fatalf("unexpected upstream calls %d", calls.Load())
	}
}

func TestPrimaryCannotInheritWorkerToolsOrCompletedTaskGrants(t *testing.T) {
	cfg := config.Default()
	v := session.Session{State: session.StateRunning, PlanID: "plan_" + strings.Repeat("a", 32), Tasks: []session.TaskMetadata{
		{ID: "primary", ExecutionMode: "primary", State: session.StatePrimary, AllowedMCPTools: map[string][]string{"plane": {"read_tool"}}},
		{ID: "worker", ExecutionMode: "worker", State: session.StateRunning, AllowedMCPTools: map[string][]string{"plane": {"write_tool"}, "github": {"read_tool"}}},
	}}
	if allowed, _ := primaryMCPGrant(v, cfg, "plane", "read_tool"); allowed {
		t.Fatal("before approval")
	}
	v.Decisions = []session.Decision{{ID: v.PlanID, Kind: "plan", State: "approved"}}
	if allowed, approved := primaryMCPGrant(v, cfg, "plane", "read_tool"); !allowed || !approved {
		t.Fatal("missing exact primary grant")
	}
	for _, target := range [][2]string{{"plane", "write_tool"}, {"github", "read_tool"}, {"plane", "delete_tool"}} {
		if allowed, _ := primaryMCPGrant(v, cfg, target[0], target[1]); allowed {
			t.Fatal("primary inherited worker/unrelated grant")
		}
	}
	v.Tasks[0].State = session.StateCompleted
	if allowed, _ := primaryMCPGrant(v, cfg, "plane", "read_tool"); allowed {
		t.Fatal("completed grant remains")
	}
}

func TestPrimaryNativeGrantHonorsExplicitRestrictions(t *testing.T) {
	cfg := config.Default()
	for _, tc := range []struct {
		name    string
		grants  map[string][]string
		tool    string
		allowed bool
	}{
		{"implicit", nil, "code_inspect", true},
		{"unrelated entry", map[string][]string{"plane": {"read_tool"}}, "code_inspect", true},
		{"empty denies", map[string][]string{nativeCodeMCP: {}}, "code_inspect", false},
		{"listed only", map[string][]string{nativeCodeMCP: {"graft_repo_map"}}, "graft_repo_map", true},
		{"unlisted denied", map[string][]string{nativeCodeMCP: {"graft_repo_map"}}, "code_inspect", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := session.Session{State: session.StateRunning, PlanID: "plan_" + strings.Repeat("a", 32), Tasks: []session.TaskMetadata{{ID: "primary", ExecutionMode: "primary", State: session.StatePrimary, AllowedMCPTools: tc.grants}}}
			v.Decisions = []session.Decision{{ID: v.PlanID, Kind: "plan", State: "approved"}}
			if allowed, _ := primaryMCPGrant(v, cfg, nativeCodeMCP, tc.tool); allowed != tc.allowed {
				t.Fatalf("allowed=%v want=%v", allowed, tc.allowed)
			}
		})
	}
}

func TestPrimaryNativeGrantAggregatesOnlyReadyPrimaryTasks(t *testing.T) {
	cfg := config.Default()
	v := session.Session{State: session.StateRunning, PlanID: "plan_" + strings.Repeat("a", 32), Tasks: []session.TaskMetadata{
		{ID: "restricted", ExecutionMode: "primary", State: session.StatePrimary, AllowedMCPTools: map[string][]string{nativeCodeMCP: {}}},
		{ID: "other", ExecutionMode: "primary", State: session.StatePrimary, AllowedMCPTools: map[string][]string{nativeCodeMCP: {"code_inspect"}}},
	}}
	v.Decisions = []session.Decision{{ID: v.PlanID, Kind: "plan", State: "approved"}}
	if allowed, _ := primaryMCPGrant(v, cfg, nativeCodeMCP, "code_inspect"); !allowed {
		t.Fatal("other ready primary grant lost")
	}
	for _, state := range []session.State{session.StateCompleted, session.StateFailed, session.StateBlocked} {
		v.Tasks[1].State = state
		if allowed, _ := primaryMCPGrant(v, cfg, nativeCodeMCP, "code_inspect"); allowed {
			t.Fatalf("inactive task grants inherited: %s", state)
		}
	}
	v.Tasks[1].State = session.StateRunning
	v.Tasks[1].ExecutionMode = "worker"
	if allowed, _ := primaryMCPGrant(v, cfg, nativeCodeMCP, "code_inspect"); allowed {
		t.Fatal("worker grant inherited")
	}
	v.Tasks[1].State = session.StatePrimary
	v.Tasks[1].ExecutionMode = "primary"
	v.Tasks[1].Dependencies = []string{"unfinished"}
	if allowed, _ := primaryMCPGrant(v, cfg, nativeCodeMCP, "code_inspect"); allowed {
		t.Fatal("dependent task granted before readiness")
	}
}
