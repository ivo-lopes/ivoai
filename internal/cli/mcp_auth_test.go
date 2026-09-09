package cli

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/app"
)

func mcpTestApp(t *testing.T) (*app.App, *bytes.Buffer) {
	t.Helper()
	root := t.TempDir()
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, filepath.Join(root, key))
	}
	var out bytes.Buffer
	a, err := app.New("test", strings.NewReader(""), &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	return a, &out
}

func TestMCPAuthCLIPrivateStdinLifecycle(t *testing.T) {
	a, out := mcpTestApp(t)
	if err := runMCP(a, []string{"add", "external", "https://example.invalid/mcp"}); err != nil {
		t.Fatal(err)
	}
	a.In = strings.NewReader("fixture-private-pat\n")
	if err := runMCP(a, []string{"auth", "set", "external", "--bearer-token-stdin"}); err != nil {
		t.Fatal(err)
	}
	a.In = strings.NewReader("fixture-private-workspace\n")
	if err := runMCP(a, []string{"header", "set", "external", "X-Workspace-Slug", "--value-stdin"}); err != nil {
		t.Fatal(err)
	}
	if err := runMCP(a, []string{"list"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "auth=bearer credential=configured") || strings.Contains(out.String(), "fixture-private") {
		t.Fatal("unsafe or missing auth status")
	}
	body, _ := os.ReadFile(a.Store.Paths.Config)
	if strings.Contains(string(body), "fixture-private") {
		t.Fatal("secret in TOML")
	}
	if err := runMCP(a, []string{"auth", "remove", "external"}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := a.Store.Load()
	if cfg.MCP.Servers["external"].AuthMode != "none" {
		t.Fatal("auth removal failed")
	}
}

func TestMCPAuthMenuPrivateInput(t *testing.T) {
	a, out := mcpTestApp(t)
	if err := a.MCPAdd("external", "https://example.invalid/mcp"); err != nil {
		t.Fatal(err)
	}
	session := menuSession{ctx: context.Background(), app: a, reader: bufio.NewReader(strings.NewReader("bearer\nfixture-private-pat\nX-Workspace-Slug\nfixture-private-workspace\n"))}
	if err := session.mcpAuthFor("external"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "fixture-private") {
		t.Fatal("menu echoed private input")
	}
	if err := a.MCPList(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "auth=bearer credential=configured") {
		t.Fatal("menu did not save credential")
	}
}

func TestMCPSecretInputIsBounded(t *testing.T) {
	a, _ := mcpTestApp(t)
	for _, value := range []string{"", strings.Repeat("x", 8195)} {
		a.In = strings.NewReader(value)
		if _, err := readMCPSecret(a); err == nil {
			t.Fatal("unbounded or empty credential accepted")
		}
	}
}
