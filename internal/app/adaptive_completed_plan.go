package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"

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

func prepareAdaptiveCompletedPlan(ctx context.Context, store session.Store, v session.Session) (session.Session, error) {
	if v.PrimaryTurn == nil || v.PrimaryTurn.PlanID == "" {
		return v, nil
	}
	if !adaptiveCompletedPlan(v) {
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
	ref, err := store.SaveCompletedPrimaryPlan(ctx, session.CompletedPrimaryPlanSnapshot{SchemaVersion: 1, Session: v, Checkpoint: raw})
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
