package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workers"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type timedAdapter struct {
	delay  time.Duration
	mu     sync.Mutex
	starts map[string]time.Time
	ends   map[string]time.Time
}

func (a *timedAdapter) Run(ctx context.Context, request workers.Request, observe func(workers.Observation)) (workers.Result, error) {
	a.mu.Lock()
	if a.starts == nil {
		a.starts, a.ends = map[string]time.Time{}, map[string]time.Time{}
	}
	a.starts[request.Task] = time.Now()
	a.mu.Unlock()
	if observe != nil {
		observe(workers.Observation{})
	}
	select {
	case <-ctx.Done():
		return workers.Result{}, ctx.Err()
	case <-time.After(a.delay):
	}
	a.mu.Lock()
	a.ends[request.Task] = time.Now()
	a.mu.Unlock()
	return workers.Result{Text: "result " + request.Task}, nil
}

type fakeLifecycle struct{}

func (fakeLifecycle) RegisterLifecycle(_ context.Context, _, id string) (string, error) {
	return "task-" + id[7:15], nil
}
func (fakeLifecycle) CancelLifecycle(context.Context, string) error { return nil }

type staticProbe struct{ value quota.ProviderQuota }

func (p staticProbe) Probe(context.Context) (quota.ProviderQuota, error) { return p.value, nil }

func TestSpawnBatchRunsIndependentTasksConcurrentlyAndHonorsDAG(t *testing.T) {
	root := t.TempDir()
	store := session.Store{Root: filepath.Join(root, "sessions")}
	id, _ := session.NewID()
	now := time.Now().UTC()
	available := func(provider quota.Provider) quota.ProviderQuota {
		return quota.ProviderQuota{Provider: provider, Authenticated: true, Eligible: true, Source: "fixture", ObservedAt: now}
	}
	value := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true, InitialPlanner: "codex", CurrentPrimary: "codex", PrimaryExecutor: "codex", WorkingDirectory: root, PrimaryModel: session.UnknownModel(), RufloEnabled: true, RufloHealthy: true, RufloSafeMode: true, SwarmID: "swarm-fixture", SwarmState: "active", Workers: []session.Worker{}, MaxWorkers: 2, ContextStatus: "ready", MemoryStatus: "ready", ServerStatus: "not-connected", State: session.StateRunning, Quota: map[quota.Provider]quota.ProviderQuota{}}
	if err := store.Create(value); err != nil {
		t.Fatal(err)
	}
	adapter := &timedAdapter{delay: 180 * time.Millisecond}
	registry := routing.Registry{Providers: map[string]routing.ProviderCapability{
		"codex":  {Provider: "codex", Authenticated: true, WorkerCapable: true, SupportsEffort: true, Models: []routing.ModelCapability{{Name: "runtime-codex", Provider: "codex", CapabilityTier: routing.TierMax, SupportedEfforts: []string{"low", "medium", "high", "max"}, IsDefault: true, Source: routing.SourceRuntimeVerified}}},
		"claude": {Provider: "claude", Authenticated: true, WorkerCapable: true, Models: []routing.ModelCapability{{Provider: "claude", IsDefault: true, Source: routing.SourceDefault}}},
	}}
	server := &Server{Store: store, SessionID: id, Directory: root, RuntimeDir: filepath.Join(root, "runtime"), ReviewExecutor: "codex", Adapter: adapter, Control: fakeLifecycle{}, Registry: registry, Weights: routing.DefaultWeights(), Parallelism: true, Quota: &quota.Manager{Store: quota.Store{Root: filepath.Join(root, "quota")}, Probes: map[quota.Provider]quota.Probe{quota.ProviderCodex: staticProbe{available(quota.ProviderCodex)}, quota.ProviderClaude: staticProbe{available(quota.ProviderClaude)}}}}
	server.initialize()

	planArgs := map[string]any{"tasks": []map[string]any{
		taskFixture("a", nil), taskFixture("b", nil), taskFixture("c", []string{"a"}), taskFixture("d", []string{"a", "b"}), taskFixture("e", []string{"d"}),
	}}
	planned, err := server.plan(context.Background(), toolRequest(planArgs))
	if err != nil {
		t.Fatal(err)
	}
	metadata := planned.StructuredContent.(map[string]any)
	planID := metadata["plan_id"].(string)
	start := time.Now()
	if _, err := server.spawnBatch(context.Background(), toolRequest(map[string]any{"plan_id": planID, "task_ids": []string{"a", "b", "c", "d", "e"}})); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("spawn_batch blocked for %s", elapsed)
	}
	if _, err := server.wait(context.Background(), toolRequest(map[string]any{"plan_id": planID, "task_ids": []string{"a", "b", "c", "d", "e"}, "mode": "all", "timeout_seconds": 5})); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Workers) != 5 {
		t.Fatalf("workers=%d", len(updated.Workers))
	}
	seenRefs := map[string]struct{}{}
	for _, worker := range updated.Workers {
		if len(worker.ResultRefs) != 1 {
			t.Fatalf("worker %s refs=%d", worker.ID, len(worker.ResultRefs))
		}
		ref := worker.ResultRefs[0].Artifact
		if ref.Owner.SessionID != id || ref.Owner.TaskID != worker.TaskID || ref.Owner.WorkerID != worker.ID {
			t.Fatalf("worker ref ownership mismatch: worker=%+v ref=%+v", worker, ref)
		}
		if _, duplicate := seenRefs[ref.ID]; duplicate {
			t.Fatalf("duplicate concurrent artifact ref %s", ref.ID)
		}
		seenRefs[ref.ID] = struct{}{}
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if delta := adapter.starts["A"].Sub(adapter.starts["B"]); delta > 75*time.Millisecond || delta < -75*time.Millisecond {
		t.Fatalf("A and B did not start concurrently: delta=%s", delta)
	}
	overlapStart := adapter.starts["A"]
	if adapter.starts["B"].After(overlapStart) {
		overlapStart = adapter.starts["B"]
	}
	overlapEnd := adapter.ends["A"]
	if adapter.ends["B"].Before(overlapEnd) {
		overlapEnd = adapter.ends["B"]
	}
	if overlap := overlapEnd.Sub(overlapStart); overlap < adapter.delay/2 {
		t.Fatalf("A and B did not overlap for long enough: overlap=%s delay=%s", overlap, adapter.delay)
	}
	if adapter.starts["C"].Before(adapter.ends["A"]) {
		t.Fatal("C started before A completed")
	}
	latestAB := adapter.ends["A"]
	if adapter.ends["B"].After(latestAB) {
		latestAB = adapter.ends["B"]
	}
	if adapter.starts["D"].Before(latestAB) {
		t.Fatal("D started before A and B completed")
	}
	if adapter.starts["E"].Before(adapter.ends["D"]) {
		t.Fatal("E started before D completed")
	}
}

