package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/codeintel"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

func TestEvidenceGatewayCodeReuseInvalidationAndAdmission(t *testing.T) {
	t.Setenv("IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY", "1")
	root := prefetchLiveRepository(t)
	a, store, id, task := prefetchLiveHost(t, root, "Inspect API mathpkg/scale.go")
	provider := &codeintel.Provider{Root: root, Scope: "cs_prefetch_fixture", Permissions: "filesystem.read", Allowed: true}
	inspections := 0
	provider.Observe = func(core.CodeResult, error) { inspections++ }
	query := core.CodeQuery{Operation: "skeleton", File: "mathpkg/scale.go", BudgetBytes: 3072}
	type envelope struct {
		Text     string                     `json:"text"`
		Hit      bool                       `json:"evidence_cache_hit"`
		Artifact workingcontext.ArtifactRef `json:"artifact"`
	}
	read := func(ownerTask, worker string) (envelope, error) {
		body, err := a.prefetchCodeEvidence(context.Background(), provider, id, ownerTask, worker, query)
		var v envelope
		if err == nil {
			err = json.Unmarshal(body, &v)
			if err == nil {
				saved, loadErr := store.Get(id)
				if loadErr != nil {
					return v, loadErr
				}
				for _, event := range saved.Observability {
					if event.Capability == "evidence.code" && event.TaskID == ownerTask && event.CacheHit != nil {
						v.Hit = *event.CacheHit
					}
				}
			}
		}
		return v, err
	}
	first, err := read(task.ID, "")
	if err != nil || first.Hit || !strings.Contains(first.Text, "x * 2") || first.Artifact.Truncated {
		t.Fatalf("first retrieval: %+v, %v", first, err)
	}
	second, err := read(task.ID, "")
	if err != nil || !second.Hit || second.Text != first.Text || inspections != 1 {
		t.Fatalf("retrieval not reused: %+v, %v, calls=%d", second, err, inspections)
	}
	workerID := "worker_" + strings.Repeat("a", 32)
	_, err = store.Update(id, func(v *session.Session) error {
		v.Tasks = append(v.Tasks, session.TaskMetadata{ID: "worker_read", Role: "research", Tier: "LIGHT", Model: session.UnknownModel(), State: session.StateStarting, ExecutionMode: "worker"})
		v.Workers = append(v.Workers, session.Worker{ID: workerID, TaskID: "worker_read", Executor: "codex", Role: "research", Model: session.UnknownModel(), State: session.StateStarting, StartedAt: time.Now().UTC()})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := read("worker_read", workerID)
	if err != nil || !worker.Hit || worker.Text != first.Text || inspections != 1 {
		t.Fatalf("primary/worker reuse failed: %+v %v calls=%d", worker, err, inspections)
	}
	if worker.Artifact.ID == first.Artifact.ID || worker.Artifact.Owner.WorkerID != workerID {
		t.Fatal("sharing widened artifact ownership")
	}
	assertCodeOwnerIsolation(t, a, worker.Artifact.Owner, first.Artifact.ID)
	if err := os.WriteFile(filepath.Join(root, "mathpkg/scale.go"), []byte("package mathpkg\nfunc Scale(x int) int { return x * 3 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := read(task.ID, "")
	if err != nil || changed.Hit || !strings.Contains(changed.Text, "x * 3") || inspections != 2 {
		t.Fatalf("source invalidation failed: %+v %v", changed, err)
	}
	_, err = store.Update(id, func(v *session.Session) error {
		v.Tasks[0].AllowedMCPTools = map[string][]string{nativeCodeMCP: {}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := read(task.ID, ""); err == nil || inspections != 2 {
		t.Fatal("revoked grant reached retrieval/cache")
	}
	_, err = store.Update(id, func(v *session.Session) error {
		v.Tasks[0].AllowedMCPTools = nil
		v.KnowledgeScopeID = "cs_other_org"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := read(task.ID, ""); err == nil || inspections != 2 {
		t.Fatal("scope change reused evidence")
	}
}

func TestExperimentalGatewayPreservesPrimaryContract(t *testing.T) {
	t.Setenv("IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY", "1")
	root := prefetchLiveRepository(t)
	a, store, id, task := prefetchLiveHost(t, root, "Inspect API mathpkg/scale.go")
	factory := func(_ context.Context, root, scope string) (*codeintel.Provider, error) {
		return &codeintel.Provider{Root: root, Scope: scope, Permissions: "filesystem.read", Allowed: true}, nil
	}
	for range 2 {
		body, err := a.preparePrimaryCodeEvidence(context.Background(), store, id, task, 4094, factory)
		if err != nil || len(body) > 4094 {
			t.Fatalf("primary envelope failed: %v", err)
		}
		value := assertExactCodeEnvelope(t, a, body, workingcontext.Ownership{SessionID: id, TaskID: task.ID})
		if !strings.Contains(value.Text, "Scale") {
			t.Fatal("primary exact-artifact compatibility lost")
		}
	}
	value, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	codeCalls, gatewayCalls := 0, 0
	for _, event := range value.Observability {
		if event.Operation == observability.OperationCodeInspect {
			codeCalls++
		}
		if event.Capability == "evidence.code" {
			gatewayCalls++
		}
	}
	if codeCalls != 1 || gatewayCalls != 2 {
		t.Fatalf("reuse not observable: %d/%d", codeCalls, gatewayCalls)
	}
}

func TestGatewayImmediatePlanRetainsApprovalBoundary(t *testing.T) {
	t.Setenv("IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY", "1")
	root := prefetchLiveRepository(t)
	a, store, id, task := prefetchLiveHost(t, root, "Inspect API mathpkg/scale.go")
	cfg, err := a.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Orchestration.Auto.PlanExecution = "immediate"
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(id, func(v *session.Session) error { v.Decisions = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	p := &codeintel.Provider{Root: root, Scope: "cs_prefetch_fixture", Permissions: "filesystem.read", Allowed: true}
	q := core.CodeQuery{Operation: "skeleton", File: "mathpkg/scale.go", BudgetBytes: 3072}
	if _, err := a.prefetchCodeEvidence(context.Background(), p, id, task.ID, "", q); err != nil {
		t.Fatal("immediate read lost existing admission:", err)
	}
	if _, err := store.Update(id, func(v *session.Session) error {
		v.Decisions = []session.Decision{{ID: v.PlanID, Kind: "plan", State: "pending", CreatedAt: time.Now().UTC()}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.prefetchCodeEvidence(context.Background(), p, id, task.ID, "", q); err == nil {
		t.Fatal("immediate mode bypassed a pending decision via cache")
	}
}

func TestGatewayWorkerMonorepoDirectoryAndRevocation(t *testing.T) {
	t.Setenv("IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY", "1")
	root := prefetchLiveRepository(t)
	for _, args := range [][]string{{"-C", root, "add", "."}, {"-C", root, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "synthetic baseline"}} {
		if err := exec.Command("git", args...).Run(); err != nil {
			t.Fatal(err)
		}
	}
	worktree := filepath.Join(t.TempDir(), "owned")
	if err := exec.Command("git", "-C", root, "worktree", "add", "--detach", "-q", worktree, "HEAD").Run(); err != nil {
		t.Fatal(err)
	}
	a, store, id, task := prefetchLiveHost(t, filepath.Join(root, "mathpkg"), "Inspect API scale.go")
	workerID := "worker_" + strings.Repeat("b", 32)
	if _, err := store.Update(id, func(v *session.Session) error {
		v.Tasks[0].ExecutionMode, v.Tasks[0].State = "worker", session.StateStarting
		v.Workers = []session.Worker{{ID: workerID, TaskID: task.ID, WorktreePath: worktree, Executor: "codex", Role: "research", Model: session.UnknownModel(), State: session.StateStarting, StartedAt: time.Now().UTC()}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	p := &codeintel.Provider{Root: filepath.Join(worktree, "mathpkg"), Scope: "cs_prefetch_fixture", Permissions: "filesystem.read", Allowed: true}
	q := core.CodeQuery{Operation: "skeleton", File: "scale.go", BudgetBytes: 3072}
	if _, err := a.prefetchCodeEvidence(context.Background(), p, id, task.ID, workerID, q); err != nil {
		t.Fatal("authorized worker subdirectory denied:", err)
	}
	p.Root = filepath.Join(root, "mathpkg")
	if _, err := a.prefetchCodeEvidence(context.Background(), p, id, task.ID, workerID, q); err == nil {
		t.Fatal("worker read original checkout instead of owned worktree")
	}
	p.Root = filepath.Join(worktree, "mathpkg")
	if _, err := store.Update(id, func(v *session.Session) error { v.Workers[0].State = session.StateStopping; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := a.prefetchCodeEvidence(context.Background(), p, id, task.ID, workerID, q); err == nil {
		t.Fatal("stopped worker reused cached evidence")
	}
}
