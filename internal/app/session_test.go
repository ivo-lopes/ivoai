package app

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/observability"
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
	arguments := string(body)
	if !strings.Contains(arguments, "developer_instructions=") || !strings.Contains(arguments, "memory_query") || !strings.HasSuffix(arguments, "--model\nargument-model\nprompt\n") {
		t.Fatalf("direct session did not preserve arguments and attach shared-memory guidance: %q", body)
	}
}

func TestOpenCodeDirectSessionUsesSkillGateAndRejectsOrchestration(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "opencode-instructions")
	opencode := appExecutable(t, root, "opencode", "#!/bin/sh\npath=$(printf '%s' \"$OPENCODE_CONFIG_CONTENT\" | sed 's/.*\\[\"\\([^\"]*\\)\"\\].*/\\1/')\ncp \"$path\" '"+marker+"'\n")
	a := sessionTestApp(t, root, appExecutable(t, root, "codex", "#!/bin/sh\nexit 0\n"), appExecutable(t, root, "claude", "#!/bin/sh\nexit 0\n"), appExecutable(t, root, "ruflo", "#!/bin/sh\nexit 0\n"))
	state, _ := a.Store.LoadState()
	state.Components["opencode"] = config.ComponentState{Installed: true, Managed: true, Version: "fixture", Path: opencode}
	if err := a.Store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	previous, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(previous) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	if err := a.SessionStart(context.Background(), "opencode", session.ModeDirect, []string{"--model", "provider/model"}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(marker)
	if err != nil || !strings.Contains(string(body), "memory_read_page") {
		t.Fatalf("instructions=%q err=%v", body, err)
	}
	values, err := a.SessionList()
	if err != nil || len(values) != 1 || values[0].PrimaryExecutor != "opencode" || values[0].HeadroomRequested || values[0].RufloEnabled || values[0].PrimaryModel.Name != "provider/model" {
		t.Fatalf("sessions=%+v err=%v", values, err)
	}
	if err := a.SessionStart(context.Background(), "opencode", session.ModeOrchestrated, nil); err == nil || !strings.Contains(err.Error(), "deferred to IVOAI-22") {
		t.Fatalf("orchestrated OpenCode error=%v", err)
	}
}

