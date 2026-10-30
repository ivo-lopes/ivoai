package codeintel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The SDK talks directly to the handler; no listener, socket or external service.
type directOnlyHandlerTransport struct{ handler http.Handler }

func (transport directOnlyHandlerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response := httptest.NewRecorder()
	transport.handler.ServeHTTP(response, request)
	return response.Result(), nil
}

func TestDirectOnlyMCPPreservesAliasesAndRefusesStructuralResults(t *testing.T) {
	p, calls := testProvider(t)
	if err := os.WriteFile(filepath.Join(p.Root, "main.go"), []byte("package fixture\nfunc ExactName() int { return 42 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	s, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: "http://in-process.invalid/mcp", HTTPClient: &http.Client{Transport: directOnlyHandlerTransport{p.WithoutStructuralRuntime().Handler()}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tools, err := s.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != len(ToolNames()) {
		t.Fatal("MCP schema catalog changed")
	}
	for _, c := range []struct {
		name        string
		args        map[string]any
		unavailable bool
	}{
		{"code_inspect", map[string]any{"operation": "grep", "query": "ExactName"}, false},
		{"graft_find_code", map[string]any{"query": "ExactName"}, false},
		{"graft_file_api", map[string]any{"file": "main.go"}, false},
		{"graft_find_all", map[string]any{"pattern": "ExactName"}, true},
		{"graft_trace_calls", map[string]any{"symbol": "ExactName"}, true},
		{"graft_repo_map", map[string]any{}, true},
		{"graft_check_freshness", map[string]any{}, true},
	} {
		result, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: c.name, Arguments: c.args})
		if err != nil || result.IsError != c.unavailable || len(result.Content) != 1 || calls.Load() != 0 {
			t.Fatalf("%s: Direct/structural contract changed", c.name)
		}
	}
}
