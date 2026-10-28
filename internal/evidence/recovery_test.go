package evidence

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

func TestProtectedRecoveryRequiresLiveBindingAndRecipientOwnership(t *testing.T) {
	f := newGatewayFixture(t)
	f.request.ProtectedRecovery = true
	primary, err := f.gateway.Retrieve(context.Background(), f.request, func(context.Context) (Document, error) {
		return Document{Raw: []byte("exact snapshot"), MediaType: "text/plain", Basis: Extracted, SourceComplete: true, Sensitivity: workingcontext.SensitivityRestricted}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	workerRequest := f.request
	workerRequest.Owner = workingcontext.Ownership{SessionID: f.request.Owner.SessionID, TaskID: "reader", WorkerID: "worker_" + strings.Repeat("a", 32)}
	worker, err := f.gateway.Retrieve(context.Background(), workerRequest, func(context.Context) (Document, error) {
		t.Fatal("same snapshot was fetched again")
		return Document{}, nil
	})
	if err != nil || !worker.CacheHit || worker.Ref.Result.Artifact.ID == primary.Ref.Result.Artifact.ID || worker.Ref.Result.Artifact.SHA256 != primary.Ref.Result.Artifact.SHA256 {
		t.Fatal("snapshot did not reissue under destination ownership", err)
	}
	if body, ref, err := f.gateway.RecoverBound(context.Background(), workerRequest.Owner, worker.Ref.Result.Artifact.ID); err != nil || string(body) != "exact snapshot" || ref.Owner != workerRequest.Owner {
		t.Fatal("exact bound recovery failed", err)
	}
	if _, _, err := f.gateway.RecoverBound(context.Background(), workingcontext.Ownership{SessionID: f.request.Owner.SessionID}, worker.Ref.Result.Artifact.ID); !errors.Is(err, ErrDenied) {
		t.Fatal("primary borrowed worker capability", err)
	}
	f.store.reads.Store(0)
	restarted, err := New(f.store)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := restarted.RecoverBound(context.Background(), workerRequest.Owner, worker.Ref.Result.Artifact.ID); !errors.Is(err, ErrBindingUnavailable) || f.store.reads.Load() != 0 {
		t.Fatal("restart used a legacy raw read", err)
	}
	f.allowed.Store(false)
	beforeRead := f.store.reads.Load()
	if _, _, err := f.gateway.RecoverBound(context.Background(), workerRequest.Owner, worker.Ref.Result.Artifact.ID); !errors.Is(err, ErrChanged) || f.store.reads.Load() != beforeRead {
		t.Fatal("revocation did not retain the existing ErrChanged/no-read contract", err)
	}
}

type unprotectedWriter struct{ workingcontext.ArtifactStore }

func (s unprotectedWriter) Put(ctx context.Context, r workingcontext.PutRequest, body io.Reader) (workingcontext.ArtifactRef, error) {
	r.ProtectedRecovery = false // Deliberately nonconforming fixture, not a production adapter.
	return s.ArtifactStore.Put(ctx, r, body)
}

func TestProtectedRecoveryRejectsStoreIgnoringHostNamespace(t *testing.T) {
	f := newGatewayFixture(t)
	g, err := New(unprotectedWriter{f.local})
	if err != nil {
		t.Fatal(err)
	}
	f.request.ProtectedRecovery = true
	if _, err := g.Retrieve(context.Background(), f.request, func(context.Context) (Document, error) { return exactDocument(), nil }); !errors.Is(err, ErrDenied) {
		t.Fatal("nonconforming writer yielded a recoverable legacy packet", err)
	}
	if len(g.bindings) != 0 || g.pendingBindings != 0 {
		t.Fatal("failed issuance retained a live binding/reservation")
	}
}

func TestProtectedRecoveryGenerationAndBoundedBindingAdmission(t *testing.T) {
	f := newGatewayFixture(t)
	f.request.ProtectedRecovery = true
	var first Ref
	for range maxRecoveryBindings {
		p, err := f.gateway.Retrieve(context.Background(), f.request, func(context.Context) (Document, error) {
			return Document{Raw: []byte("bound"), MediaType: "text/plain", Basis: Extracted, SourceComplete: true, Sensitivity: workingcontext.SensitivityInternal}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if first.Seal == "" {
			first = p.Ref
		}
	}
	before := f.store.puts.Load()
	if _, err := f.gateway.Retrieve(context.Background(), f.request, func(context.Context) (Document, error) {
		t.Fatal("full binding cache reexecuted fetch")
		return Document{}, nil
	}); err == nil || f.store.puts.Load() != before {
		t.Fatal("binding bound did not stop before persistence", err)
	}
	f.generation.Store(Digest("changed"))
	if _, _, err := f.gateway.RecoverBound(context.Background(), f.request.Owner, first.Result.Artifact.ID); !errors.Is(err, ErrChanged) {
		t.Fatal("source generation was not repeated", err)
	}
}
