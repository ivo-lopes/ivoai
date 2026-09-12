package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A fixture executor, not a replacement implementation of the gateway. The
// candidate/public IVOAI binary creates the plans, approvals and scoped grants.
func TestMCPArtifactWorkerHelper(t *testing.T) {
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 {
		t.Skip("subprocess helper")
	}
	args := os.Args[separator+1:]
	has := func(s string) bool {
		for _, a := range args {
			if a == s {
				return true
			}
		}
		return false
	}
	if has("--version") {
		fmt.Println("fixture-claude 1")
		os.Exit(0)
	}
	if has("auth") {
		fmt.Println(`{"loggedIn":true,"apiProvider":"firstParty","authMethod":"subscription"}`)
		os.Exit(0)
	}
	if has("--help") {
		fmt.Println("--print --output-format --model --restricted acceptEdits")
		os.Exit(0)
	}
	if has("--input-format") {
		bufio.NewReader(os.Stdin).ReadString('\n')
		fmt.Println(`{"type":"control_response","response":{"subtype":"success","request_id":"ivoai_catalog","response":{"models":[{"value":"fixture-strong","description":"most capable"}]}}}`)
		os.Exit(0)
	}
	var projection struct {
		Servers map[string]struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	allowed := ""
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--mcp-config" {
			if json.Unmarshal([]byte(args[i+1]), &projection) != nil {
				os.Exit(2)
			}
		}
		if args[i] == "--allowedTools" {
			allowed = args[i+1]
		}
	}
	if !has("--strict-mcp-config") || len(projection.Servers) != 1 {
		os.Exit(3)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for name, server := range projection.Servers {
		client := &http.Client{Transport: liveMCPTransport(func(r *http.Request) (*http.Response, error) {
			r = r.Clone(r.Context())
			for key, value := range server.Headers {
				r.Header.Set(key, os.ExpandEnv(value))
			}
			return http.DefaultTransport.RoundTrip(r)
		})}
		connection, err := mcp.NewClient(&mcp.Implementation{Name: "fixture-worker", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: client, DisableStandaloneSSE: true}, nil)
		if err != nil {
			os.Exit(4)
		}
		listed, err := connection.ListTools(ctx, nil)
		if err != nil || len(listed.Tools) != 1 {
			os.Exit(5)
		}
		for _, tool := range []string{"read_tool", "write_tool", "unknown_tool", "delete_tool"} {
			result, err := connection.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: map[string]any{}})
			permitted := allowed == "mcp__"+name+"__"+tool
			succeeded := err == nil && !result.IsError
			if succeeded != permitted {
				os.Exit(6)
			}
		}
		connection.Close()
	}
	fmt.Println(`{"type":"result","subtype":"success","is_error":false,"result":"fixture exact grant enforcement passed"}`)
	os.Exit(0)
}

