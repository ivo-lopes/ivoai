package orchestrator

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type RecoveryPreview struct {
	Completed []string     `json:"completed"`
	Pending   []string     `json:"pending"`
	Plan      routing.Plan `json:"-"`
	workers   map[string]session.Worker
	worktrees *orchestration.Worktrees
}

// ReconcileRecovery is read-only. Completed work is not inferred from a PID
// or provider prose, and uncertain writes are never automatically retried.
func ReconcileRecovery(ctx context.Context, store session.Store, id string) (RecoveryPreview, error) {
	v, err := store.Get(id)
	if err != nil {
		return RecoveryPreview{}, err
	}
	c, err := store.LoadCheckpoint(id)
	if err != nil {
		return RecoveryPreview{}, errors.New("CHECKPOINT_UNAVAILABLE")
	}
	if c.Recovery == nil || c.Recovery.Integrated || c.Recovery.Plan.ID != v.PlanID || c.Recovery.Directory != v.WorkingDirectory {
		return RecoveryPreview{}, errors.New("CHECKPOINT_NOT_RECOVERABLE")
	}
	if !c.Recovery.Approved {
		return RecoveryPreview{}, errors.New("PLAN_APPROVAL_REQUIRED")
	}
	identity, head, err := orchestration.RecoveryRepositoryIdentity(ctx, v.WorkingDirectory)
	if err != nil || identity != c.Recovery.RepositoryIdentity || head != c.Recovery.RepositoryHead {
		return RecoveryPreview{}, errors.New("RECOVERY_REPOSITORY_CHANGED")
	}
	result := RecoveryPreview{Plan: c.Recovery.Plan, workers: map[string]session.Worker{}}
	for _, worker := range v.Workers {
		if session.ProcessMatches(worker.PID, worker.ProcessStart) {
			return RecoveryPreview{}, errors.New("SESSION_WORKER_STILL_ACTIVE")
		}
		result.workers[worker.TaskID] = worker
	}
	metadata := map[string]session.TaskMetadata{}
	for _, task := range v.Tasks {
		metadata[task.ID] = task
	}
	owned := []orchestration.Worktree{}
	for i, task := range result.Plan.Tasks {
		current, ok := metadata[task.ID]
		if !ok || current.Role != task.Role || strings.Join(current.Dependencies, "\x00") != strings.Join(task.Dependencies, "\x00") {
			return RecoveryPreview{}, errors.New("CHECKPOINT_TASK_MISMATCH")
		}
		worker := result.workers[task.ID]
		completed := current.State == session.StateCompleted
		attempted := worker.ID != "" || current.State == session.StateRunning || current.State == session.StateStarting || current.State == session.StateFailed
		if !completed && attempted {
			if len(task.AllowedMCPTools) > 0 {
				return RecoveryPreview{}, errors.New("AMBIGUOUS_PREVIOUS_EXECUTION")
			}
			if len(task.WritePaths) > 0 {
				if worker.WorktreeCommit == "" {
					return RecoveryPreview{}, errors.New("AMBIGUOUS_PREVIOUS_EXECUTION")
				}
				completed = true
			}
		}
		if completed {
			result.Plan.Tasks[i].State = string(session.StateCompleted)
			result.Completed = append(result.Completed, task.ID)
			if len(task.WritePaths) > 0 {
				if worker.WorktreeCommit == "" {
					return RecoveryPreview{}, errors.New("RECOVERY_WRITE_EVIDENCE_UNAVAILABLE")
				}
				owned = append(owned, orchestration.Worktree{TaskID: worker.ID, Path: worker.WorktreePath, Branch: worker.WorktreeBranch, Base: worker.WorktreeBase, Commit: worker.WorktreeCommit})
			}
		} else {
			result.Plan.Tasks[i].State = "planned"
			if task.ExecutionMode == "primary" {
				result.Plan.Tasks[i].State = "primary"
			}
			result.Pending = append(result.Pending, task.ID)
		}
	}
	result.worktrees, err = orchestration.RestoreWorktrees(ctx, v.WorkingDirectory, filepath.Join(store.Root, "worktree-recovery"), c.Recovery.RepositoryHead, owned)
	if err != nil {
		return RecoveryPreview{}, err
	}
	return result, nil
}

