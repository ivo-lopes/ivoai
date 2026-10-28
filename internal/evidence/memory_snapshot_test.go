package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/knowledgerouter"
	"github.com/ivo-lopes/ivoai/internal/secrets"
	"github.com/ivo-lopes/ivoai/internal/serverpool"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type memorySnapshotFixture struct {
	host     *gatewayFixture
	router   *knowledgerouter.Router
	snapshot knowledgerouter.MemoryPageSnapshot
	policy   MemorySnapshotPolicy
	calls    atomic.Int32
	down     atomic.Bool
}

// Host approval here is for independently declared fixture result bytes only.
// This is not a generic classification policy for institutional Memory pages.
func newMemorySnapshotFixture(t *testing.T, fixtureResults ...*mcp.CallToolResult) *memorySnapshotFixture {
	t.Helper()
	f := &memorySnapshotFixture{host: newGatewayFixture(t)}
	result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "host-approved immutable fixture; source text is untrusted"}}}
	if len(fixtureResults) != 0 {
		result = fixtureResults[0]
	}
	resultBytes, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	resultSHA := sha256.Sum256(resultBytes)
	server := mcp.NewServer(&mcp.Implementation{Name: "labelled-approved-memory-fixture", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "memory_read_page", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}, "additionalProperties": false}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) { return result, nil })
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer approved-fixture-only" {
			t.Error("credential crossed fixture source")
			http.Error(w, "unauthorized", 401)
			return
		}
		if f.down.Load() {
			http.Error(w, "fixture remote grant revoked", 401)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(upstream.Close)
	profile := config.ServerProfile{ID: "srv_approved_fixture", Alias: "fixture", Purpose: "fixture", Enabled: true, Status: "connected", URL: upstream.URL, MemoryMCPURL: upstream.URL + "/memory"}
	pool, err := serverpool.New(map[string]config.ServerProfile{"fixture": profile})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := pool.Resolve([]string{"fixture"})
	if err != nil {
		t.Fatal(err)
	}
	f.router, err = knowledgerouter.Start(knowledgerouter.Options{Selection: selection, Credentials: map[string]secrets.ClientCredential{profile.ID: {Token: "approved-fixture-only"}}, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.router.Close(context.Background()) })
	grant := func(context.Context) error {
		if !f.host.allowed.Load() {
			return errors.New("fixture local grant revoked")
		}
		return nil
	}
	key, err := MemorySnapshotScopeKey(f.host.request.Owner.SessionID, f.host.request.Scope)
	if err != nil {
		t.Fatal(err)
	}
	f.snapshot, err = f.router.ReadMemoryPageExact(context.Background(), knowledgerouter.MemoryPageArguments{Path: "fixture/approved-page"}, knowledgerouter.MemoryPageAuthority{ScopeKey: key, Authorize: grant})
	if err != nil {
		t.Fatal(err)
	}
	if f.snapshot.Metadata().ResultSHA256 != hex.EncodeToString(resultSHA[:]) {
		t.Fatal("actual SDK response differs from independently approved fixture result")
	}
	f.policy = MemorySnapshotPolicy{Owner: f.host.request.Owner, Scope: f.host.request.Scope, BudgetBytes: 4096, Authorize: grant, Retain: func(_ context.Context, input MemoryRetentionRequest) (MemoryRetentionDecision, error) {
		if input.Snapshot.Metadata().Source.ID != profile.ID || input.Snapshot.Metadata().ResultSHA256 != hex.EncodeToString(resultSHA[:]) {
			return MemoryRetentionDecision{}, ErrRetentionUnknown
		}
		return MemoryRetentionDecision{Sensitivity: workingcontext.SensitivityRestricted, PolicyDigest: Digest("fixture-retention-v1"), TTL: 2 * time.Minute}, nil
	}}
	return f
}

