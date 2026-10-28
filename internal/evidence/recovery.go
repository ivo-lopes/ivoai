package evidence

import (
	"context"
	"errors"

	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

var ErrBindingUnavailable = errors.New("EVIDENCE_BINDING_UNAVAILABLE")

const maxRecoveryBindings = 64

type recoveryBinding struct {
	request Request
	ref     Ref
}

func (g *Gateway) reserveBinding() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	for id, binding := range g.bindings {
		if !g.now().Before(binding.ref.Result.Artifact.ExpiresAt) {
			delete(g.bindings, id)
		}
	}
	if len(g.bindings)+g.pendingBindings >= maxRecoveryBindings {
		return errors.New("evidence recovery binding limit reached")
	}
	g.pendingBindings++
	return nil
}

// RecoverBound uses only a canonical host-issued request/ref, never caller
// scope, grants, source generation or a reconstructed seal. A lost binding is
// terminal, including after restart or flagoff; callers must not fall back.
func (g *Gateway) RecoverBound(ctx context.Context, owner workingcontext.Ownership, id string) ([]byte, workingcontext.ArtifactRef, error) {
	if !workingcontext.ProtectedArtifactID(id) || owner.Validate() != nil {
		return nil, workingcontext.ArtifactRef{}, ErrDenied
	}
	g.mu.Lock()
	binding, ok := g.bindings[id]
	g.mu.Unlock()
	if !ok {
		return nil, workingcontext.ArtifactRef{}, ErrBindingUnavailable
	}
	issued := binding.request.Owner
	// A session-level primary can recover its primary-task refs, but it cannot
	// borrow a worker's capability. Worker projections always capture exact
	// task/worker ownership; models never supply these fields.
	if owner.SessionID != issued.SessionID || owner.WorkerID != issued.WorkerID || owner.TaskID != "" && owner.TaskID != issued.TaskID || owner.WorkerID != "" && owner.TaskID == "" {
		return nil, workingcontext.ArtifactRef{}, ErrDenied
	}
	body, err := g.Recover(ctx, binding.request, binding.ref)
	if err != nil {
		return nil, workingcontext.ArtifactRef{}, err
	}
	return body, binding.ref.Result.Artifact, nil
}
