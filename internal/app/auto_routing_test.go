package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestCodexFrontendKeepsRequestedPrimaryUntilStartupApproval(t *testing.T) {
	a := autoTestApp(t, t.TempDir(), "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	a.In = strings.NewReader("")
	a.QuotaManager = &quota.Manager{Store: quota.Store{Root: a.Store.Paths.QuotaDir}, Probes: map[quota.Provider]quota.Probe{
		quota.ProviderCodex:  probeFunc(func(context.Context) (quota.ProviderQuota, error) { return exhausted(quota.ProviderCodex), nil }),
		quota.ProviderClaude: probeFunc(func(context.Context) (quota.ProviderQuota, error) { return available(quota.ProviderClaude), nil }),
	}}
	if err := a.OrchestratedWithKnowledge(context.Background(), "codex", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	values, err := a.SessionList()
	if err != nil || len(values) != 1 {
		t.Fatal("session missing", err)
	}
	v := values[0]
	if v.CurrentPrimary != "codex" || v.PrimaryProvider != "codex" || v.FailoverCount != 0 || len(v.TurnAttempts) != 0 || len(v.Workers) != 0 {
		t.Fatal("unapproved startup route recorded as effective")
	}
}

func TestPrimaryMaterialRouteRequiresOperatorDecision(t *testing.T) {
	for _, approve := range []bool{false, true} {
		t.Run(map[bool]string{true: "approve", false: "reject"}[approve], func(t *testing.T) {
			store := session.Store{Root: t.TempDir()}
			id, _ := session.NewID()
			now := time.Now().UTC()
			if err := store.Create(session.Session{SessionID: id, Mode: session.ModeAuto, Auto: true, StartedAt: now, UpdatedAt: now, InitialPlanner: "codex", CurrentPrimary: "codex", PrimaryExecutor: "codex", WorkingDirectory: t.TempDir(), PrimaryModel: session.UnknownModel(), State: session.StateRunning, MaxWorkers: 2, MemoryStatus: "disabled", ContextStatus: "disabled", ServerStatus: "not-connected", SwarmID: "swarm-fixture"}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- confirmPrimaryRoute(ctx, store, id, "codex", "claude") }()
			for {
				v, err := store.Get(id)
				if err != nil {
					t.Fatal(err)
				}
				if len(v.Decisions) > 0 {
					d := v.Decisions[0]
					if d.Kind != "routing" || !strings.Contains(d.Summary, "codex → claude") || v.CurrentPrimary != "codex" {
						t.Fatal("invalid material route proposal")
					}
					select {
					case <-done:
						t.Fatal("changed route before approval")
					default:
					}
					if err := store.ResolveDecision(id, d.ID, approve); err != nil {
						t.Fatal(err)
					}
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("no decision")
				case <-time.After(10 * time.Millisecond):
				}
			}
			if err := <-done; (err == nil) != approve {
				t.Fatalf("approval=%v err=%v", approve, err)
			}
		})
	}
}
