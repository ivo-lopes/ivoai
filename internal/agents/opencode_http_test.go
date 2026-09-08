package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
)

type controlledFixtureRunner struct{}

func (controlledFixtureRunner) Run(_ context.Context, _ opencodebridge.ExecutorRequest, emit func(string) error) (opencodebridge.ExecutorResult, error) {
	return opencodebridge.ExecutorResult{ExecutorSessionID: "thread_contract"}, emit("controlled executor final response")
}

func TestLiveOpenCodeExecutorHTTPContract(t *testing.T) {
	path, version := os.Getenv("IVOAI_LIVE_OPENCODE_PATH"), os.Getenv("IVOAI_LIVE_OPENCODE_VERSION")
	if path == "" || version == "" {
		t.Skip("requires pinned OpenCode artifact")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	bridge, err := opencodebridge.Start(opencodebridge.Options{Runner: controlledFixtureRunner{}, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() opencodebridge.Status { return opencodebridge.Status{} }})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "fixture.txt"), []byte("safe contract fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	e := OpenCodeExecutor{Runtime: Runtime{AgentPath: path}, Version: version}
	options := opencodebridge.ManagedOptions{Directory: root, RuntimeDir: filepath.Join(root, "runtime"), StateDir: filepath.Join(root, "state"), Bridge: bridge, PermissionMode: "full"}
	s, err := e.OpenSession(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	if s.Transport() != "HTTP_SSE" {
		t.Fatal("wrong transport")
	}
	eventCtx, stop := context.WithCancel(ctx)
	seen := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- s.Events(eventCtx, func(json.RawMessage) error {
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
		t.Fatal("SSE unavailable")
	}
	for _, op := range []string{"sessions", "get", "status", "diff", "permissions", "files", "file-content", "agents", "mcp"} {
		q := url.Values{}
		if op == "files" {
			q.Set("path", root)
		}
		if op == "file-content" {
			q.Set("path", "fixture.txt")
		}
		if _, err := s.Request(ctx, op, q, nil); err != nil {
			t.Fatalf("%s: %v", op, err)
		}
	}
	prompt := map[string]any{"model": map[string]string{"providerID": "ivoai", "modelID": "auto"}, "parts": []map[string]string{{"type": "text", "text": "safe contract prompt"}}}
	if _, err := s.Prompt(ctx, prompt, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Prompt(ctx, prompt, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Cancel(ctx); err != nil {
		t.Fatal(err)
	}
	stop()
	<-done
	id := s.ID()
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Request(ctx, "status", nil, nil); err == nil {
		t.Fatal("closed session accepted request")
	}
	options.ResumeSessionID = id
	resumed, err := e.OpenSession(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close(context.Background())
	if resumed.ID() != id {
		t.Fatal("resume ID changed")
	}
	if err := resumed.Close(ctx); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	e.Runtime.Out = &output
	e.Options = options
	if err := e.StartSession(ctx, core.SessionRequest{Controlled: true, Prompt: "safe controlled StartSession", Model: "ivoai/auto"}, nil); err != nil {
		t.Fatal(err)
	}
	if output.String() != "controlled executor final response" {
		t.Fatal("controlled StartSession did not return final response")
	}
}

func TestLiveOpenCodeExecutorNativeLifecycle(t *testing.T) {
	path, version := os.Getenv("IVOAI_LIVE_OPENCODE_PATH"), os.Getenv("IVOAI_LIVE_OPENCODE_VERSION")
	if path == "" || version == "" {
		t.Skip("requires pinned OpenCode")
	}
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	e := OpenCodeExecutor{Runtime: Runtime{AgentPath: path}, Version: version}
	s, err := e.OpenSession(ctx, opencodebridge.ManagedOptions{NativeExecutor: true, Directory: root, RuntimeDir: filepath.Join(root, "runtime"), StateDir: filepath.Join(root, "state"), Environment: []string{"HOME=" + root, "PATH=" + os.Getenv("PATH")}, PermissionMode: "interactive"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	if _, err := s.Request(ctx, "get", nil, nil); err != nil {
		t.Fatal(err)
	}
	if s.Transport() != "HTTP_SSE" {
		t.Fatal("native executor not HTTP")
	}
}