func TestFirstTurnPlanRequiresBoundedSharedKnowledgeBootstrap(t *testing.T) {
	root := t.TempDir()
	store, id := automaticBridgeSession(t, root)
	server := &Server{Store: store, SessionID: id, RuntimeDir: filepath.Join(root, "runtime"), BootstrapRequired: true, Weights: routing.DefaultWeights()}
	server.initialize()
	if _, err := server.plan(context.Background(), toolRequest(map[string]any{"tasks": []map[string]any{taskFixture("a", nil)}})); err == nil {
		t.Fatal("plan accepted before bootstrap")
	}
	brief := map[string]any{"objective": "inspect project", "facts": []string{"known decision"}, "references": []string{"memory:1", "context:2"}, "memory_status": "ready", "context_status": "ready", "memory_lookup_performed": true, "context_lookup_performed": true}
	if _, err := server.bootstrap(context.Background(), toolRequest(brief)); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Get(id)
	if err != nil || !updated.KnowledgeBootstrap.Performed || updated.KnowledgeBootstrap.ReferenceCount != 2 {
		t.Fatalf("metadata=%+v err=%v", updated.KnowledgeBootstrap, err)
	}
}

func TestPlanKeepsTrivialDelegationInPrimary(t *testing.T) {
	root := t.TempDir()
	store, id := automaticBridgeSession(t, root)
	server := &Server{Store: store, SessionID: id, RuntimeDir: filepath.Join(root, "runtime"), Weights: routing.DefaultWeights(), Parallelism: true}
	server.initialize()
	task := map[string]any{
		"id": "typo", "role": "editor", "task": "Correct one word", "delegate": true,
		"scores": map[string]any{"complexity": 5, "risk": 5, "reasoning_depth": 5, "context_breadth": 5, "verification_need": 5, "parallel_value": 5, "latency_sensitivity": 50},
	}
	result, err := server.plan(context.Background(), toolRequest(map[string]any{"tasks": []map[string]any{task}}))
	if err != nil {
		t.Fatal(err)
	}
	metadata := result.StructuredContent.(map[string]any)
	tasks := metadata["tasks"].([]map[string]any)
	if got := tasks[0]["execution_mode"]; got != "primary" {
		t.Fatalf("trivial task execution_mode=%v metadata=%+v", got, tasks[0])
	}
	if tasks[0]["delegation_benefit"].(int) >= tasks[0]["delegation_overhead"].(int) {
		t.Fatalf("economic decision is inconsistent: %+v", tasks[0])
	}
}

