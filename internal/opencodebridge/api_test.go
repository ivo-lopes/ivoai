package opencodebridge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAPIRejectsUnsafeDestinationsAndResponses(t *testing.T) {
	m := &Managed{URL: "http://example.test"}
	if _, err := m.APIRequest(context.Background(), "sessions", "", nil, nil); err == nil {
		t.Fatal("nonloopback accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	m.URL = server.URL
	if _, err := m.APIRequest(context.Background(), "get", "../../config", nil, nil); err == nil {
		t.Fatal("path traversal accepted")
	}
	for _, id := range []string{".", ".."} {
		if _, err := m.APIRequest(context.Background(), "get", id, nil, nil); err == nil {
			t.Fatal("dot segment accepted")
		}
	}
	if _, err := m.APIRequest(context.Background(), "sessions", "", url.Values{"directory": {"/other"}}, nil); err == nil {
		t.Fatal("workspace override accepted")
	}
	if _, err := m.APIRequest(context.Background(), "sessions", "", nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestAPIRejectsRedirectsMediaTypesAndOversizedJSON(t *testing.T) {
	for _, kind := range []string{"redirect", "media", "oversize", "invalid-json"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch kind {
				case "redirect":
					w.Header().Set("Location", "/config")
					w.WriteHeader(302)
				case "media":
					w.Header().Set("Content-Type", "text/html")
					w.Write([]byte(`{}`))
				case "oversize":
					w.Write([]byte(`"` + strings.Repeat("x", maxAPIResponse) + `"`))
				case "invalid-json":
					w.Write([]byte(`{"incomplete":`))
				}
			}))
			defer server.Close()
			m := &Managed{URL: server.URL}
			if _, err := m.APIRequest(context.Background(), "sessions", "", nil, nil); err == nil {
				t.Fatal("unsafe response accepted")
			}
		})
	}
}

func TestAPIEventsBoundsAndCancellation(t *testing.T) {
	for _, body := range []string{"data: invalid\n\n", "data: " + strings.Repeat("x", (1<<20)+1)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Write([]byte(body))
		}))
		m := &Managed{URL: server.URL}
		if err := m.Events(context.Background(), func(json.RawMessage) error { return nil }); err == nil {
			t.Fatal("invalid event accepted")
		}
		server.Close()
	}
}

func TestLiveManagedOpenCodeAPI(t *testing.T) {
	path, version := os.Getenv("IVOAI_LIVE_OPENCODE_PATH"), os.Getenv("IVOAI_LIVE_OPENCODE_VERSION")
	if path == "" || version == "" {
		t.Skip("requires pinned OpenCode")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "fixture.txt"), []byte("safe fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	bridge, err := Start(Options{Runner: &fakeRunner{result: ExecutorResult{ExecutorSessionID: "thread_api"}}, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{} }})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	m, err := StartManaged(ctx, ManagedOptions{OpenCodePath: path, Version: version, Directory: root, RuntimeDir: filepath.Join(root, "runtime"), StateDir: filepath.Join(root, "state"), Bridge: bridge})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	eventCtx, stopEvents := context.WithCancel(ctx)
	defer stopEvents()
	seen := make(chan struct{}, 1)
	eventsDone := make(chan error, 1)
	go func() {
		eventsDone <- m.Events(eventCtx, func(json.RawMessage) error {
			select {
			case seen <- struct{}{}:
			default:
			}
			return nil
		})
	}()
	select {
	case <-seen:
	case <-ctx.Done():
		t.Fatal("SSE did not connect")
	}
	data, err := m.APIRequest(ctx, "create", "", nil, map[string]string{"title": "safe API fixture"})
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &created); err != nil || !safeID(created.ID) {
		t.Fatal("session create decode", err)
	}
	for _, operation := range []string{"sessions", "status", "get", "diff", "agents", "mcp", "permissions", "files", "file-content"} {
		query := url.Values{}
		if operation == "files" {
			query.Set("path", root)
		}
		if operation == "file-content" {
			query.Set("path", "fixture.txt")
		}
		if _, err := m.APIRequest(ctx, operation, created.ID, query, nil); err != nil {
			t.Fatalf("%s: %v", operation, err)
		}
	}
	prompt := map[string]any{"model": map[string]string{"providerID": "ivoai", "modelID": "auto"}, "parts": []any{map[string]string{"type": "text", "text": "safe API prompt"}}}
	if _, err := m.APIRequest(ctx, "prompt", created.ID, nil, prompt); err != nil {
		t.Fatal(err)
	}
	if _, err := m.APIRequest(ctx, "prompt-async", created.ID, nil, prompt); err != nil {
		t.Fatal(err)
	}
	if _, err := m.APIRequest(ctx, "abort", created.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	stopEvents()
	select {
	case err := <-eventsDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("event cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SSE cancellation did not release reader")
	}
	t.Log("HTTP create/list/get/prompt/async/status/abort/diff/files/agents/MCP/permissions=PASS SSE lifecycle=PASS")
}
