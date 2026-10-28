package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/codeintel"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/evidence"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workers"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

func TestProtectedCodePrefetchCreationFlagAndRestartRecoveryBoundary(t *testing.T) {
	t.Setenv("IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY", "1")
	t.Setenv(protectedEvidenceFlag, "")
	root := prefetchLiveRepository(t)
	a, _, id, task := prefetchLiveHost(t, root, "Inspect API mathpkg/scale.go")
	provider := &codeintel.Provider{Root: root, Scope: "cs_prefetch_fixture", Permissions: "filesystem.read", Allowed: true}
	query := core.CodeQuery{Operation: "skeleton", File: "mathpkg/scale.go", BudgetBytes: 3072}
	read := func() workingcontext.ArtifactRef {
		body, err := a.gatewayCodeEvidence(context.Background(), provider, id, task.ID, "", query)
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Artifact workingcontext.ArtifactRef `json:"artifact"`
		}
		if json.Unmarshal(body, &result) != nil || result.Artifact.Validate() != nil {
			t.Fatal("invalid code envelope")
		}
		return result.Artifact
	}
	legacy := read()
	if workingcontext.ProtectedArtifactID(legacy.ID) {
		t.Fatal("default namespace changed")
	}
	t.Setenv(protectedEvidenceFlag, "1")
	protected := read()
	if !workingcontext.ProtectedArtifactID(protected.ID) || protected.SHA256 != legacy.SHA256 {
		t.Fatal("host opt-in changed bytes or missed protected namespace")
	}
	t.Setenv(protectedEvidenceFlag, "")
	if _, _, err := a.recoverProtectedEvidence(context.Background(), workingcontext.Ownership{SessionID: id}, protected.ID); err != nil {
		t.Fatal("creation flag revoked an otherwise live bound reader", err)
	}
	restarted := &App{Store: a.Store}
	if _, _, err := restarted.recoverProtectedEvidence(context.Background(), workingcontext.Ownership{SessionID: id}, protected.ID); !errors.Is(err, evidence.ErrBindingUnavailable) {
		t.Fatal("restart/flagoff reconstructed a capability", err)
	}
}

func TestProtectedWorkerProjectionRequiresHostOwnershipAndSameWorktree(t *testing.T) {
	t.Setenv(protectedEvidenceFlag, "1")
	root := prefetchLiveRepository(t)
	a, store, id, _ := prefetchLiveHost(t, root, "inspect")
	v, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	reader := session.TaskMetadata{Role: "research"}
	workerID := "worker_" + strings.Repeat("a", 32)
	if !protectCodeEvidence(v, reader, workerID, root) || protectCodeEvidence(v, session.TaskMetadata{Role: "implementation"}, workerID, root) || protectCodeEvidence(v, reader, workerID, t.TempDir()) {
		t.Fatal("pilot escaped read-only/same-worktree capability envelope")
	}
	// The fixture issues a real binding from the SPI; no live provider, remote
	// credential, fake runtime pin or native model execution participates.
	local, err := workingcontext.NewLocalStore(filepath.Join(t.TempDir(), "artifacts"), workingcontext.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	a.evidenceGateway, err = evidence.New(local)
	if err != nil {
		t.Fatal(err)
	}
	r := evidence.Request{ProtectedRecovery: true, Owner: workingcontext.Ownership{SessionID: id, TaskID: "inspect", WorkerID: workerID}, Scope: evidence.Scope{Organization: evidence.Digest("fixture org"), Repository: evidence.Digest(root), Worktree: evidence.Digest("fixture tree"), Permissions: evidence.Digest("fixture read grant")}, Source: "code", ReadOnly: true, QueryDigest: evidence.Digest("fixture snapshot"), BudgetBytes: 32, Authorize: func(context.Context) error { return nil }, CurrentGeneration: func(context.Context) (string, error) { return evidence.Digest("immutable fixture snapshot"), nil }}
	p, err := a.evidenceGateway.Retrieve(context.Background(), r, func(context.Context) (evidence.Document, error) {
		return evidence.Document{Raw: []byte("fixture exact"), MediaType: "text/plain", Basis: evidence.Extracted, SourceComplete: true, Sensitivity: workingcontext.SensitivityRestricted}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"code_evidence": map[string]any{"artifact": p.Ref.Result.Artifact}})
	req := workers.Request{WorkerID: workerID, Directory: root, SharedContextBrief: string(body)}
	cfg, err := a.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	target, available, err := a.workerEvidenceProjection(cfg, v, req)
	if err != nil || !available || target.Local == nil || !target.Restricted || len(target.AllowedTools) != 2 {
		t.Fatal("host projection did not expose exactly the existing readers", err)
	}
	req.WorkerID = "worker_" + strings.Repeat("b", 32)
	if _, _, err := a.workerEvidenceProjection(cfg, v, req); err == nil {
		t.Fatal("foreign worker received projection")
	}
	req.WorkerID, req.Directory = workerID, t.TempDir()
	if _, _, err := a.workerEvidenceProjection(cfg, v, req); err == nil {
		t.Fatal("different worktree received projection")
	}
	entry := cfg.MCP.Servers[evidenceReaderMCP]
	entry.Enabled = true
	cfg.MCP.Servers[evidenceReaderMCP] = entry
	req.Directory = root
	if _, _, err := a.workerEvidenceProjection(cfg, v, req); err == nil {
		t.Fatal("configured MCP collision silently shadowed")
	}
}
