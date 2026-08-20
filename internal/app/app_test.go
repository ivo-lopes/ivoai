package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/components"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/connections"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/secrets"
)

type missingRunner struct{}

func (missingRunner) LookPath(string) (string, error) { return "", errors.New("not found") }
func (missingRunner) Run(context.Context, string, []string, platform.RunOptions) (platform.Result, error) {
	return platform.Result{}, errors.New("not found")
}

func TestSetupIsIdempotentAndReadyWithoutConnections(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("IVOAI_TEST_MODE", "1")
	var out bytes.Buffer
	a, err := New("v0.1.0-test", strings.NewReader(""), &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	a.Runner = missingRunner{}
	if err := a.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(a.Store.Paths.Ownership)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(a.Store.Paths.Ownership)
	if string(first) != string(second) {
		t.Fatal("ownership changed across setup")
	}
	out.Reset()
	if err := a.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := out.String()
	for _, expected := range []string{"Overall: READY", "Codex", "installed / not connected", "Ruflo", "provider execution disabled"} {
		if !strings.Contains(status, expected) {
			t.Fatalf("missing %q in:\n%s", expected, status)
		}
	}
	report := a.Doctor(context.Background())
	if report.Overall != "READY" || !report.TestMode {
		t.Fatalf("unexpected doctor: %#v", report)
	}
	if report.SecretPermissions != "0600" {
		t.Fatalf("secret mode %s", report.SecretPermissions)
	}
}

func TestConfigSetOnlyAllowsSafeKeys(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	a, _ := New("dev", strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	_ = a.Store.Save(aConfig())
	if err := a.ConfigSet("headroom.enabled", "false"); err != nil {
		t.Fatal(err)
	}
	c, _ := a.Store.Load()
	if c.Headroom.Enabled {
		t.Fatal("not disabled")
	}
	if err := a.ConfigSet("connections.server.url", "http://evil"); err == nil {
		t.Fatal("unsafe arbitrary config key accepted")
	}
}

func TestStatusReportsDegradedWhenRequiredComponentIsMissing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("IVOAI_TEST_MODE", "1")
	var output bytes.Buffer
	a, err := New("v0.1.0-test", strings.NewReader(""), &output, &output)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err := a.Store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	state.Components["headroom"] = config.ComponentState{}
	if err := a.Store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := a.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Overall: DEGRADED") {
		t.Fatalf("missing component was reported ready:\n%s", output.String())
	}
}

