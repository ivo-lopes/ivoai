package orchestrator

import (
	"context"
	"errors"
	"net/http"

	"github.com/ivo-lopes/ivoai/internal/evidence"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type EvidenceRecovery func(context.Context, workingcontext.Ownership, string) ([]byte, workingcontext.ArtifactRef, error)

func (s *Server) addArtifactTools(server *mcp.Server, read *mcp.ToolAnnotations) {
	server.AddTool(&mcp.Tool{Name: "orchestration_artifact_read", Description: "Explicitly recover exact private evidence by opaque ResultRef for the active session.", InputSchema: object(map[string]any{"artifact_id": map[string]any{"type": "string", "pattern": workingcontext.ArtifactIDPattern}}, "artifact_id"), Annotations: read}, s.artifactRead)
	server.AddTool(&mcp.Tool{Name: "orchestration_artifact_read_range", Description: "Explicitly recover a bounded byte range from private evidence by opaque ResultRef.", InputSchema: object(map[string]any{"artifact_id": map[string]any{"type": "string", "pattern": workingcontext.ArtifactIDPattern}, "offset": map[string]any{"type": "integer", "minimum": 0}, "length": map[string]any{"type": "integer", "minimum": 1, "maximum": 1048576}}, "artifact_id", "offset", "length"), Annotations: read}, s.artifactReadRange)
}

func (s *Server) recoverProtectedArtifact(ctx context.Context, id string) ([]byte, workingcontext.ArtifactRef, error) {
	if s.RecoverEvidence == nil {
		return nil, workingcontext.ArtifactRef{}, evidence.ErrBindingUnavailable
	}
	owner := workingcontext.Ownership{SessionID: s.SessionID}
	if s.artifactOwner != nil {
		owner = *s.artifactOwner
	}
	return s.RecoverEvidence(ctx, owner, id)
}

// NewEvidenceReaderProjection reuses only the existing artifact handlers. It
// owns no listener, scheduler, grants, credential store or durable capability.
// The caller places it behind its existing worker-local Access/Target.Local.
func NewEvidenceReaderProjection(owner workingcontext.Ownership, recover EvidenceRecovery) (http.Handler, error) {
	if owner.Validate() != nil || owner.TaskID == "" || owner.WorkerID == "" || recover == nil {
		return nil, errors.New("evidence reader requires an exact host worker owner")
	}
	reader := &Server{SessionID: owner.SessionID, artifactOwner: &owner, protectedArtifactsOnly: true, RecoverEvidence: recover}
	server := mcp.NewServer(&mcp.Implementation{Name: "ivoai-evidence-reader", Version: "1"}, nil)
	reader.addArtifactTools(server, &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: boolp(false), OpenWorldHint: boolp(false), IdempotentHint: true})
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}), nil
}
