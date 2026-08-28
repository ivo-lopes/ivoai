package workingcontext

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
)

func TestContractsAreProviderNeutralBoundedAndOpaque(t *testing.T) {
	now := time.Now().UTC()
	owner := Ownership{SessionID: "sess_0123456789abcdef0123456789abcdef", TaskID: "review", WorkerID: "worker_0123456789abcdef0123456789abcdef"}
	ref := ArtifactRef{ID: "artifact_0123456789abcdef0123456789abcdef", Kind: ArtifactWorkerOutput, Size: 7, SHA256: strings.Repeat("a", 64), MediaType: "text/plain; charset=utf-8", CreatedAt: now, ExpiresAt: now.Add(time.Hour), Owner: owner, Sensitivity: SensitivityInternal, Complete: true}
	result := WorkerResult{Status: ResultCompleted, PayloadType: "worker_output", Fidelity: core.CompressionCompressible, Summary: "bounded", Evidence: []ResultRef{{Artifact: ref, Role: EvidencePrimary}}, StateDelta: StateDelta{Proposed: []ProposedChange{{Kind: ChangeObservation, Summary: "advisory only", Evidence: []ResultRef{{Artifact: ref, Role: EvidenceFinding}}}}}}
	if err := result.Validate(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ref.ID, "/") || strings.Contains(ref.ID, "codex") || strings.Contains(ref.ID, "claude") {
		t.Fatal("reference is path- or provider-derived")
	}
}

func TestContractsRejectSecretsAndUnboundedInlineContent(t *testing.T) {
	now := time.Now().UTC()
	ref := ArtifactRef{ID: "artifact_0123456789abcdef0123456789abcdef", Kind: ArtifactWorkerOutput, SHA256: strings.Repeat("a", 64), MediaType: "text/plain", CreatedAt: now, ExpiresAt: now.Add(time.Hour), Owner: Ownership{SessionID: "sess_0123456789abcdef0123456789abcdef"}, Sensitivity: SensitivityCredential, Complete: true}
	if err := ref.Validate(); err == nil {
		t.Fatal("credential artifact accepted as common transient evidence")
	}
	result := WorkerResult{Status: ResultCompleted, Summary: strings.Repeat("x", MaxSummaryBytes+1)}
	if err := result.Validate(); err == nil {
		t.Fatal("unbounded summary accepted")
	}
}

type fakeStore struct{}

func (fakeStore) Put(context.Context, PutRequest, io.Reader) (ArtifactRef, error) {
	return ArtifactRef{}, nil
}
func (fakeStore) Stat(context.Context, Ownership, string) (ArtifactRef, error) {
	return ArtifactRef{}, nil
}
func (fakeStore) Read(context.Context, Ownership, string) (io.ReadCloser, ArtifactRef, error) {
	return io.NopCloser(strings.NewReader("")), ArtifactRef{}, nil
}
func (fakeStore) ReadRange(context.Context, Ownership, string, int64, int64) ([]byte, ArtifactRef, error) {
	return nil, ArtifactRef{}, nil
}
func (fakeStore) GC(context.Context) (int, error) { return 0, nil }

var _ ArtifactStore = fakeStore{}
