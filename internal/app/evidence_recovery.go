package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/evidence"
	"github.com/ivo-lopes/ivoai/internal/externalmcp"
	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/orchestrator"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workers"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

const protectedEvidenceFlag = "IVOAI_EXPERIMENTAL_EVIDENCE_RECOVERY"

func protectedEvidenceEnabled() bool { return os.Getenv(protectedEvidenceFlag) == "1" }

func protectCodeEvidence(v session.Session, task session.TaskMetadata, workerID, root string) bool {
	if !protectedEvidenceEnabled() {
		return false
	}
	if workerID == "" {
		return true
	}
	profile, err := orchestration.CapabilityProfile(task.Role)
	if err != nil || profile.Write {
		return false
	}
	primaryRoot, err := filepath.EvalSymlinks(v.WorkingDirectory)
	return err == nil && primaryRoot == root
}

// Recognizing a protected ID never depends on the creation opt-in. After a
// restart there is deliberately no binding/seal to reconstruct or legacy read
// fallback. The existing Request.Authorize/generation callbacks remain decisive.
func (a *App) recoverProtectedEvidence(ctx context.Context, owner workingcontext.Ownership, id string) ([]byte, workingcontext.ArtifactRef, error) {
	evidenceGatewayInitMu.Lock()
	gateway := a.evidenceGateway
	evidenceGatewayInitMu.Unlock()
	if gateway == nil {
		return nil, workingcontext.ArtifactRef{}, evidence.ErrBindingUnavailable
	}
	return gateway.RecoverBound(ctx, owner, id)
}

const evidenceReaderMCP = "ivoai-evidence-reader"

// Only an already-issued protected prefetch ref can create this projection.
// It receives no source credentials or scheduler tools; the existing Gateway
// binding repeats the original host grants/generation checks on every read.
func (a *App) workerEvidenceProjection(cfg config.Config, v session.Session, request workers.Request) (externalmcp.Target, bool, error) {
	if request.SharedContextBrief == "" {
		return externalmcp.Target{}, false, nil
	}
	var brief struct {
		CodeEvidence struct {
			Artifact workingcontext.ArtifactRef `json:"artifact"`
		} `json:"code_evidence"`
	}
	if json.Unmarshal([]byte(request.SharedContextBrief), &brief) != nil || !workingcontext.ProtectedArtifactID(brief.CodeEvidence.Artifact.ID) {
		return externalmcp.Target{}, false, nil
	}
	ref := brief.CodeEvidence.Artifact
	if ref.Validate() != nil || ref.Owner.WorkerID != request.WorkerID || ref.Owner.TaskID == "" || ref.Owner.SessionID != v.SessionID {
		return externalmcp.Target{}, false, errors.New("protected worker evidence owner mismatch")
	}
	if entry, exists := cfg.MCP.Servers[evidenceReaderMCP]; exists && entry.Enabled {
		return externalmcp.Target{}, false, errors.New("protected evidence reader name conflicts with configured MCP")
	}
	workerRoot, workerErr := filepath.EvalSymlinks(request.Directory)
	primaryRoot, primaryErr := filepath.EvalSymlinks(v.WorkingDirectory)
	if workerErr != nil || primaryErr != nil || workerRoot != primaryRoot {
		return externalmcp.Target{}, false, errors.New("protected evidence reader requires the primary worktree")
	}
	handler, err := orchestrator.NewEvidenceReaderProjection(ref.Owner, func(ctx context.Context, owner workingcontext.Ownership, id string) ([]byte, workingcontext.ArtifactRef, error) {
		if id != ref.ID || owner != ref.Owner {
			return nil, workingcontext.ArtifactRef{}, evidence.ErrDenied
		}
		return a.recoverProtectedEvidence(ctx, owner, id)
	})
	if err != nil {
		return externalmcp.Target{}, false, err
	}
	return externalmcp.Target{Name: evidenceReaderMCP, Local: handler, Restricted: true, AllowedTools: []string{"orchestration_artifact_read", "orchestration_artifact_read_range"}}, true, nil
}
