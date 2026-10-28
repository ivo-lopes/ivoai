package orchestrator

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func admittedPrimaryServer(t *testing.T) *Server {
	t.Helper()
	s := primaryEvidenceServer(t)
	digest := strings.Repeat("a", 64)
	r := core.PrimaryTurnReceipt{ID: "receipt_fixture", TurnID: "turn_fixture", AttemptID: "attempt_fixture", FrontendSessionID: "front_fixture", Executor: "codex", Mode: core.PrimaryReadOnly, ConfigDigest: digest, StateDigest: digest, ScopeDigest: digest}
	_, err := s.Store.Update(s.SessionID, func(v *session.Session) error {
		v.Frontend = "codex"
		v.FrontendSessionID = r.FrontendSessionID
		v.FrontendSessions = map[string]string{"codex": r.FrontendSessionID}
		v.TurnState = "running"
		v.ObservationTurnID = r.TurnID
		v.ObservationAttemptID = r.AttemptID
		v.PrimaryTurn = &r
		v.CurrentPhase = string(core.PrimaryReadOnly)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	identity := r.Identity()
	s.PrimaryTurnIdentity = &identity
	s.AuthorizePrimaryTurn = func(context.Context) error { return nil } // Explicit synthetic host callback, not production runtime attestation.
	s.DAGProtocol = "Static host DAG protocol fixture"
	return s
}

func TestReadOnlyPrimaryCheckpointCannotCreateEvidenceBeforePlan(t *testing.T) {
	s := admittedPrimaryServer(t)
	if absent, err := s.Store.CheckpointAbsent(s.SessionID); err != nil || !absent {
		t.Fatal("fixture already has checkpoint evidence")
	}
	if _, err := s.checkpoint(context.Background(), toolRequest(map[string]any{"objective": "unplanned checkpoint"})); err == nil {
		t.Error("read-only primary checkpoint was accepted before a plan")
	}
	if absent, err := s.Store.CheckpointAbsent(s.SessionID); err != nil || !absent {
		t.Error("rejected checkpoint created persistent evidence")
	}
	v, err := s.Store.Get(s.SessionID)
	if err != nil || v.CheckpointAvailable || v.CheckpointUpdatedAt != nil {
		t.Error("rejected checkpoint changed session metadata")
	}
	if _, err := s.Store.LoadCheckpoint(s.SessionID); !os.IsNotExist(err) {
		t.Error("rejected checkpoint did not preserve absence")
	}
}

func TestAdmittedPrimaryCheckpointPreservesBytesAfterLateRevocation(t *testing.T) {
	for _, next := range []string{"closed", "replacement", "legacy"} {
		t.Run(next, func(t *testing.T) {
			s := admittedPrimaryServer(t)
			_, err := s.Store.Update(s.SessionID, func(v *session.Session) error {
				v.PrimaryTurn.Mode = core.PrimaryDAG
				v.PlanID = "plan_0123456789abcdef0123456789abcdef"
				v.PrimaryTurn.PlanID = v.PlanID
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.checkpoint(context.Background(), toolRequest(map[string]any{"objective": "approved DAG checkpoint"})); err != nil {
				t.Fatal("DAG checkpoint unavailable", err)
			}
			path := filepath.Join(s.Store.Root, "checkpoints", s.SessionID+".json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			s.AuthorizePrimaryTurn = func(context.Context) error {
				_, err := s.Store.Update(s.SessionID, func(v *session.Session) error {
					switch next {
					case "closed":
						v.PrimaryTurn.Mode = core.PrimaryClosed
					case "legacy":
						v.PrimaryTurn = nil
						v.ObservationTurnID, v.ObservationAttemptID = "next_turn", "next_attempt"
					case "replacement":
						r := *v.PrimaryTurn
						r.ID, r.TurnID, r.AttemptID = "next_receipt", "next_turn", "next_attempt"
						v.PrimaryTurn = &r
						v.ObservationTurnID, v.ObservationAttemptID = r.TurnID, r.AttemptID
					}
					return nil
				})
				return err
			}
			if _, err := s.checkpoint(context.Background(), toolRequest(map[string]any{"objective": "late unowned checkpoint"})); err == nil {
				t.Fatal("late checkpoint succeeded")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("late checkpoint changed existing checkpoint bytes")
			}
		})
	}
}

func TestReadOnlyPrimaryCannotRecoverOrCancelWorkers(t *testing.T) {
	s := admittedPrimaryServer(t)
	id := "worker_" + strings.Repeat("a", 32)
	cancelled := false
	s.cancels[id] = func() { cancelled = true }
	if _, err := s.cancel(context.Background(), toolRequest(map[string]any{"worker_id": id})); err == nil || cancelled {
		t.Fatal("read-only primary cancelled a worker")
	}
	if _, err := s.recoverPlan(context.Background(), toolRequest(map[string]any{})); err == nil {
		t.Fatal("fresh admitted turn entered recovery")
	}
	if absent, err := s.Store.CheckpointAbsent(s.SessionID); err != nil || !absent {
		t.Fatal("denied recovery created checkpoint")
	}
}

func TestAdmittedPrimaryCancelRequiresLiveDAGIdentity(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "revoked"}[revoked], func(t *testing.T) {
			s := admittedPrimaryServer(t)
			_, err := s.Store.Update(s.SessionID, func(v *session.Session) error {
				v.PrimaryTurn.Mode = core.PrimaryDAG
				v.PlanID = "plan_0123456789abcdef0123456789abcdef"
				v.PrimaryTurn.PlanID = v.PlanID
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			id := "worker_" + strings.Repeat("a", 32)
			cancelled := false
			s.cancels[id] = func() { cancelled = true }
			if revoked {
				s.AuthorizePrimaryTurn = func(context.Context) error {
					_, err := s.Store.Update(s.SessionID, func(v *session.Session) error { v.PrimaryTurn.Mode = core.PrimaryClosed; return nil })
					return err
				}
			}
			_, err = s.cancel(context.Background(), toolRequest(map[string]any{"worker_id": id}))
			if revoked && (err == nil || cancelled) || !revoked && (err != nil || !cancelled) {
				t.Fatal("cancel did not respect live DAG identity")
			}
		})
	}
}

func TestAdmittedPrimaryEscalatesThroughExistingDAGAndIntegration(t *testing.T) {
	s := admittedPrimaryServer(t)
	ctx := context.Background()
	if _, err := s.bootstrap(ctx, toolRequest(map[string]any{"objective": "inspect bounded source", "memory_status": "disabled", "context_status": "disabled", "memory_lookup_performed": false, "context_lookup_performed": false})); err != nil {
		t.Fatal(err)
	}
	v, _ := s.Store.Get(s.SessionID)
	if v.PrimaryTurn.Mode != core.PrimaryReadOnly || v.CurrentPhase != string(core.PrimaryReadOnly) || v.PlanID != "" || v.PrimaryTurn.TurnID != "turn_fixture" {
		t.Fatal("read-only bootstrap escalated authority or changed the turn")
	}
	result, err := s.plan(ctx, toolRequest(map[string]any{"tasks": []any{primaryEvidenceTask("inspect", nil)}}))
	if err != nil {
		t.Fatal(err)
	}
	planID := result.StructuredContent.(map[string]any)["plan_id"].(string)
	v, _ = s.Store.Get(s.SessionID)
	if v.PrimaryTurn.PlanID != planID || v.PlanID != planID || len(v.Workers) != 0 {
		t.Fatal("plan lost admitted identity or created unnecessary worker")
	}
	if _, err := s.integrate(ctx, toolRequest(map[string]any{"plan_id": planID})); err == nil {
		t.Fatal("incomplete primary plan integrated")
	}
	if _, err := s.primaryComplete(ctx, toolRequest(map[string]any{"plan_id": planID, "task_id": "inspect"})); err != nil {
		t.Fatal(err)
	}
	if _, err := s.integrate(ctx, toolRequest(map[string]any{"plan_id": planID})); err != nil {
		t.Fatal(err)
	}
	v, _ = s.Store.Get(s.SessionID)
	if v.CurrentPhase != "synthesizing" || v.Tasks[0].State != session.StateCompleted || len(v.Workers) != 0 {
		t.Fatal("existing DAG acceptance failed")
	}
}

func TestAdmittedPrimaryInvalidPlanIsMonotonicAndStaleServerDenied(t *testing.T) {
	s := admittedPrimaryServer(t)
	s.BootstrapRequired = true
	if _, err := s.plan(context.Background(), toolRequest(map[string]any{})); err == nil {
		t.Fatal("invalid plan accepted")
	}
	v, _ := s.Store.Get(s.SessionID)
	if v.PrimaryTurn.Mode != core.PrimaryDAG || v.PlanID != "" {
		t.Fatal("failed plan revived primary-only mode")
	}
	old := s.PrimaryTurnIdentity
	_, err := s.Store.Update(s.SessionID, func(v *session.Session) error {
		copy := *v.PrimaryTurn
		copy.ID = "other_receipt"
		copy.TurnID = "other_turn"
		copy.AttemptID = "other_attempt"
		v.PrimaryTurn = &copy
		v.ObservationTurnID = copy.TurnID
		v.ObservationAttemptID = copy.AttemptID
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.plan(context.Background(), toolRequest(map[string]any{"tasks": []any{primaryEvidenceTask("inspect", nil)}})); err == nil {
		t.Fatal("stale server adopted a new receipt")
	}
	if s.PrimaryTurnIdentity != old {
		t.Fatal("server replaced its own captured identity")
	}
	v, _ = s.Store.Get(s.SessionID)
	if v.PlanID != "" || len(v.Tasks) != 0 || len(v.Workers) != 0 || len(v.Decisions) != 0 {
		t.Fatal("stale call produced plan effects")
	}
	// Removing the operational pointer for a later legacy turn also cannot
	// make the old capability current again.
	_, err = s.Store.Update(s.SessionID, func(v *session.Session) error { v.PrimaryTurn = nil; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.plan(context.Background(), toolRequest(map[string]any{})); err == nil {
		t.Fatal("stale server entered later legacy turn")
	}
}

func TestAdmittedPrimaryLatePlanPersistenceCannotAdoptNextTurn(t *testing.T) {
	for _, next := range []string{"replacement_receipt", "legacy_turn"} {
		t.Run(next, func(t *testing.T) {
			s := admittedPrimaryServer(t)
			if err := s.Store.EnterPrimaryDAG(s.SessionID, *s.PrimaryTurnIdentity); err != nil {
				t.Fatal(err)
			}
			_, err := s.Store.Update(s.SessionID, func(v *session.Session) error {
				v.ObservationTurnID = "next_turn"
				v.ObservationAttemptID = "next_attempt"
				if next == "legacy_turn" {
					v.PrimaryTurn = nil
				} else {
					r := *v.PrimaryTurn
					r.ID, r.TurnID, r.AttemptID = "next_receipt", v.ObservationTurnID, v.ObservationAttemptID
					v.PrimaryTurn = &r
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.persistPlan(routing.Plan{ID: "plan_stale"}); err == nil {
				t.Fatal("stale persistence adopted the next turn")
			}
			v, err := s.Store.Get(s.SessionID)
			if err != nil || v.PlanID != "" || len(v.Tasks) != 0 || len(v.Workers) != 0 || v.CheckpointAvailable {
				t.Fatal("stale persistence changed plan or checkpoint state")
			}
		})
	}
}

func TestAdmittedPrimaryLateBootstrapCannotOverwriteNextTurn(t *testing.T) {
	for _, next := range []string{"replacement_receipt", "legacy_turn", "closed_receipt"} {
		t.Run(next, func(t *testing.T) {
			s := admittedPrimaryServer(t)
			_, err := s.Store.SaveBrief(s.SessionID, session.SharedContextBrief{Objective: "prior safe brief", MemoryStatus: "disabled", ContextStatus: "disabled"})
			if err != nil {
				t.Fatal(err)
			}
			s.AuthorizePrimaryTurn = func(context.Context) error {
				_, err := s.Store.Update(s.SessionID, func(v *session.Session) error {
					if next == "closed_receipt" {
						v.PrimaryTurn.Mode = core.PrimaryClosed
						return nil
					}
					v.ObservationTurnID, v.ObservationAttemptID = "next_turn", "next_attempt"
					if next == "legacy_turn" {
						v.PrimaryTurn = nil
					} else {
						r := *v.PrimaryTurn
						r.ID, r.TurnID, r.AttemptID = "next_receipt", v.ObservationTurnID, v.ObservationAttemptID
						v.PrimaryTurn = &r
					}
					return nil
				})
				return err
			}
			if _, err := s.bootstrap(context.Background(), toolRequest(map[string]any{"objective": "stale write", "memory_status": "disabled", "context_status": "disabled", "memory_lookup_performed": false, "context_lookup_performed": false})); err == nil {
				t.Fatal("stale bootstrap succeeded")
			}
			v, err := s.Store.Get(s.SessionID)
			if err != nil || v.KnowledgeBootstrap.Performed {
				t.Fatal("stale bootstrap changed session metadata")
			}
			brief, err := s.Store.LoadBrief(s.SessionID)
			if err != nil || brief.Objective != "prior safe brief" {
				t.Fatal("stale bootstrap overwrote the next turn's shared brief")
			}
		})
	}
}
