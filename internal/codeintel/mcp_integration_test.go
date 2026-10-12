package codeintel

import (
	"context"
	"github.com/ivo-lopes/ivoai/internal/externalmcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type authorizedTransport struct{ token string }

func (t authorizedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(r)
}
func TestReviewedNativeMCP(t *testing.T) {
	node, dist := os.Getenv("IVOAI_GRAFT_TEST_NODE"), os.Getenv("IVOAI_GRAFT_TEST_DIST")
	if node == "" || dist == "" {
		t.Skip("reviewed native runtime required")
	}
	p, _ := testProvider(t)
	p.Node = node
	p.Distribution = dist
	p.Executable = os.Args[0]
	p.run = p.runProcess
	os.WriteFile(filepath.Join(p.Root, "main.go"), []byte("package fixture\nfunc Multiply(v int) int { return v*2 }\nfunc Caller() int { return Multiply(3) }\n"), 0600)
	gateway, err := externalmcp.Start([]externalmcp.Target{{Name: "ivoai-code", Local: p.Handler(), Restricted: true, AllowedTools: ToolNames()}}, "full")
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "ivoai-contract-test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: gateway.URL(0), HTTPClient: &http.Client{Transport: authorizedTransport{gateway.Token()}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	list, err := session.ListTools(context.Background(), nil)
	if err != nil || len(list.Tools) != 7 {
		t.Fatalf("inventory %+v %v", list, err)
	}
	calls := map[string]map[string]any{"graft_find_code": {"query": "Multiply"}, "graft_file_api": {"file": "main.go"}, "graft_trace_calls": {"symbol": "Multiply"}, "graft_find_all": {"pattern": "Multiply", "fixed": true}, "graft_repo_map": {}, "graft_check_freshness": {}, "code_inspect": {"operation": "grep", "query": "Multiply"}}
	for name, args := range calls {
		t.Run(name, func(t *testing.T) {
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
			if err != nil || result.IsError {
				t.Fatalf("native MCP failed: %v %+v", err, result)
			}
			if len(result.Content) != 1 {
				t.Fatal("unbounded content envelope")
			}
			text, ok := result.Content[0].(*mcp.TextContent)
			if !ok || !strings.Contains(text.Text, `"untrusted":true`) {
				t.Fatal("missing untrusted result contract")
			}
		})
	}
	gateway.SetToolAdmission(func(string, string) (bool, bool) { return false, false })
	if r, e := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "code_inspect", Arguments: map[string]any{"operation": "map"}}); e == nil && !r.IsError {
		t.Fatal("revoked admission accepted")
	}
}
