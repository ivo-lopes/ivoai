package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/evidence"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func adaptiveHistoryDigest(ref *session.CompletedPrimaryPlanRef) string {
	if ref == nil {
		return ""
	}
	return evidence.Digest(*ref)
}

// A closed receipt alone is insufficient: cleanup also closes failed receipts.
// The matching successful native attempt proves that final DAG validation ran.
func adaptiveCompletedPlan(v session.Session) bool {
	r := v.PrimaryTurn
	if r == nil || r.Validate() != nil || r.Mode != core.PrimaryClosed || r.PlanID == "" || r.PlanID != v.PlanID || len(v.Tasks) == 0 || v.RecoveryRequested || v.Lineage != nil || v.FailoverCount != 0 {
		return false
	}
	for _, task := range v.Tasks {
		if task.State != session.StateCompleted {
			return false
		}
	}
	for _, worker := range v.Workers {
		if worker.State != session.StateCompleted || worker.ExitCode == nil || *worker.ExitCode != 0 || worker.EndedAt == nil || session.ProcessMatches(worker.PID, worker.ProcessStart) {
			return false
		}
	}
	for _, decision := range v.Decisions {
		if decision.State != "approved" || decision.ResolvedAt == nil {
			return false
		}
	}
	for _, raw := range v.TurnAttempts {
		var attempt opencodebridge.TurnAttempt
		if json.Unmarshal(raw, &attempt) != nil {
			return false
		}
		if attempt.ID == r.AttemptID {
			return attempt.TurnID == r.TurnID && attempt.SessionID == v.SessionID && attempt.FrontendSessionID == r.FrontendSessionID && attempt.RequestedExecutor == "codex" && attempt.EffectiveExecutor == "codex" && attempt.State == "completed" && attempt.EndedAt != nil && attempt.FinalResponse && attempt.NativeSessionID == v.ExecutorSessionID && attempt.Trace != nil && attempt.Trace.Completion && attempt.Trace.FinalResponse && attempt.Trace.ExitCode == 0 && attempt.Trace.Failure == ""
		}
	}
	return false
}

// hydrateAdaptiveCompletedAttempt retrieves the exact immutable terminal event,
// not a reconstructed trace or inferred success. Its complete nontrace fields
// must equal the host's current bounded summary before it can certify history.
func hydrateAdaptiveCompletedAttempt(ctx context.Context, store session.Store, v session.Session) (session.Session, error) {
	if v.PrimaryTurn == nil {
		return v, nil
	}
	for index, raw := range v.TurnAttempts {
		var summary opencodebridge.TurnAttempt
		if json.Unmarshal(raw, &summary) != nil {
			return v, errors.New("ADAPTIVE_PRIMARY_AUDIT_INVALID")
		}
		if summary.ID != v.PrimaryTurn.AttemptID || summary.Trace != nil {
			continue
		}
		events, err := store.ReadTurnAttemptEvents(ctx, v.SessionID, summary.ID)
		if errors.Is(err, os.ErrNotExist) && v.RequestJournalVersion == 0 {
			return v, nil
		}
		if err != nil {
			return v, errors.New("ADAPTIVE_PRIMARY_AUDIT_UNAVAILABLE")
		}
		var terminal []byte
		for _, event := range events {
			var original opencodebridge.TurnAttempt
			if json.Unmarshal(event, &original) != nil || original.ID != summary.ID || original.TurnID != summary.TurnID || original.SessionID != v.SessionID || original.Frontend != summary.Frontend || original.FrontendSessionID != summary.FrontendSessionID || original.RequestedExecutor != summary.RequestedExecutor || original.EffectiveExecutor != summary.EffectiveExecutor {
				return v, errors.New("ADAPTIVE_PRIMARY_AUDIT_IDENTITY_CHANGED")
			}
			if original.State == "running" {
				continue
			}
			if original.State != "completed" || original.Trace == nil || terminal != nil {
				return v, errors.New("ADAPTIVE_PRIMARY_AUDIT_TERMINAL_INVALID")
			}
			withoutTrace := original
			withoutTrace.Trace = nil
			if !reflect.DeepEqual(withoutTrace, summary) {
				return v, errors.New("ADAPTIVE_PRIMARY_AUDIT_SUMMARY_CHANGED")
			}
			terminal = event
		}
		if terminal == nil {
			return v, errors.New("ADAPTIVE_PRIMARY_AUDIT_TERMINAL_MISSING")
		}
		v.TurnAttempts = append([]json.RawMessage(nil), v.TurnAttempts...)
		v.TurnAttempts[index] = append(json.RawMessage(nil), terminal...)
		return v, nil
	}
	return v, nil
}

