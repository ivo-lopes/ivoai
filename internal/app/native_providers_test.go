package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/codexfrontend"
	"github.com/ivo-lopes/ivoai/internal/codexresolver"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"golang.org/x/sys/unix"
)

type noOpenCodeRunner struct{ calls int }

func (r *noOpenCodeRunner) LookPath(name string) (string, error) {
	if name == "opencode" {
		r.calls++
		return "", errors.New("unexpected legacy lookup")
	}
	return (platform.ExecRunner{}).LookPath(name)
}
func (r *noOpenCodeRunner) Run(ctx context.Context, path string, args []string, options platform.RunOptions) (platform.Result, error) {
	if filepath.Base(path) == "opencode" {
		r.calls++
		return platform.Result{}, errors.New("unexpected legacy execution")
	}
	return (platform.ExecRunner{}).Run(ctx, path, args, options)
}

func nativeProviderTestApp(t *testing.T) (*App, string, *noOpenCodeRunner, *int) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("IVOAI_TEST_MODE", "1")
	client := appExecutable(t, root, "codex", "#!/bin/sh\nif [ \"$1 $2 $3\" = 'mcp list --json' ]; then printf '[]'; fi\n")
	a := sessionTestApp(t, root, client, appExecutable(t, root, "claude", "#!/bin/sh\nexit 0\n"), appExecutable(t, root, "ruflo", "#!/bin/sh\nexit 0\n"))
	marker := filepath.Join(root, "unexpected-opencode-process")
	legacy := appExecutable(t, root, "opencode", "#!/bin/sh\n: > "+shellArgument(marker)+"\nexit 0\n")
	state, _ := a.Store.LoadState()
	state.Components["opencode"] = config.ComponentState{Installed: true, Managed: true, Path: legacy, Version: "fixture"}
	if err := a.Store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	runner := &noOpenCodeRunner{}
	a.Runner = runner
	probeCalls := new(int)
	a.QuotaManager = &quota.Manager{Store: quota.Store{Root: a.Store.Paths.QuotaDir}, Probes: map[quota.Provider]quota.Probe{
		quota.ProviderCodex:  probeFunc(func(context.Context) (quota.ProviderQuota, error) { return available(quota.ProviderCodex), nil }),
		quota.ProviderClaude: probeFunc(func(context.Context) (quota.ProviderQuota, error) { return available(quota.ProviderClaude), nil }),
		quota.ProviderOpenCode: probeFunc(func(context.Context) (quota.ProviderQuota, error) {
			*probeCalls++
			return available(quota.ProviderOpenCode), nil
		}),
	}}
	if err := a.QuotaManager.Store.Put(available(quota.ProviderOpenCode)); err != nil {
		t.Fatal(err)
	}
	catalog := opencodebridge.CatalogFromRegistry(routing.Registry{Providers: map[string]routing.ProviderCapability{
		"codex":    {Authenticated: true, Models: []routing.ModelCapability{{Name: "fixture-codex", CapabilityTier: routing.TierStrong, Source: routing.SourceRuntimeVerified}}},
		"opencode": {Authenticated: true, Models: []routing.ModelCapability{{Name: "legacy-cached", CapabilityTier: routing.TierStrong, Source: routing.SourceRuntimeVerified}}},
	}})
	a.OpenCodeModelCatalog = &catalog
	a.StartOpenCodeManaged = func(context.Context, opencodebridge.ManagedOptions) (managedOpenCodeFrontend, error) {
		t.Fatal("native session started legacy renderer")
		return nil, errors.New("unexpected")
	}
	return a, marker, runner, probeCalls
}

func assertNoOpenCode(t *testing.T, marker string, runner *noOpenCodeRunner, probes *int) {
	t.Helper()
	if runner.calls != 0 || *probes != 0 {
		t.Fatalf("legacy lookup/run=%d probe=%d", runner.calls, *probes)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("OpenCode process executed", err)
	}
}

