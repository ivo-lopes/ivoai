package codexfrontend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/session"
)

// This starts the official native CLI outside the IVOAI facade. Its model
// endpoint is an isolated fixture, never a provider account or paid request.
func TestLiveNativeCodexStatePortability(t *testing.T) {
	binary := os.Getenv("IVOAI_LIVE_CODEX_PATH")
	if binary == "" {
		t.Skip("requires explicit official Codex binary")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.WriteHeader(http.StatusUpgradeRequired)
			return
		}
		if r.Method != "POST" || r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []any{
			map[string]any{"type": "response.created", "response": map[string]any{"id": "fixture_response"}},
			map[string]any{"type": "response.output_item.done", "item": map[string]any{"id": "fixture_answer", "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "native portability fixture answer"}}}},
			map[string]any{"type": "response.completed", "response": map[string]any{"id": "fixture_response", "status": "completed"}},
		} {
			body, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", body)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	home := filepath.Join(root, "provider-home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	state := NativeState{Home: home, SQLiteHome: home}
	var thread string
	run := func(home, id string) string {
		t.Helper()
		args := []string{"-c", "openai_base_url=" + quote(server.URL), "-c", `cli_auth_credentials_store="ephemeral"`, "-c", "sqlite_home=" + quote(state.SQLiteHome), "-c", `features.hooks=false`, "-c", `features.plugins=false`, "-c", `features.remote_plugin=false`, "-c", `features.apps=false`, "-c", `sandbox_mode="read-only"`, "-c", `approval_policy="never"`, "exec"}
		if id != "" {
			args = append(args, "resume", id)
		}
		args = append(args, "--json", "--skip-git-repo-check", "--model", "fixture-strong", "Return the fixture answer without tools.")
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = root
		cmd.Env = []string{"HOME=" + root, "CODEX_HOME=" + home, "CODEX_API_KEY=synthetic-local-fixture-key", "PATH=/usr/bin:/bin"}
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		// Never echo arbitrary provider stderr or the serialized conversation.
		if err := cmd.Run(); err != nil {
			t.Fatalf("official fixture CLI failed: %v", err)
		}
		found := ""
		for _, line := range strings.Split(stdout.String(), "\n") {
			var event struct {
				Type     string `json:"type"`
				ThreadID string `json:"thread_id"`
			}
			if json.Unmarshal([]byte(line), &event) == nil && event.Type == "thread.started" {
				found = event.ThreadID
			}
		}
		if !session.ValidNativeUUID(found) {
			t.Fatal("official native thread ID unavailable")
		}
		return found
	}
	thread = run(home, "")
	threads, err := DiscoverNative(ctx, binary, root, filepath.Join(root, "discovery"), state, thread)
	if err != nil || len(threads) != 1 || threads[0].ID != thread || threads[0].Provider != "openai" {
		t.Fatal("external canonical native thread not discoverable", err)
	}
	store := session.Store{Root: filepath.Join(root, "ivoai-sessions")}
	adopted, err := store.AdoptCodex(thread, root, state.ScopeID(), true)
	if err != nil {
		t.Fatal(err)
	}
	view, err := os.MkdirTemp(root, "managed-view-")
	if err != nil {
		t.Fatal(err)
	}
	if err := state.PrepareView(view); err != nil {
		t.Fatal(err)
	}
	if run(view, thread) != thread || run(home, thread) != thread {
		t.Fatal("same-provider resume created a different native thread")
	}
	again, err := store.AdoptCodex(thread, root, state.ScopeID(), true)
	if err != nil || again.SessionID != adopted.SessionID {
		t.Fatal("adoption duplicated logical identity", err)
	}
	for _, providerHome := range []string{home, view} {
		for _, name := range []string{"auth.json", "config.toml", "hooks.json"} {
			if _, err := os.Lstat(filepath.Join(providerHome, name)); !os.IsNotExist(err) {
				t.Fatal("fixture created persistent auth/config")
			}
		}
	}
	if err := filepath.WalkDir(store.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, forbidden := range []string{"native portability fixture answer", "Return the fixture answer without tools.", "synthetic-local-fixture-key"} {
			if bytes.Contains(body, []byte(forbidden)) {
				return fmt.Errorf("native transcript or fixture credential entered IVOAI metadata")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
