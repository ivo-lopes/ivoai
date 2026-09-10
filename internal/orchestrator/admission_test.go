package orchestrator

import (
	"context"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestBatchAdmissionFailureIsVisibleWithoutWorker(t *testing.T) {
	store, id := automaticBridgeSession(t, t.TempDir())
	task := routing.Task{TaskInput: routing.TaskInput{ID: "read", Role: "research", Task: "read fixture"}, State: "planned", Tier: routing.TierLight, ExecutionMode: "worker", Profile: routing.ExecutionProfile{Provider: "codex", ModelSource: routing.SourceDefault}}
	planID, err := newPlanID()
	if err != nil {
		t.Fatal(err)
	}
	plan := routing.Plan{ID: planID, Strategy: "efficient", Tasks: []routing.Task{task}}
	s := &Server{Store: store, SessionID: id, Parallelism: true}
	s.initialize()
	s.plans[plan.ID] = &runtimePlan{Plan: plan, Tasks: map[string]*runtimeTask{task.ID: {Task: task}}, Workers: map[string]string{}}
	// Missing runtime is a permanent admission error, not a resource wait.
	if _, err := s.spawnBatch(context.Background(), toolRequest(map[string]any{"plan_id": plan.ID, "task_ids": []string{task.ID}})); err != nil {
		t.Fatal(err)
	}
	current := s.plans[plan.ID].Tasks[task.ID]
	if current.Task.State != "failed" || current.Queued || !current.Settled {
		t.Fatal("permanent admission failure silently queued")
	}
	v, err := store.Get(id)
	if err != nil || len(v.Workers) != 0 || len(v.Tasks) != 1 || v.Tasks[0].State != session.StateFailed {
		t.Fatalf("failure metadata not visible: %v", err)
	}
}
