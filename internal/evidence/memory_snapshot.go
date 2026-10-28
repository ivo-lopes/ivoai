package evidence

import (
	"context"
	"errors"
	"time"

	"github.com/ivo-lopes/ivoai/internal/knowledgerouter"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

var ErrRetentionUnknown = errors.New("memory snapshot retention is unknown or denied")

// This is a decision from trusted host policy about the complete response,
// including its transport framing. Upstream annotations are never a decision.
// TTL is explicit per issued artifact; there is no implicit classification.
type MemoryRetentionDecision struct {
	Sensitivity  workingcontext.Sensitivity
	PolicyDigest string
	TTL          time.Duration
}

type MemoryRetentionRequest struct {
	Snapshot knowledgerouter.MemoryPageSnapshot
	Owner    workingcontext.Ownership
	Scope    Scope
}

type MemorySnapshotPolicy struct {
	Owner       workingcontext.Ownership
	Scope       Scope
	BudgetBytes int
	Authorize   func(context.Context) error                                                    `json:"-"`
	Retain      func(context.Context, MemoryRetentionRequest) (MemoryRetentionDecision, error) `json:"-"`
}

// MemorySnapshotScopeKey binds a live read to the same session and Core scope
// later used for retention. It is host input, not an upstream RPC argument.
func MemorySnapshotScopeKey(sessionID string, scope Scope) (string, error) {
	if (workingcontext.Ownership{SessionID: sessionID}).Validate() != nil || !isDigest(scope.Organization) || !isDigest(scope.Repository) || !isDigest(scope.Worktree) || !isDigest(scope.Permissions) {
		return "", ErrDenied
	}
	return Digest("memory-snapshot-context-v1", sessionID, scope), nil
}

// IssueMemoryPageSnapshot never reads the network or reexecutes a tool. Only an
// opaque fresh Router response can enter. Unknown retention produces no write;
// local grant and retention callbacks are rechecked during recovery. No App,
// scheduler, worker or native-client route is wired by this experimental SPI.
func (g *Gateway) IssueMemoryPageSnapshot(ctx context.Context, snapshot knowledgerouter.MemoryPageSnapshot, policy MemorySnapshotPolicy) (Packet, error) {
	if err := ctx.Err(); err != nil {
		return Packet{}, err
	}
	if policy.Authorize == nil || policy.Authorize(ctx) != nil {
		return Packet{}, ErrDenied
	}
	if policy.Retain == nil {
		return Packet{}, ErrRetentionUnknown
	}
	if snapshot.Validate() != nil {
		return Packet{}, ErrDenied
	}
	key, err := MemorySnapshotScopeKey(policy.Owner.SessionID, policy.Scope)
	if err != nil || key != snapshot.Metadata().ScopeKey {
		return Packet{}, ErrDenied
	}
	input := MemoryRetentionRequest{Snapshot: snapshot, Owner: policy.Owner, Scope: policy.Scope}
	decision, err := policy.Retain(ctx, input)
	if err != nil || !decision.Sensitivity.Persistable() || !isDigest(decision.PolicyDigest) || decision.TTL < time.Minute || decision.TTL > workingcontext.DefaultLimits().MaxTTL {
		return Packet{}, ErrRetentionUnknown
	}
	authorize := func(ctx context.Context) error {
		if policy.Authorize(ctx) != nil {
			return ErrDenied
		}
		current, err := policy.Retain(ctx, input)
		if err != nil || current != decision {
			return ErrRetentionUnknown
		}
		return ctx.Err()
	}
	meta := snapshot.Metadata()
	scope := policy.Scope
	// Cached bytes retain their classification, so every retention attribute
	// must participate in the domain used to issue a new recipient's artifact.
	scope.Permissions = Digest(scope.Permissions, meta.Source, decision)
	// This identifies already captured immutable bytes, not the current version
	// of a live Memory page. Every LIVE call happened before this nonce existed.
	generation := Digest("immutable-memory-response-v1", meta.ID, meta.SHA256, meta.Source)
	request := Request{ProtectedRecovery: true, ArtifactTTL: decision.TTL, Owner: policy.Owner, Scope: scope, Source: "memory", ReadOnly: true, BudgetBytes: policy.BudgetBytes, QueryDigest: Digest("memory_read_page", meta.ArgumentsSHA256, meta.Source), Authorize: authorize, CurrentGeneration: func(ctx context.Context) (string, error) { return generation, ctx.Err() }}
	media := "application/json"
	if meta.Transport == "text/event-stream" {
		media = "text/plain; charset=utf-8"
	}
	return g.Retrieve(ctx, request, func(context.Context) (Document, error) {
		return Document{Raw: snapshot.Bytes(), MediaType: media, Basis: Extracted, SourceComplete: false, Sensitivity: decision.Sensitivity}, nil
	})
}