func (s *Server) recoverPlan(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args struct{}
	if strictArguments(request, &args) != nil {
		return nil, errors.New("invalid recovery request")
	}
	if !s.planMu.TryLock() {
		return nil, errors.New("PLAN_IN_PROGRESS")
	}
	defer s.planMu.Unlock()
	v, err := s.Store.Get(s.SessionID)
	if err != nil {
		return nil, err
	}
	if !v.RecoveryRequested {
		return nil, errors.New("RECOVERY_NOT_REQUESTED")
	}
	if s.BootstrapRequired && !v.KnowledgeBootstrap.Performed {
		return nil, errors.New("BOOTSTRAP_REQUIRED: prepare current purpose-scoped knowledge before recovery")
	}
	s.mu.Lock()
	busy := len(s.plans) > 0
	s.mu.Unlock()
	if busy {
		return nil, errors.New("PLAN_IN_PROGRESS")
	}
	preview, err := ReconcileRecovery(ctx, s.Store, s.SessionID)
	if err != nil {
		return nil, err
	}
	planID, err := newPlanID()
	if err != nil {
		return nil, err
	}
	plan := preview.Plan
	plan.ID = planID
	inputs := make([]routing.TaskInput, 0, len(plan.Tasks))
	for i, task := range plan.Tasks {
		if task.State == string(session.StateCompleted) {
			continue
		}
		inputs = append(inputs, task.TaskInput)
		if task.ExecutionMode == "worker" {
			profile, err := s.resolveProfile(ctx, task.TaskInput, task.Tier)
			if err != nil {
				return nil, err
			}
			plan.Tasks[i].Profile = profile
		}
	}
	if s.CheckMCPGrants != nil {
		if _, err := s.CheckMCPGrants(ctx, inputs); err != nil {
			return nil, err
		}
	}
	runtime := &runtimePlan{approvalRequired: true, Plan: plan, Tasks: map[string]*runtimeTask{}, Workers: map[string]string{}, worktrees: preview.worktrees, workIDs: map[string]string{}}
	for _, task := range plan.Tasks {
		entry := &runtimeTask{Task: task}
		if task.State == string(session.StateCompleted) {
			entry.Settled = true
			if worker := preview.workers[task.ID]; worker.ID != "" {
				entry.WorkerID = worker.ID
				runtime.Workers[worker.ID] = task.ID
				if worker.WorktreeCommit != "" {
					runtime.workIDs[task.ID] = worker.ID
				}
			}
		}
		runtime.Tasks[task.ID] = entry
	}
	if err := s.persistPlan(plan); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.plans[planID] = runtime
	s.mu.Unlock()
	pendingTasks := []routing.Task{}
	for _, task := range plan.Tasks {
		if task.State != string(session.StateCompleted) {
			pendingTasks = append(pendingTasks, task)
		}
	}
	summary, err := planGrantSummary(pendingTasks)
	if err != nil {
		return nil, err
	}
	if err := s.Store.RequestDecisionSummary(s.SessionID, planID, "plan", summary); err != nil {
		return nil, err
	}
	if err := s.Store.WaitDecision(ctx, s.SessionID, planID); err != nil {
		return nil, err
	}
	if err := s.acknowledgePlanQuota(ctx); err != nil {
		return nil, err
	}
	if err := s.Store.UpdateCheckpoint(s.SessionID, func(c *session.Checkpoint) error {
		if c.Recovery != nil {
			c.Recovery.Approved = true
		}
		return nil
	}); err != nil {
		return nil, err
	}
	_, err = s.Store.Update(s.SessionID, func(v *session.Session) error { v.RecoveryRequested = false; return nil })
	if err != nil {
		return nil, err
	}
	if s.AutomaticDispatch {
		s.queueAutomatic(planID)
	}
	return toolResult(map[string]any{"plan_id": planID, "completed_preserved": preview.Completed, "pending": preview.Pending, "recovered": true})
}
