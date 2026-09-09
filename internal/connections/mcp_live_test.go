package connections

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/externalmcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Optional operator smoke: reads only list_projects on an explicitly selected
// registry entry. Credentials are resolved by the normal IVOAI boundary and
// never printed. CI uses hermetic gateway/native-provider fixtures instead.
func TestLivePlaneExternalMCPAuthentication(t *testing.T) {
	name := os.Getenv("IVOAI_LIVE_PLANE_MCP")
	if name == "" {
		t.Skip("requires explicitly configured operator MCP")
	}
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	r := Registry{Store: config.NewStore(paths)}
	entries, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := entries[name]
	if !ok || entry.Kind != "external" || entry.AuthMode != "bearer" {
		t.Fatal("configured bearer MCP required")
	}
	headers, err := r.Headers(entry)
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := externalmcp.Start([]externalmcp.Target{{Name: name, URL: entry.URL, Headers: headers}}, "full")
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "ivoai-operator-read-smoke", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: gateway.URL(0), DisableStandaloneSSE: true, HTTPClient: &http.Client{Transport: &probeTransport{token: gateway.Token()}}}, nil)
	if err != nil {
		t.Fatal("initialize failed (upstream details withheld)")
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_projects", Arguments: map[string]any{"per_page": 100}})
	if err != nil || result.IsError {
		t.Fatal("list_projects failed (private payload withheld)")
	}
	for _, content := range result.Content {
		text, ok := content.(*mcp.TextContent)
		if !ok {
			continue
		}
		var body any
		if json.Unmarshal([]byte(text.Text), &body) != nil {
			continue
		}
		if count, ok := projectResultCount(body); ok {
			t.Logf("REAL_PLANE_MCP_AUTH=PASS PLANE_LIST_PROJECTS=PASS PLANE_PROJECT_COUNT=%d FULL_MODE_PERMISSION_PROMPT_COUNT=%d", count, len(gateway.Pending()))
			return
		}
	}
	t.Fatal("successful tool result had an unrecognized count envelope (private payload withheld)")
}

func projectResultCount(body any) (int, bool) {
	switch v := body.(type) {
	case []any:
		return len(v), true
	case map[string]any:
		for _, key := range []string{"results", "projects", "data"} {
			if inner, ok := v[key]; ok {
				if n, ok := projectResultCount(inner); ok {
					return n, true
				}
			}
		}
	}
	return 0, false
}
