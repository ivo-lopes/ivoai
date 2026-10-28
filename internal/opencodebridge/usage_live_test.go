package opencodebridge

import (
	"context"
	"crypto/sha256"
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
)

// Uses the real official executable with an isolated synthetic Responses
// server. It verifies the client's report mapping, never subscription inference,
// provider accounting accuracy, price, quota consumption or model identity.
func TestLiveCodexReportedUsageLocalProvider(t *testing.T) {
	binary := os.Getenv("IVOAI_LIVE_CODEX_PATH")
	if binary == "" {
		t.Skip("NOT_RUN: explicit official Codex path required; synthetic local provider")
	}
	binary, err := filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	file.Close()
	if err != nil {
		t.Fatal("binary identity unavailable")
	}
	digest := fmt.Sprintf("%x", hash.Sum(nil))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := t.TempDir()
	home, codexHome, project := filepath.Join(root, "home"), filepath.Join(root, "codex"), filepath.Join(root, "project")
	for _, dir := range []string{home, codexHome, project} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{"HOME=" + home, "CODEX_HOME=" + codexHome, "XDG_CONFIG_HOME=" + home, "XDG_CACHE_HOME=" + home, "PATH=/usr/bin:/bin", "LANG=C.UTF-8", "IVOAI_USAGE_FIXTURE_KEY=local-synthetic-not-a-provider-credential"}
	version := exec.CommandContext(ctx, binary, "--version")
	version.Env, version.Dir = env, project
	rawVersion, err := version.Output()
	if err != nil || !strings.HasPrefix(string(rawVersion), "codex-cli ") {
		t.Fatal("official version probe failed")
	}
	t.Logf("client=%s sha256=%s; synthetic local provider, no subscription inference", strings.TrimSpace(string(rawVersion)), digest)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}
		defer r.Body.Close()
		if _, err := io.Copy(io.Discard, http.MaxBytesReader(w, r.Body, 2<<20)); err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(event any) {
			body, _ := json.Marshal(event)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", body)
		}
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_usage_fixture"}})
		emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": map[string]any{"id": "msg_usage_fixture", "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "SYNTHETIC_USAGE_OK"}}}})
		emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_usage_fixture", "status": "completed", "usage": map[string]any{"input_tokens": 100, "input_tokens_details": map[string]any{"cached_tokens": 60}, "output_tokens": 2, "total_tokens": 102}}})
	}))
	defer server.Close()
	settings := []string{`model_provider="ivoai_usage_fixture"`, `model="gpt-6.1-sol"`, `model_reasoning_effort="low"`, `model_providers.ivoai_usage_fixture.name="Synthetic local usage fixture"`, "model_providers.ivoai_usage_fixture.base_url=" + fmt.Sprintf("%q", server.URL+"/v1"), `model_providers.ivoai_usage_fixture.env_key="IVOAI_USAGE_FIXTURE_KEY"`, `model_providers.ivoai_usage_fixture.wire_api="responses"`, `model_providers.ivoai_usage_fixture.requires_openai_auth=false`, `model_providers.ivoai_usage_fixture.supports_websockets=false`, `model_providers.ivoai_usage_fixture.request_max_retries=0`, `model_providers.ivoai_usage_fixture.stream_max_retries=0`, `features.hooks=false`, `features.apps=false`, `features.plugins=false`, `features.remote_plugin=false`, `features.multi_agent=false`, `features.shell_tool=false`, `web_search="disabled"`, `history.persistence="none"`}
	args := []string{"--sandbox", "read-only", "-a", "never"}
	for _, setting := range settings {
		args = append(args, "-c", setting)
	}
	result, err := (CLIRunner{Codex: ExecutorSpec{Path: binary, SHA256: digest, Dir: project, Env: env, Args: args}}).Run(ctx, ExecutorRequest{Executor: "codex", Prompt: "Synthetic local protocol test. Return the fixture answer."}, func(string) error { return nil })
	if err != nil {
		t.Fatalf("official CLI local usage contract failed: %v", err)
	}
	if requests.Load() != 1 || result.Trace == nil || result.Trace.UsageStatus != "reported" || result.Trace.Usage == nil {
		t.Fatal("official CLI usage report missing or unexpected request count")
	}
	usage := result.Trace.Usage
	if usage.InputTokens != 100 || usage.OutputTokens != 2 || usage.CachedInputTokens == nil || *usage.CachedInputTokens != 60 || usage.Basis != "client_reported" {
		t.Fatalf("official CLI usage mapping differs: %+v", usage)
	}
}
