package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ivo-lopes/ivoai/internal/codeintel"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/evidence"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

// Only the admitted host places its existing receipt controller in this context.
// Nothing in RPC arguments, repository data or a frontend can construct it.
type adaptiveCodeContextKey struct{}

func (a *App) adaptiveCodeHandler(ctx context.Context, p *codeintel.Provider) http.Handler {
	t, _ := ctx.Value(adaptiveCodeContextKey{}).(*adaptivePrimaryTurn)
	if t == nil || t.app != a || os.Getenv("IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY") != "1" {
		return p.Handler()
	}
	wrap := func(kind string, next codeintel.QueryRunner) codeintel.QueryRunner {
		return func(ctx context.Context, q core.CodeQuery) (core.CodeResult, error) {
			return a.adaptiveCodeQuery(ctx, t, p, kind, q, next)
		}
	}
	return p.HandlerWithQueries(wrap("inspect", p.Inspect), wrap("query", p.Query))
}

func adaptiveCodeCacheable(q core.CodeQuery) bool {
	switch q.Operation {
	case "ask", "skeleton", "grep", "callers", "map", "blast":
		return true
	default:
		return false
	}
}

func adaptiveCodeProviderBound(t *adaptivePrimaryTurn, p *codeintel.Provider) error {
	if p == nil || !p.Allowed || p.Scope != t.scopeID || p.Permissions != "filesystem.read" {
		return evidence.ErrDenied
	}
	root, err := filepath.EvalSymlinks(p.Root)
	if err != nil || root != t.root {
		return evidence.ErrDenied
	}
	return nil
}

func (a *App) adaptiveCodeQuery(ctx context.Context, t *adaptivePrimaryTurn, p *codeintel.Provider, kind string, q core.CodeQuery, next codeintel.QueryRunner) (core.CodeResult, error) {
	denied := func() (core.CodeResult, error) { return core.CodeResult{}, evidence.ErrDenied }
	current, err := t.check(ctx)
	if err != nil || adaptiveCodeProviderBound(t, p) != nil {
		return denied()
	}
	q, err = codeintel.Normalize(q)
	if err != nil {
		return core.CodeResult{}, err
	}
	if current.PrimaryTurn.Mode != core.PrimaryReadOnly || !adaptiveCodeCacheable(q) {
		// Diagnostics/index builders retain their original behavior. They never enter
		// the cache, and changing authority during their call withholds the result.
		result, err := next(ctx, q)
		if err != nil {
			return core.CodeResult{}, err
		}
		if _, err := t.check(ctx); err != nil || adaptiveCodeProviderBound(t, p) != nil {
			return denied()
		}
		return result, nil
	}
	return a.retrieveAdaptiveCode(ctx, t, p, kind, q, next)
}

