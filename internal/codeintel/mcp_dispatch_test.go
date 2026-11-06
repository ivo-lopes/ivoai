package codeintel

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNativeMCPWrappedQueriesKeepSchemasAndExactPayload(t *testing.T) {
	p, _ := testProvider(t)
	plain := httptest.NewServer(p.Handler())
	defer plain.Close()
	result := core.CodeResult{Text: "exact fixture data", SourceDigest: "fixture", ProviderVersion: "direct", Fresh: true, Untrusted: true, DurationMS: 137}
	var inspections, queries int
	wrapped := httptest.NewServer(p.HandlerWithQueries(func(_ context.Context, q core.CodeQuery) (core.CodeResult, error) { inspections++; return result, nil }, func(_ context.Context, q core.CodeQuery) (core.CodeResult, error) { queries++; return result, nil }))
	defer wrapped.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connect := func(url string) *mcp.ClientSession {
		t.Helper()
		c := mcp.NewClient(&mcp.Implementation{Name: "contract", Version: "1"}, nil)
		s, err := c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url, DisableStandaloneSSE: true}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	first, second := connect(plain.URL), connect(wrapped.URL)
	defer first.Close()
	defer second.Close()
	a, err := first.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	rawA, _ := json.Marshal(a.Tools)
	rawB, _ := json.Marshal(b.Tools)
	if !bytes.Equal(rawA, rawB) {
		t.Fatal("wrapping changed native tool schemas")
	}
	cases := map[string]map[string]any{"code_inspect": {"operation": "ask", "query": "Run"}, "graft_find_code": {"query": "Run"}, "graft_file_api": {"file": "main.go"}, "graft_find_all": {"pattern": "Run"}, "graft_trace_calls": {"symbol": "Run"}, "graft_repo_map": {}, "graft_check_freshness": {}}
	want, _ := json.Marshal(result)
	for name, args := range cases {
		got, err := second.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || got.IsError || len(got.Content) != 1 {
			t.Fatal("native dispatch failed", name, err)
		}
		text, ok := got.Content[0].(*mcp.TextContent)
		if !ok || !bytes.Equal([]byte(text.Text), want) {
			t.Fatal("native typed result payload changed", name)
		}
	}
	if inspections != 3 || queries != 4 {
		t.Fatal("automatic and structural dispatch semantics changed")
	}
}
