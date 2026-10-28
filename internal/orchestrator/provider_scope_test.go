package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
)

type deniedLegacyProbe struct{ calls int }

func (p *deniedLegacyProbe) Probe(context.Context) (quota.ProviderQuota, error) {
	p.calls++
	return quota.ProviderQuota{Provider: quota.ProviderOpenCode, Authenticated: true, Eligible: true}, nil
}

func TestScopedLowQuotaStillRequiresApprovalWithoutLegacyDispatch(t *testing.T) {
	store, id := automaticBridgeSession(t, t.TempDir())
	now := time.Now()
	nativeQuota := quota.ProviderQuota{Provider: quota.ProviderCodex, Authenticated: true, Eligible: true, ObservedAt: now, Source: "fixture", Windows: []quota.Window{quota.FromUsed(quota.KindWeekly, 91, nil, "fixture", now)}}
	legacy := &deniedLegacyProbe{}
	manager := (&quota.Manager{Store: quota.Store{Root: t.TempDir()}, Probes: map[quota.Provider]quota.Probe{quota.ProviderCodex: staticProbe{nativeQuota}, quota.ProviderOpenCode: legacy}}).RestrictedTo(quota.ProviderCodex, quota.ProviderClaude)
	server := &Server{Store: store, SessionID: id, LowQuotaThreshold: 10, Quota: manager, Registry: routing.Registry{Providers: map[string]routing.ProviderCapability{"codex": {Authenticated: true}, "opencode": {Authenticated: true, WorkerCapable: true}}}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.acknowledgePlanQuota(ctx) }()
	for {
		value, err := store.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if len(value.Decisions) > 0 {
			if len(value.Workers) != 0 || value.CurrentPrimary != "codex" {
				t.Fatal("quota proposal changed execution")
			}
			select {
			case err := <-done:
				t.Fatal("quota confirmation bypassed", err)
			default:
			}
			if err := store.ResolveDecision(id, value.Decisions[0].ID, true); err != nil {
				t.Fatal(err)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("scoped quota approval was not requested")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	value, _ := store.Get(id)
	if value.QuotaMode != "conservation_active" || len(value.Decisions) != 1 || len(value.Workers) != 0 || legacy.calls != 0 {
		t.Fatal("quota scope changed approval/dispatch semantics")
	}
}

func TestNativeProviderScopeDeniesInjectedLegacyPlanRetryAndCatalog(t *testing.T) {
	store, id := recoveryFixture(t)
	if _, err := store.Update(id, func(v *session.Session) error { v.Frontend = "codex"; v.RecoveryRequested = false; return nil }); err != nil {
		t.Fatal(err)
	}
	probe := &deniedLegacyProbe{}
	manager := (&quota.Manager{Store: quota.Store{Root: t.TempDir()}, Probes: map[quota.Provider]quota.Probe{quota.ProviderOpenCode: probe}}).RestrictedTo(quota.ProviderCodex, quota.ProviderClaude)
	server := &Server{Store: store, SessionID: id, Quota: manager, Registry: routing.Registry{Providers: map[string]routing.ProviderCapability{"opencode": {Provider: "opencode", Authenticated: true, WorkerCapable: true, Models: []routing.ModelCapability{{Name: "cached-legacy", CapabilityTier: routing.TierMax, Source: routing.SourceRuntimeVerified}}}}}}
	server.initialize()
	before, _ := store.Get(id)
	for _, input := range []routing.TaskInput{{Executor: "opencode"}, {PreferredExecutor: "opencode"}} {
		if _, err := server.resolveProfile(context.Background(), input, routing.TierStrong); !errors.Is(err, session.ErrOpenCodeManagedDeprecated) {
			t.Fatal("injected legacy route admitted", err)
		}
		if _, err := server.refreshNativeRoute(context.Background(), routing.Task{TaskInput: input, Profile: routing.ExecutionProfile{Provider: "codex"}, Tier: routing.TierStrong}); !errors.Is(err, session.ErrOpenCodeManagedDeprecated) {
			t.Fatal("legacy retry intent admitted", err)
		}
	}
	if _, err := server.refreshNativeRoute(context.Background(), routing.Task{Profile: routing.ExecutionProfile{Provider: "opencode"}, Tier: routing.TierStrong}); !errors.Is(err, session.ErrOpenCodeManagedDeprecated) {
		t.Fatal("retained legacy profile admitted on retry", err)
	}
	task := taskFixture("t5", nil)
	task["executor"] = "opencode"
	if _, err := server.plan(context.Background(), toolRequest(map[string]any{"tasks": []any{task}})); !errors.Is(err, session.ErrOpenCodeManagedDeprecated) {
		t.Fatal("explicit legacy plan admitted", err)
	}
	if _, err := server.delegate(context.Background(), toolRequest(map[string]any{"role": "research", "task": "Inspect fixture", "preferred_executor": "opencode"})); !errors.Is(err, session.ErrOpenCodeManagedDeprecated) {
		t.Fatal("legacy delegation admitted", err)
	}
	response, err := server.capabilities(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(response.StructuredContent)
	if strings.Contains(string(encoded), "cached-legacy") || strings.Contains(string(encoded), "opencode") {
		t.Fatal("excluded cached catalog exposed")
	}
	encoded, _ = json.Marshal(planSchemaFor(server.providerNames()))
	if strings.Contains(string(encoded), "opencode") {
		t.Fatal("legacy executor advertised in native plan schema")
	}
	after, _ := store.Get(id)
	if !reflect.DeepEqual(before, after) || probe.calls != 0 {
		t.Fatal("refused legacy route changed plan/approval state or probed legacy client")
	}
}

func TestRecoveryRefusesRetainedOpenCodeBeforeRepositoryOperations(t *testing.T) {
	store, id := recoveryFixture(t)
	if _, err := store.Update(id, func(v *session.Session) error { v.Frontend = "codex"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateCheckpoint(id, func(c *session.Checkpoint) error { c.Recovery.Plan.Tasks[0].Profile.Provider = "opencode"; return nil }); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Root, "checkpoints", id+".json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Any attempted repository command now fails lookup. Migration must be
	// identified from the retained immutable snapshot before those operations.
	t.Setenv("PATH", t.TempDir())
	if _, err := ReconcileRecovery(context.Background(), store, id); !errors.Is(err, session.ErrOpenCodeMigrationRequired) {
		t.Fatal("retained provider reached repository recovery", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("refused recovery rewrote checkpoint")
	}
}
