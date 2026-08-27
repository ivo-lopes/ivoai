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
	"time"

	"github.com/ivo-lopes/ivoai/internal/components"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/connections"
	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/secrets"
	"github.com/ivo-lopes/ivoai/internal/terminalui"
)

type missingRunner struct{}

func (missingRunner) LookPath(string) (string, error) { return "", errors.New("not found") }
func (missingRunner) Run(context.Context, string, []string, platform.RunOptions) (platform.Result, error) {
	return platform.Result{}, errors.New("not found")
}

type authBoundaryRunner struct{ paths []string }

func (r *authBoundaryRunner) LookPath(string) (string, error) {
	return "", errors.New("managed path must be used")
}
func (r *authBoundaryRunner) Run(_ context.Context, path string, _ []string, _ platform.RunOptions) (platform.Result, error) {
	r.paths = append(r.paths, path)
	return platform.Result{Stdout: `{"authenticated":true}`}, nil
}

func TestConnectAndDisconnectAreQuotaAuthenticationBoundaries(t *testing.T) {
	root := t.TempDir()
	a := autoTestApp(t, root, "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	state, err := a.Store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	managedCodex := state.Components["codex"].Path
	runner := &authBoundaryRunner{}
	a.Runner = runner
	now := time.Now().UTC()
	quotaStore := quota.Store{Root: a.Store.Paths.QuotaDir}
	if err := quotaStore.Put(quota.ProviderQuota{Provider: quota.ProviderCodex, Authenticated: true, HardLimitReached: true, Reason: "old account exhausted", ObservedAt: now}); err != nil {
		t.Fatal(err)
	}
	a.QuotaManager = &quota.Manager{Store: quotaStore, Probes: map[quota.Provider]quota.Probe{quota.ProviderCodex: probeFunc(func(context.Context) (quota.ProviderQuota, error) {
		return quota.ProviderQuota{Provider: quota.ProviderCodex, Authenticated: true, Windows: []quota.Window{quota.FromUsedDuration(quota.KindRolling, 300, 20, nil, "fixture", now)}, ObservedAt: now}, nil
	})}}
	if err := a.ConnectAgent(context.Background(), "chatgpt"); err != nil {
		t.Fatal(err)
	}
	if len(runner.paths) == 0 || runner.paths[0] != managedCodex {
		t.Fatalf("managed Codex path not used: got=%v want=%q", runner.paths, managedCodex)
	}
	snapshot, err := quotaStore.Load()
	if err != nil || !snapshot.Providers[quota.ProviderCodex].Eligible || snapshot.Providers[quota.ProviderCodex].HardLimitReached {
		t.Fatalf("connect retained old account quota: %+v err=%v", snapshot, err)
	}
	if err := a.DisconnectAgent(context.Background(), "chatgpt"); err != nil {
		t.Fatal(err)
	}
	snapshot, err = quotaStore.Load()
	if err != nil || len(snapshot.Providers) != 0 {
		t.Fatalf("disconnect retained quota: %+v err=%v", snapshot, err)
	}
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

func TestStatusAndDoctorAgreeWhenConfiguredServerIsUnreachable(t *testing.T) {
	runner := &setupRunner{}
	a, output := managedSetupApp(t, true, runner)
	if err := a.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/ivoai":
			_ = json.NewEncoder(w).Encode(connections.Discovery{ProtocolVersion: connections.ProtocolVersion, HealthEndpoint: "/health", ReadyEndpoint: "/ready"})
		case "/health":
			w.WriteHeader(http.StatusNoContent)
		case "/ready":
			http.Error(w, "not ready", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	cfg, err := a.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Connections.ChatGPT.Status = "connected"
	cfg.Connections.Claude.Status = "connected"
	cfg.Connections.Server = config.Connection{Status: "connected", URL: server.URL, Protocol: connections.ProtocolVersion}
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	a.HTTPClient = server.Client()
	output.Reset()
	if err := a.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Server") || !strings.Contains(output.String(), "unreachable") || !strings.Contains(output.String(), "Overall: DEGRADED") || strings.Contains(output.String(), "all connections active") {
		t.Fatalf("untruthful status:\n%s", output.String())
	}
	report := a.Doctor(context.Background())
	if report.Overall != "DEGRADED" || !report.Server.Configured || report.Server.Reachable {
		t.Fatalf("status/doctor disagreement: %+v", report.Server)
	}
}

func TestStatusDoesNotDeclareReachableServerDownDuringSlowDNSClassLatency(t *testing.T) {
	runner := &setupRunner{}
	a, output := managedSetupApp(t, true, runner)
	if err := a.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/ivoai" {
			time.Sleep(2100 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(connections.Discovery{ProtocolVersion: connections.ProtocolVersion, HealthEndpoint: "/health", ReadyEndpoint: "/ready"})
			return
		}
		if r.URL.Path == "/health" || r.URL.Path == "/ready" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	cfg, err := a.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Connections.Server = config.Connection{Status: "connected", URL: server.URL, Protocol: connections.ProtocolVersion}
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	a.HTTPClient = server.Client()
	output.Reset()
	if err := a.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "reachable / compatible") {
		t.Fatalf("healthy server was falsely reported unreachable:\n%s", output.String())
	}
}

func TestSemanticStatusKindsDoNotInferMeaningFromDisabled(t *testing.T) {
	ruflo := terminalui.Semantic("ready / provider execution disabled", terminalui.StatusSuccess, true)
	headroom := terminalui.Semantic("installed / disabled", terminalui.StatusNeutral, true)
	missing := terminalui.Semantic("not installed", terminalui.StatusFailure, true)
	if !strings.Contains(ruflo, "\x1b[38;5;77m") {
		t.Fatalf("safe Ruflo state is not green: %q", ruflo)
	}
	if strings.Contains(headroom, "\x1b[38;5;77m") || !strings.Contains(headroom, "\x1b[38;5;245m") {
		t.Fatalf("disabled Headroom inherited success semantics: %q", headroom)
	}
	if !strings.Contains(missing, "\x1b[38;5;203m") {
		t.Fatalf("missing component is not red: %q", missing)
	}
	t.Setenv("NO_COLOR", "1")
	if plain := terminalui.Semantic("ready", terminalui.StatusSuccess, false); strings.Contains(plain, "\x1b[") {
		t.Fatalf("NO_COLOR output contains ANSI: %q", plain)
	}
}

func TestRufloStatusRequiresVerifiedSafeProfile(t *testing.T) {
	safe := safeStatus(orchestration.Status{Installed: true, SafeMode: true})
	unsafe := safeStatus(orchestration.Status{Installed: true, ProviderExecution: true})
	unverified := safeStatus(orchestration.Status{Installed: true})
	if safe.Kind != terminalui.StatusSuccess || safe.Text != "ready / provider execution disabled" {
		t.Fatalf("safe Ruflo status=%+v", safe)
	}
	if unsafe.Kind != terminalui.StatusFailure || !strings.Contains(unsafe.Text, "unsafe") {
		t.Fatalf("unsafe Ruflo status=%+v", unsafe)
	}
	if unverified.Kind != terminalui.StatusWarning || !strings.Contains(unverified.Text, "not verified") {
		t.Fatalf("unverified Ruflo status=%+v", unverified)
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

func TestLaunchInjectsServerTokenOnlyIntoChildEnvironment(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv(connections.ServerTokenEnvironment, "pre-existing")
	t.Setenv("AI_MEMORY_AUTH_TOKEN", "pre-existing-memory")
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
	if err := os.WriteFile(agent, []byte("#!/bin/sh\nprintf '%s/%s' \"$"+connections.ServerTokenEnvironment+"\" \"$AI_MEMORY_AUTH_TOKEN\"\n"), 0o700); err != nil {
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
	if output.String() != "scoped-secret/scoped-secret" {
		t.Fatalf("agent did not receive scoped credential: %q", output.String())
	}
	if got := os.Getenv(connections.ServerTokenEnvironment); got != "pre-existing" {
		t.Fatalf("parent environment was modified: %q", got)
	}
	if got := os.Getenv("AI_MEMORY_AUTH_TOKEN"); got != "pre-existing-memory" {
		t.Fatal("ai-memory token environment was not restored")
	}
}

func TestLaunchBypassesHeadroomForExactSharedKnowledge(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	var output bytes.Buffer
	a, err := New("test", strings.NewReader(""), &output, &output)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.MCP.Servers["ivoai-memory"] = config.MCPServer{Enabled: true, Kind: "memory"}
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	agentMarker := filepath.Join(root, "agent-launched")
	headroomMarker := filepath.Join(root, "headroom-launched")
	agent := appExecutable(t, root, "codex", "#!/bin/sh\nprintf direct > '"+agentMarker+"'\n")
	headroom := appExecutable(t, root, "headroom", "#!/bin/sh\nprintf wrapped > '"+headroomMarker+"'\nexit 2\n")
	state, _ := a.Store.LoadState()
	state.Components["codex"] = config.ComponentState{Installed: true, Path: agent}
	state.Components["headroom"] = config.ComponentState{Installed: true, Path: headroom}
	if err := a.Store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	if err := a.Launch(context.Background(), "codex", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(agentMarker); err != nil {
		t.Fatalf("official client was not launched directly: %v", err)
	}
	if _, err := os.Stat(headroomMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Headroom was invoked despite exact shared knowledge: %v", err)
	}
	if !strings.Contains(output.String(), "Headroom bypassed") {
		t.Fatalf("bypass was not reported: %q", output.String())
	}
}

func TestManagedCodexWithoutCodeModeHostRefusesToollessLaunch(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	var output bytes.Buffer
	a, err := New("test", strings.NewReader(""), &output, &output)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Headroom.Enabled = false
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(root, "args")
	agent := filepath.Join(root, "codex")
	if err := os.WriteFile(agent, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > '"+argsFile+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	state, _ := a.Store.LoadState()
	state.Components["codex"] = config.ComponentState{Installed: true, Managed: true, Path: agent}
	if err := a.Store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	if err := a.Launch(context.Background(), "codex", []string{"original-prompt"}); err == nil || !strings.Contains(err.Error(), "run ivoai setup") {
		t.Fatalf("toolless managed Codex launch error=%v", err)
	}
	if _, err := os.Stat(argsFile); !os.IsNotExist(err) {
		t.Fatal("Codex launched without its required tool host")
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
