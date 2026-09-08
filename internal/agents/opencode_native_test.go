package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestLiveNativeOpenCodeAUTOExecutor(t *testing.T) {
	path, version := os.Getenv("IVOAI_LIVE_OPENCODE_PATH"), os.Getenv("IVOAI_LIVE_OPENCODE_VERSION")
	if path == "" || version == "" {
		t.Skip("requires pinned OpenCode")
	}
	var memoryCalls, contextCalls atomic.Int32
	var approvalMode atomic.Bool
	knowledge := func(name, text string, count *atomic.Int32) *httptest.Server {
		server := mcp.NewServer(&mcp.Implementation{Name: name, Version: "fixture"}, nil)
		server.AddTool(&mcp.Tool{Name: name, InputSchema: map[string]any{"type": "object", "properties": map[string]any{}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			count.Add(1)
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil
		})
		return httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	}
	memory := knowledge("memory_status", "fixture-memory-ok", &memoryCalls)
	defer memory.Close()
	knowledgeContext := knowledge("context_health", "fixture-context-ok", &contextCalls)
	defer knowledgeContext.Close()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected native provider path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var request struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if json.Unmarshal(body, &request) != nil {
			t.Error("invalid provider fixture request")
			return
		}
		target := ""
		arguments := "{}"
		if approvalMode.Load() {
			if !strings.Contains(string(body), "fixture-approval-ok") {
				target = "bash"
				arguments = `{"command":"printf fixture-approval-ok","description":"Read-only approval fixture"}`
			}
		} else if !strings.Contains(string(body), "fixture-memory-ok") {
			target = "memory_status"
		} else if !strings.Contains(string(body), "fixture-context-ok") {
			target = "context_health"
		}
		if target != "" {
			name := ""
			for _, tool := range request.Tools {
				if strings.HasSuffix(tool.Function.Name, target) {
					name = tool.Function.Name
				}
			}
			if name == "" {
				t.Errorf("native knowledge tool unavailable: %s", target)
				return
			}
			chunk, _ := json.Marshal(map[string]any{"id": "fixture", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call_" + target, "type": "function", "function": map[string]string{"name": name, "arguments": arguments}}}}, "finish_reason": nil}}})
			fmt.Fprintf(w, "data: %s\n\ndata: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", chunk)
			return
		}
		fmt.Fprint(w, "data: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Native controlled executor final response\"},\"finish_reason\":null}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer provider.Close()
	root := t.TempDir()
	config, _ := json.Marshal(map[string]any{"provider": map[string]any{"native-fixture": map[string]any{"npm": "@ai-sdk/openai-compatible", "name": "Native Fixture", "options": map[string]string{"baseURL": provider.URL + "/v1"}, "models": map[string]any{"model": map[string]any{"name": "Native fixture model", "tool_call": true, "variants": map[string]any{"high": map[string]string{"reasoningEffort": "high"}}}}}}})
	wrapper := filepath.Join(root, "opencode-fixture")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nexport OPENCODE_CONFIG_CONTENT='"+string(config)+"'\nexec '"+path+"' \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	native := &NativeOpenCode{Options: opencodebridge.ManagedOptions{NativeExecutor: true, PermissionMode: "full", OpenCodePath: wrapper, Version: version, Directory: root, RuntimeDir: filepath.Join(root, "runtime"), StateDir: filepath.Join(root, "state"), Environment: []string{"HOME=" + root, "PATH=" + os.Getenv("PATH")}}, AuthMetadata: func(context.Context, string, []string) (map[string]bool, error) {
		return map[string]bool{"native-fixture": true}, nil
	}}
	native.Options.NativeMCP = map[string]any{"ivoai-memory": map[string]any{"type": "remote", "url": memory.URL, "oauth": false}, "ivoai-context": map[string]any{"type": "remote", "url": knowledgeContext.URL, "oauth": false}}
	native.Options.NativePermissions = opencodebridge.NativePermissionPolicy("full", true)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	value, err := native.Probe(ctx)
	if err != nil || !value.Eligible || !value.TelemetryUnknown || !native.CanModel("native-fixture/model") {
		t.Fatalf("native discovery failed: eligible=%t unknown=%t models=%+v err=%v", value.Eligible, value.TelemetryUnknown, native.Capability().Models, err)
	}
	var output strings.Builder
	result, err := native.Run(ctx, opencodebridge.ExecutorRequest{Executor: "opencode", SelectionMode: "explicit", Model: "native-fixture/model", Effort: "high", Prompt: "Read-only native provider fixture."}, func(text string) error { output.WriteString(text); return nil })
	if err != nil || result.Model != "native-fixture/model" || result.Effort != "high" || output.String() != "Native controlled executor final response" {
		t.Fatalf("native run: model=%q effort=%q output=%q err=%v", result.Model, result.Effort, output.String(), err)
	}
	t.Log("OPENCODE_NATIVE_CONTROLLED_HTTP=PASS; requested/effective=PASS; recursion=0; quota=UNKNOWN")
	if memoryCalls.Load() == 0 || contextCalls.Load() == 0 {
		t.Fatal("native Memory/Context tools not called")
	}
	native.Options.NativePermissions = opencodebridge.NativePermissionPolicy("full", false)
	bridge, err := opencodebridge.Start(opencodebridge.Options{Runner: native, Select: func(context.Context, string) (string, error) { return "opencode", nil }, Catalog: opencodebridge.CatalogFromRegistry(routing.Registry{Providers: map[string]routing.ProviderCapability{"opencode": native.Capability()}}), Status: func() opencodebridge.Status { return opencodebridge.Status{Primary: "opencode"} }})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	frontend, err := (OpenCodeExecutor{}).OpenSession(ctx, opencodebridge.ManagedOptions{OpenCodePath: path, Version: version, Directory: root, RuntimeDir: filepath.Join(root, "frontend-runtime"), StateDir: filepath.Join(root, "frontend-state"), Bridge: bridge, PermissionMode: "full"})
	if err != nil {
		t.Fatal(err)
	}
	defer frontend.Close(context.Background())
	response, err := frontend.Prompt(ctx, map[string]any{"model": map[string]string{"providerID": "ivoai", "modelID": "auto"}, "parts": []map[string]string{{"type": "text", "text": "Consulte o contexto e memória e comente um pouco sobre o projeto Voicehub."}}}, false)
	if err != nil || !strings.Contains(string(response), "Native controlled executor final response") || memoryCalls.Load() < 2 || contextCalls.Load() < 2 {
		t.Fatalf("frontend→IVOAI→native→knowledge failed: memory=%d context=%d err=%v", memoryCalls.Load(), contextCalls.Load(), err)
	}
	t.Log("AUTO_FRONTEND_NATIVE_E2E=PASS; MCP_MEMORY=PASS; MCP_CONTEXT=PASS")
	approvalMode.Store(true)
	native.Options.NativePermissions = opencodebridge.NativePermissionPolicy("interactive", false)
	for _, cancelTurn := range []bool{false, true} {
		turnCtx, stop := context.WithTimeout(ctx, 40*time.Second)
		done := make(chan error, 1)
		go func() {
			_, runErr := native.Run(turnCtx, opencodebridge.ExecutorRequest{Executor: "opencode", Model: "native-fixture/model", Prompt: "Run the safe approval fixture."}, func(string) error { return nil })
			done <- runErr
		}()
		approved := false
		for !approved {
			select {
			case runErr := <-done:
				stop()
				t.Fatalf("native turn ended before approval: %v", runErr)
			case <-turnCtx.Done():
				stop()
				t.Fatal("native permission was not observable")
			case <-time.After(100 * time.Millisecond):
				pending := native.PendingPermissions()
				if len(pending) == 0 {
					continue
				}
				if len(pending) != 1 || !strings.Contains(pending[0].Description, "fixture-approval-ok") {
					t.Fatal("unexpected native approval metadata", pending)
				}
				if cancelTurn {
					stop()
				} else if err := native.ReplyPermission(turnCtx, pending[0].ID, true); err != nil {
					t.Fatal(err)
				}
				approved = true
			}
		}
		select {
		case runErr := <-done:
			if (runErr != nil) != cancelTurn {
				t.Fatalf("native permission/cancel result: cancelled=%t err=%v", cancelTurn, runErr)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("native permission/cancel did not close")
		}
		stop()
	}
	t.Log("NATIVE_INTERACTIVE_APPROVAL=PASS; NATIVE_CANCELLATION=PASS")
}

func TestOfficialNativeAuthMetadataDoesNotReturnCredentialPaths(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "official-fixture")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'Credentials /private/auth.json\\n│ Native Fixture oauth\\n│ Other api\\n2 credentials\\nEnvironment FAKE_KEY\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	value, err := officialOpenCodeAuthMetadata(context.Background(), path, []string{"PATH=" + os.Getenv("PATH")})
	if err != nil || len(value) != 2 || !value["Native Fixture"] || !value["Other"] {
		t.Fatalf("metadata projection=%v err=%v", value, err)
	}
}

func TestNativeFailoverNeverReplaysToolEffects(t *testing.T) {
	for _, tc := range []struct {
		body string
		safe bool
	}{
		{`[{"info":{"role":"user"},"parts":[{"type":"text"}]},{"info":{"role":"assistant"},"parts":[]}]`, true},
		{`[{"info":{"role":"assistant"},"parts":[{"type":"tool"}]}]`, false},
		{`[{"info":{"role":"assistant"},"parts":[{"type":"unknown-future-effect"}]}]`, false},
		{`[{"info":{"role":"user"},"parts":[]}]`, false},
		{`[]`, false}, {`null`, false}, {`invalid`, false},
	} {
		if nativeHistoryAllowsFailover(json.RawMessage(tc.body)) != tc.safe {
			t.Fatal("unsafe native replay classification")
		}
	}
}