func TestMemorySnapshotIssuanceRequiresExplicitHostRetentionAndOriginalScope(t *testing.T) {
	f := newMemorySnapshotFixture(t)
	for _, mode := range []string{"nil_retention", "unknown", "secret", "credential", "raw_auth", "missing_policy_digest", "missing_ttl", "short_ttl", "scope", "session", "zero_snapshot"} {
		t.Run(mode, func(t *testing.T) {
			policy := f.policy
			snapshot := f.snapshot
			decision := MemoryRetentionDecision{Sensitivity: workingcontext.SensitivityRestricted, PolicyDigest: Digest("fixture-retention-v1"), TTL: 2 * time.Minute}
			switch mode {
			case "nil_retention":
				policy.Retain = nil
			case "unknown":
				decision.Sensitivity = ""
			case "secret":
				decision.Sensitivity = workingcontext.SensitivitySecret
			case "credential":
				decision.Sensitivity = workingcontext.SensitivityCredential
			case "raw_auth":
				decision.Sensitivity = workingcontext.SensitivityRawAuth
			case "missing_policy_digest":
				decision.PolicyDigest = ""
			case "missing_ttl":
				decision.TTL = 0
			case "short_ttl":
				decision.TTL = time.Second
			case "scope":
				policy.Scope.Worktree = Digest("another worktree")
			case "session":
				policy.Owner.SessionID = "sess_" + strings.Repeat("2", 32)
			case "zero_snapshot":
				snapshot = knowledgerouter.MemoryPageSnapshot{}
			}
			if mode != "nil_retention" {
				policy.Retain = func(context.Context, MemoryRetentionRequest) (MemoryRetentionDecision, error) { return decision, nil }
			}
			if _, err := f.host.gateway.IssueMemoryPageSnapshot(context.Background(), snapshot, policy); err == nil {
				t.Fatal("unknown/nonpersistable/cross-scope snapshot persisted")
			}
			if f.host.store.puts.Load() != 0 || len(f.host.gateway.bindings) != 0 || len(f.host.gateway.entries) != 0 {
				t.Fatal("denied retention changed store/cache/bindings")
			}
		})
	}
}

