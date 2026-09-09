package connections

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/secrets"
)

func TestExternalMCPAuthenticationLifecycle(t *testing.T) {
	store := connStore(t.TempDir())
	registry := Registry{Store: store}
	if err := registry.Add("plane", config.MCPServer{URL: "https://example.invalid/mcp", Enabled: true, Kind: "external"}); err != nil {
		t.Fatal(err)
	}
	if err := registry.SetBearer("plane", "fixture-private-token"); err != nil {
		t.Fatal(err)
	}
	if err := registry.SetHeader("plane", "X-Workspace-Slug", "fixture-workspace"); err != nil {
		t.Fatal(err)
	}
	cfg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry := cfg.MCP.Servers["plane"]
	if entry.ID == "" || entry.AuthMode != "bearer" {
		t.Fatal("authentication metadata missing")
	}
	body, _ := os.ReadFile(store.Paths.Config)
	if strings.Contains(string(body), "fixture-private") || strings.Contains(string(body), "fixture-workspace") {
		t.Fatal("secret in public config")
	}
	info, err := os.Stat(store.Paths.Secrets)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private store permissions")
	}
	headers, err := registry.Headers(entry)
	if err != nil || headers.Get("Authorization") != "Bearer fixture-private-token" || headers.Get("X-Workspace-Slug") != "fixture-workspace" {
		t.Fatal("incorrect credential resolution")
	}
	if err := registry.SetBearer("plane", "replacement-fixture"); err != nil {
		t.Fatal(err)
	}
	cfg, _ = store.Load()
	if cfg.MCP.Servers["plane"].ID != entry.ID {
		t.Fatal("rotation changed identity")
	}
	changed := entry
	changed.URL = "https://other.invalid/mcp"
	if _, err := registry.Headers(changed); err == nil {
		t.Fatal("credential crossed endpoint")
	}
	if err := registry.Remove("plane"); err != nil {
		t.Fatal(err)
	}
	data, err := (secrets.Store{Path: store.Paths.Secrets}).Load()
	if err != nil || len(data.MCP) != 0 {
		t.Fatal("orphaned credential")
	}
}

func TestExternalMCPLegacyAndSanitizedChallenge(t *testing.T) {
	store := connStore(t.TempDir())
	cfg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="fixture-private-realm"`)
		http.Error(w, "fixture-private-error", 401)
	}))
	defer server.Close()
	cfg.MCP.Servers = map[string]config.MCPServer{"legacy": {URL: server.URL, Enabled: true, Kind: "external"}}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	registry := Registry{Store: store}
	headers, err := registry.Headers(cfg.MCP.Servers["legacy"])
	if err != nil || len(headers) != 0 {
		t.Fatal("legacy no-auth entry broken")
	}
	_, err = registry.Test(context.Background(), cfg.MCP.Servers["legacy"])
	if err == nil || !strings.Contains(err.Error(), "HTTP_STATUS=401 AUTH_CHALLENGE=bearer") || strings.Contains(err.Error(), "fixture-private") {
		t.Fatal("unsafe or missing challenge diagnostic")
	}
	if err := registry.SetHeader("legacy", "X-Workspace-Slug", "fixture"); err != nil {
		t.Fatal(err)
	}
	current, _ := store.Load()
	if current.MCP.Servers["legacy"].ID == "" || current.MCP.Servers["legacy"].AuthMode != "header" {
		t.Fatal("legacy migration missing")
	}
}

func TestExternalMCPAuthenticationRejectsUnsafeInput(t *testing.T) {
	registry := Registry{Store: connStore(t.TempDir())}
	if err := registry.Add("safe", config.MCPServer{URL: "https://example.invalid/mcp", Enabled: true, Kind: "external"}); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", "value\r\nInjected: yes", strings.Repeat("x", 8193)} {
		if registry.SetBearer("safe", token) == nil {
			t.Fatal("accepted invalid credential")
		}
	}
	for _, header := range []string{"Host", "Authorization", "Cookie", "Connection", "X-Bad\nName"} {
		if registry.SetHeader("safe", header, "fixture") == nil {
			t.Fatal("accepted unsafe header")
		}
	}
	if registry.SetBearer("missing", "fixture") == nil {
		t.Fatal("created phantom entry")
	}
	for _, name := range []string{"ivoai-memory", "ivoai-context", "ivoai-orchestrator"} {
		if registry.Add(name, config.MCPServer{URL: "https://example.invalid/mcp", Kind: "external"}) == nil {
			t.Fatal("external MCP shadowed control plane")
		}
	}
	if err := registry.Add("leak", config.MCPServer{URL: "https://example.invalid/mcp?token=fixture", Enabled: true, Kind: "external"}); err == nil {
		t.Fatal("accepted URL credential surface")
	}
}
