package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyServerNormalizesToDefaultProfile(t *testing.T) {
	root := t.TempDir()
	paths := testPaths(root)
	store := NewStore(paths)
	legacy := `[ivoai]
version = 1
[connections.server]
status = "connected"
url = "https://example.invalid"
protocol = 1
[mcp.servers.ivoai-context]
url = "https://example.invalid/mcp/context"
enabled = true
kind = "context"
`
	if err := os.MkdirAll(filepath.Dir(paths.Config), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Config, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	profile := value.Connections.Servers["default"]
	if profile.ID != LegacyServerID || profile.URL != "https://example.invalid" || profile.ContextMCPURL == "" {
		t.Fatalf("profile=%+v", profile)
	}
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.Load()
	if err != nil || len(reloaded.Connections.Servers) != 1 || reloaded.Connections.Server.Status != "connected" {
		t.Fatalf("round trip=%+v err=%v", reloaded.Connections, err)
	}
}
