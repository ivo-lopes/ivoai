package webauth

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScopedStoreIsolatedPolicyAndDefaultCompatibility(t *testing.T) {
	defaults, supported := []string{"admin:read"}, []string{"admin:read"}
	admin, err := NewScopedStore(filepath.Join(t.TempDir(), "admin.json"), defaults, supported)
	if err != nil {
		t.Fatal(err)
	}
	defaults[0] = "memory:delete"
	supported[0] = "memory:delete"
	if _, err := admin.CreateActivation(time.Minute, []string{ScopeMemoryRead}); err == nil {
		t.Fatal("admin accepted MCP scope")
	}
	if _, err := NewStore(filepath.Join(t.TempDir(), "mcp.json")).CreateActivation(time.Minute, []string{"admin:read"}); err == nil {
		t.Fatal("default scope policy expanded")
	}
	a, err := admin.CreateActivation(time.Minute, nil)
	if err != nil || len(a.Scopes) != 1 || a.Scopes[0] != "admin:read" {
		t.Fatal("policy aliased caller slice", err)
	}
	copy := admin.DefaultScopeNames()
	copy[0] = "memory:delete"
	if admin.DefaultScopeNames()[0] != "admin:read" {
		t.Fatal("policy exported mutable slice")
	}
	server := &Server{Store: admin, Issuer: "http://127.0.0.1:7781", ResourcePath: "/api/admin", CSRFCookieName: "admin_nonce"}
	w := httptest.NewRecorder()
	server.metadata(w, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(w.Body.String(), "admin:read") || strings.Contains(w.Body.String(), "memory:read") {
		t.Fatal("metadata leaked default namespace")
	}
	if (&Server{Issuer: "https://ivoai.test"}).Resource() != "https://ivoai.test/mcp" {
		t.Fatal("default resource changed")
	}
	for _, policy := range []struct{ defaults, supported []string }{{nil, nil}, {[]string{"a"}, []string{"b"}}, {[]string{"a"}, []string{"a", "a"}}, {[]string{"bad scope"}, []string{"bad scope"}}} {
		if _, err := NewScopedStore("unused", policy.defaults, policy.supported); err == nil {
			t.Fatal("invalid policy accepted")
		}
	}
}
