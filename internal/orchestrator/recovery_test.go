package orchestrator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestRecoveryCollectedInterruptedWriterKeepsCompletedTasks(t *testing.T) {
	store, id := recoveryFixture(t)
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture git: %v %s", err, output)
		}
	}
	git("init", "-q")
	git("config", "user.name", "Fixture")
	git("config", "user.email", "fixture@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "protected.txt"), []byte("intact"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "protected.txt")
	git("-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
	ctx := context.Background()
	identity, head, err := orchestration.RecoveryRepositoryIdentity(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	workRoot := filepath.Join(store.Root, "worktree-recovery")
	if err := os.MkdirAll(workRoot, 0700); err != nil {
		t.Fatal(err)
	}
	manager, err := orchestration.NewWorktrees(ctx, repo, workRoot)
	if err != nil {
		t.Fatal(err)
	}
	w, err := manager.Create(ctx, "worker_11111111111111111111111111111111")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Path, "feature.txt"), []byte("completed once"), 0600); err != nil {
		t.Fatal(err)
	}
	w, err = manager.Collect(ctx, w.TaskID, []string{"feature.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateCheckpoint(id, func(c *session.Checkpoint) error {
		c.Recovery.Directory, c.Recovery.RepositoryIdentity, c.Recovery.RepositoryHead = repo, identity, head
		c.Recovery.Plan.Tasks[2].Role = "implementation"
		c.Recovery.Plan.Tasks[2].WritePaths = []string{"feature.txt"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(id, func(v *session.Session) error {
		v.WorkingDirectory = repo
		v.Tasks[2].Role, v.Tasks[2].State = "implementation", session.StateFailed
		v.Workers = []session.Worker{{ID: w.TaskID, TaskID: "t3", Role: "implementation", Executor: "codex", State: session.StateFailed, Model: session.UnknownModel(), WorktreePath: w.Path, WorktreeBranch: w.Branch, WorktreeBase: w.Base, WorktreeCommit: w.Commit}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	preview, err := ReconcileRecovery(ctx, store, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Completed) != 3 || len(preview.Pending) != 1 || preview.Pending[0] != "t4" {
		t.Fatal("completed or collected work scheduled twice")
	}
	if _, err := preview.worktrees.Integrate(ctx, []string{w.TaskID}); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(filepath.Join(repo, "feature.txt")); string(body) != "completed once" {
		t.Fatal("recovered writer lost")
	}
}

func TestRecoveryDispatchesOnlyPendingAfterNewApproval(t *testing.T) {
	store, id := recoveryFixture(t)
	value, _ := store.Get(id)
	adapter := &timedAdapter{delay: time.Millisecond}
	provider := quota.ProviderQuota{Provider: quota.ProviderCodex, Authenticated: true, Eligible: true, Source: "fixture", ObservedAt: time.Now()}
	s := &Server{Store: store, SessionID: id, Directory: value.WorkingDirectory, RuntimeDir: filepath.Join(store.Root, "runtime", id), NativePolicy: true, AutomaticDispatch: true, Parallelism: true, Adapter: adapter, Control: orchestration.NativeOrchestrator{Store: store, SessionID: id},
		Registry: routing.Registry{Providers: map[string]routing.ProviderCapability{"codex": {Provider: "codex", Authenticated: true, WorkerCapable: true, Capabilities: map[string]bool{"filesystem_read": true}, Models: []routing.ModelCapability{{Name: "fixture", Provider: "codex", Source: routing.SourceRuntimeVerified, CapabilityTier: routing.TierMax}}}}},
		Quota:    &quota.Manager{Store: quota.Store{Root: filepath.Join(t.TempDir(), "quota")}, Probes: map[quota.Provider]quota.Probe{quota.ProviderCodex: staticProbe{provider}}},
	}
	checked := false
	s.CheckMCPGrants = func(_ context.Context, inputs []routing.TaskInput) (bool, error) {
		checked = len(inputs) == 2
		return true, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.initializeContext(ctx)
	done := make(chan error, 1)
	go func() { _, err := s.recoverPlan(ctx, toolRequest(map[string]any{})); done <- err }()
	newID := ""
	for newID == "" && ctx.Err() == nil {
		current, err := store.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		for _, decision := range current.Decisions {
			if decision.State == "pending" {
				newID = decision.ID
			}
		}
		if newID == "" {
			select {
			case err := <-done:
				t.Fatal("recovery exited before approval", err)
			default:
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if newID == "" {
		t.Fatal("new plan approval missing")
	}
	adapter.mu.Lock()
	started := len(adapter.starts)
	adapter.mu.Unlock()
	if started != 0 {
		t.Fatal("worker started before recovery approval")
	}
	if err := store.ResolveDecision(id, newID, true); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("fresh tool grants not checked")
	}
	for ctx.Err() == nil {
		current, _ := store.Get(id)
		complete := true
		for _, task := range current.Tasks {
			complete = complete && task.State == session.StateCompleted
		}
		if complete {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ctx.Err() != nil {
		t.Fatal("pending DAG did not complete")
	}
	adapter.mu.Lock()
	if len(adapter.starts) != 2 {
		t.Errorf("completed tasks repeated or pending tasks omitted: %d starts", len(adapter.starts))
	}
	adapter.mu.Unlock()
	if _, err := s.startTask(newID, "t1", false); err == nil {
		t.Fatal("completed task replay allowed")
	}
	if _, err := s.wait(ctx, toolRequest(map[string]any{"plan_id": newID, "task_ids": []string{"t3", "t4"}, "mode": "all", "timeout_seconds": 5})); err != nil {
		t.Fatal(err)
	}
	if _, err := s.integrate(ctx, toolRequest(map[string]any{"plan_id": newID})); err != nil {
		t.Fatal(err)
	}
}

func recoveryFixture(t *testing.T) (session.Store, string) {
	t.Helper()
	root := t.TempDir()
	store, id := automaticBridgeSession(t, root)
	identity, head, err := orchestration.RecoveryRepositoryIdentity(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	planID, err := newPlanID()
	if err != nil {
		t.Fatal(err)
	}
	plan := routing.Plan{ID: planID, Strategy: "efficient", CreatedAt: time.Now().UTC()}
	for _, taskID := range []string{"t1", "t2", "t3", "t4"} {
		task := routing.Task{TaskInput: routing.TaskInput{ID: taskID, Role: "research", Task: "Inspect the bounded fixture", Acceptance: []string{"report the fixture finding"}}, State: "planned", ExecutionMode: "worker", Tier: routing.TierLight}
		task.Profile = routing.ExecutionProfile{Provider: "codex", Model: "fixture", ModelSource: routing.SourceRuntimeVerified, EffortSource: routing.SourceUnsupported, Tier: routing.TierLight}
		task.Task += " " + taskID
		if taskID == "t1" || taskID == "t2" {
			task.State = string(session.StateCompleted)
		}
		if taskID == "t4" {
			task.Dependencies = []string{"t3"}
		}
		plan.Tasks = append(plan.Tasks, task)
	}
	_, err = store.Update(id, func(v *session.Session) error {
		v.WorkingDirectory, v.Coordinator, v.SwarmID = root, "native", "native_"+id
		v.PlanID, v.RecoveryRequested = plan.ID, true
		for _, task := range plan.Tasks {
			v.Tasks = append(v.Tasks, taskMetadata(task))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCheckpoint(id, session.Checkpoint{Objective: "Continue the fixture", Interrupted: true, Recovery: &session.RecoveryPlan{Approved: true, Plan: plan, Directory: root, RepositoryIdentity: identity, RepositoryHead: head}}); err != nil {
		t.Fatal(err)
	}
	return store, id
}

func TestRecoveryPreservesCompletedAndRefusesAmbiguousMutations(t *testing.T) {
	store, id := recoveryFixture(t)
	preview, err := ReconcileRecovery(context.Background(), store, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Completed) != 2 || len(preview.Pending) != 2 || preview.Plan.Tasks[0].State != "completed" {
		t.Fatal("completed work lost")
	}
	if err := store.UpdateCheckpoint(id, func(c *session.Checkpoint) error {
		c.Recovery.Plan.Tasks[2].WritePaths = []string{"feature.txt"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(id, func(v *session.Session) error { v.Tasks[2].State = session.StateRunning; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileRecovery(context.Background(), store, id); err == nil || err.Error() != "AMBIGUOUS_PREVIOUS_EXECUTION" {
		t.Fatalf("uncertain writer admitted: %v", err)
	}
	if err := store.UpdateCheckpoint(id, func(c *session.Checkpoint) error {
		c.Recovery.Plan.Tasks[2].WritePaths = nil
		c.Recovery.Plan.Tasks[2].AllowedMCPTools = map[string][]string{"fixture": {"write_tool"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileRecovery(context.Background(), store, id); err == nil || err.Error() != "AMBIGUOUS_PREVIOUS_EXECUTION" {
		t.Fatalf("uncertain tool call replayed: %v", err)
	}
}

func TestRecoveryCheckpointSurvivesRuntimeCleanupAndRequiresApproval(t *testing.T) {
	store, id := recoveryFixture(t)
	if err := os.MkdirAll(filepath.Join(store.Root, "runtime", id), 0700); err != nil {
		t.Fatal(err)
	}
	if err := store.CleanupRuntime(id); err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileRecovery(context.Background(), store, id); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateCheckpoint(id, func(c *session.Checkpoint) error { c.Recovery.Approved = false; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := ReconcileRecovery(context.Background(), store, id); err == nil || err.Error() != "PLAN_APPROVAL_REQUIRED" {
		t.Fatal("unapproved DAG recovered", err)
	}
}
