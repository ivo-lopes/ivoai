package connections

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestInventoryPolicyAndSelectiveLifecycle(t *testing.T) {
	var calls atomic.Int32
	s := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	destructive := false
	annotations := map[string]*mcp.ToolAnnotations{
		"read_tool": {ReadOnlyHint: true}, "write_tool": {DestructiveHint: &destructive},
		"get_unknown": nil, "delete_tool": {DestructiveHint: &destructive},
	}
	for name, hints := range annotations {
		s.AddTool(&mcp.Tool{Name: name, Description: "fixture-private-token\n\x1b[31m", Annotations: hints, InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			calls.Add(1)
			return &mcp.CallToolResult{}, nil
		})
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, &mcp.StreamableHTTPOptions{JSONResponse: true})
	server := httptest.NewServer(handler)
	defer server.Close()
	store := connStore(t.TempDir())
	r := Registry{Store: store}
	for _, name := range []string{"a", "personal"} {
		if err := r.Add(name, config.MCPServer{URL: server.URL, Enabled: true, Kind: "external"}); err != nil {
			t.Fatal(err)
		}
		if err := r.SetBearer(name, "fixture-private-token"); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := r.List()
	if before["a"].MCPHealth() != "UNKNOWN" {
		t.Fatal("unprobed treated as healthy")
	}
	if count, err := r.Refresh(context.Background(), "a"); err != nil || count != 4 {
		t.Fatalf("inventory count=%d err=%v", count, err)
	}
	entries, _ := r.List()
	for _, tool := range entries["a"].Tools {
		want := "MUTATING"
		if tool.Name == "read_tool" {
			want = "READ_ONLY"
		}
		if tool.Name == "get_unknown" {
			want = "UNKNOWN"
		}
		if tool.Classification != want || len(tool.SchemaSHA256) != 64 || strings.Contains(tool.Description, "fixture-private-token") || strings.Contains(tool.Description, "\x1b") {
			t.Fatal("unsafe/misclassified inventory")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("discovery executed a tool")
	}
	for _, enabled := range []bool{false, true} {
		if err := r.Enable("a", enabled); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.SetPolicy("a", "read_only", false); err != nil {
		t.Fatal(err)
	}
	if err := r.SetPolicy("a", "disabled", true); err != nil {
		t.Fatal(err)
	}
	entries, _ = r.List()
	if !reflect.DeepEqual(before["personal"], entries["personal"]) {
		t.Fatal("personal entry changed")
	}
	if entries["a"].ID != before["a"].ID || entries["a"].MCPHealth() != "HEALTHY" {
		t.Fatal("identity/health lost")
	}
	if err := r.SetPolicy("a", "all", true); err == nil {
		t.Fatal("grant-all policy accepted")
	}
	if err := r.Enable("ivoai-memory", false); err == nil {
		t.Fatal("managed identity mutable")
	}
	body, _ := os.ReadFile(store.Paths.Config)
	if strings.Contains(string(body), "fixture-private-token") {
		t.Fatal("credential persisted in metadata")
	}
	if err := r.Remove("a"); err != nil {
		t.Fatal(err)
	}
	entries, _ = r.List()
	if !reflect.DeepEqual(before["personal"], entries["personal"]) {
		t.Fatal("remove changed personal entry")
	}
	if _, err := r.Headers(entries["personal"]); err != nil {
		t.Fatal("other credential removed")
	}
}

func TestFailedInventoryHasTypedHealthAndNoStaleTools(t *testing.T) {
	for _, status := range []int{401, 403, 503} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "private upstream details", status) }))
		store := connStore(t.TempDir())
		r := Registry{Store: store}
		if err := r.Add("a", config.MCPServer{URL: server.URL, Kind: "external", Enabled: true}); err != nil {
			t.Fatal(err)
		}
		if err := r.updateMCP("a", func(entry *config.MCPServer) error {
			entry.Tools = []config.MCPTool{{Name: "stale_tool", Classification: "READ_ONLY"}}
			entry.Health = "HEALTHY"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		_, err := r.Refresh(context.Background(), "a")
		if err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("unsafe probe error")
		}
		entries, _ := r.List()
		want := map[int]string{401: "AUTH_REQUIRED", 403: "AUTH_FAILED", 503: "UNREACHABLE"}[status]
		if entries["a"].Health != want || len(entries["a"].Tools) != 0 {
			t.Fatal("incorrect health")
		}
		server.Close()
	}
}

func TestRegistryRejectsNormalizedAliasCollisionBeforeMutation(t *testing.T) {
	r := Registry{Store: connStore(t.TempDir())}
	entry := config.MCPServer{URL: "https://example.invalid/mcp", Kind: "external", Enabled: true}
	if err := r.Add("foo.bar", entry); err != nil {
		t.Fatal(err)
	}
	if err := r.Add("foo_bar", entry); err == nil {
		t.Fatal("normalized alias collision accepted")
	}
	entries, _ := r.List()
	if len(entries) != 1 {
		t.Fatal("failed addition changed registry")
	}
}