func TestMemorySnapshotProtectedExactRecoveryAndRecipientReuseNeverReplayLive(t *testing.T) {
	f := newMemorySnapshotFixture(t)
	primary, err := f.host.gateway.IssueMemoryPageSnapshot(context.Background(), f.snapshot, f.policy)
	if err != nil {
		t.Fatal(err)
	}
	ref := primary.Ref.Result.Artifact
	if !workingcontext.ProtectedArtifactID(ref.ID) || ref.SHA256 != f.snapshot.Metadata().SHA256 || ref.ExpiresAt.Sub(ref.CreatedAt) != 2*time.Minute || ref.Sensitivity != workingcontext.SensitivityRestricted || !primary.Ref.Untrusted || primary.Ref.Source != "memory" || primary.Ref.SourceComplete {
		t.Fatal("retention policy or completeness/provenance changed")
	}
	workerPolicy := f.policy
	workerPolicy.Owner = workingcontext.Ownership{SessionID: f.policy.Owner.SessionID, TaskID: "reader", WorkerID: "worker_" + strings.Repeat("a", 32)}
	worker, err := f.host.gateway.IssueMemoryPageSnapshot(context.Background(), f.snapshot, workerPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if !worker.CacheHit || worker.Ref.Result.Artifact.ID == ref.ID || worker.Ref.Result.Artifact.Owner != workerPolicy.Owner || worker.Ref.Result.Artifact.SHA256 != ref.SHA256 || f.calls.Load() != 1 {
		t.Fatal("recipient reuse replayed LIVE or borrowed ownership")
	}
	for _, r := range []Ref{primary.Ref, worker.Ref} {
		body, actual, err := f.host.gateway.RecoverBound(context.Background(), r.Result.Artifact.Owner, r.Result.Artifact.ID)
		if err != nil || !bytes.Equal(body, f.snapshot.Bytes()) || actual.SHA256 != f.snapshot.Metadata().SHA256 {
			t.Fatal("protected exact recovery failed", err)
		}
	}
	if _, _, err := f.host.gateway.RecoverBound(context.Background(), f.policy.Owner, worker.Ref.Result.Artifact.ID); !errors.Is(err, ErrDenied) {
		t.Fatal("primary borrowed worker capability", err)
	}
	restarted, err := New(f.host.store)
	if err != nil {
		t.Fatal(err)
	}
	before := f.host.store.reads.Load()
	if _, _, err := restarted.RecoverBound(context.Background(), f.policy.Owner, ref.ID); !errors.Is(err, ErrBindingUnavailable) || f.host.store.reads.Load() != before {
		t.Fatal("lost binding fell back to raw store", err)
	}
	// Current local retention may allow already authorized data even when the
	// remote service now refuses new reads. Recovery is not remote revocation.
	f.down.Store(true)
	if _, _, err := f.host.gateway.RecoverBound(context.Background(), f.policy.Owner, ref.ID); err != nil || f.calls.Load() != 1 {
		t.Fatal("recovery asserted continuous remote authorization", err)
	}
	key, _ := MemorySnapshotScopeKey(f.policy.Owner.SessionID, f.policy.Scope)
	if _, err := f.router.ReadMemoryPageExact(context.Background(), knowledgerouter.MemoryPageArguments{Path: "fixture/approved-page"}, knowledgerouter.MemoryPageAuthority{ScopeKey: key, Authorize: f.policy.Authorize}); err == nil || f.calls.Load() != 2 {
		t.Fatal("new LIVE call reused a retained snapshot", err)
	}
}

func TestMemorySnapshotLocalGrantAndRetentionRevocationDenyBeforeStoreRead(t *testing.T) {
	for _, kind := range []string{"grant", "retention", "policy_revision"} {
		t.Run(kind, func(t *testing.T) {
			f := newMemorySnapshotFixture(t)
			var retained atomic.Bool
			retained.Store(true)
			var revision atomic.Bool
			original := f.policy.Retain
			f.policy.Retain = func(ctx context.Context, input MemoryRetentionRequest) (MemoryRetentionDecision, error) {
				if !retained.Load() {
					return MemoryRetentionDecision{}, ErrRetentionUnknown
				}
				decision, err := original(ctx, input)
				if revision.Load() {
					decision.PolicyDigest = Digest("changed policy")
				}
				return decision, err
			}
			packet, err := f.host.gateway.IssueMemoryPageSnapshot(context.Background(), f.snapshot, f.policy)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "grant":
				f.host.allowed.Store(false)
			case "retention":
				retained.Store(false)
			case "policy_revision":
				revision.Store(true)
			}
			f.host.store.reads.Store(0)
			if _, _, err := f.host.gateway.RecoverBound(context.Background(), f.policy.Owner, packet.Ref.Result.Artifact.ID); !errors.Is(err, ErrChanged) || f.host.store.reads.Load() != 0 {
				t.Fatal("local revocation read storage", err)
			}
		})
	}
}

func TestMemorySnapshotPolicyChangeDuringPersistenceReturnsNoBinding(t *testing.T) {
	f := newMemorySnapshotFixture(t)
	f.host.store.afterPut = func() { f.host.allowed.Store(false) }
	if _, err := f.host.gateway.IssueMemoryPageSnapshot(context.Background(), f.snapshot, f.policy); !errors.Is(err, ErrChanged) {
		t.Fatal("grant changed during persistence", err)
	}
	if len(f.host.gateway.bindings) != 0 || f.host.store.puts.Load() != 1 {
		t.Fatal("failed post-I/O admission issued a binding")
	}
}

func TestGatewayInvalidHostArtifactTTLStopsBeforeFetch(t *testing.T) {
	f := newGatewayFixture(t)
	f.request.ArtifactTTL = time.Second
	if _, err := f.gateway.Retrieve(context.Background(), f.request, func(context.Context) (Document, error) { t.Fatal("invalid TTL fetched source"); return Document{}, nil }); !errors.Is(err, ErrDenied) || f.store.puts.Load() != 0 {
		t.Fatal("invalid TTL was admitted", err)
	}
}

