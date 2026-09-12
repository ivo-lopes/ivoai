package codexfrontend

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/routing"
)

type writeBuffer struct{ bytes.Buffer }

func (*writeBuffer) Close() error { return nil }

func TestNativeCloseStopsBackgroundDirectoryWriters(t *testing.T) {
	root := t.TempDir()
	ready, leaked := filepath.Join(root, "ready"), filepath.Join(root, "leaked")
	binary := filepath.Join(root, "fake-codex")
	body := "#!/bin/sh\n(sleep 0.5; touch '" + leaked + "') &\ntouch '" + ready + "'\ncat >/dev/null\n"
	if err := os.WriteFile(binary, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	catalog := opencodebridge.CatalogFromRegistry(routing.Registry{Providers: map[string]routing.ProviderCapability{"codex": {Provider: "codex", Authenticated: true, Models: []routing.ModelCapability{{Name: "fixture-strong", CapabilityTier: routing.TierStrong, SupportedEfforts: []string{"high"}, DefaultEffort: "high", Source: routing.SourceRuntimeVerified}}}}})
	bridge, err := opencodebridge.Start(opencodebridge.Options{Frontend: "codex", Catalog: catalog, Runner: &fixtureRunner{}, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() opencodebridge.Status { return opencodebridge.Status{Frontend: "codex"} }})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	f, err := Start(context.Background(), Options{Binary: binary, Directory: root, RuntimeDir: root, SessionID: "cleanup", Bridge: bridge, Environment: []string{"PATH=/usr/bin:/bin", "HOME=" + root}})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := os.Stat(ready); err != nil {
		t.Fatal("fixture did not start")
	}
	args := strings.Join(f.Args(), " ")
	if !strings.Contains(args, "features.plugins=false") || !strings.Contains(args, "features.remote_plugin=false") {
		t.Fatal("native plugin plane must remain disabled")
	}
	f.Close()
	time.Sleep(600 * time.Millisecond)
	if _, err := os.Stat(leaked); !os.IsNotExist(err) {
		t.Fatal("native descendant survived frontend cleanup")
	}
}

func TestNativeAdmissionFailsClosed(t *testing.T) {
	for _, method := range []string{"turn/start", "turn/steer", "command/exec", "thread/shellCommand", "process/spawn", "review/start", "thread/inject_items", "config/value/write", "unknown/newExecution"} {
		t.Run(method, func(t *testing.T) {
			upstream := &writeBuffer{}
			f := &Facade{ctx: context.Background(), upstream: upstream, threads: map[string]bool{"fixture": true}, decisions: map[string]string{}}
			f.handle(rpc{ID: json.RawMessage(`1`), Method: method, Params: json.RawMessage(`{"threadId":"fixture","input":[{"type":"text","text":"corrija isso"}]}`)})
			if upstream.Len() != 0 || f.active != nil {
				t.Fatal("unadmitted execution reached native App Server")
			}
		})
	}
}

func TestNativeTransportRequiresAdmissionAndScopedAuthentication(t *testing.T) {
	f := &Facade{providerToken: "local-fixture-key"}
	for _, header := range []string{"", "Bearer wrong", "Bearer local-fixture-key"} {
		r := httptest.NewRequest("POST", "http://127.0.0.1/v1/responses", strings.NewReader(`{}`))
		r.Header.Set("Authorization", header)
		w := httptest.NewRecorder()
		f.responses(w, r)
		if w.Code != 401 && w.Code != 403 {
			t.Fatal("provider bypassed transport admission")
		}
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1", nil)
	r.Header.Set("Authorization", "Bearer local-fixture-key")
	r.Header.Set("Origin", "https://example.invalid")
	if authorized(r, f.providerToken) {
		t.Fatal("browser origin admitted")
	}
}

func TestNativeEnvironmentDoesNotCopyProviderOrMCPAuth(t *testing.T) {
	f := &Facade{home: "/private/fixture", remoteToken: "remote-fixture", providerToken: "provider-fixture", options: Options{Environment: []string{"HOME=/operator", "PATH=/bin", "DISPLAY=:1", "OPENAI_API_KEY=must-not-pass", "ANTHROPIC_API_KEY=must-not-pass", "SSH_AUTH_SOCK=/must-not-pass", "DBUS_SESSION_BUS_ADDRESS=must-not-pass", "CODEX_HOME=/operator/.codex", "IVOAI_MCP_TOKEN=must-not-pass"}}}
	env := strings.Join(f.Environment(), "\n")
	if strings.Contains(env, "must-not-pass") || strings.Contains(env, "CODEX_HOME=/operator") || !strings.Contains(env, "CODEX_HOME=/private/fixture") {
		t.Fatal("native frontend environment isolation failed")
	}
}

type fixtureRunner struct {
	calls     atomic.Int32
	decision  chan bool
	cancelled atomic.Bool
	effort    atomic.Value
}

func (r *fixtureRunner) Run(ctx context.Context, request opencodebridge.ExecutorRequest, output func(string) error) (opencodebridge.ExecutorResult, error) {
	r.calls.Add(1)
	r.effort.Store(request.Effort)
	if r.decision != nil {
		select {
		case allow := <-r.decision:
			if !allow {
				return opencodebridge.ExecutorResult{}, context.Canceled
			}
		case <-ctx.Done():
			r.cancelled.Store(true)
			return opencodebridge.ExecutorResult{}, ctx.Err()
		}
	}
	if err := output("fixture synthesis"); err != nil {
		return opencodebridge.ExecutorResult{}, err
	}
	return opencodebridge.ExecutorResult{ExecutorSessionID: "native_fixture_worker"}, nil
}

func TestLiveNativeAppServerAdmission(t *testing.T) {
	binary := os.Getenv("IVOAI_LIVE_CODEX_PATH")
	if binary == "" {
		t.Skip("requires explicit official Codex binary")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	runner := &fixtureRunner{decision: make(chan bool, 1)}
	var decided atomic.Bool
	catalog := opencodebridge.CatalogFromRegistry(routing.Registry{Providers: map[string]routing.ProviderCapability{"codex": {Provider: "codex", Authenticated: true, Models: []routing.ModelCapability{{Name: "fixture-strong", DisplayName: "Fixture strong", CapabilityTier: routing.TierStrong, SupportedEfforts: []string{"medium", "high"}, DefaultEffort: "high", Source: routing.SourceRuntimeVerified}}}}})
	bridge, err := opencodebridge.Start(opencodebridge.Options{Frontend: "codex", RequirePromptGate: true, PreferredExecutor: "codex", Catalog: catalog, Runner: runner, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() opencodebridge.Status { return opencodebridge.Status{Frontend: "codex"} },
		NativePermissions: func() []opencodebridge.PermissionView {
			if runner.calls.Load() > 0 && !decided.Load() {
				return []opencodebridge.PermissionView{{ID: "plan_fixture", Description: "Approve fixture plan"}}
			}
			return nil
		},
		ReplyNativePermission: func(_ context.Context, id string, allow bool) error {
			if id != "plan_fixture" {
				t.Error("wrong native decision identity")
			}
			decided.Store(true)
			runner.decision <- allow
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	root := t.TempDir()
	f, err := Start(ctx, Options{Binary: binary, Directory: root, RuntimeDir: root, SessionID: "native_fixture", Bridge: bridge, Environment: os.Environ()})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+f.remoteToken)
	client, _, err := websocket.Dial(ctx, "ws://"+f.listener.Addr().String(), &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatal("native transport connection failed")
	}
	defer client.CloseNow()
	client.SetReadLimit(maxFrame)
	send := func(id int, method string, params any) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
		if client.Write(ctx, websocket.MessageText, body) != nil {
			t.Fatal("native request failed")
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
			t.Fatal("native event invalid")
		}
		return event
	}
	waitID := func(id string) rpc {
		t.Helper()
		for {
			event := read()
			if string(event.ID) == id {
				return event
			}
		}
	}
	send(1, "initialize", map[string]any{"clientInfo": map[string]any{"name": "ivoai_fixture", "version": "0.10.1"}, "capabilities": map[string]any{"experimentalApi": true}})
	if event := waitID("1"); len(event.Error) > 0 {
		t.Fatal("native initialization rejected")
	}
	if client.Write(ctx, websocket.MessageText, []byte(`{"method":"initialized"}`)) != nil {
		t.Fatal("initialize ack failed")
	}
	send(2, "thread/start", map[string]any{})
	event := waitID("2")
	if len(event.Error) > 0 {
		t.Fatalf("fixture thread start error: %.1000s", event.Error)
	}
	var started struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	_ = json.Unmarshal(event.Result, &started)
	if started.Thread.ID == "" {
		t.Fatal("native thread missing")
	}
	send(3, "turn/start", map[string]any{"threadId": started.Thread.ID, "input": []any{map[string]any{"type": "text", "text": "corrija isso"}}})
	if event := waitID("3"); !bytes.Contains(event.Error, []byte("PROMPT_INSUFFICIENT")) {
		t.Fatal("first turn did not fail closed")
	}
	if runner.calls.Load() != 0 {
		t.Fatal("worker started before gate")
	}
	send(4, "turn/start", map[string]any{"threadId": started.Thread.ID, "input": []any{map[string]any{"type": "text", "text": "Read VERSION and report the value. Acceptance: return only the version; do not modify files."}}})
	if event := waitID("4"); len(event.Error) > 0 {
		t.Fatalf("fixture admitted turn error: %.1000s", event.Error)
	}
	synthesis := false
	for {
		event := read()
		if event.Method == "item/tool/requestUserInput" {
			answer, _ := json.Marshal(map[string]any{"id": event.ID, "result": map[string]any{"answers": map[string]any{"decision": map[string]any{"answers": []string{"Approve"}}}}})
			if client.Write(ctx, websocket.MessageText, answer) != nil {
				t.Fatal("native approval reply failed")
			}
		}
		if event.Method == "error" {
			t.Fatalf("fixture native error: %.1000s", event.Params)
		}
		if event.Method == "item/completed" && strings.Contains(string(event.Params), "fixture synthesis") {
			synthesis = true
		}
		if event.Method == "turn/completed" {
			break
		}
	}
	if !synthesis || runner.calls.Load() != 1 || !decided.Load() {
		t.Fatalf("native synthesis=%t worker calls=%d", synthesis, runner.calls.Load())
	}
	decided.Store(false)
	send(5, "turn/start", map[string]any{"threadId": started.Thread.ID, "input": []any{map[string]any{"type": "text", "text": "Read VERSION and report the value. Acceptance: return only the version; do not modify files."}}})
	if event := waitID("5"); len(event.Error) > 0 {
		t.Fatal("second native turn rejected")
	}
	for {
		event := read()
		if event.Method == "item/tool/requestUserInput" {
			var p struct {
				ThreadID string `json:"threadId"`
				TurnID   string `json:"turnId"`
			}
			_ = json.Unmarshal(event.Params, &p)
			send(6, "turn/interrupt", map[string]any{"threadId": p.ThreadID, "turnId": p.TurnID})
		}
		if event.Method == "turn/completed" {
			break
		}
	}
	deadline := time.Now().Add(time.Second)
	for !runner.cancelled.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if f.TurnError() == nil || !runner.cancelled.Load() {
		t.Fatal("native interrupt lost the worker cancellation or failure state")
	}
}

var _ io.WriteCloser = (*writeBuffer)(nil)
