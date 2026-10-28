//go:build linux && amd64

package workers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/codexresolver"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

// This optional protocol proof has no inference or credentials. The baseline
// must actually execute the advertised native worker before denial is counted.
func TestPinnedCodexManagedNativeWorkerBoundary(t *testing.T) {
	binary := os.Getenv("IVOAI_WORKER_BOUNDARY_CODEX_PATH")
	if binary == "" {
		t.Skip("NOT_RUN: explicit pinned official Codex 0.162.1 and companion required")
	}
	verify := func(path, expected string) {
		t.Helper()
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		h := sha256.New()
		if _, err = io.Copy(h, f); err != nil {
			t.Fatal(err)
		}
		if hex.EncodeToString(h.Sum(nil)) != expected {
			t.Fatal("official fixture executable digest mismatch")
		}
	}
	verify(binary, "628e35487d888a28f0427fc37501da529dd3d81fcbad7a2c3421c1e8670d92e1")
	verify(filepath.Join(filepath.Dir(binary), "codex-code-mode-host"), "ec71830690a7c0024916ee0c99fbdc635bc6e747ff665ea4f03a8389bbcb6b30")
	for _, tc := range []struct {
		name                                 string
		managed, profile, enable, v2, worker bool
	}{
		{name: "legacy_flags_positive", worker: true},
		{name: "v2_overrides_agents_false", v2: true, worker: true},
		{name: "cli_enable_overrides_config", enable: true, worker: true},
		{name: "hostile_profile_positive", profile: true, worker: true},
		{name: "managed_clean", managed: true},
		{name: "managed_hostile_profile_and_compact_config", managed: true, profile: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, dir := range []string{"home", "codehome", "repo"} {
				if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if tc.profile {
				body := "[agents]\nenabled = true\n[features]\nmulti_agent_v2 = true\n"
				if err := os.WriteFile(filepath.Join(root, "codehome", "config.toml"), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "codehome", "hostile.config.toml"), []byte("model = \"gpt-6-sol\"\n"+body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var mu sync.Mutex
			count, children := 0, 0
			advertised, identity, unsupported := false, false, false
			childStarted := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/v1/responses" {
					http.NotFound(w, r)
					return
				}
				raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
				r.Body.Close()
				if err != nil {
					w.WriteHeader(413)
					return
				}
				var request struct {
					Model string `json:"model"`
					Input []struct {
						Type   string          `json:"type"`
						Output json.RawMessage `json:"output"`
						Tools  []struct {
							Name  string `json:"name"`
							Tools []struct {
								Name string `json:"name"`
							} `json:"tools"`
						} `json:"tools"`
					} `json:"input"`
				}
				if json.Unmarshal(raw, &request) != nil {
					w.WriteHeader(400)
					return
				}
				if request.Model != "gpt-6-luna" && tc.profile && request.Model != "gpt-6-sol" {
					t.Error("hostile profile model sentinel did not reach the primary request")
				}
				mu.Lock()
				count++
				n := count
				if request.Model == "gpt-6-luna" {
					children++
					select {
					case childStarted <- struct{}{}:
					default:
					}
				}
				for _, item := range request.Input {
					for _, namespace := range item.Tools {
						if namespace.Name == "collaboration" {
							for _, tool := range namespace.Tools {
								if tool.Name == "spawn_agent" {
									advertised = true
								}
							}
						}
					}
					if item.Type == "function_call_output" {
						identity = identity || strings.Contains(string(item.Output), `task_name`)
						unsupported = unsupported || strings.Contains(strings.ToLower(string(item.Output)), "unsupported")
					}
				}
				mu.Unlock()
				// Keep the primary alive until the positive-control child reaches
				// the synthetic provider. No timing-based worker inference.
				if tc.worker && n > 1 && request.Model != "gpt-6-luna" {
					select {
					case <-childStarted:
					case <-r.Context().Done():
						return
					case <-time.After(5 * time.Second):
						t.Error("positive-control child did not reach local provider")
					}
				}
				w.Header().Set("Content-Type", "text/event-stream")
				emit := func(v any) { encoded, _ := json.Marshal(v); _, _ = fmt.Fprintf(w, "data: %s\n\n", encoded) }
				rid := fmt.Sprintf("response_%s_%d", tc.name, n)
				emit(map[string]any{"type": "response.created", "response": map[string]any{"id": rid}})
				var item any
				if n == 1 {
					args, _ := json.Marshal(map[string]string{"message": "Return SYNTHETIC_CHILD only. Do not use tools.", "task_name": "unique_" + tc.name, "model": "gpt-6-luna"})
					item = map[string]any{"type": "function_call", "namespace": "collaboration", "name": "spawn_agent", "arguments": string(args), "id": "fc_" + tc.name, "call_id": "call_" + tc.name}
				} else {
					item = map[string]any{"type": "message", "role": "assistant", "status": "completed", "id": fmt.Sprintf("answer_%s_%d", tc.name, n), "content": []any{map[string]any{"type": "output_text", "text": "SYNTHETIC_FINAL"}}}
				}
				emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
				emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": rid, "status": "completed"}})
			}))
			defer server.Close()
			environment := []string{"HOME=" + filepath.Join(root, "home"), "CODEX_HOME=" + filepath.Join(root, "codehome"), "PATH=" + filepath.Dir(binary) + ":/usr/bin:/bin", "LANG=C.UTF-8"}
			args := []string{"exec", "--json", "--skip-git-repo-check", "--sandbox", "read-only", "-c", `approval_policy="never"`, "-c", "features.multi_agent=false", "-c", "features.multi_agent_v2=false", "-c", "features.apps=false", "-c", "features.hooks=false", "-c", "features.plugins=false", "-c", "features.remote_plugin=false", "-c", `model_provider="boundary_fixture"`, "-c", `model_providers.boundary_fixture.name="Synthetic local fixture"`, "-c", "model_providers.boundary_fixture.base_url=" + strconv.Quote(server.URL+"/v1"), "-c", `model_providers.boundary_fixture.wire_api="responses"`, "-c", "model_providers.boundary_fixture.requires_openai_auth=false", "-c", "model_providers.boundary_fixture.supports_websockets=false"}
			if !tc.profile {
				args = append(args, "-m", "gpt-6.1-sol")
			}
			if tc.v2 || tc.enable {
				args = append(args, "-c", "agents.enabled=false", "-c", "features.multi_agent_v2="+strconv.FormatBool(tc.v2))
			}
			if tc.enable {
				args = append(args, "--enable=multi_agent_v2")
			}
			if tc.profile {
				args = append(args, "--profile", "hostile", "-cagents.enabled=true", "-c=features.multi_agent_v2=true")
			}
			if tc.managed {
				var err error
				args, err = (Adapter{Runner: sterilePolicyRunner{environment}}).IsolateCodexMCPs(context.Background(), binary, args)
				if err != nil {
					t.Fatal(err)
				}
			}
			args = append(args, "SYNTHETIC PRIMARY. Return final only.")
			args = codexresolver.ConfigurationArgs(args)
			result, err := (platform.ExecRunner{}).Run(context.Background(), binary, args, platform.RunOptions{Dir: filepath.Join(root, "repo"), Env: environment, CleanEnv: true, Timeout: 30 * time.Second})
			if err != nil {
				t.Fatalf("official protocol failed: %v stderr=%s", err, result.Stderr)
			}
			if !strings.Contains(result.Stdout, `"turn.completed"`) || !strings.Contains(result.Stdout, "SYNTHETIC_FINAL") {
				t.Fatal("official primary did not complete the synthetic task")
			}
			mu.Lock()
			defer mu.Unlock()
			if tc.worker {
				if !advertised || !identity || children != 1 || count != 3 {
					t.Fatalf("baseline did not prove execution: advertised=%v identity=%v children=%d requests=%d", advertised, identity, children, count)
				}
			} else if identity || children != 0 || count != 2 || !unsupported {
				t.Fatalf("native worker not refused: identity=%v children=%d requests=%d unsupported=%v", identity, children, count, unsupported)
			}
			t.Logf("official0.162.1: advertised=%v spawned_identity=%v child_requests=%d http_requests=%d unsupported=%v; synthetic local only", advertised, identity, children, count, unsupported)
		})
	}
}

type sterilePolicyRunner struct{ environment []string }

func (r sterilePolicyRunner) LookPath(name string) (string, error) {
	return (platform.ExecRunner{}).LookPath(name)
}
func (r sterilePolicyRunner) Run(ctx context.Context, command string, args []string, opts platform.RunOptions) (platform.Result, error) {
	opts.Env = append([]string(nil), r.environment...)
	opts.CleanEnv = true
	return (platform.ExecRunner{}).Run(ctx, command, args, opts)
}