func TestUninstallRemovesOnlyRecordedIvoaiAssets(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	var output bytes.Buffer
	a, err := New("v0.1.0-test", strings.NewReader(""), &output, &output)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Store.Ensure(); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(binDir, "ivoai")
	launcher := filepath.Join(binDir, "ivoai-launcher")
	preexisting := filepath.Join(binDir, "codex")
	preexistingMemory := filepath.Join(binDir, "ai-memory")
	if err := os.WriteFile(executable, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(executable, launcher); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(preexisting, []byte("keep"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(preexistingMemory, []byte("keep memory"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.SaveOwnership(config.Ownership{Schema: config.SchemaVersion, Components: map[string]config.OwnedItem{
		"ivoai": {Managed: true, Path: executable, Launchers: []string{launcher}},
		"codex": {Managed: false, Path: preexisting},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.SaveState(config.State{Schema: config.SchemaVersion, Components: map[string]config.ComponentState{
		"ai-memory": {Installed: true, Managed: false, Path: preexistingMemory},
	}}); err != nil {
		t.Fatal(err)
	}
	runner := &setupRunner{}
	a.Runner = runner
	if err := a.Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{executable, launcher, a.Store.Paths.ConfigDir, a.Store.Paths.DataDir, a.Store.Paths.StateDir, a.Store.Paths.CacheDir} {
		if _, err := os.Lstat(removed); !os.IsNotExist(err) {
			t.Errorf("managed path still exists: %s", removed)
		}
	}
	if _, err := os.Stat(preexisting); err != nil {
		t.Fatalf("pre-existing tool was removed: %v", err)
	}
	if _, err := os.Stat(preexistingMemory); err != nil {
		t.Fatalf("pre-existing ai-memory executable was removed: %v", err)
	}
	foundCleanup := false
	for _, call := range runner.calls {
		foundCleanup = foundCleanup || strings.Join(call, " ") == preexistingMemory+" uninstall --apply"
	}
	if !foundCleanup {
		t.Fatal("ivoai-managed ai-memory integration was not removed")
	}
}

func TestUninstallRefusesReplacedLauncher(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	a, err := New("v0.1.0-test", strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Store.Ensure(); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "ivoai")
	launcher := filepath.Join(root, "launcher")
	if err := os.WriteFile(executable, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcher, []byte("user replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.SaveOwnership(config.Ownership{Schema: config.SchemaVersion, Components: map[string]config.OwnedItem{
		"ivoai": {Managed: true, Path: executable, Launchers: []string{launcher}},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Uninstall(context.Background()); err == nil {
		t.Fatal("replaced launcher was removed")
	}
	if _, err := os.Stat(executable); err != nil {
		t.Fatal("owned executable was removed after launcher validation failed")
	}
}

func aConfig() (c config.Config) { return config.Default() }

type setupRunner struct {
	calls     [][]string
	envs      [][]string
	failHooks bool
}

func (r *setupRunner) LookPath(string) (string, error) { return "", errors.New("not found") }
func (r *setupRunner) Run(_ context.Context, command string, args []string, options platform.RunOptions) (platform.Result, error) {
	r.calls = append(r.calls, append([]string{command}, args...))
	r.envs = append(r.envs, append([]string(nil), options.Env...))
	if r.failHooks && len(args) > 0 && args[0] == "install-hooks" {
		return platform.Result{}, errors.New("offline hook fixture")
	}
	if len(args) == 1 && args[0] == "--version" {
		return platform.Result{Stdout: "fixture version"}, nil
	}
	return platform.Result{}, nil
}

func managedSetupApp(t *testing.T, memoryEnabled bool, runner *setupRunner) (*App, *bytes.Buffer) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("IVOAI_TEST_MODE", "")
	output := &bytes.Buffer{}
	a, err := New("v0.1.0-test", strings.NewReader(""), output, output)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Store.Ensure(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Memory.Enabled = memoryEnabled
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	state, err := a.Store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(a.Store.Paths.BinDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, spec := range components.DefaultCatalog() {
		path := filepath.Join(a.Store.Paths.BinDir, spec.Executable)
		if err := os.WriteFile(path, []byte("fixture"), 0o700); err != nil {
			t.Fatal(err)
		}
		state.Components[spec.Name] = config.ComponentState{Installed: true, Managed: true, Version: spec.Version, Path: path}
	}
	if err := a.Store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	a.Runner = runner
	return a, output
}

func TestSetupCompletesWhenMemoryHooksFail(t *testing.T) {
	runner := &setupRunner{failHooks: true}
	a, output := managedSetupApp(t, true, runner)
	if err := a.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, _ := a.Store.LoadState()
	if state.SetupCompletedAt.IsZero() {
		t.Fatal("setup completion was not persisted")
	}
	if !strings.Contains(output.String(), "ai-memory hooks are degraded") || !strings.Contains(output.String(), "client setup complete") {
		t.Fatalf("missing degraded-but-ready diagnostics:\n%s", output.String())
	}
}

func TestSetupRespectsDisabledMemory(t *testing.T) {
	runner := &setupRunner{}
	a, _ := managedSetupApp(t, false, runner)
	if err := a.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, call := range runner.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "install-mcp") || strings.Contains(joined, "install-hooks") {
			t.Fatalf("memory was configured while disabled: %s", joined)
		}
	}
}

func TestDisabledMemoryIsExcludedFromAgentMCPRegistration(t *testing.T) {
	cfg := config.Default()
	cfg.Memory.Enabled = false
	cfg.MCP.Servers["ivoai-context"] = config.MCPServer{URL: "https://ai.example.com/context", Enabled: true, Kind: "context"}
	cfg.MCP.Servers["ivoai-memory"] = config.MCPServer{URL: "https://ai.example.com/memory", Enabled: true, Kind: "memory"}
	servers := enabledServerMCPs(cfg)
	if _, exists := servers["ivoai-memory"]; exists {
		t.Fatal("disabled memory MCP remained enabled")
	}
	if _, exists := servers["ivoai-context"]; !exists {
		t.Fatal("context MCP was incorrectly removed")
	}
}

func TestLaunchInjectsScopedServerTokenAndRestoresEnvironment(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv(connections.ServerTokenEnvironment, "pre-existing")
	var output bytes.Buffer
	a, err := New("test", strings.NewReader(""), &output, &output)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Headroom.Enabled = false
	cfg.Connections.Server.Status = "connected"
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	agent := filepath.Join(root, "codex-fixture")
	if err := os.WriteFile(agent, []byte("#!/bin/sh\nprintf '%s' \"$"+connections.ServerTokenEnvironment+"\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	state, _ := a.Store.LoadState()
	state.Components["codex"] = config.ComponentState{Installed: true, Path: agent}
	if err := a.Store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.Store{Path: a.Store.Paths.Secrets}
	if err := secretStore.Save(secrets.Data{Server: &secrets.ClientCredential{Token: "scoped-secret", ClientID: "client"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Launch(context.Background(), "codex", nil); err != nil {
		t.Fatal(err)
	}
	if output.String() != "scoped-secret" {
		t.Fatalf("agent did not receive scoped credential: %q", output.String())
	}
	if got := os.Getenv(connections.ServerTokenEnvironment); got != "pre-existing" {
		t.Fatalf("parent environment was not restored: %q", got)
	}
}

func TestConnectServerUsesDiscoveredMCPAndHooksEndpoints(t *testing.T) {
	runner := &setupRunner{}
	a, _ := managedSetupApp(t, true, runner)
	if err := a.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.calls, runner.envs = nil, nil
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/ivoai":
			json.NewEncoder(w).Encode(connections.Discovery{ProtocolVersion: connections.ProtocolVersion, HealthEndpoint: "/health", ReadyEndpoint: "/ready", ContextMCPEndpoint: "/v1/context/mcp", MemoryMCPEndpoint: "/v1/memory/mcp", MemoryHooksEndpoint: "/v1/memory", EnrollmentEndpoint: "/enroll", Features: map[string]bool{"context": true, "memory": true}})
		case "/health":
			json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
		case "/ready":
			json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
		case "/enroll":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"token": "server-scoped-token", "client_id": "client-1", "scopes": []string{"context:read", "memory:read", "memory:write"}})
		case "/v1/context/mcp", "/v1/memory/mcp":
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"tools": []any{}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	if err := a.ConnectServer(context.Background(), server.URL, "one-time-code"); err != nil {
		t.Fatal(err)
	}
	var commands, environments []string
	for index, call := range runner.calls {
		commands = append(commands, strings.Join(call, " "))
		if index < len(runner.envs) {
			environments = append(environments, strings.Join(runner.envs[index], " "))
		}
	}
	allCommands := strings.Join(commands, "\n")
	allEnvironment := strings.Join(environments, "\n")
	for _, endpoint := range []string{server.URL + "/v1/context/mcp", server.URL + "/v1/memory/mcp"} {
		if !strings.Contains(allCommands, endpoint) {
			t.Fatalf("discovered endpoint %q not registered:\n%s", endpoint, allCommands)
		}
	}
	if !strings.Contains(allEnvironment, "AI_MEMORY_SERVER_URL="+server.URL+"/v1/memory") {
		t.Fatalf("hook base endpoint not configured:\n%s", allEnvironment)
	}
	if strings.Contains(allCommands, "server-scoped-token") {
		t.Fatalf("server token leaked into command argv:\n%s", allCommands)
	}
	runner.calls, runner.envs = nil, nil
	if err := a.DisconnectServer(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg, _ := a.Store.Load()
	secretData, _ := (secrets.Store{Path: a.Store.Paths.Secrets}).Load()
	if cfg.Connections.Server.Status != "not-connected" || secretData.Server != nil || len(cfg.MCP.Servers) != 0 {
		t.Fatalf("server state retained after disconnect: %#v %#v", cfg.Connections.Server, secretData.Server)
	}
	commands, environments = nil, nil
	for index, call := range runner.calls {
		commands = append(commands, strings.Join(call, " "))
		if index < len(runner.envs) {
			environments = append(environments, strings.Join(runner.envs[index], " "))
		}
	}
	allCommands = strings.Join(commands, "\n")
	allEnvironment = strings.Join(environments, "\n")
	if !strings.Contains(allCommands, "uninstall --apply") || !strings.Contains(allCommands, "mcp remove ivoai-context") {
		t.Fatalf("managed integration cleanup missing:\n%s", allCommands)
	}
	if strings.Contains(allCommands+allEnvironment, "server-scoped-token") || strings.Contains(allEnvironment, server.URL) {
		t.Fatalf("stale remote credential or endpoint retained:\n%s\n%s", allCommands, allEnvironment)
	}
}
