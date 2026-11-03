package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func adaptivePersistMapping(f *localMetadataFixture) func(opencodebridge.Mapping) error {
	return func(m opencodebridge.Mapping) error {
		_, err := f.store.Update(f.id, func(v *session.Session) error {
			v.ExecutorSessionID = m.ExecutorSessionID
			v.ExecutorSessions = map[string]session.ExecutorSessionMapping{"codex:" + m.FrontendSessionID: {Executor: m.Executor, ExecutorSessionID: m.ExecutorSessionID, AuthReference: m.AuthReference, SelectionMode: m.SelectionMode, RequestedModel: m.RequestedModel, EffectiveModel: m.EffectiveModel, EffectiveEffort: m.EffectiveEffort, CatalogRevision: m.CatalogRevision, UpdatedAt: time.Now().UTC()}}
			return nil
		})
		return err
	}
}

func TestAdaptiveFreshSessionRejectsUnownedNativeThread(t *testing.T) {
	f := newAdaptiveFixture(t)
	v, err := f.store.Get(f.id)
	if err != nil {
		t.Fatal(err)
	}
	v.FrontendRequests = map[string]time.Time{"fresh_frontend:first": time.Now().UTC()}
	v.ObservationTurnID, v.ObservationAttemptID, v.TurnState = "fresh_turn", "fresh_attempt", "running"
	raw, err := json.Marshal(opencodebridge.TurnAttempt{SessionID: f.id, TurnID: v.ObservationTurnID, ID: v.ObservationAttemptID, State: "running", Frontend: "codex", FrontendSessionID: "fresh_frontend", RequestedExecutor: "codex", EffectiveExecutor: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	v.TurnAttempts = []json.RawMessage{raw}
	request := core.ExecutorRequest{Executor: "codex", FrontendSessionID: "fresh_frontend"}
	if !adaptivePrimarySession(v, request, f.root, v.KnowledgeScopeID) {
		t.Fatal("fresh fixture refused")
	}
	request.ExecutorSessionID = "foreign_native_thread"
	if adaptivePrimarySession(v, request, f.root, v.KnowledgeScopeID) {
		t.Fatal("unowned native thread admitted")
	}
}

func TestAdaptiveOwnedContinuationKeepsOneNativeThreadAndFreshReceipts(t *testing.T) {
	f := newAdaptiveFixture(t)
	var receipts []core.PrimaryTurnReceipt
	base := turnRunnerFunc(func(ctx context.Context, request core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
		v, _ := f.store.Get(f.id)
		if !session.PrimaryTurnActive(v) || len(v.Tasks) != 0 || len(v.Workers) != 0 {
			return core.ExecutorResult{}, errors.New("continuation was not admitted without a DAG")
		}
		if len(receipts) > 0 && request.ExecutorSessionID != "owned_native_thread" {
			return core.ExecutorResult{}, errors.New("native conversation was restarted")
		}
		receipts = append(receipts, *v.PrimaryTurn)
		return core.ExecutorResult{ExecutorSessionID: "owned_native_thread", Trace: &core.ExecutionTrace{FinalResponse: true, Completion: true, ExitCode: 0}}, emit("owned answer")
	})
	b, results := adaptiveBridge(t, f, base, func(o *opencodebridge.Options) {
		o.AuthReference = func(context.Context, string) (string, error) { return "opaque_fixture_account_epoch", nil }
		o.Mapping = adaptivePersistMapping(f)
	})
	for _, id := range []string{"first", "second", "third"} {
		if _, err := b.SubmitTurn(context.Background(), "fresh_frontend", id, "Explique como funciona a autenticação deste projeto.", "auto", ""); err != nil {
			t.Fatal(err)
		}
		if r := <-results; !r.Policy.NoReplay || !r.Policy.NoAutomaticFailover {
			t.Fatal("continuation lost non-replay policy")
		}
	}
	if len(receipts) != 3 {
		t.Fatal("missing admitted turns")
	}
	for i := 1; i < len(receipts); i++ {
		if receipts[i].ID == receipts[i-1].ID || receipts[i].TurnID == receipts[i-1].TurnID || receipts[i].AttemptID == receipts[i-1].AttemptID {
			t.Fatal("receipt identity reused")
		}
	}
	v, _ := f.store.Get(f.id)
	if len(v.TurnAttempts) != 3 || len(v.FrontendRequests) != 3 || v.PrimaryTurn.Mode != core.PrimaryClosed || v.ExecutorSessionID != "owned_native_thread" {
		t.Fatal("history or closure lost")
	}
	if _, err := b.SubmitTurn(context.Background(), "fresh_frontend", "second", "retry", "auto", ""); err == nil || len(receipts) != 3 {
		t.Fatal("duplicate resumed turn executed")
	}
}

func TestAdaptiveContinuationRefusesUnprovedHistory(t *testing.T) {
	f := newAdaptiveFixture(t)
	var captured session.Session
	base := turnRunnerFunc(func(ctx context.Context, request core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
		if request.ExecutorSessionID != "" {
			captured, _ = f.store.Get(f.id)
		}
		return core.ExecutorResult{ExecutorSessionID: "owned_native_thread", Trace: &core.ExecutionTrace{FinalResponse: true, Completion: true, ExitCode: 0}}, emit("answer")
	})
	b, results := adaptiveBridge(t, f, base, func(o *opencodebridge.Options) {
		o.AuthReference = func(context.Context, string) (string, error) { return "opaque_fixture_account_epoch", nil }
		o.Mapping = adaptivePersistMapping(f)
	})
	for _, id := range []string{"one", "two"} {
		if _, err := b.SubmitTurn(context.Background(), "fresh_frontend", id, "Onde estão implementados os endpoints da API?", "auto", ""); err != nil {
			t.Fatal(err)
		}
		<-results
	}
	// Restore the captured pre-admission shape with the previous closed receipt
	// and the currently running attempt, then mutate each independent binding.
	if captured.PrimaryTurn == nil {
		t.Fatal("continuation unavailable")
	}
	prior := receiptsForContinuation(t, captured)
	request := core.ExecutorRequest{Executor: "codex", FrontendSessionID: "fresh_frontend", ExecutorSessionID: "owned_native_thread"}
	if !adaptivePrimarySession(prior, request, f.root, prior.KnowledgeScopeID) {
		t.Fatal("positive continuation fixture refused")
	}
	for _, mode := range []string{"foreign_mapping", "missing_account", "missing_previous", "failed_previous", "foreign_frontend", "active_receipt", "same_turn", "pending_plan", "worker", "checkpoint", "recovery", "lineage", "foreign_current"} {
		t.Run(mode, func(t *testing.T) {
			raw, _ := json.Marshal(prior)
			var v session.Session
			_ = json.Unmarshal(raw, &v)
			switch mode {
			case "foreign_mapping":
				v.ExecutorSessionID = "foreign"
			case "missing_account":
				m := v.ExecutorSessions["codex:fresh_frontend"]
				m.AuthReference = ""
				v.ExecutorSessions["codex:fresh_frontend"] = m
			case "missing_previous":
				v.PrimaryTurn.AttemptID = "absent"
			case "failed_previous":
				var a opencodebridge.TurnAttempt
				_ = json.Unmarshal(v.TurnAttempts[0], &a)
				a.State = "failed"
				v.TurnAttempts[0], _ = json.Marshal(a)
			case "foreign_frontend":
				v.FrontendSessions["codex"] = "foreign"
			case "active_receipt":
				v.PrimaryTurn.Mode = core.PrimaryReadOnly
			case "same_turn":
				v.PrimaryTurn.TurnID = v.ObservationTurnID
			case "pending_plan":
				v.PlanID = "unapproved_plan"
			case "worker":
				v.Workers = append(v.Workers, session.Worker{})
			case "checkpoint":
				v.CheckpointAvailable = true
			case "recovery":
				v.RecoveryRequested = true
			case "lineage":
				v.Lineage = &session.HandoffLineage{}
			case "foreign_current":
				var a opencodebridge.TurnAttempt
				_ = json.Unmarshal(v.TurnAttempts[1], &a)
				a.SessionID = "foreign"
				v.TurnAttempts[1], _ = json.Marshal(a)
			}
			if adaptivePrimarySession(v, request, f.root, prior.KnowledgeScopeID) {
				t.Fatal("unproved continuation admitted")
			}
		})
	}
}

func receiptsForContinuation(t *testing.T, current session.Session) session.Session {
	t.Helper()
	var previous opencodebridge.TurnAttempt
	if len(current.TurnAttempts) != 2 || json.Unmarshal(current.TurnAttempts[0], &previous) != nil {
		t.Fatal("attempts unavailable")
	}
	r := *current.PrimaryTurn
	r.ID = "previous_receipt"
	r.TurnID = previous.TurnID
	r.AttemptID = previous.ID
	r.Mode = core.PrimaryClosed
	current.PrimaryTurn = &r
	return current
}
