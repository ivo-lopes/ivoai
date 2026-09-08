package opencodebridge

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
)

// The actual pinned OpenCode tool dispatcher executes a safe filesystem fixture.
// A local provider fixture emits the tool call; no executor credentials or
// operator project/config are used. IVOAI bridge dispatch is covered separately.
func TestLiveManagedOpenCodePermissions(t *testing.T) {
	path, version := os.Getenv("IVOAI_LIVE_OPENCODE_PATH"), os.Getenv("IVOAI_LIVE_OPENCODE_VERSION")
	if path == "" || version == "" {
		t.Skip("set the pinned OpenCode path and version")
	}
	for _, mode := range []string{"full", "interactive"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			external := filepath.Join(t.TempDir(), "external.txt")
			if err := os.WriteFile(external, []byte("external safe fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			var externalRead atomic.Bool
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				step := calls.Add(1)
				body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
				if step > 2 && strings.Contains(string(body), "external safe fixture") {
					externalRead.Store(true)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				delta := map[string]any{"content": "fixture completed"}
				finish := "stop"
				if step == 1 {
					args, _ := json.Marshal(map[string]string{"command": "mkdir fixture-dir && printf original > fixture-dir/file && cat fixture-dir/file && printf edited > fixture-dir/file && grep edited fixture-dir/file && git init -q fixture-dir && git -C fixture-dir status --porcelain && git -C fixture-dir diff && rm fixture-dir/file", "description": "safe temporary filesystem fixture"})
					delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call_fixture", "type": "function", "function": map[string]string{"name": "bash", "arguments": string(args)}}}}
					finish = "tool_calls"
				}
				if step == 2 {
					args, _ := json.Marshal(map[string]string{"filePath": external})
					delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call_read", "type": "function", "function": map[string]string{"name": "read", "arguments": string(args)}}}}
					finish = "tool_calls"
				}
				if step == 3 || step == 4 {
					name := "write"
					arguments := map[string]string{"filePath": filepath.Join(root, "native-edit.txt"), "content": "original"}
					if step == 4 {
						name = "edit"
						arguments = map[string]string{"filePath": filepath.Join(root, "native-edit.txt"), "oldString": "original", "newString": "edited"}
					}
					args, _ := json.Marshal(arguments)
					delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("call_%d", step), "type": "function", "function": map[string]string{"name": name, "arguments": string(args)}}}}
					finish = "tool_calls"
				}
				for _, choice := range []map[string]any{{"index": 0, "delta": delta, "finish_reason": nil}, {"index": 0, "delta": map[string]any{}, "finish_reason": finish}} {
					body, _ := json.Marshal(map[string]any{"id": "chatcmpl-fixture", "object": "chat.completion.chunk", "created": 1, "model": "fixture", "choices": []any{choice}})
					fmt.Fprintf(w, "data: %s\n\n", body)
				}
				fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer provider.Close()
			bridge, err := Start(Options{Runner: &fakeRunner{}, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{PermissionMode: mode} }})
			if err != nil {
				t.Fatal(err)
			}
			defer bridge.Close(context.Background())
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			managed, err := StartManaged(ctx, ManagedOptions{OpenCodePath: path, Version: version, RuntimeDir: filepath.Join(root, "runtime"), StateDir: filepath.Join(root, "state"), Directory: root, Bridge: bridge, PermissionMode: mode})
			if err != nil {
				t.Fatal(err)
			}
			defer managed.Close(context.Background())
			call := func(method, target string, value any) (int, []byte, error) {
				body, _ := json.Marshal(value)
				req, err := http.NewRequestWithContext(ctx, method, managed.URL+target, strings.NewReader(string(body)))
				if err != nil {
					return 0, nil, err
				}
				req.SetBasicAuth("ivoai", managed.password)
				req.Header.Set("Content-Type", "application/json")
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					return 0, nil, err
				}
				defer resp.Body.Close()
				data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
				return resp.StatusCode, data, err
			}
			// Override only this private fixture backend's provider transport.
			configPath := envValue(managed.Env(), "OPENCODE_CONFIG")
			configBody, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			var fixtureConfig map[string]any
			if err := json.Unmarshal(configBody, &fixtureConfig); err != nil {
				t.Fatal(err)
			}
			fixtureConfig["provider"].(map[string]any)["ivoai"].(map[string]any)["options"].(map[string]any)["baseURL"] = provider.URL + "/v1"
			configBody, _ = json.Marshal(fixtureConfig)
			if err := os.WriteFile(configPath, configBody, 0600); err != nil {
				t.Fatal(err)
			}
			code, _, err := call("POST", "/instance/dispose", nil)
			if err != nil || code != 200 {
				t.Fatalf("fixture provider configuration: %d %v", code, err)
			}
			code, data, err := call("POST", "/session", map[string]any{})
			var created struct {
				ID string `json:"id"`
			}
			if err != nil || code != 200 || json.Unmarshal(data, &created) != nil || created.ID == "" {
				t.Fatal("session create", code, err)
			}
			done := make(chan error, 1)
			go func() {
				code, result, err := call("POST", "/session/"+created.ID+"/message", map[string]any{"model": map[string]string{"providerID": "ivoai", "modelID": "auto"}, "parts": []any{map[string]string{"type": "text", "text": "run the safe fixture"}}})
				_ = result
				if err == nil && code != 200 {
					err = fmt.Errorf("prompt status %d", code)
				}
				done <- err
			}()
			prompts := 0
			finished := false
			for !finished {
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
					finished = true
				case <-ctx.Done():
					t.Fatal("permission fixture timed out")
				case <-time.After(100 * time.Millisecond):
					code, data, err := call("GET", "/permission", nil)
					if err != nil || code != 200 {
						t.Fatal("permission list", code, err)
					}
					var pending []struct {
						ID string `json:"id"`
					}
					if err := json.Unmarshal(data, &pending); err != nil {
						t.Fatal(err)
					}
					for _, p := range pending {
						prompts++
						if mode == "full" {
							t.Fatal("full mode unexpectedly asked for permission")
						}
						_, err = managed.APIRequest(ctx, "permission-reply", p.ID, nil, map[string]string{"reply": "once"})
						if err != nil {
							t.Fatal("interactive reply", err)
						}
					}
				}
			}
			if calls.Load() < 5 || !externalRead.Load() {
				t.Fatalf("provider did not complete tool lifecycle calls=%d", calls.Load())
			}
			if data, err := os.ReadFile(filepath.Join(root, "native-edit.txt")); err != nil || string(data) != "edited" {
				t.Fatal("native write/edit tools did not complete", err)
			}
			if _, err := os.Stat(filepath.Join(root, "fixture-dir", ".git")); err != nil {
				t.Fatal("fixture command did not execute", err)
			}
			if _, err := os.Stat(filepath.Join(root, "fixture-dir", "file")); !os.IsNotExist(err) {
				t.Fatal("fixture cleanup failed", err)
			}
			if mode == "interactive" && prompts == 0 {
				t.Fatal("interactive mode never asked")
			}
			t.Logf("mode=%s permission_prompt_count=%d filesystem_fixture=PASS", mode, prompts)
		})
	}
}
