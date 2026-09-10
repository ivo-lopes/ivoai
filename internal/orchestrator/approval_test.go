package orchestrator

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestPlanWaitsForUserDecisionAndCannotBeBypassed(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "approve"}[allow], func(t *testing.T) {
			root := t.TempDir()
			id, _ := session.NewID()
			now := time.Now().UTC()
			store := session.Store{Root: filepath.Join(root, "sessions")}
			value := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true, InitialPlanner: "codex", CurrentPrimary: "codex", PrimaryExecutor: "codex", WorkingDirectory: root, PrimaryModel: session.UnknownModel(), Workers: []session.Worker{}, MaxWorkers: 2, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected", State: session.StateRunning}
			value.SwarmID, value.RufloHealthy, value.RufloSafeMode = "swarm-fixture", true, true
			if err := store.Create(value); err != nil {
				t.Fatal(err)
			}
			s := &Server{Store: store, SessionID: id, RequirePlanApproval: true}
			s.initialize()
			task := taskFixture("a", nil)
			task["delegate"] = false
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			finished := make(chan error, 1)
			go func() { _, err := s.plan(ctx, toolRequest(map[string]any{"tasks": []any{task}})); finished <- err }()
			planID := ""
			for planID == "" && ctx.Err() == nil {
				v, err := store.Get(id)
				if err != nil {
					t.Fatal(err)
				}
				if len(v.Decisions) > 0 {
					planID = v.Decisions[0].ID
				} else {
					time.Sleep(10 * time.Millisecond)
				}
			}
			if planID == "" {
				t.Fatal("plan approval not presented")
			}
			select {
			case err := <-finished:
				t.Fatalf("plan returned before decision: %v", err)
			default:
			}
			if _, err := s.startTask(planID, "a", false); err == nil {
				t.Fatal("started unapproved task")
			}
			if _, err := s.spawnBatch(ctx, toolRequest(map[string]any{"plan_id": planID, "task_ids": []string{"a"}})); err == nil {
				t.Fatal("queued unapproved batch")
			}
			if _, err := s.primaryComplete(ctx, toolRequest(map[string]any{"plan_id": planID, "task_id": "a"})); err == nil {
				t.Fatal("completed unapproved primary task")
			}
			if _, err := s.delegate(ctx, toolRequest(map[string]any{})); err == nil {
				t.Fatal("unplanned delegation bypassed approval")
			}
			if err := store.ResolveDecision(id, planID, allow); err != nil {
				t.Fatal(err)
			}
			if err := <-finished; (err == nil) != allow {
				t.Fatalf("allow=%v error=%v", allow, err)
			}
			v, err := store.Get(id)
			if err != nil || len(v.Workers) != 0 {
				t.Fatal("planning spawned worker")
			}
			if !allow && v.CurrentPhase != "plan_rejected" {
				t.Fatalf("rejected plan phase = %q", v.CurrentPhase)
			}
		})
	}
}
