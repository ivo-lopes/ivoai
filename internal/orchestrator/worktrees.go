package orchestrator

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workers"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func writeScopesOverlap(a, b []string) bool {
	for _, left := range a {
		for _, right := range b {
			if left == right || strings.HasPrefix(left, strings.TrimSuffix(right, "/")+"/") || strings.HasPrefix(right, strings.TrimSuffix(left, "/")+"/") {
				return true
			}
		}
	}
	return false
}

func (s *Server) prepareNativeRequest(ctx context.Context, planID, workerID string, task routing.Task, request workers.Request) (workers.Request, func(workers.Result) error, error) {
	complete := func(workers.Result) error { return nil }
	var releaseWorkspace func()
	prepared := false
	defer func() {
		if !prepared && releaseWorkspace != nil {
			releaseWorkspace()
		}
	}()
	profile, err := orchestration.CapabilityProfile(task.Role)
	if err != nil {
		return request, complete, err
	}
	write := profile.Write && len(task.WritePaths) > 0
	s.mu.Lock()
	plan := s.plans[planID]
	ancestors := map[string]bool{}
	dependencies := []string{}
	var visit func(string)
	visit = func(id string) {
		if ancestors[id] {
			return
		}
		ancestors[id] = true
		for _, dep := range plan.Tasks[id].Task.Dependencies {
			visit(dep)
		}
		if len(plan.Tasks[id].Task.WritePaths) > 0 {
			dependencies = append(dependencies, id)
		}
	}
	for _, id := range task.Dependencies {
		visit(id)
	}
	s.mu.Unlock()
	if write || len(dependencies) > 0 {
		plan.workMu.Lock()
		if plan.workIDs == nil {
			plan.workIDs = map[string]string{}
		}
		dependencyWorkIDs := make([]string, 0, len(dependencies))
		for _, dep := range dependencies {
			dependencyWorkIDs = append(dependencyWorkIDs, plan.workIDs[dep])
		}
		if plan.worktrees == nil && plan.sequential == nil {
			// Recovery data is outside the disposable session runtime. Failed or
			// conflicting work must survive closing the frontend.
			root := s.WorktreeRoot
			if root == "" {
				root = filepath.Join(s.Store.Root, "worktree-recovery")
			}
			if err = platform.EnsurePrivateDir(root); err == nil {
				plan.worktrees, err = orchestration.NewWorktrees(ctx, s.Directory, root)
			}
			if err != nil {
				plan.sequential = orchestration.NewSequentialPatch(s.Directory)
			}
		}
		manager := plan.worktrees
		sequential := plan.sequential
		plan.workMu.Unlock()
		if sequential != nil {
			releaseWorkspace, err = sequential.Acquire(ctx)
			if err != nil {
				return request, complete, err
			}
			request.PatchOnly = write
			write = false // The child remains sandboxed read-only in fallback.
			if request.PatchOnly {
				request.Task += "\n\nSequential fallback: return ONLY a complete unified Git diff for the approved write_paths, without fences or prose. Do not apply it yourself."
				complete = func(result workers.Result) error {
					if result.Truncated || result.ExitCode != 0 {
						return errors.New("WORKTREE_FAILED: incomplete sequential patch")
					}
					return sequential.Apply(ctx, result.Text, task.WritePaths)
				}
			}
			_, err = s.Store.Update(s.SessionID, func(v *session.Session) error {
				v.ParallelWriteDegraded = true
				return nil
			})
			if err != nil {
				return request, complete, err
			}
		} else {
			if manager == nil {
				return request, complete, errors.New("WORKTREE_FAILED: isolated writes unavailable")
			}
			// Every attempt owns a distinct checkout. Retrying a failed writer must
			// never erase its previous uncollected evidence or reuse its branch.
			w, err := manager.CreateWithDependencies(ctx, workerID, dependencyWorkIDs)
			if w.Path != "" {
				plan.workMu.Lock()
				plan.workIDs[task.ID] = workerID
				plan.workMu.Unlock()
				_, saveErr := s.Store.Update(s.SessionID, func(value *session.Session) error {
					if worker := findWorker(value, workerID); worker != nil {
						worker.WorktreePath, worker.WorktreeBranch, worker.WorktreeBase = w.Path, w.Branch, w.Base
					}
					return nil
				})
				if saveErr != nil {
					return request, complete, saveErr
				}
			}
			if err != nil {
				return request, complete, err
			}
			request.Directory = manager.WorkingDirectory(w)
			complete = func(workers.Result) error {
				collected, err := manager.Collect(ctx, workerID, task.WritePaths)
				if err != nil {
					return err
				}
				_, err = s.Store.Update(s.SessionID, func(value *session.Session) error {
					if worker := findWorker(value, workerID); worker != nil {
						worker.WorktreeCommit = collected.Commit
					}
					return nil
				})
				return err
			}
		}
	}
	// A native task never inherits the legacy session-wide knowledge envelope.
	// Host policy must explicitly project any requested MCP/skill capability.
	if s.PrepareWorker != nil {
		request.WorkerID = workerID
		request, err = s.PrepareWorker(ctx, task, request)
	} else if len(task.AllowedMCPs) > 0 || len(task.Skills) > 0 {
		err = errors.New("MCP_DENIED: task capability projection unavailable")
	}
	if err != nil {
		return request, complete, err
	}
	_, err = s.Store.Update(s.SessionID, func(value *session.Session) error {
		if worker := findWorker(value, workerID); worker != nil {
			worker.SelectedSkills = append([]string(nil), request.SelectedSkills...)
		}
		return nil
	})
	if err != nil {
		if request.Release != nil {
			request.Release()
			request.Release = nil
		}
		return request, complete, err
	}
	if request.Access == nil {
		request.Access, err = workers.NewAccess(request.Directory, write, nil)
	}
	if err == nil && releaseWorkspace != nil {
		prior := request.Release
		request.Release = func() {
			if prior != nil {
				prior()
			}
			releaseWorkspace()
		}
	}
	prepared = err == nil
	return request, complete, err
}

