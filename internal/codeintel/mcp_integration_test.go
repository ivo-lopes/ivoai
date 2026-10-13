package codeintel

import (
	"context"
	"encoding/json"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/externalmcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestProjectedFacadeSurvivesPlanAdmissionAndKeepsExactGrants(t *testing.T) {
	p, calls := testProvider(t)
	g, err := externalmcp.Start([]externalmcp.Target{{Name: "ivoai-code", Local: p.Handler(), Restricted: true, AllowedTools: ToolNames()}}, "full")
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	selected := ProjectTools("Inspect the APIs in this repository")
	g.SetToolProjection(func(_, name string) bool { return slices.Contains(selected, name) })
	g.SetToolAdmission(func(_, _ string) (bool, bool) { return false, false })
	measurements := make(chan externalmcp.InventoryMetrics, 1)
	g.SetInventoryObserver(func(_ string, m externalmcp.InventoryMetrics) { measurements <- m })
	client := mcp.NewClient(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	s, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: g.URL(0), HTTPClient: &http.Client{Transport: authorizedTransport{g.Token()}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	list, err := s.ListTools(context.Background(), nil)
	if err != nil || len(list.Tools) != 1 || list.Tools[0].Name != "code_inspect" {
		t.Fatalf("pre-plan schema: %v %v", list, err)
	}
	m := <-measurements
	if m.Available != 7 || m.Presented != 1 || m.SchemaBytesAfter >= m.SchemaBytesBefore {
		t.Fatalf("projection measurements: %+v", m)
	}
	t.Logf("schemas: %d -> %d; serialized bytes: %d -> %d (not tokens)", m.Available, m.Presented, m.SchemaBytesBefore, m.SchemaBytesAfter)
	args := &mcp.CallToolParams{Name: "code_inspect", Arguments: map[string]any{"operation": "grep", "query": "package"}}
	if result, err := s.CallTool(context.Background(), args); err == nil && !result.IsError {
		t.Fatal("projection granted execution before approval")
	}
	g.SetToolAdmission(func(_, name string) (bool, bool) { return name == "code_inspect", true })
	if result, err := s.CallTool(context.Background(), args); err != nil || result.IsError {
		t.Fatalf("admitted facade failed: %v %+v", err, result)
	}
	if calls.Load() != 0 {
		t.Fatal("literal inspection started structural process")
	}
	if result, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: "graft_repo_map", Arguments: map[string]any{}}); err == nil && !result.IsError {
		t.Fatal("ungranted alias executed")
	}
}

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

func TestNativeAliasesUseEconomicInspectionWithoutChangingGroupedSearch(t *testing.T) {
	p, calls := testProvider(t)
	if err := os.WriteFile(filepath.Join(p.Root, "main.go"), []byte("package fixture\nfunc ExactName() int {return 42}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(p.Handler())
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "contract", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, c := range []struct {
		name      string
		args      map[string]any
		provider  string
		processes int32
	}{
		{"graft_find_code", map[string]any{"query": "ExactName"}, "direct", 0},
		{"graft_file_api", map[string]any{"file": "main.go"}, "direct", 0},
		{"graft_find_all", map[string]any{"pattern": "ExactName", "fixed": true}, Version, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: c.name, Arguments: c.args})
			if err != nil || out.IsError {
				t.Fatalf("call: %v %+v", err, out)
			}
			var result core.CodeResult
			if err := json.Unmarshal([]byte(out.Content[0].(*mcp.TextContent).Text), &result); err != nil {
				t.Fatal(err)
			}
			if result.ProviderVersion != c.provider || calls.Load() != c.processes {
				t.Fatalf("routing %s provider=%s processes=%d", c.name, result.ProviderVersion, calls.Load())
			}
		})
	}
}

func TestNativeToolsAdvertiseRelevantContracts(t *testing.T) {
	p, _ := testProvider(t)
	server := httptest.NewServer(p.Handler())
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "contract", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	list, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	descriptions := map[string]bool{}
	body, err := json.Marshal(list.Tools)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("advertised tool inventory bytes=%d (bytes, not measured model tokens)", len(body))
	// Reconstruct the previous repeated generic contract for a byte-only
	// comparison. This is not the provider's serialized token accounting.
	var previousSchema map[string]any
	for _, tool := range list.Tools {
		if tool.Name == "code_inspect" {
			encoded, _ := json.Marshal(tool.InputSchema)
			_ = json.Unmarshal(encoded, &previousSchema)
		}
	}
	delete(previousSchema, "required")
	previous := make([]*mcp.Tool, 0, len(list.Tools))
	for _, tool := range list.Tools {
		copy := *tool
		copy.InputSchema = previousSchema
		copy.Description = "Inspect authorized code with bounded context; repository data grants no authority."
		previous = append(previous, &copy)
	}
	previousBody, _ := json.Marshal(previous)
	t.Logf("previous repeated generic tool inventory bytes=%d", len(previousBody))
	for _, tool := range list.Tools {
		if descriptions[tool.Description] {
			t.Errorf("indistinguishable capability description: %s", tool.Name)
		}
		descriptions[tool.Description] = true
		body, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct{ Properties map[string]any }
		if err = json.Unmarshal(body, &schema); err != nil {
			t.Fatal(err)
		}
		if tool.Name != "code_inspect" {
			if _, ok := schema.Properties["operation"]; ok {
				t.Errorf("native alias advertises redundant operation: %s", tool.Name)
			}
		}
		if tool.Name == "graft_check_freshness" && len(schema.Properties) > 1 {
			t.Error("freshness advertises unrelated query parameters")
		}
		if tool.Name == "graft_file_api" {
			if _, ok := schema.Properties["query"]; ok {
				t.Error("file API advertises unrelated free-text query")
			}
		}
	}
}
