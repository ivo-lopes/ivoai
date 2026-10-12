package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func primaryEvidenceServer(t *testing.T) *Server {
	t.Helper()
	id, _ := session.NewID()
	now := time.Now().UTC()
	root := t.TempDir()
	store := session.Store{Root: filepath.Join(root, "sessions")}
	value := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true, CurrentPrimary: "codex", InitialPlanner: "codex", PrimaryExecutor: "codex", WorkingDirectory: root, PrimaryModel: session.UnknownModel(), MaxWorkers: 2, State: session.StateRunning, Coordinator: "native", SwarmID: "native_" + id, MemoryStatus: "disabled", ContextStatus: "disabled", ServerStatus: "not-connected"}
	if err := store.Create(value); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: store, SessionID: id, Directory: root, NativePolicy: true}
	s.Quota = &quota.Manager{Store: quota.Store{Root: filepath.Join(root, "quota")}, Probes: map[quota.Provider]quota.Probe{quota.ProviderCodex: staticProbe{quota.ProviderQuota{Provider: quota.ProviderCodex, Authenticated: true, Eligible: true, Source: "fixture", ObservedAt: now}}}}
	s.initialize()
	return s
}
func primaryEvidenceTask(id string, deps []string) map[string]any {
	task := taskFixture(id, deps)
	task["role"] = "research"
	task["delegate"] = false
	task["acceptance"] = []string{"fixture remains readable"}
	return task
}
func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result == nil || len(result.Content) == 0 {
		t.Fatal("missing result")
	}
	return result.Content[0].(*mcp.TextContent).Text
}
func TestPlanAutomaticallyDeliversPrimaryEvidenceOnce(t *testing.T) {
	s := primaryEvidenceServer(t)
	calls := 0
	s.PreparePrimaryEvidence = func(ctx context.Context, task routing.Task, budget int) (json.RawMessage, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 20*time.Second {
			t.Fatal("missing total prefetch time budget")
		}
		calls++
		if budget > 4096 {
			t.Fatal("budget too large")
		}
		return json.RawMessage(`{"text":"exact fixture source","untrusted":true,"fidelity":"exact_required"}`), nil
	}
	result, err := s.plan(context.Background(), toolRequest(map[string]any{"tasks": []any{primaryEvidenceTask("a", nil), primaryEvidenceTask("b", []string{"a"})}}))
	if err != nil {
		t.Fatal(err)
	}
	text := resultText(t, result)
	if !strings.Contains(text, "exact fixture source") || calls != 1 {
		t.Fatalf("automatic primary evidence missing: calls=%d %s", calls, text)
	}
	v, _ := s.Store.Get(s.SessionID)
	result, err = s.wait(context.Background(), toolRequest(map[string]any{"plan_id": v.PlanID, "task_ids": []string{"a"}, "mode": "all"}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(resultText(t, result), "exact fixture source") || calls != 1 {
		t.Fatal("repeated evidence")
	}
	if _, err = s.primaryComplete(context.Background(), toolRequest(map[string]any{"plan_id": v.PlanID, "task_id": "a"})); err != nil {
		t.Fatal(err)
	}
	result, err = s.wait(context.Background(), toolRequest(map[string]any{"plan_id": v.PlanID, "task_ids": []string{"b"}, "mode": "all"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resultText(t, result), "exact fixture source") || calls != 2 {
		t.Fatal("newly runnable primary not inspected")
	}
	saved, _ := s.Store.Get(s.SessionID)
	body, _ := json.Marshal(saved)
	if strings.Contains(string(body), "exact fixture source") {
		t.Fatal("source persisted in session")
	}
}
func TestPrimaryEvidenceWaitCannotBypassApproval(t *testing.T) {
	s := primaryEvidenceServer(t)
	s.RequirePlanApproval = true
	var calls atomic.Int32
	s.PreparePrimaryEvidence = func(context.Context, routing.Task, int) (json.RawMessage, error) {
		calls.Add(1)
		return json.RawMessage(`{"text":"source"}`), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := s.plan(ctx, toolRequest(map[string]any{"tasks": []any{primaryEvidenceTask("a", nil)}}))
		done <- err
	}()
	planID := ""
	for planID == "" && ctx.Err() == nil {
		v, _ := s.Store.Get(s.SessionID)
		if len(v.Decisions) > 0 {
			planID = v.Decisions[0].ID
		} else {
			time.Sleep(time.Millisecond)
		}
	}
	if planID == "" {
		t.Fatal("missing approval")
	}
	if _, err := s.wait(ctx, toolRequest(map[string]any{"plan_id": planID, "task_ids": []string{"a"}, "mode": "all"})); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("preapproval fetch")
	}
	if err := s.Store.ResolveDecision(s.SessionID, planID, false); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("rejected plan accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("rejected plan fetched")
	}
}
func TestPrimaryEvidenceFailureDegradesWithoutRawError(t *testing.T) {
	s := primaryEvidenceServer(t)
	s.PreparePrimaryEvidence = func(context.Context, routing.Task, int) (json.RawMessage, error) {
		return nil, errors.New("private source secret")
	}
	result, err := s.plan(context.Background(), toolRequest(map[string]any{"tasks": []any{primaryEvidenceTask("a", nil)}}))
	if err != nil {
		t.Fatal(err)
	}
	text := resultText(t, result)
	if strings.Contains(text, "private source secret") || !strings.Contains(text, "degraded") {
		t.Fatalf("unsafe degradation: %s", text)
	}
}

func TestPrimaryEvidenceAggregateBudgetDoesNotDropDeferredTasks(t *testing.T) {
	s := primaryEvidenceServer(t)
	calls := 0
	s.PreparePrimaryEvidence = func(_ context.Context, task routing.Task, budget int) (json.RawMessage, error) {
		calls++
		return json.Marshal(map[string]string{"task_id": task.ID, "text": strings.Repeat("x", 1800)})
	}
	result, err := s.plan(context.Background(), toolRequest(map[string]any{"tasks": []any{primaryEvidenceTask("a", nil), primaryEvidenceTask("b", nil), primaryEvidenceTask("c", nil)}}))
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(resultText(t, result)), &body); err != nil {
		t.Fatal(err)
	}
	if len(body["primary_code_evidence"]) > 4096 || calls != 2 {
		t.Fatalf("bad aggregate budget/calls %d %d", len(body["primary_code_evidence"]), calls)
	}
	value, _ := s.Store.Get(s.SessionID)
	result, err = s.wait(context.Background(), toolRequest(map[string]any{"plan_id": value.PlanID, "task_ids": []string{"c"}, "mode": "all"}))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || !strings.Contains(resultText(t, result), `"task_id":"c"`) {
		t.Fatal("budget-deferred primary lost")
	}
}

func TestPrimaryEvidenceConcurrentWaitIsDeduplicatedAndRevocationDiscards(t *testing.T) {
	s := primaryEvidenceServer(t)
	if _, err := s.plan(context.Background(), toolRequest(map[string]any{"tasks": []any{primaryEvidenceTask("a", nil)}})); err != nil {
		t.Fatal(err)
	}
	value, _ := s.Store.Get(s.SessionID)
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s.PreparePrimaryEvidence = func(context.Context, routing.Task, int) (json.RawMessage, error) {
		calls.Add(1)
		close(entered)
		<-release
		return json.RawMessage(`{"text":"private evidence"}`), nil
	}
	done := make(chan map[string]any, 1)
	go func() { done <- s.primaryEvidence(context.Background(), value.PlanID, map[string]any{}) }()
	<-entered
	if got := s.primaryEvidence(context.Background(), value.PlanID, map[string]any{}); len(got) != 0 || calls.Load() != 1 {
		t.Fatal("duplicate concurrent retrieval")
	}
	if _, err := s.Store.Update(s.SessionID, func(value *session.Session) error { value.State = session.StateStopping; return nil }); err != nil {
		t.Fatal(err)
	}
	close(release)
	if got := <-done; len(got) != 0 {
		t.Fatal("revoked evidence returned")
	}
}