func TestWaitRequiresExplicitPrimaryCompletion(t *testing.T) {
	root := t.TempDir()
	store, id := automaticBridgeSession(t, root)
	server := &Server{Store: store, SessionID: id, RuntimeDir: filepath.Join(root, "runtime"), Weights: routing.DefaultWeights(), Parallelism: true}
	server.initialize()
	task := map[string]any{
		"id": "primary", "role": "writer", "task": "Apply the authoritative change", "delegate": false,
		"scores": map[string]any{"complexity": 50, "risk": 40, "reasoning_depth": 40, "context_breadth": 30, "verification_need": 50, "parallel_value": 10, "latency_sensitivity": 50},
	}
	result, err := server.plan(context.Background(), toolRequest(map[string]any{"tasks": []map[string]any{task}}))
	if err != nil {
		t.Fatal(err)
	}
	planID := result.StructuredContent.(map[string]any)["plan_id"].(string)
	server.mu.Lock()
	ready, err := server.waitReadyLocked(planID, []string{"primary"}, "all")
	server.mu.Unlock()
	if err != nil || ready {
		t.Fatalf("pending primary task reported complete: ready=%v err=%v", ready, err)
	}
	if _, err := server.primaryComplete(context.Background(), toolRequest(map[string]any{"plan_id": planID, "task_id": "primary"})); err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	ready, err = server.waitReadyLocked(planID, []string{"primary"}, "all")
	server.mu.Unlock()
	if err != nil || !ready {
		t.Fatalf("completed primary task did not unblock wait: ready=%v err=%v", ready, err)
	}
}

func TestStrictArgumentsRejectsTrailingJSON(t *testing.T) {
	request := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: []byte(`{"plan_id":"plan_a"}{"extra":true}`)}}
	var value struct {
		PlanID string `json:"plan_id"`
	}
	if err := strictArguments(request, &value); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
}

func TestRelatedTurnUsesDeltaPlanWithoutRepeatingBootstrap(t *testing.T) {
	root := t.TempDir()
	store, id := automaticBridgeSession(t, root)
	server := &Server{Store: store, SessionID: id, RuntimeDir: filepath.Join(root, "runtime"), BootstrapRequired: true, Weights: routing.DefaultWeights(), Parallelism: true}
	server.initialize()
	brief := map[string]any{"objective": "inspect project", "references": []string{"memory:1", "context:2"}, "memory_status": "ready", "context_status": "ready", "memory_lookup_performed": true, "context_lookup_performed": true}
	if _, err := server.bootstrap(context.Background(), toolRequest(brief)); err != nil {
		t.Fatal(err)
	}
	before, _ := store.Get(id)
	trivial := map[string]any{"id": "delta", "role": "primary", "task": "Answer related follow-up", "delegate": false, "scores": map[string]any{"complexity": 10, "risk": 5, "reasoning_depth": 10, "context_breadth": 10, "verification_need": 10, "parallel_value": 0, "latency_sensitivity": 80}}
	if _, err := server.plan(context.Background(), toolRequest(map[string]any{"tasks": []map[string]any{trivial}})); err != nil {
		t.Fatal(err)
	}
	after, _ := store.Get(id)
	if !after.KnowledgeBootstrap.Performed || after.KnowledgeBootstrap.BriefHash != before.KnowledgeBootstrap.BriefHash || !after.KnowledgeBootstrap.UpdatedAt.Equal(*before.KnowledgeBootstrap.UpdatedAt) {
		t.Fatalf("related delta plan unexpectedly refreshed bootstrap: before=%+v after=%+v", before.KnowledgeBootstrap, after.KnowledgeBootstrap)
	}
}

func taskFixture(id string, dependencies []string) map[string]any {
	return map[string]any{"id": id, "role": "analyst", "task": stringsUpper(id), "dependencies": dependencies, "parallel_group": "g", "scores": map[string]any{"complexity": 20, "risk": 10, "reasoning_depth": 20, "context_breadth": 10, "verification_need": 20, "parallel_value": 90, "latency_sensitivity": 80}, "preferred_executor": "codex", "delegate": true}
}

func stringsUpper(value string) string { return fmt.Sprintf("%c", value[0]-32) }

func toolRequest(value any) *mcp.CallToolRequest {
	body, _ := json.Marshal(value)
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: body}}
}
