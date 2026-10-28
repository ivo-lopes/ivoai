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
	server.AddTool(&mcp.Tool{Name: "orchestration_artifact_read", Description: "Explicitly recover exact private evidence by opaque ResultRef for the active session.", InputSchema: object(map[string]any{"artifact_id": map[string]any{"type": "string", "pattern": workingcontext.ArtifactIDPattern}}, "artifact_id"), Annotations: read}, s.guardArtifactTool(s.artifactRead))
	server.AddTool(&mcp.Tool{Name: "orchestration_artifact_read_range", Description: "Explicitly recover a bounded byte range from private evidence by opaque ResultRef.", InputSchema: object(map[string]any{"artifact_id": map[string]any{"type": "string", "pattern": workingcontext.ArtifactIDPattern}, "offset": map[string]any{"type": "integer", "minimum": 0}, "length": map[string]any{"type": "integer", "minimum": 1, "maximum": 1048576}}, "artifact_id", "offset", "length"), Annotations: read}, s.guardArtifactTool(s.artifactReadRange))
}

// Worker projections have no full primary Store/receipt. Their private host
// constructor captures an exact owner and canonical recovery callback; that
// callback repeats grant, scope, generation and store checks. Full primary
// servers retain their Core and primary-turn authorization on every read.
func (s *Server) checkArtifactAuthority(ctx context.Context) error {
	if s.protectedArtifactsOnly {
		return ctx.Err()
	}
	return s.authorizedContext(ctx)
}

func (s *Server) guardArtifactTool(next func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error)) func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := s.checkArtifactAuthority(ctx); err != nil {
			return nil, err
		}
		result, err := next(ctx, request)
		if err != nil {
			return nil, err
		}
		if err := s.checkArtifactAuthority(ctx); err != nil {
			return nil, err
		}
		return result, nil
	}
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
