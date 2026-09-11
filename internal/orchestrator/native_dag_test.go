package orchestrator

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workers"
)

type isolatedFixtureAdapter struct {
	mu          sync.Mutex
	arrived     int
	gate        chan struct{}
	directories map[string]string
}

func (a *isolatedFixtureAdapter) Run(ctx context.Context, request workers.Request, observe func(workers.Observation)) (workers.Result, error) {
	if request.Access == nil {
		return workers.Result{}, errors.New("missing deny-by-default access")
	}
	a.mu.Lock()
	a.directories[request.Task] = request.Directory
	if request.Task != "validate" {
		a.arrived++
		if a.arrived == 2 {
			close(a.gate)
		}
	}
	a.mu.Unlock()
	if request.Task == "validate" {
		for _, file := range []string{"a.txt", "b.txt"} {
			body, err := os.ReadFile(filepath.Join(request.Directory, file))
			if err != nil || string(body) != file {
				return workers.Result{}, errors.New("dependency changes missing")
			}
		}
		return workers.Result{Text: "both features validated"}, nil
	}
	select {
	case <-ctx.Done():
		return workers.Result{}, ctx.Err()
	case <-a.gate:
	}
	file := request.Task + ".txt"
	err := os.WriteFile(filepath.Join(request.Directory, file), []byte(file), 0600)
	return workers.Result{Text: "feature implemented"}, err
}

func TestNativeDAGWorkersUseIsolatedWorktreesAndDependencyView(t *testing.T) {
	t.Run("manual-dispatch-compatibility", func(t *testing.T) { testNativeDAGWorktrees(t, false) })
	t.Run("automatic-dispatch", func(t *testing.T) { testNativeDAGWorktrees(t, true) })
}

func testNativeDAGWorktrees(t *testing.T, automatic bool) {
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if body, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture git: %v: %s", err, body)
		}
	}
	git("init")
	git("config", "user.name", "Fixture")
	git("config", "user.email", "fixture@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "protected.txt"), []byte("protected"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "protected.txt")
	git("-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "commit", "-m", "fixture")
	root := t.TempDir()
	store, id := automaticBridgeSession(t, root)
	_, err := store.Update(id, func(v *session.Session) error {
		v.Coordinator, v.SwarmID, v.WorkingDirectory, v.MaxWorkers = "native", "native_"+id, repo, 6
		v.RufloHealthy, v.RufloSafeMode, v.RufloEnabled = false, false, false
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter := &isolatedFixtureAdapter{gate: make(chan struct{}), directories: map[string]string{}}
	provider := quota.ProviderQuota{Provider: quota.ProviderCodex, Authenticated: true, Eligible: true, Source: "fixture", ObservedAt: time.Now()}
	s := &Server{Store: store, SessionID: id, NativePolicy: true, AutomaticDispatch: automatic, Parallelism: true, ParallelWrites: true, ProviderPreference: "auto",
		Directory: repo, RuntimeDir: root, WorktreeRoot: t.TempDir(), Adapter: adapter, Control: orchestration.NativeOrchestrator{Store: store, SessionID: id},
		HostResources: func() orchestration.HostResources {
			return orchestration.HostResources{CPUs: 8, AvailableMemoryBytes: 16 << 30}
		}, Weights: routing.DefaultWeights(),
		Registry: routing.Registry{Providers: map[string]routing.ProviderCapability{"codex": {Provider: "codex", Authenticated: true, WorkerCapable: true, Models: []routing.ModelCapability{{Name: "fixture-observed-model", Provider: "codex", Source: routing.SourceRuntimeVerified, CapabilityTier: routing.TierMax}}}}},
		Quota:    &quota.Manager{Store: quota.Store{Root: filepath.Join(root, "quota")}, Probes: map[quota.Provider]quota.Probe{quota.ProviderCodex: staticProbe{provider}}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	capability := s.Registry.Providers["codex"]
	capability.Capabilities = map[string]bool{"filesystem_read": true, "filesystem_write": true, "shell_read": true}
	s.Registry.Providers["codex"] = capability
	defer cancel()
	s.initializeContext(ctx)
	if err := s.authorized(); err != nil {
		t.Fatal(err)
	}
	tasks := []map[string]any{}
	for _, taskID := range []string{"a", "b", "validate"} {
		task := taskFixture(taskID, nil)
		delete(task, "preferred_executor")
		task["task"], task["acceptance"] = taskID, []string{"fixture feature returns expected result"}
		if taskID == "validate" {
			task["role"], task["dependencies"] = "review", []string{"a", "b"}
		} else {
			task["role"], task["write_paths"] = "implementation", []string{taskID + ".txt"}
		}
		tasks = append(tasks, task)
	}
	// Input order deliberately differs from DAG order.
	tasks[0], tasks[2] = tasks[2], tasks[0]
	result, err := s.plan(ctx, toolRequest(map[string]any{"tasks": tasks}))
	if err != nil {
		t.Fatal(err)
	}
	planID := result.StructuredContent.(map[string]any)["plan_id"].(string)
	if !automatic {
		if _, err := s.spawnBatch(ctx, toolRequest(map[string]any{"plan_id": planID, "task_ids": []string{"validate", "a", "b"}})); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.wait(ctx, toolRequest(map[string]any{"plan_id": planID, "task_ids": []string{"validate", "a", "b"}, "mode": "all", "timeout_seconds": 8})); err != nil {
		t.Fatal(err)
	}
	v, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, worker := range v.Workers {
		if worker.State != session.StateCompleted || worker.WorktreeCommit == "" || worker.LifecycleID == "" || worker.RufloTaskID != "" {
			t.Fatalf("incomplete native worker metadata: task=%s state=%s", worker.TaskID, worker.State)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("primary modified before integration")
	}
	if _, err := s.integrate(ctx, toolRequest(map[string]any{"plan_id": planID})); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"a.txt", "b.txt", "protected.txt"} {
		if _, err := os.Stat(filepath.Join(repo, file)); err != nil {
			t.Fatal(err)
		}
	}
	if body, err := os.ReadFile(filepath.Join(repo, "protected.txt")); err != nil || string(body) != "protected" {
		t.Fatal("protected fixture changed")
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.directories["a"] == adapter.directories["b"] || adapter.directories["a"] == repo || len(adapter.directories) != 3 {
		t.Fatal("writers did not use independent worktrees")
	}
}
