package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"sync"

	"github.com/ivo-lopes/ivoai/internal/codeintel"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/evidence"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

// Initialization only; App is copied by existing callers and must not embed a lock.
var evidenceGatewayInitMu sync.Mutex

// This opt-in adapter deliberately handles only already-admitted code prefetch.
// It does not introduce an MCP server, scheduler, source fan-out or config write.
func (a *App) gatewayCodeEvidence(ctx context.Context, provider *codeintel.Provider, id, taskID, workerID string, query core.CodeQuery) (json.RawMessage, error) {
	if provider == nil || !provider.Allowed {
		return nil, evidence.ErrDenied
	}
	query, err := codeintel.Normalize(query)
	if err != nil || query.Operation == "build" || query.Operation == "viz" {
		return nil, evidence.ErrDenied
	}
	// One retrieval budget makes equivalent primary/worker queries reusable and
	// reserves space for the established exact-artifact envelope.
	query.BudgetBytes = min(query.BudgetBytes, 3072)
	identity, err := codeintel.Identity(provider.Root, provider.Scope, provider.Permissions)
	if err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(provider.Root)
	if err != nil {
		return nil, err
	}
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	initial, err := store.Get(id)
	if err != nil {
		return nil, err
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return nil, err
	}
	var expected session.TaskMetadata
	for _, task := range initial.Tasks {
		if task.ID == taskID {
			expected = task
		}
	}
	if expected.ID == "" {
		return nil, evidence.ErrDenied
	}
	permissionDigest := evidence.Digest(provider.Permissions, expected.AllowedMCPTools, expected.Skills, cfg.Skills)
	authorize := func(ctx context.Context) error {
		current, err := store.Get(id)
		if err != nil || !provider.Allowed || !current.Active() || current.State == session.StateStopping || current.State == session.StateWaiting || current.PlanID != initial.PlanID || current.KnowledgeScopeID != provider.Scope {
			return evidence.ErrDenied
		}
		latest, err := a.Store.Load()
		if err != nil || evidence.Digest(latest.Skills) != evidence.Digest(cfg.Skills) {
			return evidence.ErrDenied
		}
		if workerID == "" {
			if allowed, _ := primaryMCPGrant(current, latest, nativeCodeMCP, "code_inspect"); !allowed {
				return evidence.ErrDenied
			}
		} else if !approvedPlan(current) {
			return evidence.ErrDenied
		}
		rootAllowed := false
		if workerID == "" {
			canonical, err := filepath.EvalSymlinks(current.WorkingDirectory)
			rootAllowed = err == nil && canonical == root
		} else {
			for _, worker := range current.Workers {
				if worker.ID == workerID && worker.TaskID == taskID && (worker.State == session.StateStarting || worker.State == session.StateRunning) {
					directory := worker.WorktreePath
					if directory == "" {
						directory = current.WorkingDirectory
					}
					canonical, err := filepath.EvalSymlinks(directory)
					if err == nil {
						relative, err := filepath.Rel(canonical, root)
						// The host may launch a worker in the session's monorepo
						// subdirectory. It must remain inside its owned worktree.
						rootAllowed = err == nil && filepath.IsLocal(relative)
					}
				}
			}
		}
		if !rootAllowed {
			return evidence.ErrDenied
		}
		completed := map[string]bool{}
		for _, task := range current.Tasks {
			completed[task.ID] = task.State == session.StateCompleted
		}
		for _, task := range current.Tasks {
			if task.ID != taskID {
				continue
			}
			if workerID == "" && (task.ExecutionMode != "primary" || task.State != session.StatePrimary) {
				return evidence.ErrDenied
			}
			if workerID != "" && (task.ExecutionMode != "worker" || (task.State != session.StateStarting && task.State != session.StateRunning)) {
				return evidence.ErrDenied
			}
			if evidence.Digest(provider.Permissions, task.AllowedMCPTools, task.Skills, latest.Skills) != permissionDigest {
				return evidence.ErrDenied
			}
			if allowed, restricted := task.AllowedMCPTools[nativeCodeMCP]; restricted && !slices.Contains(allowed, "code_inspect") {
				return evidence.ErrDenied
			}
			for _, dependency := range task.Dependencies {
				if !completed[dependency] {
					return evidence.ErrDenied
				}
			}
			return ctx.Err()
		}
		return evidence.ErrDenied
	}
	request := evidence.Request{ProtectedRecovery: protectCodeEvidence(initial, expected, workerID, root), Owner: workingcontext.Ownership{SessionID: id, TaskID: taskID, WorkerID: workerID}, Source: "code", ReadOnly: true, BudgetBytes: query.BudgetBytes,
		Scope:       evidence.Scope{Organization: evidence.Digest(provider.Scope), Repository: evidence.Digest(root), Worktree: identity, Permissions: permissionDigest},
		QueryDigest: evidence.Digest(query, codeintel.Version, provider.Distribution, provider.Node, map[string]string{"execution_mode": provider.ExecutionMode()}), Authorize: authorize,
		CurrentGeneration: func(ctx context.Context) (string, error) {
			files, err := codeintel.Inventory(ctx, provider.Root)
			if err != nil {
				return "", err
			}
			snapshot, err := codeintel.Snapshot(provider.Root, files)
			return snapshot.Digest, err
		}}
	evidenceGatewayInitMu.Lock()
	if a.evidenceGateway == nil {
		artifacts, storeErr := workingcontext.NewLocalStore(filepath.Join(a.Store.Paths.CacheDir, "working-context"), workingcontext.LocalOptions{})
		if storeErr == nil {
			a.evidenceGateway, storeErr = evidence.New(artifacts)
		}
		if storeErr != nil {
			evidenceGatewayInitMu.Unlock()
			return nil, storeErr
		}
	}
	gateway := a.evidenceGateway
	evidenceGatewayInitMu.Unlock()
	packet, err := gateway.Retrieve(ctx, request, func(ctx context.Context) (evidence.Document, error) {
		result, err := provider.Inspect(ctx, query)
		if err != nil {
			return evidence.Document{}, err
		}
		if !result.Fresh || !result.Untrusted || len(result.Text) > query.BudgetBytes {
			return evidence.Document{}, errors.New("code evidence incomplete or outside budget")
		}
		return evidence.Document{Raw: []byte(result.Text), MediaType: "text/plain; charset=utf-8", Basis: evidence.Extracted, SourceComplete: !result.Truncated, Sensitivity: workingcontext.SensitivityRestricted}, nil
	})
	if err != nil {
		return nil, err
	}
	// Preserve the established exact-artifact wire contract. Cache/provenance
	// metadata belongs to the host, rather than adding context to every cold read.
	// A truncated source remains explicitly marked by Artifact.Truncated.
	body, err := json.Marshal(struct {
		Text      string                     `json:"text"`
		Artifact  workingcontext.ArtifactRef `json:"artifact"`
		Fidelity  core.CompressionFidelity   `json:"fidelity"`
		Untrusted bool                       `json:"untrusted"`
	}{packet.Text, packet.Ref.Result.Artifact, core.CompressionExactRequired, true})
	if err == nil {
		_, _ = store.Update(id, func(value *session.Session) error {
			return session.AppendObservation(value, observability.Event{Category: observability.CategoryCapability, Operation: observability.OperationCapabilityResolve, State: observability.StateCompleted, SessionID: id, TaskID: taskID, WorkerID: workerID, Capability: "evidence.code", CacheHit: &packet.CacheHit})
		})
	}
	return body, err
}
