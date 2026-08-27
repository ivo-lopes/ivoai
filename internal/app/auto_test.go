package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/session"
)

type probeFunc func(context.Context) (quota.ProviderQuota, error)

func (f probeFunc) Probe(ctx context.Context) (quota.ProviderQuota, error) { return f(ctx) }

func available(provider quota.Provider) quota.ProviderQuota {
	now := time.Now().UTC()
	return quota.ProviderQuota{Provider: provider, Authenticated: true, Eligible: true, Source: "fixture", ObservedAt: now, Windows: []quota.Window{
		quota.FromUsed(quota.KindWeekly, 20, nil, "fixture", now),
	}}
}

func exhausted(provider quota.Provider) quota.ProviderQuota {
	value := available(provider)
	value.Eligible, value.HardLimitReached, value.Reason = false, true, "subscription quota exhausted"
	value.Windows = []quota.Window{quota.FromUsed(quota.KindWeekly, 100, nil, "fixture", value.ObservedAt)}
	return value
}

func autoTestApp(t *testing.T, root, codexBody, claudeBody string) *App {
	t.Helper()
	ruflo := appExecutable(t, root, "ruflo", `#!/bin/sh
case "$*" in
  "--version") echo 'ruflo v3.38.12' ;;
  "swarm init"*) echo 'Swarm ID: swarm-auto-123' ;;
  "swarm status") echo 'swarm-auto-123 active' ;;
  "task create"*) echo 'task-auto-123' ;;
esac
`)
	a := sessionTestApp(t, root, appExecutable(t, root, "codex", codexBody), appExecutable(t, root, "claude", claudeBody), ruflo)
	t.Setenv("IVOAI_TEST_MODE", "1")
	state, err := a.Store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.orchestrationManager(state).Configure(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	previous, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(previous) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestSelectPlannerUsesPersistentDefault(t *testing.T) {
	a := &App{In: strings.NewReader("\n"), Out: &bytes.Buffer{}}
	selected, err := a.selectPlanner("claude")
	if err != nil || selected != "claude" {
		t.Fatalf("selected=%q err=%v", selected, err)
	}
}

func TestAutoStartupFallbackNeverLaunchesExhaustedProvider(t *testing.T) {
	root := t.TempDir()
	codexMarker := filepath.Join(root, "codex-launched")
	claudeArgs := filepath.Join(root, "claude-args")
	a := autoTestApp(t, root, "#!/bin/sh\n: > '"+codexMarker+"'\n", "#!/bin/sh\nprintf '%s\\n' \"$@\" > '"+claudeArgs+"'\n")
	a.QuotaManager = &quota.Manager{Store: quota.Store{Root: a.Store.Paths.QuotaDir}, Probes: map[quota.Provider]quota.Probe{
		quota.ProviderCodex:  probeFunc(func(context.Context) (quota.ProviderQuota, error) { return exhausted(quota.ProviderCodex), nil }),
		quota.ProviderClaude: probeFunc(func(context.Context) (quota.ProviderQuota, error) { return available(quota.ProviderClaude), nil }),
	}}
	if err := a.Auto(context.Background(), "codex", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(codexMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("exhausted Codex primary was launched")
	}
	if _, err := os.Stat(claudeArgs); err != nil {
		t.Fatalf("Claude fallback was not launched: %v", err)
	}
	values, _ := a.SessionList()
	if len(values) != 1 || values[0].InitialPlanner != "codex" || values[0].CurrentPrimary != "claude" || values[0].FailoverCount != 1 || values[0].State != session.StateCompleted {
		t.Fatalf("unexpected automatic session: %+v", values)
	}
}

func TestAutoMidSessionFailoverPreservesWorkingTreeAndHandoff(t *testing.T) {
	root := t.TempDir()
	tracked := filepath.Join(root, "preserve.txt")
	if err := os.WriteFile(tracked, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	claudeArgs := filepath.Join(root, "claude-args")
	a := autoTestApp(t, root, "#!/bin/sh\ntrap 'exit 0' TERM INT\nwhile :; do sleep 1; done\n", "#!/bin/sh\nprintf '%s\\n' \"$@\" > '"+claudeArgs+"'\n")
	var mu sync.Mutex
	codexCalls := 0
	a.QuotaManager = &quota.Manager{Store: quota.Store{Root: a.Store.Paths.QuotaDir}, TTL: time.Minute, Probes: map[quota.Provider]quota.Probe{
		quota.ProviderCodex: probeFunc(func(context.Context) (quota.ProviderQuota, error) {
			mu.Lock()
			defer mu.Unlock()
			codexCalls++
			if codexCalls > 1 {
				return exhausted(quota.ProviderCodex), nil
			}
			return available(quota.ProviderCodex), nil
		}),
		quota.ProviderClaude: probeFunc(func(context.Context) (quota.ProviderQuota, error) { return available(quota.ProviderClaude), nil }),
	}}
	a.AutoPollInterval = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Auto(ctx, "codex", nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(tracked)
	if err != nil || string(body) != "keep" {
		t.Fatalf("working tree content changed: %q %v", body, err)
	}
	args, err := os.ReadFile(claudeArgs)
	if err != nil || !strings.Contains(string(args), "IvoAI automatic failover") || !strings.Contains(string(args), "Last confirmed checkpoint") {
		t.Fatalf("bounded handoff not passed to alternate: %q %v", args, err)
	}
	values, _ := a.SessionList()
	if len(values) != 1 || values[0].CurrentPrimary != "claude" || values[0].FailoverCount != 1 || values[0].LastFailoverReason == "" || values[0].State != session.StateCompleted {
		t.Fatalf("unexpected failover state: %+v", values)
	}
}

func TestAutoBlocksWhenBothSubscriptionsAreExhausted(t *testing.T) {
	root := t.TempDir()
	a := autoTestApp(t, root, "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	a.QuotaManager = &quota.Manager{Store: quota.Store{Root: a.Store.Paths.QuotaDir}, Probes: map[quota.Provider]quota.Probe{
		quota.ProviderCodex:  probeFunc(func(context.Context) (quota.ProviderQuota, error) { return exhausted(quota.ProviderCodex), nil }),
		quota.ProviderClaude: probeFunc(func(context.Context) (quota.ProviderQuota, error) { return exhausted(quota.ProviderClaude), nil }),
	}}
	if err := a.Auto(context.Background(), "codex", nil); err == nil {
		t.Fatal("both exhausted providers were accepted")
	}
	values, _ := a.SessionList()
	if len(values) != 1 || values[0].State != session.StateBlocked || values[0].PrimaryPID != 0 || len(values[0].Workers) != 0 {
		t.Fatalf("blocked session started work: %+v", values)
	}
}

func TestAutoClaudeUsesCompatibleHeadroomWrapper(t *testing.T) {
	root := t.TempDir()
	agentMarker := filepath.Join(root, "claude-launched")
	a := autoTestApp(t, root, "#!/bin/sh\nexit 0\n", "#!/bin/sh\nprintf launched > '"+agentMarker+"'\n")
	headroom := appExecutable(t, root, "headroom", `#!/bin/sh
if [ "$1" = "--version" ]; then echo 'headroom 0.36.0'; exit 0; fi
if [ "$1" = "wrap" ] && [ "$3" = "--help" ]; then exit 0; fi
if [ "$1" = "wrap" ]; then agent=$2; shift 3; exec "$agent" "$@"; fi
exit 2
`)
	cfg, _ := a.Store.Load()
	cfg.Headroom.Enabled = true
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	state, _ := a.Store.LoadState()
	state.Components["headroom"] = config.ComponentState{Installed: true, Path: headroom, Version: "fixture"}
	if err := a.Store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	a.QuotaManager = &quota.Manager{Store: quota.Store{Root: a.Store.Paths.QuotaDir}, Probes: map[quota.Provider]quota.Probe{
		quota.ProviderCodex:  probeFunc(func(context.Context) (quota.ProviderQuota, error) { return available(quota.ProviderCodex), nil }),
		quota.ProviderClaude: probeFunc(func(context.Context) (quota.ProviderQuota, error) { return available(quota.ProviderClaude), nil }),
	}}
	if err := a.Auto(context.Background(), "claude", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(agentMarker); err != nil {
		t.Fatalf("Headroom did not launch Claude Code: %v", err)
	}
	values, err := a.SessionList()
	if err != nil || len(values) != 1 || !values[0].HeadroomUsed || values[0].State != session.StateCompleted {
		t.Fatalf("automatic Headroom session=%+v err=%v", values, err)
	}
}

func TestAutoBypassesHeadroomForExactSharedKnowledge(t *testing.T) {
	root := t.TempDir()
	agentMarker := filepath.Join(root, "codex-launched")
	headroomMarker := filepath.Join(root, "headroom-launched")
	a := autoTestApp(t, root, "#!/bin/sh\nprintf direct > '"+agentMarker+"'\n", "#!/bin/sh\nexit 0\n")
	headroom := appExecutable(t, root, "headroom", "#!/bin/sh\nprintf wrapped > '"+headroomMarker+"'\nexit 2\n")
	cfg, _ := a.Store.Load()
	cfg.Headroom.Enabled = true
	cfg.MCP.Servers["ivoai-memory"] = config.MCPServer{Enabled: true, Kind: "memory"}
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	state, _ := a.Store.LoadState()
	state.Components["headroom"] = config.ComponentState{Installed: true, Path: headroom, Version: "fixture"}
	if err := a.Store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	a.QuotaManager = &quota.Manager{Store: quota.Store{Root: a.Store.Paths.QuotaDir}, Probes: map[quota.Provider]quota.Probe{
		quota.ProviderCodex:  probeFunc(func(context.Context) (quota.ProviderQuota, error) { return available(quota.ProviderCodex), nil }),
		quota.ProviderClaude: probeFunc(func(context.Context) (quota.ProviderQuota, error) { return available(quota.ProviderClaude), nil }),
	}}
	if err := a.Auto(context.Background(), "codex", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(agentMarker); err != nil {
		t.Fatalf("official Codex did not run: %v", err)
	}
	if _, err := os.Stat(headroomMarker); !os.IsNotExist(err) {
		t.Fatalf("Headroom was invoked despite shared knowledge: %v", err)
	}
	values, err := a.SessionList()
	if err != nil || len(values) != 1 || values[0].HeadroomUsed {
		t.Fatalf("unexpected automatic Headroom telemetry: sessions=%+v err=%v", values, err)
	}
	output := a.Out.(*bytes.Buffer).String()
	if !strings.Contains(output, "BYPASSED / PRESERVING EXACT SHARED KNOWLEDGE") {
		t.Fatalf("automatic preflight omitted the bypass: %q", output)
	}
}

func TestQuotaStatuslineRejectsUnauthorizedSessionBeforeCacheWrite(t *testing.T) {
	root := t.TempDir()
	a := autoTestApp(t, root, "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	if _, err := a.QuotaStatusline("sess_0123456789abcdef0123456789abcdef", []byte(`{"rate_limits":{"seven_day":{"used_percentage":100}}}`)); err == nil {
		t.Fatal("unauthorized statusline accepted")
	}
	snapshot, err := (quota.Store{Root: a.Store.Paths.QuotaDir}).Load()
	if err != nil || len(snapshot.Providers) != 0 {
		t.Fatalf("unauthorized telemetry reached cache: %+v %v", snapshot, err)
	}
}

func TestClaudeStatuslineFlowsThroughCacheAndStatus(t *testing.T) {
	root := t.TempDir()
	a := autoTestApp(t, root, "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	id := "sess_0123456789abcdef0123456789abcdef"
	now := time.Now().UTC()
	value := session.Session{
		SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true,
		InitialPlanner: "claude", CurrentPrimary: "claude", PrimaryExecutor: "claude",
		WorkingDirectory: root, PrimaryModel: session.UnknownModel(), Workers: []session.Worker{},
		MaxWorkers: 2, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected", State: session.StateStarting, CurrentPhase: "conversation",
	}
	if err := (session.Store{Root: a.Store.Paths.SessionsDir}).Create(value); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"rate_limits":{"five_hour":{"used_percentage":23.5,"resets_at":1738425600},"seven_day":{"used_percentage":41.2,"resets_at":1738857600}}}`)
	line, err := a.QuotaStatusline(id, payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "Claude 5h 76.5% remaining") || !strings.Contains(line, "weekly 58.8% remaining") {
		t.Fatalf("statusline output=%q", line)
	}
	snapshot, err := (quota.Store{Root: a.Store.Paths.QuotaDir}).Load()
	if err != nil {
		t.Fatal(err)
	}
	fiveHour, _ := snapshot.Providers[quota.ProviderClaude].Window(quota.KindSession)
	weekly, _ := snapshot.Providers[quota.ProviderClaude].Window(quota.KindWeekly)
	if fiveHour.RemainingPercent != 76.5 || weekly.RemainingPercent != 58.8 || fiveHour.ResetsAt == nil || weekly.ResetsAt == nil {
		t.Fatalf("cached quota=%+v", snapshot.Providers[quota.ProviderClaude])
	}
	output, ok := a.Out.(*bytes.Buffer)
	if !ok {
		t.Fatal("fixture output is not a buffer")
	}
	output.Reset()
	if err := a.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Claude 5h", "76.5% remaining", "Claude weekly", "58.8% remaining"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("status missing %q:\n%s", expected, output.String())
		}
	}
}

func TestClaudeAutomaticSettingsComposeExistingStatuslinePrivately(t *testing.T) {
	root := t.TempDir()
	a := autoTestApp(t, root, "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	claudeDir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	originalSettings := []byte(`{"theme":"dark","statusLine":{"type":"command","command":"printf user-statusline"}}`)
	settingsPath := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(settingsPath, originalSettings, 0o600); err != nil {
		t.Fatal(err)
	}
	runtimeDir := filepath.Join(root, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	instructions := filepath.Join(runtimeDir, "automatic-instructions.md")
	if err := os.WriteFile(instructions, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := a.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	args, err := a.autoAgentArgs("claude", nil, "sess_0123456789abcdef0123456789abcdef", runtimeDir, instructions, "", cfg)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--settings") || !strings.Contains(joined, "--append-system-prompt-file") {
		t.Fatalf("Claude session arguments=%q", joined)
	}
	generated, err := os.ReadFile(filepath.Join(runtimeDir, "claude-auto-settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), "claude-statusline-wrapper.sh") {
		t.Fatalf("existing statusline was not composed: %s", generated)
	}
	wrapper, err := os.ReadFile(filepath.Join(runtimeDir, "claude-statusline-wrapper.sh"))
	if err != nil || !strings.Contains(string(wrapper), "_quota-statusline") || !strings.Contains(string(wrapper), "claude-original-statusline") {
		t.Fatalf("invalid statusline wrapper: %q %v", wrapper, err)
	}
	for _, name := range []string{"claude-auto-settings.json", "claude-statusline-wrapper.sh", "claude-original-statusline", "claude-mcp.json"} {
		info, err := os.Stat(filepath.Join(runtimeDir, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode=%v err=%v", name, info.Mode().Perm(), err)
		}
	}
	unchanged, _ := os.ReadFile(settingsPath)
	if string(unchanged) != string(originalSettings) {
		t.Fatal("persistent Claude settings were modified")
	}
}

func TestCodexAutomaticArgsApproveOnlySharedKnowledgeReads(t *testing.T) {
	root := t.TempDir()
	a := autoTestApp(t, root, "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	runtimeDir := filepath.Join(root, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	instructions := filepath.Join(runtimeDir, "automatic-instructions.md")
	if err := os.WriteFile(instructions, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := a.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.MCP.Servers["ivoai-memory"] = config.MCPServer{Enabled: true, Kind: "memory"}
	cfg.MCP.Servers["ivoai-context"] = config.MCPServer{Enabled: true, Kind: "context"}
	args, err := a.autoAgentArgs("codex", nil, "sess_0123456789abcdef0123456789abcdef", runtimeDir, instructions, "", cfg)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, "\n")
	for _, expected := range []string{
		`developer_instructions="fixture"`,
		`mcp_servers.ivoai-memory.tools.memory_query.approval_mode="approve"`,
		`mcp_servers.ivoai-context.tools.context_search.approval_mode="approve"`,
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("automatic Codex arguments missing %q: %q", expected, joined)
		}
	}
	if strings.Contains(joined, "memory_write_page.approval_mode") {
		t.Fatalf("automatic Codex arguments auto-approved a memory write: %q", joined)
	}
}

func TestQuotaSummaryShowsCodexFiveHourAndWeeklySeparately(t *testing.T) {
	now := time.Now().UTC()
	values := map[quota.Provider]quota.ProviderQuota{quota.ProviderCodex: {Provider: quota.ProviderCodex, Windows: []quota.Window{quota.FromUsedDuration(quota.KindRolling, 300, 20, nil, "fixture", now), quota.FromUsedDuration(quota.KindWeekly, 10080, 30, nil, "fixture", now)}}}
	var output bytes.Buffer
	printQuotaSummary(&output, values)
	text := output.String()
	if !strings.Contains(text, "Codex       5h      80% remaining") || !strings.Contains(text, "weekly  70% remaining") {
		t.Fatalf("Codex windows not shown separately: %s", text)
	}
}
