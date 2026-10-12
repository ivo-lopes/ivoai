package orchestrator

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ivo-lopes/ivoai/internal/session"
)

const primaryEvidenceBudget = 4096

// primaryEvidence is transient tool-result data. It is never part of plan
// metadata, checkpoints, observations, or the shared worker bootstrap.
func (s *Server) primaryEvidence(ctx context.Context, planID string, response map[string]any) map[string]any {
	if !s.NativePolicy || s.PreparePrimaryEvidence == nil || !s.primaryEvidenceAdmitted(planID) {
		return response
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	remaining := primaryEvidenceBudget - 2 // JSON array brackets
	var evidence []json.RawMessage
	s.mu.Lock()
	plan := s.plans[planID]
	if plan == nil {
		s.mu.Unlock()
		return response
	}
	ids := make([]string, 0, len(plan.Plan.Tasks))
	for _, task := range plan.Plan.Tasks {
		ids = append(ids, task.ID)
	}
	s.mu.Unlock()
	for _, id := range ids {
		if remaining < 1024 || fetchCtx.Err() != nil || !s.primaryEvidenceAdmitted(planID) {
			break
		}
		s.mu.Lock()
		task := plan.Tasks[id]
		eligible := task != nil && task.Task.ExecutionMode == "primary" && task.Task.State == "primary" && !task.PrimaryEvidenceAttempted && !task.PrimaryEvidencePending && s.dependenciesCompleteLocked(plan, task.Task.Dependencies)
		if !eligible {
			s.mu.Unlock()
			continue
		}
		task.PrimaryEvidencePending = true
		candidate := task.Task
		s.mu.Unlock()
		// Host callback performs policy/Skill Gate checks and bounded retrieval. No
		// scheduler lock is held during file, artifact, or provider operations.
		body, err := s.PreparePrimaryEvidence(fetchCtx, candidate, remaining)
		admitted := ctx.Err() == nil && s.primaryEvidenceAdmitted(planID)
		s.mu.Lock()
		task.PrimaryEvidencePending = false
		stillReady := task.Task.ExecutionMode == "primary" && task.Task.State == "primary" && s.dependenciesCompleteLocked(plan, task.Task.Dependencies)
		if admitted && stillReady && (err != nil || len(body) > 0) {
			if err != nil || !json.Valid(body) || len(body) > remaining {
				body, _ = json.Marshal(map[string]any{"task_id": id, "state": "degraded", "reason": "Code evidence unavailable; continue with permitted inspection."})
			}
			// An attempt is consumed only by a classified task with a stable response.
			// No source is retained on the runtime task; only the delivery bit remains.
			task.PrimaryEvidenceAttempted = true
			evidence = append(evidence, body)
			remaining -= len(body) + 1
		}
		s.mu.Unlock()
	}
	if len(evidence) > 0 {
		response["primary_code_evidence"] = evidence
	}
	return response
}

func (s *Server) primaryEvidenceAdmitted(planID string) bool {
	if s.requireApprovedPlan(planID) != nil {
		return false
	}
	// Plan decisions precede quota acknowledgement. A concurrent wait must
	// observe the current admitted checkpoint, not merely the plan decision.
	checkpoint, err := s.Store.LoadCheckpoint(s.SessionID)
	if err != nil || checkpoint.Recovery == nil || checkpoint.Recovery.Plan.ID != planID || !checkpoint.Recovery.Approved {
		return false
	}
	value, err := s.Store.Get(s.SessionID)
	if err != nil || !value.Active() || value.PlanID != planID || value.RecoveryRequested {
		return false
	}
	for _, decision := range value.Decisions {
		if decision.ID == planID && decision.Kind == "plan" && decision.State != "approved" {
			return false
		}
	}
	return value.State != session.StateBlocked && value.State != session.StateStopping && value.State != session.StateWaiting
}
