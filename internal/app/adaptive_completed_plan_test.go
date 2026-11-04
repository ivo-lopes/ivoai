package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestAdaptiveCompletedDAGContinuesReadOnlyAndRetainsPagedHistory(t *testing.T) {
	f := newAdaptiveFixture(t)
	calls := 0
	var firstPlan string
	base := turnRunnerFunc(func(ctx context.Context, request core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
		calls++
		v, _ := f.store.Get(f.id)
		if !session.PrimaryTurnActive(v) || v.PrimaryTurn.Mode != core.PrimaryReadOnly || len(v.Tasks) != 0 || len(v.Workers) != 0 || v.PlanID != "" {
			t.Fatal("next turn inherited an active DAG or missing receipt")
		}
		if calls > 1 && (request.ExecutorSessionID != "owned_native_thread" || v.CompletedPrimaryPlan == nil || v.PrimaryTurn.HistoryDigest == "") {
			t.Fatal("owned history or native thread lost")
		}
		if calls == 1 || calls == 3 {
			if err := f.store.EnterPrimaryDAG(f.id, v.PrimaryTurn.Identity()); err != nil {
				t.Fatal(err)
			}
			plan := fmt.Sprintf("plan_%032x", calls)
			if firstPlan == "" {
				firstPlan = plan
			}
			f.update(t, func(v *session.Session) {
				v.PrimaryTurn.PlanID, v.PlanID, v.CurrentPhase = plan, plan, "synthesizing"
				v.Tasks = []session.TaskMetadata{{ID: "inspect", Role: "research", Tier: "LIGHT", State: session.StateCompleted, Model: session.UnknownModel(), ExecutionMode: "primary"}}
			})
		} else {
			snapshot, err := f.store.ReadCompletedPrimaryPlan(ctx, f.id, *v.CompletedPrimaryPlan)
			if err != nil || len(snapshot.Session.Tasks) != 1 || snapshot.Session.PrimaryTurn.Mode != core.PrimaryClosed {
				t.Fatal("completed plan journal was discarded", err)
			}
			if calls == 4 {
				if snapshot.Session.CompletedPrimaryPlan == nil {
					t.Fatal("previous history reference was truncated")
				}
				previous, err := f.store.ReadCompletedPrimaryPlan(ctx, f.id, *snapshot.Session.CompletedPrimaryPlan)
				if err != nil || previous.Session.PlanID != firstPlan {
					t.Fatal("older completed plan not recoverable", err)
				}
			}
		}
		return core.ExecutorResult{ExecutorSessionID: "owned_native_thread", Trace: &core.ExecutionTrace{Completion: true, FinalResponse: true, ExitCode: 0}}, emit("answer")
	})
	b, results := adaptiveBridge(t, f, base, func(o *opencodebridge.Options) {
		o.AuthReference = func(context.Context, string) (string, error) { return "opaque_fixture_account_epoch", nil }
		o.Mapping = adaptivePersistMapping(f)
	})
	for _, id := range []string{"dag-one", "explain-one", "dag-two", "explain-two"} {
		if _, err := b.SubmitTurn(context.Background(), "fresh_frontend", id, "Explique a alteração aprovada.", "auto", ""); err != nil {
			t.Fatal(err)
		}
		if r := <-results; !r.Policy.NoReplay || !r.Policy.NoAutomaticFailover {
			t.Fatal("continuity lost admission policy")
		}
	}
	v, _ := f.store.Get(f.id)
	if calls != 4 || len(v.TurnAttempts) != 4 || len(v.FrontendRequests) != 4 || v.PrimaryTurn.Mode != core.PrimaryClosed || v.PlanID != "" {
		t.Fatal("continuity replayed or lost journal")
	}
}

func TestAdaptiveCompletedHistoryCannotBeReboundToTheActiveReceipt(t *testing.T) {
	f := newAdaptiveFixture(t)
	v, _ := f.store.Get(f.id)
	request := core.ExecutorRequest{Executor: "codex", FrontendSessionID: v.FrontendSessionID}
	// Existing adverse receipt tests cover active configuration/runtime changes;
	// a fabricated historical pointer must not become a new read capability.
	v.CompletedPrimaryPlan = &session.CompletedPrimaryPlanRef{SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Bytes: 1, ReceiptID: "foreign_receipt", PlanID: "foreign_plan"}
	if adaptiveCheckpointSafe(context.Background(), f.store, v) {
		t.Fatal("missing immutable history was accepted")
	}
	if adaptivePrimarySession(v, request, f.root, v.KnowledgeScopeID) {
		t.Fatal("fresh turn adopted forged history")
	}
}

func TestAdaptivePostDAGTamperingWithholdsFinalAndDoesNotReplay(t *testing.T) {
	for _, kind := range []string{"checkpoint", "archive", "reference"} {
		t.Run(kind, func(t *testing.T) {
			f := newAdaptiveFixture(t)
			calls := 0
			base := turnRunnerFunc(func(ctx context.Context, request core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
				calls++
				v, _ := f.store.Get(f.id)
				if calls == 1 {
					if err := f.store.EnterPrimaryDAG(f.id, v.PrimaryTurn.Identity()); err != nil {
						t.Fatal(err)
					}
					f.update(t, func(v *session.Session) {
						v.PlanID = "plan_0123456789abcdef0123456789abcdef"
						v.PrimaryTurn.PlanID, v.CurrentPhase = v.PlanID, "synthesizing"
						v.Tasks = []session.TaskMetadata{{ID: "inspect", Role: "research", Tier: "LIGHT", State: session.StateCompleted, Model: session.UnknownModel()}}
					})
				} else {
					if v.CompletedPrimaryPlan == nil {
						t.Fatal("history not archived before admission")
					}
					switch kind {
					case "checkpoint":
						if err := f.store.SaveCheckpoint(f.id, session.Checkpoint{Objective: "unapproved replacement"}); err != nil {
							t.Fatal(err)
						}
					case "archive":
						if err := os.WriteFile(filepath.Join(f.store.Root, "completed-primary", f.id, v.CompletedPrimaryPlan.SHA256+".json"), []byte("changed"), 0600); err != nil {
							t.Fatal(err)
						}
					case "reference":
						f.update(t, func(v *session.Session) {
							ref := *v.CompletedPrimaryPlan
							ref.SHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
							v.CompletedPrimaryPlan = &ref
						})
					}
				}
				return core.ExecutorResult{ExecutorSessionID: "owned_native_thread", Trace: &core.ExecutionTrace{Completion: true, FinalResponse: true, ExitCode: 0}}, emit("buffered final")
			})
			b, results := adaptiveBridge(t, f, base, func(o *opencodebridge.Options) {
				o.AuthReference = func(context.Context, string) (string, error) { return "opaque_fixture_account_epoch", nil }
				o.Mapping = adaptivePersistMapping(f)
			})
			if _, err := b.SubmitTurn(context.Background(), "fresh_frontend", "first", "Explique a mudança.", "auto", ""); err != nil {
				t.Fatal(err)
			}
			<-results
			if _, err := b.SubmitTurn(context.Background(), "fresh_frontend", "second", "Resuma o resultado.", "auto", ""); err == nil {
				t.Fatal("tampered evidence produced a completed answer")
			}
			if r := <-results; !r.Policy.NoReplay || !r.Policy.NoAutomaticFailover {
				t.Fatal("denial lost no-replay policy")
			}
			if _, err := b.SubmitTurn(context.Background(), "fresh_frontend", "second", "retry", "auto", ""); err == nil || calls != 2 {
				t.Fatal("denied turn replayed")
			}
		})
	}
}
