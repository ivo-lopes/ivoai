package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/secrets"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Runs real previous/candidate binaries against isolated fixtures. This verifies
// config rollback/reapply; the separate official updater smoke certifies delivery.
func TestLiveMCPArtifactConfigUpgrade(t *testing.T) {
	old, candidate := os.Getenv("IVOAI_MCP_UPGRADE_PREVIOUS_BINARY"), os.Getenv("IVOAI_NATIVE_SMOKE_BINARY")
	if old == "" || candidate == "" {
		t.Skip("requires previous and candidate/public artifacts")
	}
	root := t.TempDir()
	for _, kind := range []string{"CONFIG", "DATA", "STATE", "CACHE"} {
		t.Setenv("XDG_"+kind+"_HOME", filepath.Join(root, kind))
	}
	a, err := New("fixture", strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	for _, alias := range []string{"default", "company-b"} {
		id := "srv_" + strings.ReplaceAll(alias, "-", "_")
		cfg.Connections.Servers[alias] = config.ServerProfile{ID: id, Alias: alias, Purpose: alias, URL: "https://example.invalid", Enabled: true, Status: "connected"}
		if err := (secrets.Store{Path: a.Store.Paths.Secrets}).Set(id, secrets.ClientCredential{Token: "fixture-credential-" + alias}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	run := func(binary string, args ...string) {
		t.Helper()
		command := exec.Command(binary, args...)
		command.Dir = root
		if _, err := command.Output(); err != nil {
			t.Fatalf("artifact operation %s failed: %v", args[0], err)
		}
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	s.AddTool(&mcp.Tool{Name: "read_tool", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		t.Error("upgrade invoked a tool")
		return nil, nil
	})
	upstream := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, &mcp.StreamableHTTPOptions{JSONResponse: true}))
	defer upstream.Close()
	run(old, "connect", "mcp", "add", "company-a", upstream.URL)
	if err := a.MCPSetBearer("company-a", "fixture-upgrade-credential"); err != nil {
		t.Fatal(err)
	}
	before, _ := a.Store.Load()
	secretBefore, _ := os.ReadFile(a.Store.Paths.Secrets)
	run(candidate, "connect", "mcp", "test", "company-a")
	run(candidate, "connect", "mcp", "policy", "company-a", "read_only")
	run(candidate, "connect", "mcp", "direct-policy", "company-a", "disabled")
	run(old, "config", "set", "opencode.permission_mode", "full")
	run(old, "connect", "mcp", "list")
	run(candidate, "connect", "mcp", "tools", "company-a")
	after, err := a.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	entry := after.MCP.Servers["company-a"]
	if entry.ID != before.MCP.Servers["company-a"].ID || entry.AuthMode != "bearer" || entry.Policy != "read_only" || entry.DirectPolicy != "disabled" || len(entry.Tools) != 1 || entry.Health != "HEALTHY" {
		t.Fatal("upgrade/rollback/reapply lost MCP metadata")
	}
	if !reflect.DeepEqual(before.Connections.Servers, after.Connections.Servers) {
		t.Fatal("server profiles changed")
	}
	secretAfter, _ := os.ReadFile(a.Store.Paths.Secrets)
	if string(secretBefore) != string(secretAfter) {
		t.Fatal("credential store changed")
	}
	if info, err := os.Stat(a.Store.Paths.Secrets); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private store permissions changed")
	}
}