func (a *App) retrieveAdaptiveCode(ctx context.Context, t *adaptivePrimaryTurn, p *codeintel.Provider, kind string, q core.CodeQuery, next codeintel.QueryRunner) (core.CodeResult, error) {
	start := time.Now()
	root, err := filepath.EvalSymlinks(p.Root)
	if err != nil || root != t.root || p.Scope != t.scopeID || !p.Allowed {
		return core.CodeResult{}, evidence.ErrDenied
	}
	identity, err := codeintel.Identity(p.Root, p.Scope, p.Permissions)
	if err != nil {
		return core.CodeResult{}, err
	}
	owner := workingcontext.Ownership{SessionID: t.id, TaskID: "primary_" + t.receipt.ID}
	authorize := func(ctx context.Context) error {
		v, err := t.check(ctx)
		if err != nil || v.PrimaryTurn.Mode != core.PrimaryReadOnly || adaptiveCodeProviderBound(t, p) != nil {
			return evidence.ErrDenied
		}
		canonical, err := filepath.EvalSymlinks(p.Root)
		if err != nil || canonical != t.root {
			return evidence.ErrDenied
		}
		currentIdentity, err := codeintel.Identity(p.Root, p.Scope, p.Permissions)
		if err != nil || currentIdentity != identity {
			return evidence.ErrDenied
		}
		return nil
	}
	req := evidence.Request{ProtectedRecovery: true, Owner: owner, Source: "code.mcp", ReadOnly: true, BudgetBytes: 32768,
		Scope:       evidence.Scope{Organization: evidence.Digest(p.Scope), Repository: evidence.Digest(root), Worktree: identity, Permissions: evidence.Digest("adaptive-readonly-code-v1", p.Permissions, t.receipt.ScopeDigest, t.receipt.ConfigDigest, t.receipt.StateDigest)},
		QueryDigest: evidence.Digest(kind, q, codeintel.Version, p.Node, p.Distribution, p.ExecutionMode()), Authorize: authorize,
		CurrentGeneration: func(ctx context.Context) (string, error) {
			files, err := codeintel.Inventory(ctx, p.Root)
			if err != nil {
				return "", err
			}
			snapshot, err := codeintel.Snapshot(p.Root, files)
			return snapshot.Digest, err
		}}
	evidenceGatewayInitMu.Lock()
	if a.evidenceGateway == nil {
		store, err := workingcontext.NewLocalStore(filepath.Join(a.Store.Paths.CacheDir, "working-context"), workingcontext.LocalOptions{})
		if err == nil {
			a.evidenceGateway, err = evidence.New(store)
		}
		if err != nil {
			evidenceGatewayInitMu.Unlock()
			return core.CodeResult{}, err
		}
	}
	gateway := a.evidenceGateway
	evidenceGatewayInitMu.Unlock()
	packet, err := gateway.Retrieve(ctx, req, func(ctx context.Context) (evidence.Document, error) {
		result, err := next(ctx, q)
		if err != nil {
			return evidence.Document{}, err
		}
		if !result.Fresh || !result.Untrusted || len(result.Text) > q.BudgetBytes {
			return evidence.Document{}, errors.New("code query did not return fresh bounded untrusted evidence")
		}
		if err := evidence.ValidatePersistableContent([]byte(result.Text)); err != nil {
			return evidence.Document{}, err
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return evidence.Document{}, err
		}
		return evidence.Document{Raw: raw, MediaType: "application/json", Basis: evidence.Extracted, SourceComplete: !result.Truncated, Sensitivity: workingcontext.SensitivityRestricted}, nil
	})
	if err != nil {
		return core.CodeResult{}, err
	}
	// Reference-only packets must recover the original JSON, never truncate it or
	// substitute a summary. The existing sealed gateway repeats owner/grant/source
	// checks before and after the exact ArtifactStore read.
	raw, err := gateway.Recover(ctx, req, packet.Ref)
	if err != nil {
		return core.CodeResult{}, err
	}
	var result core.CodeResult
	if json.Unmarshal(raw, &result) != nil {
		return core.CodeResult{}, errors.New("invalid exact code result")
	}
	encoded, err := json.Marshal(result)
	if err != nil || !bytes.Equal(encoded, raw) {
		return core.CodeResult{}, errors.New("code result is not byte-equivalent")
	}
	if err := authorize(ctx); err != nil {
		return core.CodeResult{}, err
	}
	_, err = t.store.Update(t.id, func(v *session.Session) error {
		if !t.boundTo(*v) || v.PrimaryTurn.Mode != core.PrimaryReadOnly {
			return evidence.ErrDenied
		}
		return session.AppendObservation(v, observability.Event{Category: observability.CategoryCapability, Operation: observability.OperationCapabilityResolve, State: observability.StateCompleted, SessionID: t.id, TaskID: owner.TaskID, TurnID: t.receipt.TurnID, AttemptID: t.receipt.AttemptID, Phase: string(core.PrimaryReadOnly), Capability: "evidence.code", ArtifactID: packet.Ref.Result.Artifact.ID, ReferenceCount: 1, ArtifactBytes: int64(len(raw)), CacheHit: &packet.CacheHit, DurationMilliseconds: time.Since(start).Milliseconds(), FidelityClass: string(core.CompressionExactRequired)})
	})
	if err != nil {
		return core.CodeResult{}, err
	}
	return result, nil
}
