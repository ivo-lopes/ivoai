package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNativeMCPProjectionPrivateAndNonTraversable(t *testing.T) {
	root := t.TempDir()
	for _, invalid := range []string{"../foreign", "a/b", ".hidden", ""} {
		if _, err := nativeProjectionPath(root, "sess_"+strings.Repeat("a", 32), invalid); err == nil {
			t.Fatal("server path traversal accepted")
		}
	}
	p := nativeMCPProjection{Identity: core.PrimaryTurnIdentity{ID: "sess_" + strings.Repeat("a", 32)}, Server: "ivoai-code", Endpoint: "http://127.0.0.1:12345/mcp", Token: strings.Repeat("b", 48)}
	path, err := writeNativeMCPProjection(root, p)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private file: %v %v", info, err)
	}
	if filepath.Dir(path) != filepath.Join(root, "native-mcp-projections", p.Identity.ID) {
		t.Fatal("unscoped projection")
	}
}

// This is an offline transport/authorization fixture, not provider inference.
func TestNativeMCPProxyPreservesSchemasAndRejectsRevokedCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	var allowed atomic.Bool
	allowed.Store(true)
	upstream := mcp.NewServer(&mcp.Implementation{Name: "upstream-fixture", Version: "1"}, nil)
	upstream.AddTool(&mcp.Tool{Name: "exact_read", Description: "Exact fixture evidence.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}, "required": []string{"query"}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "exact-evidence"}}}, nil
	})
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return upstream }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-capability" {
			http.Error(w, "unauthorized", 401)
			return
		}
		transport.ServeHTTP(w, r)
	}))
	defer server.Close()
	proxy, closeProxy, err := newNativeMCPProxy(ctx, nativeMCPProjection{Server: "ivoai-code", Endpoint: server.URL, Token: "fixture-capability"}, func(context.Context) error {
		if !allowed.Load() {
			return errors.New("revoked")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeProxy()
	st, ct := mcp.NewInMemoryTransports()
	go func() { _ = proxy.Run(ctx, st) }()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "fixture-consumer", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	listed, err := client.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil || len(listed.Tools) != 1 || listed.Tools[0].Name != "exact_read" || !listed.Tools[0].Annotations.ReadOnlyHint {
		t.Fatalf("schema projection=%#v err=%v", listed, err)
	}
	result, err := client.CallTool(ctx, &mcp.CallToolParams{Name: "exact_read", Arguments: map[string]any{"query": "fixture"}})
	if err != nil || result.IsError || calls.Load() != 1 {
		t.Fatalf("call result=%#v err=%v calls=%d", result, err, calls.Load())
	}
	allowed.Store(false)
	result, err = client.CallTool(ctx, &mcp.CallToolParams{Name: "exact_read", Arguments: map[string]any{"query": "fixture"}})
	if err == nil && !result.IsError || calls.Load() != 1 {
		t.Fatalf("revoked call escaped: %v calls=%d", err, calls.Load())
	}
}