func TestNativeCodexStartupDoesNotDiscoverInstalledOpenCode(t *testing.T) {
	a, marker, runner, probes := nativeProviderTestApp(t)
	a.StartCodexNative = func(_ context.Context, options codexfrontend.Options) (nativeCodexFrontend, error) {
		for _, model := range options.Bridge.Catalog().Entries() {
			if model.Executor == "opencode" {
				t.Fatal("cached legacy model reached native picker")
			}
		}
		return fixtureNativeFrontend{}, nil
	}
	if err := a.OrchestratedWithKnowledge(context.Background(), "codex", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	assertNoOpenCode(t, marker, runner, probes)
	if !a.QuotaManager.Allows(quota.ProviderOpenCode) || a.QuotaManager.Probes[quota.ProviderOpenCode] == nil {
		t.Fatal("shared legacy manager changed")
	}
	for _, model := range a.OpenCodeModelCatalog.Entries() {
		if model.Executor == "opencode" {
			return
		}
	}
	t.Fatal("shared legacy catalog changed")
}

func TestNativeProvidersRefuseLegacyFallbackAndExplicitPlannerBeforeDiscovery(t *testing.T) {
	a, marker, runner, probes := nativeProviderTestApp(t)
	for _, planner := range []string{"opencode", "claude"} {
		err := a.OrchestratedWithKnowledge(context.Background(), "codex", planner, nil, nil)
		if err == nil || !strings.Contains(err.Error(), map[string]string{"opencode": "OPENCODE_MANAGED_DEPRECATED", "claude": "CLAUDE_NATIVE_MANAGED_FRONTEND_UNAVAILABLE"}[planner]) {
			t.Fatal("unsupported initial planner admitted", planner, err)
		}
	}
	for _, provider := range []quota.Provider{quota.ProviderCodex, quota.ProviderClaude} {
		a.QuotaManager.Probes[provider] = probeFunc(func(context.Context) (quota.ProviderQuota, error) { return exhausted(provider), nil })
	}
	if err := a.OrchestratedWithKnowledge(context.Background(), "codex", "", nil, nil); err == nil {
		t.Fatal("exhausted native providers fell back to legacy")
	}
	assertNoOpenCode(t, marker, runner, probes)
}

func TestNativeWorkerProviderAssemblyDropsInstalledAndCachedOpenCode(t *testing.T) {
	for _, executor := range []string{"codex", "claude"} {
		t.Run(executor, func(t *testing.T) {
			a, marker, runner, probes := nativeProviderTestApp(t)
			cfg, _ := a.Store.Load()
			state, _ := a.Store.LoadState()
			value := session.Session{Frontend: executor, PrimaryExecutor: executor, WorkingDirectory: filepath.Dir(marker)}
			native, manager, registry := a.workerProviders(context.Background(), cfg, state, value, t.TempDir())
			if native != nil || manager.Allows(quota.ProviderOpenCode) || manager.Probes[quota.ProviderOpenCode] != nil {
				t.Fatal("legacy worker registered")
			}
			if _, ok := registry.Providers["opencode"]; ok {
				t.Fatal("legacy capability registered")
			}
			stale := routing.Registry{Providers: map[string]routing.ProviderCapability{"opencode": {Authenticated: true, WorkerCapable: true}, "codex": {Authenticated: true}}}
			if _, ok := nativeProviderRegistry(stale).Providers["opencode"]; ok {
				t.Fatal("cached legacy capability survived")
			}
			if _, ok := stale.Providers["opencode"]; !ok {
				t.Fatal("shared registry mutated")
			}
			assertNoOpenCode(t, marker, runner, probes)
		})
	}
}

func nativeHistoryFixture(t *testing.T, a *App) (session.Store, session.Session) {
	t.Helper()
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	id, _ := session.NewID()
	now := time.Now()
	v := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true, Coordinator: "native", Frontend: "codex", FrontendSessionID: "fixture-thread", PrimaryExecutor: "codex", CurrentPrimary: "codex", InitialPlanner: "codex", WorkingDirectory: a.Store.Paths.ConfigDir, PrimaryModel: session.UnknownModel(), State: session.StateCompleted, MaxWorkers: 2, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected", SwarmID: "native_" + id}
	if err := store.Create(v); err != nil {
		t.Fatal(err)
	}
	return store, v
}

func treeSnapshot(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			relative, _ := filepath.Rel(root, path)
			out[relative] = sha256.Sum256(body)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestNativeResumeRecoveryAndHandoffRefuseBeforeAnyProviderOrHistoryChange(t *testing.T) {
	for _, operation := range []string{"resume", "recover", "handoff"} {
		t.Run(operation, func(t *testing.T) {
			a, marker, runner, probes := nativeProviderTestApp(t)
			store, value := nativeHistoryFixture(t, a)
			if operation != "handoff" {
				if _, err := store.Update(value.SessionID, func(v *session.Session) error {
					v.Tasks = []session.TaskMetadata{{ID: "legacy", Role: "research", Executor: "opencode", Tier: string(routing.TierLight), State: session.StatePlanned, Model: session.UnknownModel()}}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			resolved := 0
			a.CodexResolution = func(_ context.Context, s config.State) (config.State, codexresolver.Resolution, error) {
				resolved++
				return s, codexresolver.Resolution{}, nil
			}
			before := treeSnapshot(t, filepath.Dir(marker))
			var err error
			switch operation {
			case "resume":
				err = a.SessionResume(context.Background(), value.SessionID)
			case "recover":
				err = a.SessionRecover(context.Background(), value.SessionID)
			case "handoff":
				err = a.SessionHandoff(context.Background(), value.SessionID, "claude", true)
			}
			if err == nil || resolved != 0 {
				t.Fatal("migration ran provider discovery", err, resolved)
			}
			if !reflect.DeepEqual(before, treeSnapshot(t, filepath.Dir(marker))) {
				t.Fatal("rejected migration changed retained state")
			}
			assertNoOpenCode(t, marker, runner, probes)
		})
	}
}

func TestNativeTurnRejectsLegacyExecutorBeforePreparingKnowledge(t *testing.T) {
	a, marker, runner, probes := nativeProviderTestApp(t)
	store, value := nativeHistoryFixture(t, a)
	cfg, _ := a.Store.Load()
	state, _ := a.Store.LoadState()
	called := false
	base := turnRunnerFunc(func(context.Context, core.ExecutorRequest, func(string) error) (core.ExecutorResult, error) {
		called = true
		return core.ExecutorResult{}, nil
	})
	a.OpenCodeBridgeRunner = base
	wrapped := a.scopeAutoRunner(base, cfg, state, store, value.SessionID, value.WorkingDirectory, t.TempDir(), "unused", nil, nil, &autoTurnState{}, *a.OpenCodeModelCatalog)
	_, err := wrapped.Run(context.Background(), core.ExecutorRequest{Executor: "opencode", Prompt: "Inspect the fixture"}, func(string) error { return nil })
	if !errors.Is(err, session.ErrOpenCodeManagedDeprecated) || called {
		t.Fatal("legacy turn reached preparation", err)
	}
	assertNoOpenCode(t, marker, runner, probes)
}

// A closed prior receipt could otherwise be retired before native migration or
// checkpoint inspection fails. Denial must preserve both the pointer and the
// recorded bytes, including unsafe current/legacy FIFO checkpoint entries.
func TestNativeCompatibilityDenialPrecedesReceiptRetirement(t *testing.T) {
	for _, mode := range []string{"explicit_legacy", "retained_legacy", "fifo_current", "fifo_legacy"} {
		t.Run(mode, func(t *testing.T) {
			a, marker, runner, probes := nativeProviderTestApp(t)
			store, value := nativeHistoryFixture(t, a)
			_, err := store.Update(value.SessionID, func(v *session.Session) error {
				v.State = session.StateRunning
				v.TurnState = "running"
				v.ObservationTurnID = "next_turn"
				v.ObservationAttemptID = "next_attempt"
				v.PrimaryTurn = &core.PrimaryTurnReceipt{ID: "closed_receipt", TurnID: "previous_turn", AttemptID: "prior_attempt", FrontendSessionID: "fixture-thread", Executor: "codex", Mode: core.PrimaryClosed, ConfigDigest: strings.Repeat("a", 64), StateDigest: strings.Repeat("a", 64), ScopeDigest: strings.Repeat("a", 64)}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			checkpoint := filepath.Join(store.Root, "checkpoints", value.SessionID+".json")
			if mode == "fifo_legacy" {
				checkpoint = filepath.Join(store.Root, "runtime", value.SessionID, "checkpoint.json")
			}
			var checkpointBytes []byte
			if mode == "retained_legacy" {
				plan := routing.Plan{ID: "plan_fixture", Strategy: "efficient", CreatedAt: time.Now(), Tasks: []routing.Task{{TaskInput: routing.TaskInput{ID: "retained", Role: "research", Task: "Inspect the fixture", Acceptance: []string{"report findings"}}, Profile: routing.ExecutionProfile{Provider: "opencode", Tier: routing.TierLight}, Tier: routing.TierLight, State: "planned", ExecutionMode: "worker"}}}
				if err := store.SaveCheckpoint(value.SessionID, session.Checkpoint{Recovery: &session.RecoveryPlan{Plan: plan, Directory: store.Root, Approved: true}}); err != nil {
					t.Fatal(err)
				}
				checkpointBytes, err = os.ReadFile(checkpoint)
				if err != nil {
					t.Fatal(err)
				}
			}
			var fifo os.FileInfo
			if strings.HasPrefix(mode, "fifo_") {
				if err := os.MkdirAll(filepath.Dir(checkpoint), 0700); err != nil {
					t.Fatal(err)
				}
				if err := unix.Mkfifo(checkpoint, 0600); err != nil {
					t.Fatal(err)
				}
				fifo, err = os.Lstat(checkpoint)
				if err != nil {
					t.Fatal(err)
				}
			}
			before, err := store.Get(value.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			beforeBytes, err := json.Marshal(before)
			if err != nil {
				t.Fatal(err)
			}
			cfg, _ := a.Store.Load()
			state, _ := a.Store.LoadState()
			called := false
			base := turnRunnerFunc(func(context.Context, core.ExecutorRequest, func(string) error) (core.ExecutorResult, error) {
				called = true
				return core.ExecutorResult{}, nil
			})
			wrapped := a.scopeAutoRunner(base, cfg, state, store, value.SessionID, value.WorkingDirectory, t.TempDir(), "unused", nil, nil, &autoTurnState{}, *a.OpenCodeModelCatalog)
			executor := "codex"
			if mode == "explicit_legacy" {
				executor = "opencode"
			}
			started := time.Now()
			_, err = wrapped.Run(context.Background(), core.ExecutorRequest{Executor: executor, Prompt: "Inspect the fixture"}, func(string) error { return nil })
			if err == nil || called {
				t.Fatal("compatibility denial reached execution", err)
			}
			if mode == "explicit_legacy" && !errors.Is(err, session.ErrOpenCodeManagedDeprecated) {
				t.Fatal(err)
			}
			if mode == "retained_legacy" && !errors.Is(err, session.ErrOpenCodeMigrationRequired) {
				t.Fatal(err)
			}
			if strings.HasPrefix(mode, "fifo_") && (!strings.Contains(err.Error(), "NATIVE_CHECKPOINT_UNAVAILABLE") || time.Since(started) > time.Second) {
				t.Fatal("unsafe checkpoint was not promptly denied", err)
			}
			after, err := store.Get(value.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			afterBytes, err := json.Marshal(after)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(beforeBytes, afterBytes) {
				t.Fatal("denial retired receipt or rewrote session")
			}
			if checkpointBytes != nil {
				after, err := os.ReadFile(checkpoint)
				if err != nil || !bytes.Equal(checkpointBytes, after) {
					t.Fatal("denial rewrote checkpoint", err)
				}
			}
			if fifo != nil {
				after, err := os.Lstat(checkpoint)
				if err != nil || !os.SameFile(fifo, after) || after.Mode()&os.ModeNamedPipe == 0 {
					t.Fatal("denial replaced FIFO", err)
				}
			}
			assertNoOpenCode(t, marker, runner, probes)
		})
	}
}
