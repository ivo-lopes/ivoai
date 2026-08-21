package webmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	contextsvc "github.com/ivo-lopes/ivoai/internal/context"
	"github.com/ivo-lopes/ivoai/internal/webauth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fixedEmbedder struct{}

func (fixedEmbedder) Dimensions() int { return 2 }
func (fixedEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return [][]float32{{1, 0}}, nil
}

func testService(t *testing.T) *contextsvc.Service {
	t.Helper()
	svc, err := contextsvc.NewService(fixedEmbedder{}, contextsvc.NewMemoryStore(), contextsvc.NewMemoryCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestOfficialSDKListsAllowlistAndDeleteConfirmation(t *testing.T) {
	var mu sync.Mutex
	var called string
	var args map[string]any
	handler, err := New(Config{Version: "test", Context: testService(t), Memory: func(_ context.Context, name string, raw json.RawMessage) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		called = name
		_ = json.Unmarshal(raw, &args)
		return map[string]any{"ok": true}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	principal := webauth.Principal{ClientID: "test", Scopes: append([]string(nil), webauth.DefaultScopes...)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r.WithContext(webauth.WithPrincipal(r.Context(), principal)))
	}))
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
		encoded, err := json.Marshal(tool.OutputSchema)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(encoded, []byte(`"properties"`)) || !bytes.Contains(encoded, []byte(`"required"`)) {
			t.Errorf("tool %s has incomplete output schema: %s", tool.Name, encoded)
		}
	}
	for _, name := range []string{"context_search", "context_get_document", "context_recent", "context_health", "memory_query", "memory_recent", "memory_read_page", "memory_write_page", "memory_delete_page", "memory_status", "memory_feedback"} {
		if !names[name] {
			t.Errorf("missing tool %s", name)
		}
	}
	if names["memory_handoff_accept"] || names["memory_sweep"] {
		t.Fatal("unsafe maintenance tool exposed")
	}
	if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_delete_page", Arguments: map[string]any{"path": "notes/a.md", "confirm_path": "notes/b.md"}}); err == nil {
		t.Fatal("mismatched confirmation accepted")
	}
	if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_delete_page", Arguments: map[string]any{"path": "notes/a.md", "confirm_path": "notes/a.md"}}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if called != "memory_delete_page" || args["path"] != "notes/a.md" {
		t.Fatalf("call=%q args=%v", called, args)
	}
	if _, ok := args["confirm_path"]; ok {
		t.Fatal("confirm_path forwarded upstream")
	}
}

func TestToolScopeEnforced(t *testing.T) {
	handler, _ := New(Config{Version: "test", Context: testService(t), Memory: func(context.Context, string, json.RawMessage) (any, error) { return map[string]any{}, nil }})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := webauth.Principal{Scopes: []string{webauth.ScopeMemoryRead}}
		handler.ServeHTTP(w, r.WithContext(webauth.WithPrincipal(r.Context(), p)))
	}))
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "memory_write_page", Arguments: map[string]any{"path": "a.md", "body": "# Note\n\nx"}}); err == nil {
		t.Fatal("write allowed with read-only scope")
	}
}

func TestEmbeddedSkillMatchesCanonicalAndIsServed(t *testing.T) {
	canonical, err := os.ReadFile("../../skills/ivoai-memory-context/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := embeddedSkill()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, embedded) {
		t.Fatal("embedded skill differs from canonical SKILL.md")
	}
	handler, _ := New(Config{Version: "test", Context: testService(t), Memory: func(context.Context, string, json.RawMessage) (any, error) { return map[string]any{}, nil }})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal := webauth.Principal{Scopes: []string{webauth.ScopeContextRead}}
		handler.ServeHTTP(w, r.WithContext(webauth.WithPrincipal(r.Context(), principal)))
	}))
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "skill-test"}, nil)
	if err := mcp.AddSendingCustomMethod[*skillsListParams, *skillsListResult](client, "skills/list"); err != nil {
		t.Fatal(err)
	}
	if err := mcp.AddSendingCustomMethod[*skillsGetParams, *skillsGetResult](client, "skills/get"); err != nil {
		t.Fatal(err)
	}
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	resource, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: skillURI})
	if err != nil {
		t.Fatal(err)
	}
	if len(resource.Contents) != 1 || resource.Contents[0].Text != string(canonical) {
		t.Fatal("served skill content mismatch")
	}
	listed, err := mcp.CallCustomMethod[*skillsListParams, *skillsListResult](context.Background(), session, "skills/list", &skillsListParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Skills) != 1 || listed.Skills[0].URI != skillURI || len(listed.Skills[0].Resources) != 1 {
		t.Fatalf("unexpected skills/list: %#v", listed)
	}
	got, err := mcp.CallCustomMethod[*skillsGetParams, *skillsGetResult](context.Background(), session, "skills/get", &skillsGetParams{URI: skillURI})
	if err != nil {
		t.Fatal(err)
	}
	if got.Skill.Resources[0].Digest != listed.Skills[0].Resources[0].Digest {
		t.Fatal("skills/get digest mismatch")
	}
}
