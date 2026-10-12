package app

import (
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/session"
	"testing"
	"time"
)

func TestOperationalQuotaSnapshotFreshness(t *testing.T) {
	now := time.Now().UTC()
	store := quota.Store{Root: t.TempDir()}
	for _, p := range []quota.Provider{quota.ProviderCodex, quota.ProviderClaude} {
		v := available(p)
		v.ObservedAt = now
		if p == quota.ProviderClaude {
			v.ObservedAt = now.Add(-time.Hour)
		}
		if err := store.Put(v); err != nil {
			t.Fatal(err)
		}
	}
	values, failures := operationalQuotas(store, time.Minute, now)
	if failures[quota.ProviderCodex] != nil || !values[quota.ProviderCodex].Authenticated {
		t.Fatal("fresh cache lost")
	}
	if failures[quota.ProviderClaude] == nil || failures[quota.ProviderOpenCode] == nil {
		t.Fatal("stale or missing cache promoted")
	}
}

func TestOperationalTurnCorrelatesWithoutTranscripts(t *testing.T) {
	value := session.Session{SessionID: "sess_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	attempt := opencodebridge.TurnAttempt{ID: "attempt_1", TurnID: "turn_1", State: "running", EffectiveExecutor: "codex"}
	if err := appendTurnObservation(&value, attempt); err != nil {
		t.Fatal(err)
	}
	if len(value.Observability) != 1 || value.Observability[0].TurnID != "turn_1" || value.Observability[0].AttemptID != "attempt_1" {
		t.Fatal("missing turn identity")
	}
	attempt.State = "completed"
	if err := appendTurnObservation(&value, attempt); err != nil {
		t.Fatal(err)
	}
	if string(value.Observability[1].State) != "completed" {
		t.Fatal("missing completion")
	}
}

func TestOperationalTerminalAuditDoesNotChangeRunningCorrelation(t *testing.T) {
	value := session.Session{SessionID: "sess_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PlanID: "plan_current"}
	first := opencodebridge.TurnAttempt{ID: "attempt_1", TurnID: "turn_1", State: "running", EffectiveExecutor: "codex"}
	second := opencodebridge.TurnAttempt{ID: "attempt_2", TurnID: "turn_2", State: "running", EffectiveExecutor: "codex"}
	for _, attempt := range []opencodebridge.TurnAttempt{first, second} {
		if err := appendTurnObservation(&value, attempt); err != nil {
			t.Fatal(err)
		}
	}
	first.State = "cancelled"
	if err := appendTurnObservation(&value, first); err != nil {
		t.Fatal(err)
	}
	if value.ObservationTurnID != "turn_2" || value.ObservationAttemptID != "attempt_2" {
		t.Fatal("terminal audit changed running correlation")
	}
	if len(value.Observability) != 3 || value.Observability[2].TurnID != "turn_1" || value.Observability[2].PlanID != "" {
		t.Fatal("terminal audit lost or assigned unrelated plan")
	}
	if err := session.AppendObservation(&value, observability.Event{Category: observability.CategoryWorker, Operation: observability.OperationWorkerLifecycle, State: observability.StateRunning}); err != nil {
		t.Fatal(err)
	}
	if value.Observability[3].TurnID != "turn_2" {
		t.Fatal("worker incorrectly attributed")
	}
}