func prepareAdaptiveCompletedPlan(ctx context.Context, store session.Store, v session.Session) (session.Session, error) {
	if v.PrimaryTurn == nil || v.PrimaryTurn.PlanID == "" {
		return v, nil
	}
	if !session.AdaptiveHistoryEligible(v) {
		// Incomplete legacy or already denied native history cannot create a
		// read capability. Preserve its existing guarded legacy DAG path.
		return v, nil
	}
	completed, err := hydrateAdaptiveCompletedAttempt(ctx, store, v)
	if err != nil {
		return v, err
	}
	if !adaptiveCompletedPlan(completed) {
		// Older/legacy transcripts lack the successful native trace required
		// by this capability. Preserve their existing guarded DAG path.
		return v, nil
	}
	checkpoint, raw, err := store.LoadCheckpointSnapshot(v.SessionID)
	if errors.Is(err, os.ErrNotExist) {
		if v.CheckpointAvailable || v.CheckpointUpdatedAt != nil {
			return v, errors.New("ADAPTIVE_PRIMARY_CHECKPOINT_UNAVAILABLE")
		}
	} else if err != nil {
		return v, errors.New("ADAPTIVE_PRIMARY_CHECKPOINT_UNAVAILABLE")
	} else {
		if checkpoint.Interrupted || checkpoint.Recovery == nil || !checkpoint.Recovery.Approved || !checkpoint.Recovery.Integrated || checkpoint.Recovery.Plan.ID != v.PlanID || checkpoint.Recovery.Directory != v.WorkingDirectory || len(checkpoint.Recovery.Plan.Tasks) != len(v.Tasks) {
			return v, errors.New("ADAPTIVE_PRIMARY_DAG_NOT_INTEGRATED")
		}
		known := map[string]bool{}
		for _, task := range v.Tasks {
			known[task.ID] = true
		}
		for _, task := range checkpoint.Recovery.Plan.Tasks {
			if !known[task.ID] {
				return v, errors.New("ADAPTIVE_PRIMARY_DAG_NOT_INTEGRATED")
			}
		}
	}
	if v.CompletedPrimaryPlan != nil {
		if _, err := store.ReadCompletedPrimaryPlan(ctx, v.SessionID, *v.CompletedPrimaryPlan); err != nil {
			return v, errors.New("ADAPTIVE_PRIMARY_HISTORY_UNAVAILABLE")
		}
	}
	ref, err := store.SaveCompletedPrimaryPlan(ctx, session.CompletedPrimaryPlanSnapshot{SchemaVersion: 1, Session: completed, Checkpoint: raw})
	if err != nil {
		return v, errors.New("ADAPTIVE_PRIMARY_HISTORY_WRITE_FAILED")
	}
	v.CompletedPrimaryPlan = &ref
	v.PlanID, v.Tasks, v.Workers, v.Decisions = "", nil, nil, nil
	v.CheckpointAvailable, v.CheckpointUpdatedAt = false, nil
	v.KnowledgeBootstrap = session.BootstrapMetadata{}
	v.CurrentPhase = "conversation"
	return v, nil
}

// A current checkpoint can be retained as historical evidence only when its
// exact bytes match the immutable owner-scoped archive. It is never replayed.
func adaptiveCheckpointSafe(ctx context.Context, store session.Store, v session.Session) bool {
	if v.CompletedPrimaryPlan == nil {
		return localMetadataNoCheckpoint(store, v.SessionID)
	}
	if v.CheckpointAvailable || v.CheckpointUpdatedAt != nil {
		return false
	}
	snapshot, err := store.ReadCompletedPrimaryPlan(ctx, v.SessionID, *v.CompletedPrimaryPlan)
	if err != nil {
		return false
	}
	_, raw, err := store.LoadCheckpointSnapshot(v.SessionID)
	if errors.Is(err, os.ErrNotExist) {
		return len(snapshot.Checkpoint) == 0
	}
	return err == nil && len(snapshot.Checkpoint) > 0 && bytes.Equal(raw, snapshot.Checkpoint)
}