func TestMCPArtifactTaskGrants(t *testing.T) {
	binary := os.Getenv("IVOAI_NATIVE_SMOKE_BINARY")
	if binary == "" {
		t.Skip("provide candidate/public artifact")
	}
	root := t.TempDir()
	for _, kind := range []string{"CONFIG", "DATA", "STATE", "CACHE"} {
		t.Setenv("XDG_"+kind+"_HOME", filepath.Join(root, kind))
	}
	a, err := New("fixture", strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Headroom.Enabled = false
	cfg.Compression.Provider = "direct"
	cfg.OpenCode.PermissionMode = "full"
	cfg.Orchestration.Auto.PlanExecution = "immediate"
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(root, "claude")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec '"+strings.ReplaceAll(self, "'", "'\\''")+"' -test.run=^TestMCPArtifactWorkerHelper$ -- \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.SaveState(config.State{Schema: config.StateSchemaVersion, Components: map[string]config.ComponentState{"claude-code": {Installed: true, Path: fake, Version: "fixture"}}}); err != nil {
		t.Fatal(err)
	}
	var reads, writes, forbidden atomic.Int32
	s := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	for _, name := range []string{"read_tool", "write_tool", "unknown_tool", "delete_tool"} {
		var annotations *mcp.ToolAnnotations
		if name == "read_tool" {
			annotations = &mcp.ToolAnnotations{ReadOnlyHint: true}
		}
		s.AddTool(&mcp.Tool{Name: name, Annotations: annotations, InputSchema: map[string]any{"type": "object"}}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			switch name {
			case "read_tool":
				reads.Add(1)
			case "write_tool":
				writes.Add(1)
			default:
				forbidden.Add(1)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "fixture"}}}, nil
		})
	}
	upstream := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, &mcp.StreamableHTTPOptions{JSONResponse: true}))
	defer upstream.Close()
	for _, alias := range []string{"fixture-a", "fixture-b"} {
		cmd := exec.Command(binary, "connect", "mcp", "add", alias, upstream.URL)
		if err := cmd.Run(); err != nil {
			t.Fatal("artifact registry add failed", err)
		}
	}
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	id, _ := session.NewID()
	now := time.Now().UTC()
	v := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Coordinator: "native", SwarmID: "native_" + id, PrimaryExecutor: "claude", PrimaryModel: session.UnknownModel(), WorkingDirectory: root, MaxWorkers: 2, State: session.StateRunning, MemoryStatus: "disabled", ContextStatus: "disabled", ServerStatus: "not-connected"}
	v.Auto, v.InitialPlanner, v.CurrentPrimary = true, "claude", "claude"
	if err := store.Create(v); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "_orchestrator-serve", "--session", id)
	command.Dir = root
	client, err := mcp.NewClient(&mcp.Implementation{Name: "fixture-primary", Version: "1"}, nil).Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatal("artifact orchestration connection failed", err)
	}
	defer client.Close()
	call := func(name string, args any) error {
		result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			return err
		}
		if result.IsError {
			return fmt.Errorf("%s rejected fixture", name)
		}
		return nil
	}
	if err := call("orchestration_bootstrap", map[string]any{"objective": "validate fixture exact tool grants", "memory_status": "disabled", "context_status": "disabled", "memory_lookup_performed": false, "context_lookup_performed": false}); err != nil {
		t.Fatal(err)
	}
	tasks := []map[string]any{}
	for i, tool := range []string{"read_tool", "write_tool"} {
		alias := []string{"fixture-a", "fixture-b"}[i]
		description := []string{"Read fixture A while rejecting any mutation", "Execute the approved fixture B write without expanding to read or delete"}[i]
		tasks = append(tasks, map[string]any{"id": fmt.Sprintf("task%d", i), "role": "research", "task": description, "acceptance": []string{"only the exact grant succeeds"}, "delegate": true, "preferred_executor": "claude", "allowed_mcp_tools": map[string][]string{alias: {tool}}, "scores": map[string]int{"complexity": 50, "risk": 30, "reasoning_depth": 30, "context_breadth": 30, "verification_need": 60, "parallel_value": 90, "latency_sensitivity": 30}})
	}
	planned := make(chan error, 1)
	go func() { planned <- call("orchestration_plan", map[string]any{"tasks": tasks}) }()
	approved := false
	for ctx.Err() == nil {
		v, err = store.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range v.Decisions {
			if d.Kind == "plan" && d.State == "pending" && !approved {
				if reads.Load() != 0 || writes.Load() != 0 || len(v.Workers) != 0 {
					t.Fatal("worker started before mutating approval")
				}
				if err := store.ResolveDecision(id, d.ID, true); err != nil {
					t.Fatal(err)
				}
				approved = true
			}
		}
		if len(v.Workers) == 2 {
			done := true
			for _, w := range v.Workers {
				if w.State == session.StateFailed {
					t.Fatal("fixture worker failed")
				}
				done = done && w.State == session.StateCompleted
			}
			if done {
				break
			}
		}
		select {
		case err := <-planned:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !approved || reads.Load() != 1 || writes.Load() != 1 || forbidden.Load() != 0 {
		t.Fatalf("artifact grant smoke failed: approved=%v reads=%d writes=%d forbidden=%d", approved, reads.Load(), writes.Load(), forbidden.Load())
	}
	t.Log("ARTIFACT_EXACT_GRANTS=PASS UNAPPROVED_WRITE=DENIED APPROVED_WRITE=PASS UNRELATED_TOOLS=DENIED FULL_MODE_BYPASS=false")
}
