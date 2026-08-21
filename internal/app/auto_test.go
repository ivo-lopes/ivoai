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
