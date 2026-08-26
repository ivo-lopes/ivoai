package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testPaths(root string) Paths {
	return Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), BinDir: filepath.Join(root, "bin"), Config: filepath.Join(root, "config", "config.toml"), State: filepath.Join(root, "state", "state.toml"), Secrets: filepath.Join(root, "config", "secrets.json"), Ownership: filepath.Join(root, "state", "ownership.toml"), HooksDir: filepath.Join(root, "data", "hooks")}
}

func TestStoresPreserveUnknownFieldsAndRemoveKnownDynamicEntries(t *testing.T) {
	store := NewStore(testPaths(t.TempDir()))
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	configDocument := `[ivoai]
version = 1
future = "preserve"
[client]
profile = "default"
[headroom]
enabled = true
[memory]
enabled = true
[orchestration]
enabled = true
provider_execution = false
[connections.chatgpt]
status = "not-connected"
[connections.claude]
status = "not-connected"
[connections.server]
status = "not-connected"
[mcp.servers.keep]
url = "https://example.invalid/mcp"
enabled = true
kind = "external"
[mcp.servers.remove]
url = "https://remove.invalid/mcp"
enabled = true
kind = "external"
[future]
nested = 42
`
	if err := os.WriteFile(store.Paths.Config, []byte(configDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	delete(value.MCP.Servers, "remove")
	keep := value.MCP.Servers["keep"]
	keep.HooksURL = ""
	value.MCP.Servers["keep"] = keep
	value.Connections.Server = Connection{Status: "not-connected"}
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(store.Paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	text := string(saved)
	for _, expected := range []string{`future = 'preserve'`, "[future]", "nested = 42", "[mcp.servers.keep]"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("unknown field was lost; missing %q in:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "mcp.servers.remove") {
		t.Fatalf("removed known MCP entry was resurrected:\n%s", text)
	}
	if strings.Contains(text, "hooks_url") || strings.Contains(text, "connections.server.url") || strings.Contains(text, "protocol =") {
		t.Fatalf("cleared known optional field was resurrected:\n%s", text)
	}

	if err := os.WriteFile(store.Paths.State, []byte("schema=1\nfuture='state'\n[components.codex]\ninstalled=true\nmanaged=true\npath='/tmp/codex'\nextra='keep'\n[components.removed]\ninstalled=true\nmanaged=false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	delete(state.Components, "removed")
	codexState := state.Components["codex"]
	codexState.Version = ""
	codexState.Path = ""
	state.Components["codex"] = codexState
	if err := store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	saved, _ = os.ReadFile(store.Paths.State)
	if !strings.Contains(string(saved), "future = 'state'") || !strings.Contains(string(saved), "extra = 'keep'") || strings.Contains(string(saved), "components.removed") || strings.Contains(string(saved), "path =") || strings.Contains(string(saved), "version =") {
		t.Fatalf("state unknown-field policy failed:\n%s", saved)
	}

	if err := os.WriteFile(store.Paths.Ownership, []byte("schema=1\nfuture='ownership'\n[components.codex]\nmanaged=false\npath='/usr/bin/codex'\nextra='keep'\n[components.removed]\nmanaged=true\npath='/tmp/remove'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ownership, err := store.LoadOwnership()
	if err != nil {
		t.Fatal(err)
	}
	delete(ownership.Components, "removed")
	codexOwnership := ownership.Components["codex"]
	codexOwnership.Path = ""
	codexOwnership.Launchers = nil
	ownership.Components["codex"] = codexOwnership
	if err := store.SaveOwnership(ownership); err != nil {
		t.Fatal(err)
	}
	saved, _ = os.ReadFile(store.Paths.Ownership)
	if !strings.Contains(string(saved), "future = 'ownership'") || !strings.Contains(string(saved), "extra = 'keep'") || strings.Contains(string(saved), "components.removed") || strings.Contains(string(saved), "path =") || strings.Contains(string(saved), "launchers =") {
		t.Fatalf("ownership unknown-field policy failed:\n%s", saved)
	}
}

func TestLoadStateRejectsUnknownSchemaAndSymlink(t *testing.T) {
	store := NewStore(testPaths(t.TempDir()))
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Paths.State, []byte("schema=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadState(); err == nil || !strings.Contains(err.Error(), "unsupported state schema") {
		t.Fatalf("future state schema accepted: %v", err)
	}
	if err := os.Remove(store.Paths.State); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "state.toml")
	if err := os.WriteFile(target, []byte("schema=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, store.Paths.State); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadState(); err == nil {
		t.Fatal("state symlink was followed")
	}
}

func TestUpdateProjectionsAcceptSourceSchemaWithoutWeakeningNormalLoads(t *testing.T) {
	root := t.TempDir()
	store := NewStore(testPaths(root))
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Paths.State, []byte("schema=2\n[components.tool]\ninstalled=true\nmanaged=true\npath='"+filepath.Join(root, "data", "tool")+"'\nfuture='preserved'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Paths.Ownership, []byte("schema=2\n[components.tool]\nmanaged=true\npath='"+filepath.Join(root, "data", "tool")+"'\nfuture='preserved'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := store.LoadStateForUpdate()
	if err != nil || state.Schema != 2 || !state.Components["tool"].Managed {
		t.Fatalf("state update projection=%+v err=%v", state, err)
	}
	ownership, err := store.LoadOwnershipForUpdate()
	if err != nil || ownership.Schema != 2 || !ownership.Components["tool"].Managed {
		t.Fatalf("ownership update projection=%+v err=%v", ownership, err)
	}
	if _, err := store.LoadState(); err == nil || !strings.Contains(err.Error(), "unsupported state schema 2") {
		t.Fatalf("normal state load accepted a future schema: %v", err)
	}
	if _, err := store.LoadOwnership(); err == nil || !strings.Contains(err.Error(), "unsupported ownership schema 2") {
		t.Fatalf("normal ownership load accepted a future schema: %v", err)
	}
}

func TestInspectSchemasRejectsMissingOrInvalidEnvelopes(t *testing.T) {
	root := t.TempDir()
	store := NewStore(testPaths(root))
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Paths.State, []byte("schema=0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InspectSchemas(); err == nil || !strings.Contains(err.Error(), "state schema is invalid") {
		t.Fatalf("invalid schema envelope accepted: %v", err)
	}
}

func TestStoreRoundTripAndPermissions(t *testing.T) {
	store := NewStore(testPaths(t.TempDir()))
	c := Default()
	c.Client.Profile = "work"
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Client.Profile != "work" || got.Connections.ChatGPT.Status != "not-connected" {
		t.Fatalf("bad config: %#v", got)
	}
	info, _ := os.Stat(store.Paths.Config)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	first, err := os.ReadFile(store.Paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(got); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(store.Paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("semantic no-op save changed config bytes")
	}
}

func TestResolvePathsHonorsAbsoluteXDGOnly(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xc"))
	t.Setenv("XDG_DATA_HOME", "relative")
	p, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	if p.ConfigDir != filepath.Join(root, "xc", "ivoai") {
		t.Fatalf("config %s", p.ConfigDir)
	}
	if p.DataDir != filepath.Join(root, ".local", "share", "ivoai") {
		t.Fatalf("data %s", p.DataDir)
	}
}

func TestOrchestrationConfigurationMigratesAndRejectsUnsafeValues(t *testing.T) {
	root := t.TempDir()
	paths := testPaths(root)
	store := NewStore(paths)
	if err := os.MkdirAll(paths.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := []byte("[ivoai]\nversion=1\n[orchestration]\nenabled=true\nprovider_execution=false\n")
	if err := os.WriteFile(paths.Config, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if value.Orchestration.DefaultMode != "direct" || value.Orchestration.PrimaryExecutor != "codex" || value.Orchestration.ReviewExecutor != "claude" || value.Orchestration.MaxWorkers != 2 || !value.Orchestration.Auto.Enabled || value.Orchestration.Auto.DefaultPlanner != "codex" || !value.Orchestration.Auto.Quota.Enabled || value.Orchestration.Auto.QuotaRefreshSeconds != 45 {
		t.Fatalf("legacy migration=%+v", value.Orchestration)
	}
	value.Orchestration.ProviderExecution = true
	if err := store.Save(value); err == nil {
		t.Fatal("provider execution was persisted")
	}
	value.Orchestration.ProviderExecution = false
	value.Orchestration.MaxWorkers = 4
	if err := store.Save(value); err == nil {
		t.Fatal("worker limit above hard bound was persisted")
	}
}
