package orchestrator

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/routing"
)

func TestNativeLowQuotaRequiresConfirmationAndPreservesExplicitRoute(t *testing.T) {
	for _, tc := range []struct {
		name              string
		remaining         float64
		approve, explicit bool
		want              string
		decision          bool
	}{
		{"eleven remains normal", 11, false, false, "codex", false},
		{"ten keep current", 10, false, false, "codex", true},
		{"nine approve conservation", 9, true, false, "claude", true},
		{"explicit Codex remains authoritative", 9, true, true, "codex", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			store, id := automaticBridgeSession(t, root)
			registry := routing.Registry{Providers: map[string]routing.ProviderCapability{}}
			probes := map[quota.Provider]quota.Probe{}
			for _, provider := range []quota.Provider{quota.ProviderCodex, quota.ProviderClaude} {
				remaining := 75.0
				if provider == quota.ProviderCodex {
					remaining = tc.remaining
				}
				registry.Providers[string(provider)] = routing.ProviderCapability{Authenticated: true, WorkerCapable: true, Models: []routing.ModelCapability{{Name: "fixture-" + string(provider), Source: routing.SourceRuntimeVerified, CapabilityTier: routing.TierLight}}}
				probes[provider] = staticProbe{quota.ProviderQuota{Provider: provider, Authenticated: true, Eligible: true, Source: "fixture", ObservedAt: time.Now(), Windows: []quota.Window{{Kind: quota.KindWeekly, Available: true, Authoritative: true, RemainingPercent: remaining, UsedPercent: 100 - remaining, ObservedAt: time.Now(), Source: "fixture", State: quota.TelemetryAvailable}}}}
			}
			s := &Server{Store: store, SessionID: id, Registry: registry, LowQuotaThreshold: 10, Quota: &quota.Manager{Store: quota.Store{Root: filepath.Join(root, "quota")}, Probes: probes}}
			task := routing.Task{TaskInput: routing.TaskInput{ID: "research", Role: "research"}, Tier: routing.TierLight, Profile: routing.ExecutionProfile{Provider: "codex", Model: "fixture-codex", Tier: routing.TierLight}}
			if tc.explicit {
				task.Executor = "codex"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			type result struct {
				profile routing.ExecutionProfile
				err     error
			}
			done := make(chan result, 1)
			go func() { p, err := s.refreshNativeRoute(ctx, task); done <- result{p, err} }()
			if tc.decision {
				for {
					v, err := store.Get(id)
					if err != nil {
						t.Fatal(err)
					}
					if len(v.Decisions) > 0 {
						d := v.Decisions[0]
						if d.Kind != "routing" || d.State != "pending" || !strings.Contains(d.Summary, "preserve strong primary") {
							t.Fatal("missing visible routing proposal")
						}
						select {
						case <-done:
							t.Fatal("route changed before confirmation")
						default:
						}
						if err := store.ResolveDecision(id, d.ID, tc.approve); err != nil {
							t.Fatal(err)
						}
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal("routing approval not requested")
					case <-time.After(10 * time.Millisecond):
					}
				}
			}
			got := <-done
			if got.err != nil || got.profile.Provider != tc.want {
				t.Fatalf("provider=%s err=%v", got.profile.Provider, got.err)
			}
			v, _ := store.Get(id)
			if !tc.decision && len(v.Decisions) != 0 {
				t.Fatal("unexpected quota confirmation")
			}
			if tc.decision && tc.approve && v.QuotaMode != "conservation_active" {
				t.Fatal("conservation not active")
			}
			if tc.decision && !tc.approve && v.QuotaMode != "keep_current" {
				t.Fatal("routing rejection ignored")
			}
		})
	}
}

func TestPrimaryOnlyPlanLowQuotaIsVisibleWithoutSpawningWorkers(t *testing.T) {
	for _, approve := range []bool{false, true} {
		root := t.TempDir()
		store, id := automaticBridgeSession(t, root)
		now := time.Now()
		q := quota.ProviderQuota{Provider: quota.ProviderCodex, Authenticated: true, Eligible: true, ObservedAt: now, Source: "fixture", Windows: []quota.Window{quota.FromUsed(quota.KindWeekly, 91, nil, "fixture", now)}}
		s := &Server{Store: store, SessionID: id, Registry: routing.Registry{Providers: map[string]routing.ProviderCapability{"codex": {Authenticated: true}}}, LowQuotaThreshold: 10, Quota: &quota.Manager{Store: quota.Store{Root: filepath.Join(root, "quota")}, Probes: map[quota.Provider]quota.Probe{quota.ProviderCodex: staticProbe{q}}}}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		done := make(chan error, 1)
		go func() { done <- s.acknowledgePlanQuota(ctx) }()
		for {
			v, err := store.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			if len(v.Decisions) > 0 {
				if len(v.Workers) != 0 || v.CurrentPrimary != "codex" {
					t.Fatal("quota proposal executed work")
				}
				if err := store.ResolveDecision(id, v.Decisions[0].ID, approve); err != nil {
					t.Fatal(err)
				}
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("missing primary-only quota alert")
			case <-time.After(10 * time.Millisecond):
			}
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		v, _ := store.Get(id)
		want := "keep_current"
		if approve {
			want = "conservation_active"
		}
		if v.QuotaMode != want {
			t.Fatalf("quota mode=%s", v.QuotaMode)
		}
		if err := s.acknowledgePlanQuota(ctx); err != nil {
			t.Fatal(err)
		}
		v, _ = store.Get(id)
		if len(v.Decisions) != 1 {
			t.Fatal("repeated acknowledged quota proposal")
		}
		cancel()
	}
}
