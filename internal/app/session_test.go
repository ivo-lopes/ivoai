package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestDirectSessionUsesExistingRuntimeWithoutRuflo(t *testing.T) {
	root := t.TempDir()
	rufloMarker := filepath.Join(root, "ruflo-called")
	codexArgs := filepath.Join(root, "codex-args")
	codex := appExecutable(t, root, "codex", "#!/bin/sh\nprintf '%s\\n' \"$@\" > '"+codexArgs+"'\n")
	ruflo := appExecutable(t, root, "ruflo", "#!/bin/sh\n: > '"+rufloMarker+"'\n")
	a := sessionTestApp(t, root, codex, appExecutable(t, root, "claude", "#!/bin/sh\nexit 0\n"), ruflo)
	previous, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(previous) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	if err := a.SessionStart(context.Background(), "codex", session.ModeDirect, []string{"--model", "argument-model", "prompt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rufloMarker); !os.IsNotExist(err) {
		t.Fatal("direct session invoked Ruflo")
	}
	values, err := a.SessionList()
	if err != nil || len(values) != 1 {
		t.Fatalf("sessions=%+v err=%v", values, err)
	}
	if values[0].Mode != session.ModeDirect || values[0].State != session.StateCompleted || values[0].PrimaryModel.Source != session.ModelArgument || values[0].PrimaryPID <= 0 {
		t.Fatalf("session=%+v", values[0])
	}
	body, _ := os.ReadFile(codexArgs)
	if string(body) != "--model\nargument-model\nprompt\n" {
		t.Fatalf("direct arguments changed: %q", body)
	}
}

func TestOrchestratedSessionInitializesSwarmBeforeOfficialPrimary(t *testing.T) {
	root := t.TempDir()
	rufloCalls := filepath.Join(root, "ruflo-calls")
	codexArgs := filepath.Join(root, "codex-args")
	codex := appExecutable(t, root, "codex", "#!/bin/sh\nprintf '%s\\n' \"$@\" > '"+codexArgs+"'\n")
	ruflo := appExecutable(t, root, "ruflo", `#!/bin/sh
printf '%s\n' "$*" >> "`+rufloCalls+`"
case "$*" in
  "--version") echo 'ruflo v3.38.12' ;;
  "swarm init"*) echo 'Swarm ID: swarm-smoke-123' ;;
  "swarm status") echo 'swarm-smoke-123 active' ;;
  "task create"*) echo 'task-smoke-123' ;;
esac
`)
	a := sessionTestApp(t, root, codex, appExecutable(t, root, "claude", "#!/bin/sh\nexit 0\n"), ruflo)
	t.Setenv("IVOAI_TEST_MODE", "1")
	state, _ := a.Store.LoadState()
	if err := a.orchestrationManager(state).Configure(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	previous, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(previous) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	if err := a.SessionStart(context.Background(), "codex", session.ModeOrchestrated, nil); err != nil {
		t.Fatal(err)
	}
	values, err := a.SessionList()
	if err != nil || len(values) != 1 {
		t.Fatalf("sessions=%+v err=%v", values, err)
	}
	value := values[0]
	if value.Mode != session.ModeOrchestrated || value.SwarmID != "swarm-smoke-123" || value.PrimaryRufloTaskID != "task-smoke-123" || !value.RufloHealthy || !value.RufloSafeMode || value.ProviderExecution || value.State != session.StateCompleted {
		t.Fatalf("session=%+v", value)
	}
	calls, _ := os.ReadFile(rufloCalls)
	callText := string(calls)
	if strings.Index(callText, "swarm init") < 0 || strings.Index(callText, "task create") < strings.Index(callText, "swarm init") {
		t.Fatalf("Ruflo lifecycle order invalid:\n%s", calls)
	}
	args, _ := os.ReadFile(codexArgs)
	if !strings.Contains(string(args), "mcp_servers.ivoai-orchestrator.command=") || !strings.Contains(string(args), "_orchestrator-serve") {
		t.Fatalf("local bridge not attached to primary: %q", args)
	}
	if _, err := os.Stat(filepath.Join(a.Store.Paths.SessionsDir, "runtime", value.SessionID)); !os.IsNotExist(err) {
		t.Fatalf("session runtime was not cleaned: %v", err)
	}
}

func sessionTestApp(t *testing.T, root, codex, claude, ruflo string) *App {
	t.Helper()
	paths := config.Paths{
		ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), BinDir: filepath.Join(root, "data", "bin"),
		Config: filepath.Join(root, "config", "config.toml"), State: filepath.Join(root, "state", "state.toml"), Secrets: filepath.Join(root, "config", "secrets.json"), Ownership: filepath.Join(root, "state", "ownership.toml"), HooksDir: filepath.Join(root, "data", "hooks"), SessionsDir: filepath.Join(root, "state", "sessions"),
	}
	store := config.NewStore(paths)
	cfg := config.Default()
	cfg.Headroom.Enabled = false
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	components := map[string]config.ComponentState{
		"codex": {Installed: true, Path: codex, Version: "fixture"}, "claude-code": {Installed: true, Path: claude, Version: "fixture"}, "ruflo": {Installed: true, Path: ruflo, Version: "fixture"},
	}
	if err := store.SaveState(config.State{Schema: config.SchemaVersion, Components: components}); err != nil {
		t.Fatal(err)
	}
	return &App{Version: "test", Store: store, Runner: platform.ExecRunner{}, In: bytes.NewBuffer(nil), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
}

func appExecutable(t *testing.T, directory, name, body string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