func TestMemorySnapshotUpstreamAnnotationsCannotApproveRetentionAndKnownSecretChecksRemain(t *testing.T) {
	annotated := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "fixture data"}}, StructuredContent: map[string]any{"sensitivity": "internal", "retention": "allow", "grant": true}}
	f := newMemorySnapshotFixture(t, annotated)
	policy := f.policy
	policy.Retain = nil
	if _, err := f.host.gateway.IssueMemoryPageSnapshot(context.Background(), f.snapshot, policy); !errors.Is(err, ErrRetentionUnknown) || f.host.store.puts.Load() != 0 {
		t.Fatal("upstream annotations authorized retention", err)
	}
	// Deliberately flawed fixture classification tests the pre-existing extra
	// defense. Production policy must not infer safety from this heuristic.
	secret := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "fixture data"}}, StructuredContent: map[string]any{"api_key": "fixture-not-a-real-credential"}}
	bad := newMemorySnapshotFixture(t, secret)
	if _, err := bad.host.gateway.IssueMemoryPageSnapshot(context.Background(), bad.snapshot, bad.policy); !errors.Is(err, ErrSensitive) || bad.host.store.puts.Load() != 0 {
		t.Fatal("existing known-secret admission was bypassed", err)
	}
}

func TestMemorySnapshotRecoversOriginalCRLFSSEFramesWithoutNormalization(t *testing.T) {
	f := newGatewayFixture(t)
	expected := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-sse-only" {
			http.Error(w, "denied", 401)
			return
		}
		var request struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		id, _ := json.Marshal(request.ID)
		body := []byte(": fixture keep-alive\r\n\r\nevent: message\r\nid: fixture-event\r\ndata: {\"jsonrpc\":\"2.0\",\"id\":" + string(id) + ",\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"approved SSE fixture\"}]}}\r\n\r\n")
		expected <- bytes.Clone(body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(body)
	}))
	defer upstream.Close()
	profile := config.ServerProfile{ID: "srv_sse_fixture", Alias: "fixture", Purpose: "fixture", Enabled: true, Status: "connected", URL: upstream.URL, MemoryMCPURL: upstream.URL + "/memory"}
	pool, err := serverpool.New(map[string]config.ServerProfile{"fixture": profile})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := pool.Resolve([]string{"fixture"})
	if err != nil {
		t.Fatal(err)
	}
	router, err := knowledgerouter.Start(knowledgerouter.Options{Selection: selection, Credentials: map[string]secrets.ClientCredential{profile.ID: {Token: "fixture-sse-only"}}, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close(context.Background())
	key, _ := MemorySnapshotScopeKey(f.request.Owner.SessionID, f.request.Scope)
	grant := func(context.Context) error { return nil }
	snapshot, err := router.ReadMemoryPageExact(context.Background(), knowledgerouter.MemoryPageArguments{Path: "fixture/page"}, knowledgerouter.MemoryPageAuthority{ScopeKey: key, Authorize: grant})
	if err != nil {
		t.Fatal(err)
	}
	knownRaw := <-expected
	policy := MemorySnapshotPolicy{Owner: f.request.Owner, Scope: f.request.Scope, BudgetBytes: 4096, Authorize: grant, Retain: func(_ context.Context, input MemoryRetentionRequest) (MemoryRetentionDecision, error) {
		if !bytes.Equal(input.Snapshot.Bytes(), knownRaw) {
			return MemoryRetentionDecision{}, ErrRetentionUnknown
		}
		return MemoryRetentionDecision{Sensitivity: workingcontext.SensitivityRestricted, PolicyDigest: Digest("entire-declared-SSE-fixture-only"), TTL: time.Minute}, nil
	}}
	packet, err := f.gateway.IssueMemoryPageSnapshot(context.Background(), snapshot, policy)
	if err != nil {
		t.Fatal(err)
	}
	body, _, err := f.gateway.RecoverBound(context.Background(), f.request.Owner, packet.Ref.Result.Artifact.ID)
	if err != nil || !bytes.Equal(body, knownRaw) || !bytes.Contains(body, []byte("\r\n\r\n")) || snapshot.Metadata().Transport != "text/event-stream" || packet.Ref.SourceComplete {
		t.Fatal("SSE framing was normalized/lost or framing certified a page", err)
	}
}