func TestConcurrentDirectSessionsRemainIndependent(t *testing.T) {
	root := t.TempDir()
	release := filepath.Join(root, "release")
	t.Cleanup(func() { _ = os.WriteFile(release, []byte("release"), 0o600) })
	agentBody := `#!/bin/sh
printf '%s\n' "$$" > "` + root + `/started-$$"
while [ ! -f "` + release + `" ]; do sleep 0.02; done
`
	codex := appExecutable(t, root, "codex", agentBody)
	claude := appExecutable(t, root, "claude", agentBody)
	a := sessionTestApp(t, root, codex, claude, appExecutable(t, root, "ruflo", "#!/bin/sh\nexit 0\n"))
	previous, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(previous) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	executors := []string{"codex", "codex", "claude"}
	apps := make([]*App, len(executors))
	for index := range apps {
		copy := *a
		copy.In, copy.Out, copy.Err = strings.NewReader(""), io.Discard, io.Discard
		apps[index] = &copy
	}
	errorsBySession := make(chan error, len(executors))
	var started sync.WaitGroup
	started.Add(len(executors))
	for index, executor := range executors {
		client := apps[index]
		executor := executor
		go func() {
			started.Done()
			errorsBySession <- client.SessionStart(context.Background(), executor, session.ModeDirect, nil)
		}()
	}
	started.Wait()

	deadline := time.Now().Add(3 * time.Second)
	var active []session.Session
	for time.Now().Before(deadline) {
		active, _ = (session.Store{Root: a.Store.Paths.SessionsDir}).Active()
		allRunning := len(active) == len(executors)
		for _, value := range active {
			if value.State != session.StateRunning || value.PrimaryPID <= 0 {
				allRunning = false
				break
			}
		}
		if allRunning {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(active) != len(executors) {
		t.Fatalf("active sessions=%+v", active)
	}
	ids, pids := map[string]bool{}, map[int]bool{}
	counts := map[string]int{}
	for _, value := range active {
		ids[value.SessionID], pids[value.PrimaryPID] = true, true
		counts[value.PrimaryExecutor]++
		if value.State != session.StateRunning || value.PrimaryPID <= 0 {
			t.Fatalf("session not independently running: %+v", value)
		}
	}
	if len(ids) != 3 || len(pids) != 3 || counts["codex"] != 2 || counts["claude"] != 1 {
		t.Fatalf("sessions collided: ids=%d pids=%d executors=%v", len(ids), len(pids), counts)
	}
	if err := os.WriteFile(release, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	for range executors {
		if err := <-errorsBySession; err != nil {
			t.Fatal(err)
		}
	}
	values, err := a.SessionList()
	if err != nil || len(values) != 3 {
		t.Fatalf("sessions=%+v err=%v", values, err)
	}
	for _, value := range values {
		if value.State != session.StateCompleted {
			t.Fatalf("session completion crossed streams: %+v", value)
		}
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

func TestClaudeCodeDirectAndOrchestratedSessions(t *testing.T) {
	for _, mode := range []session.Mode{session.ModeDirect, session.ModeOrchestrated} {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(root, "claude-ran")
			claude := appExecutable(t, root, "claude", "#!/bin/sh\nprintf launched > '"+marker+"'\n")
			ruflo := appExecutable(t, root, "ruflo", `#!/bin/sh
case "$*" in
  "--version") echo 'ruflo v3.38.12' ;;
  "swarm init"*) echo 'Swarm ID: swarm-claude-123' ;;
  "swarm status") echo 'swarm-claude-123 active' ;;
  "task create"*) echo 'task-claude-123' ;;
esac
`)
			a := sessionTestApp(t, root, appExecutable(t, root, "codex", "#!/bin/sh\nexit 0\n"), claude, ruflo)
			t.Setenv("IVOAI_TEST_MODE", "1")
			if mode == session.ModeOrchestrated {
				state, _ := a.Store.LoadState()
				if err := a.orchestrationManager(state).Configure(context.Background(), true); err != nil {
					t.Fatal(err)
				}
			}
			previous, _ := os.Getwd()
			t.Cleanup(func() { _ = os.Chdir(previous) })
			if err := os.Chdir(root); err != nil {
				t.Fatal(err)
			}
			if err := a.SessionStart(context.Background(), "claude", mode, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("Claude Code did not launch: %v", err)
			}
			values, err := a.SessionList()
			if err != nil || len(values) != 1 || values[0].Mode != mode || values[0].State != session.StateCompleted {
				t.Fatalf("sessions=%+v err=%v", values, err)
			}
		})
	}
}

func TestObservedSessionBypassesHeadroomForExactSharedKnowledge(t *testing.T) {
	root := t.TempDir()
	agentMarker := filepath.Join(root, "codex-ran")
	headroomMarker := filepath.Join(root, "headroom-ran")
	codex := appExecutable(t, root, "codex", "#!/bin/sh\nprintf direct > '"+agentMarker+"'\n")
	ruflo := appExecutable(t, root, "ruflo", "#!/bin/sh\nexit 0\n")
	a := sessionTestApp(t, root, codex, appExecutable(t, root, "claude", "#!/bin/sh\nexit 0\n"), ruflo)
	headroom := appExecutable(t, root, "headroom", "#!/bin/sh\nprintf wrapped > '"+headroomMarker+"'\nexit 2\n")
	cfg, _ := a.Store.Load()
	cfg.Headroom.Enabled = true
	cfg.MCP.Servers["ivoai-context"] = config.MCPServer{Enabled: true, Kind: "context"}
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	state, _ := a.Store.LoadState()
	state.Components["headroom"] = config.ComponentState{Installed: true, Path: headroom, Version: "fixture"}
	if err := a.Store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	previous, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(previous) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	if err := a.SessionStart(context.Background(), "codex", session.ModeDirect, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(agentMarker); err != nil {
		t.Fatalf("official Codex did not run: %v", err)
	}
	if _, err := os.Stat(headroomMarker); !os.IsNotExist(err) {
		t.Fatalf("Headroom was invoked despite shared knowledge: %v", err)
	}
	values, err := a.SessionList()
	if err != nil || len(values) != 1 || !values[0].HeadroomRequested || values[0].HeadroomUsed {
		t.Fatalf("unexpected Headroom telemetry: sessions=%+v err=%v", values, err)
	}
}

func TestObservedSessionBypassesCavemanWithoutHeadroomForExactSharedKnowledge(t *testing.T) {
	root := t.TempDir()
	agentMarker := filepath.Join(root, "codex-ran")
	codex := appExecutable(t, root, "codex", "#!/bin/sh\nprintf direct > '"+agentMarker+"'\n")
	a := sessionTestApp(t, root, codex, appExecutable(t, root, "claude", "#!/bin/sh\nexit 0\n"), appExecutable(t, root, "ruflo", "#!/bin/sh\nexit 0\n"))
	cfg, _ := a.Store.Load()
	cfg.Compression.Provider = "caveman"
	cfg.Headroom.Enabled = false
	cfg.MCP.Servers["ivoai-context"] = config.MCPServer{Enabled: true, Kind: "context"}
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	previous, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(previous) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	if err := a.SessionStart(context.Background(), "codex", session.ModeDirect, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(agentMarker); err != nil {
		t.Fatalf("official Codex did not run: %v", err)
	}
	values, err := a.SessionList()
	if err != nil || len(values) != 1 || values[0].CompressionProvider != "direct" || values[0].CompressionUsed {
		t.Fatalf("session=%+v err=%v", values, err)
	}
	found := false
	for _, event := range values[0].Observability {
		if event.Operation != observability.OperationCompressionSelect {
			continue
		}
		found = event.Provider == "direct" && event.RequestedProvider == "caveman" && event.CompressionBypassed && event.AuthoritativeKnowledge && event.RoutingReason == observability.ReasonAuthoritativeSharedKnowledge
	}
	if !found {
		t.Fatalf("provider-neutral bypass event missing: %+v", values[0].Observability)
	}
}

func sessionTestApp(t *testing.T, root, codex, claude, ruflo string) *App {
	t.Helper()
	paths := config.Paths{
		ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), BinDir: filepath.Join(root, "data", "bin"),
		Config: filepath.Join(root, "config", "config.toml"), State: filepath.Join(root, "state", "state.toml"), Secrets: filepath.Join(root, "config", "secrets.json"), Ownership: filepath.Join(root, "state", "ownership.toml"), HooksDir: filepath.Join(root, "data", "hooks"), SessionsDir: filepath.Join(root, "state", "sessions"), QuotaDir: filepath.Join(root, "state", "quota"),
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
