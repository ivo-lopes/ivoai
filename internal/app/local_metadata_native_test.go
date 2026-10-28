package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/ivo-lopes/ivoai/internal/codexfrontend"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/session"
)

// This opt-in uses the official App Server process and its local Responses
// facade, not subscription inference. The model catalog and App session are
// synthetic; the turn uses the production scoped runner/Bridge admission.
func TestLiveLocalMetadataOfficialAppServer(t *testing.T) {
	binary := os.Getenv("IVOAI_LIVE_CODEX_PATH")
	if binary == "" {
		t.Skip("requires explicit official Codex binary; no provider inference")
	}
	binary, err := filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.New()
	_, hashErr := io.Copy(digest, file)
	file.Close()
	if hashErr != nil {
		t.Fatal(hashErr)
	}
	t.Logf("official binary SHA-256: %x", digest.Sum(nil))
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	f := newLocalMetadataFixture(t)
	// The real thread/start callback must establish the presentation mapping;
	// no fixture thread or backend resume identity can make admission pass.
	f.update(t, func(v *session.Session) { v.FrontendSessionID = ""; v.FrontendSessions = nil })
	b, results := f.bridge(t, true, nil)
	isolated := t.TempDir()
	home, nativeHome := filepath.Join(isolated, "home"), filepath.Join(isolated, "native-home")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	environment := []string{"HOME=" + home, "PATH=/usr/bin:/bin", "LANG=C.UTF-8", "TERM=xterm-256color"}
	version := exec.CommandContext(ctx, binary, "--version")
	version.Dir, version.Env = f.root, append(append([]string{}, environment...), "CODEX_HOME="+nativeHome)
	if raw, err := version.Output(); err != nil {
		t.Fatal("official binary version probe failed")
	} else {
		t.Logf("official binary version: %s", strings.TrimSpace(string(raw)))
	}
	// An untrusted project's MCP configuration must not gain a process before
	// or during a deterministic Core metadata turn.
	marker := filepath.Join(f.root, "unexpected-project-mcp")
	if err := os.Mkdir(filepath.Join(f.root, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}
	projectConfig := "[mcp_servers.untrusted_fixture]\ncommand = \"/bin/sh\"\nargs = [\"-c\", \"touch " + marker + "\"]\n"
	if err := os.WriteFile(filepath.Join(f.root, ".codex", "config.toml"), []byte(projectConfig), 0600); err != nil {
		t.Fatal(err)
	}
	native, err := codexfrontend.Start(ctx, codexfrontend.Options{Binary: binary, Directory: f.root, RuntimeDir: filepath.Join(isolated, "runtime"), NativeHome: nativeHome, SessionID: f.id, Bridge: b, Environment: environment, ThreadAvailable: func(thread string) bool {
		v, err := f.store.Get(f.id)
		return err == nil && v.FrontendSessionID == thread
	}, ThreadSelected: func(thread string) error {
		_, err := f.store.Update(f.id, func(v *session.Session) error { v.SetFrontend("codex", thread); return nil })
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	endpoint, token := "", ""
	args := native.Args()
	for index, arg := range args {
		if arg == "--remote" && index+1 < len(args) {
			endpoint = args[index+1]
		}
	}
	for _, entry := range native.Environment() {
		name, value, _ := strings.Cut(entry, "=")
		switch name {
		case "IVOAI_NATIVE_REMOTE_TOKEN":
			token = value
		case "HOME":
			if value != home {
				t.Fatal("personal HOME inherited")
			}
		case "CODEX_HOME":
			if value != nativeHome {
				t.Fatal("personal CODEX_HOME inherited")
			}
		default:
			if strings.Contains(name, "API_KEY") || strings.Contains(name, "ACCESS_TOKEN") {
				t.Fatal("personal provider credential inherited")
			}
		}
	}
	if endpoint == "" || token == "" {
		t.Fatal("native facade remote capability unavailable")
	}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)
	client, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatal("official native facade connection failed:", err)
	}
	defer client.CloseNow()
	client.SetReadLimit(2 << 20)
	type rpc struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	send := func(id int, method string, params any) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
		if err := client.Write(ctx, websocket.MessageText, body); err != nil {
			t.Fatal("native request failed:", err)
		}
	}
	read := func() rpc {
		t.Helper()
		_, body, err := client.Read(ctx)
		if err != nil {
			t.Fatal("native event unavailable:", err)
		}
		var event rpc
		if json.Unmarshal(body, &event) != nil {
			t.Fatal("invalid native event")
		}
		return event
	}
	wait := func(id string) rpc {
		t.Helper()
		for {
			event := read()
			if string(event.ID) == id {
				return event
			}
		}
	}
	send(1, "initialize", map[string]any{"clientInfo": map[string]any{"name": "ivoai_local_metadata_fixture", "version": "1"}, "capabilities": map[string]any{"experimentalApi": true}})
	if event := wait("1"); len(event.Error) > 0 {
		t.Fatal("official initialization rejected")
	}
	if err := client.Write(ctx, websocket.MessageText, []byte(`{"method":"initialized"}`)); err != nil {
		t.Fatal(err)
	}
	send(2, "thread/start", map[string]any{})
	started := wait("2")
	if len(started.Error) > 0 {
		t.Fatalf("official thread start failed: %.600s", started.Error)
	}
	var thread struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if json.Unmarshal(started.Result, &thread) != nil || thread.Thread.ID == "" {
		t.Fatal("official presentation thread missing")
	}
	send(3, "turn/start", map[string]any{"threadId": thread.Thread.ID, "input": []map[string]any{{"type": "text", "text": "Qual versão deste projeto?"}}})
	if event := wait("3"); len(event.Error) > 0 {
		t.Fatalf("official turn rejected: %.600s", event.Error)
	}
	answer := ""
	for {
		event := read()
		if event.Method == "error" {
			t.Fatalf("official local turn failed: %.600s", event.Params)
		}
		if event.Method == "item/tool/requestUserInput" {
			t.Fatal("metadata turn unexpectedly requested tool/plan approval")
		}
		if event.Method == "item/completed" {
			var p struct {
				Item struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"item"`
			}
			_ = json.Unmarshal(event.Params, &p)
			if p.Item.Type == "agentMessage" {
				answer = p.Item.Text
			}
		}
		if event.Method == "turn/completed" {
			break
		}
	}
	select {
	case result := <-results:
		if result.ConfigurationSource != core.LocalMetadataSource || result.Trace != nil || result.ExecutorSessionID != "" || result.Model != "" {
			t.Fatalf("official facade reached another route: %+v", result)
		}
	case <-ctx.Done():
		t.Fatal("Core result missing")
	}
	if !strings.Contains(answer, "IVOAI Core local metadata (no model execution)") || !strings.Contains(answer, "Source: VERSION\nVersion: 0.12.1\nEvidence reference: artifact_") {
		t.Fatalf("official output did not contain Core metadata: %q", answer)
	}
	if f.legacy.Load() != 0 || f.exec.calls.Load() != 0 {
		t.Fatal("official presentation invoked hidden executor/discovery")
	}
	if _, err := os.Lstat(marker); !os.IsNotExist(err) {
		t.Fatal("untrusted project MCP process executed")
	}
	saved, err := f.store.Get(f.id)
	if err != nil || saved.PlanID != "" || len(saved.Tasks) != 0 || len(saved.Workers) != 0 || len(saved.Decisions) != 0 || len(saved.ExecutorSessions) != 0 || saved.ExecutorSessionID != "" || saved.FrontendSessionID != thread.Thread.ID {
		t.Fatalf("metadata route lost real frontend identity or created DAG/backend: %v", err)
	}
	var attempt opencodebridge.TurnAttempt
	_ = json.Unmarshal(saved.TurnAttempts[0], &attempt)
	if attempt.State != "completed" || !attempt.FinalResponse || attempt.EffectiveExecutor != "" || attempt.EffectiveModel != "" || attempt.ExitCode != nil || attempt.Trace != nil {
		t.Fatalf("local turn fabricated executor execution: %+v", attempt)
	}
	if err := native.TurnError(); err != nil {
		t.Fatal(err)
	}
	t.Log("official App Server delivered Core metadata via local facade; no subscription inference, hidden primary, MCP, workers or DAG")
}