func (s *Server) integrate(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args struct {
		PlanID string `json:"plan_id"`
	}
	if strictArguments(request, &args) != nil {
		return nil, errors.New("valid plan_id is required")
	}
	if err := s.requireApprovedPlan(args.PlanID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	plan := s.plans[args.PlanID]
	if plan == nil {
		s.mu.Unlock()
		return nil, errors.New("plan unavailable")
	}
	ids := []string{}
	visited := map[string]bool{}
	var visit func(string)
	visit = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true
		for _, dependency := range plan.Tasks[id].Task.Dependencies {
			visit(dependency)
		}
		if len(plan.Tasks[id].Task.WritePaths) > 0 {
			ids = append(ids, id)
		}
	}
	for _, item := range plan.Plan.Tasks {
		task := plan.Tasks[item.ID]
		if task.Task.State != string(session.StateCompleted) || task.WorkerID != "" && !task.Settled {
			s.mu.Unlock()
			return nil, errors.New("plan acceptance not ready: tasks are incomplete or failed")
		}
		visit(item.ID)
	}
	s.mu.Unlock()
	plan.workMu.Lock()
	defer plan.workMu.Unlock()
	if plan.integrated {
		return toolResult(map[string]any{"plan_id": args.PlanID, "integrated": true})
	}
	if len(ids) > 0 && plan.sequential == nil {
		if plan.worktrees == nil {
			return nil, errors.New("WORKTREE_FAILED: no collected implementation work")
		}
		workIDs := make([]string, 0, len(ids))
		for _, id := range ids {
			workIDs = append(workIDs, plan.workIDs[id])
		}
		if _, err := plan.worktrees.Integrate(ctx, workIDs); err != nil {
			return nil, err
		}
		for _, w := range plan.worktrees.Snapshot() {
			if w.Commit == "" {
				continue
			} // Failed attempts remain recoverable.
			if err := plan.worktrees.Cleanup(ctx, w.TaskID); err != nil {
				return nil, err
			}
		}
	}
	plan.integrated = true
	_, err := s.Store.Update(s.SessionID, func(value *session.Session) error { value.CurrentPhase = "synthesizing"; return nil })
	if err != nil {
		return nil, err
	}
	return toolResult(map[string]any{"plan_id": args.PlanID, "integrated": true, "task_count": len(plan.Plan.Tasks)})
}
