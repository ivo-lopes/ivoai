package orchestrator

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/evidence"
	"github.com/ivo-lopes/ivoai/internal/externalmcp"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workers"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type artifactReadTripwire struct {
	workingcontext.ArtifactStore
	reads, ranges atomic.Int32
}

func (s *artifactReadTripwire) Read(ctx context.Context, o workingcontext.Ownership, id string) (io.ReadCloser, workingcontext.ArtifactRef, error) {
	s.reads.Add(1)
	return s.ArtifactStore.Read(ctx, o, id)
}
func (s *artifactReadTripwire) ReadRange(ctx context.Context, o workingcontext.Ownership, id string, offset, length int64) ([]byte, workingcontext.ArtifactRef, error) {
	s.ranges.Add(1)
	return s.ArtifactStore.ReadRange(ctx, o, id, offset, length)
}

func protectedReaderFixture(t *testing.T) (*evidence.Gateway, *artifactReadTripwire, evidence.Packet, evidence.Request, *atomic.Bool) {
	t.Helper()
	store, err := workingcontext.NewLocalStore(filepath.Join(t.TempDir(), "artifacts"), workingcontext.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	trip := &artifactReadTripwire{ArtifactStore: store}
	g, err := evidence.New(trip)
	if err != nil {
		t.Fatal(err)
	}
	allowed := &atomic.Bool{}
	allowed.Store(true)
	r := evidence.Request{ProtectedRecovery: true, Owner: workingcontext.Ownership{SessionID: "sess_" + strings.Repeat("1", 32), TaskID: "inspect"}, ReadOnly: true, Source: "code", BudgetBytes: 32, QueryDigest: evidence.Digest("snapshot operation"), Scope: evidence.Scope{Organization: evidence.Digest("org"), Repository: evidence.Digest("repo"), Worktree: evidence.Digest("tree"), Permissions: evidence.Digest("host grant")}, Authorize: func(context.Context) error {
		if !allowed.Load() {
			return errors.New("fixture revoked")
		}
		return nil
	}, CurrentGeneration: func(context.Context) (string, error) { return evidence.Digest("immutable fixture snapshot"), nil }}
	p, err := g.Retrieve(context.Background(), r, func(context.Context) (evidence.Document, error) {
		return evidence.Document{Raw: []byte("exact protected snapshot"), MediaType: "text/plain", Basis: evidence.Extracted, SourceComplete: true, Sensitivity: workingcontext.SensitivityRestricted}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return g, trip, p, r, allowed
}

// The full reader is an active Core server, unlike the intentionally Store-less
// worker projection. Retain the fixture owner while using real session checks.
func fullArtifactReaderFixture(t *testing.T, id string, store workingcontext.ArtifactStore, recover EvidenceRecovery) *Server {
	t.Helper()
	s := primaryEvidenceServer(t)
	v, err := s.Store.Get(s.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	v.SessionID, v.SwarmID = id, "native_"+id
	if err := s.Store.Create(v); err != nil {
		t.Fatal(err)
	}
	s.SessionID, s.WorkingContext, s.RecoverEvidence = id, store, recover
	return s
}

func TestProtectedArtifactRoutesNeverFallBackAfterRestartOrFlagoff(t *testing.T) {
	g, store, packet, r, allowed := protectedReaderFixture(t)
	id := packet.Ref.Result.Artifact.ID
	s := fullArtifactReaderFixture(t, r.Owner.SessionID, store, g.RecoverBound)
	if result, err := s.artifactRead(context.Background(), toolRequest(map[string]any{"artifact_id": id})); err != nil || !strings.Contains(resultText(t, result), base64.StdEncoding.EncodeToString([]byte("exact protected snapshot"))) {
		t.Fatal("bound primary read failed", err)
	}
	for _, mode := range []string{"restart", "missing_callback", "revoked"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("IVOAI_EXPERIMENTAL_EVIDENCE_RECOVERY", "")
			reader := fullArtifactReaderFixture(t, r.Owner.SessionID, store, nil)
			if mode == "restart" {
				fresh, err := evidence.New(store)
				if err != nil {
					t.Fatal(err)
				}
				reader.RecoverEvidence = fresh.RecoverBound
			}
			if mode == "revoked" {
				allowed.Store(false)
				defer allowed.Store(true)
				reader.RecoverEvidence = g.RecoverBound
			}
			store.reads.Store(0)
			store.ranges.Store(0)
			if _, err := reader.artifactRead(context.Background(), toolRequest(map[string]any{"artifact_id": id})); err == nil {
				t.Fatal("protected read survived loss/revocation")
			}
			if _, err := reader.artifactReadRange(context.Background(), toolRequest(map[string]any{"artifact_id": id, "offset": 0, "length": 4})); err == nil {
				t.Fatal("protected range survived loss/revocation")
			}
			if store.reads.Load() != 0 || store.ranges.Load() != 0 {
				t.Fatal("protected ID fell through to legacy storage read")
			}
		})
	}
	legacy, err := store.Put(context.Background(), workingcontext.PutRequest{Kind: workingcontext.ArtifactWorkerOutput, Owner: r.Owner, MediaType: "text/plain", Sensitivity: workingcontext.SensitivityRestricted}, strings.NewReader("legacy remains readable"))
	if err != nil {
		t.Fatal(err)
	}
	legacyServer := fullArtifactReaderFixture(t, r.Owner.SessionID, store, nil)
	if _, err := legacyServer.artifactRead(context.Background(), toolRequest(map[string]any{"artifact_id": legacy.ID})); err != nil {
		t.Fatal("legacy read changed", err)
	}
}

type readerTransport func(*http.Request) (*http.Response, error)

func (f readerTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestProtectedWorkerProjectionUsesActualMCPAccessWithoutSchedulerTools(t *testing.T) {
	g, _, primary, r, allowed := protectedReaderFixture(t)
	r.Owner.TaskID = "review"
	r.Owner.WorkerID = "worker_" + strings.Repeat("a", 32)
	worker, err := g.Retrieve(context.Background(), r, func(context.Context) (evidence.Document, error) {
		t.Fatal("snapshot reuse re-fetched")
		return evidence.Document{}, nil
	})
	if err != nil || !worker.CacheHit || worker.Ref.Result.Artifact.SHA256 != primary.Ref.Result.Artifact.SHA256 {
		t.Fatal("worker did not receive exact reissued snapshot", err)
	}
	handler, err := NewEvidenceReaderProjection(r.Owner, g.RecoverBound)
	if err != nil {
		t.Fatal(err)
	}
	tools := []string{"orchestration_artifact_read", "orchestration_artifact_read_range"}
	proxy, err := externalmcp.Start([]externalmcp.Target{{Name: "fixture-evidence", Local: handler, Restricted: true, AllowedTools: tools}}, "full")
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	access, err := workers.NewAccess(t.TempDir(), false, []workers.MCPGrant{{Name: "fixture-evidence", URL: proxy.URL(0), Token: proxy.Token(), Tools: tools}})
	if err != nil {
		t.Fatal(err)
	}
	write, metadata := access.Metadata()
	if write || len(metadata) != 1 || len(metadata["fixture-evidence"]) != 2 {
		t.Fatal("worker projection broadened its grant")
	}
	client := &http.Client{Transport: readerTransport(func(req *http.Request) (*http.Response, error) {
		copy := req.Clone(req.Context())
		copy.Header = req.Header.Clone()
		copy.Header.Set("Authorization", "Bearer "+proxy.Token())
		return http.DefaultTransport.RoundTrip(copy)
	})}
	connection, err := mcp.NewClient(&mcp.Implementation{Name: "protected-evidence-fixture", Version: "1"}, nil).Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: proxy.URL(0), HTTPClient: client}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	listed, err := connection.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil || len(listed.Tools) != 2 {
		t.Fatal("read projection exposed unexpected tools", err)
	}
	call := func(name, id string, args map[string]any) (*mcp.CallToolResult, error) {
		if args == nil {
			args = map[string]any{}
		}
		args["artifact_id"] = id
		return connection.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	}
	result, err := call(tools[0], worker.Ref.Result.Artifact.ID, nil)
	if err != nil || result.IsError || !strings.Contains(resultText(t, result), base64.StdEncoding.EncodeToString([]byte("exact protected snapshot"))) {
		t.Fatal("real transport failed exact worker recovery", err)
	}
	result, err = call(tools[1], worker.Ref.Result.Artifact.ID, map[string]any{"offset": 1, "length": 3})
	if err != nil || result.IsError || !strings.Contains(resultText(t, result), base64.StdEncoding.EncodeToString([]byte("xac"))) {
		t.Fatal("real transport failed exact range", err)
	}
	for _, id := range []string{primary.Ref.Result.Artifact.ID, "artifact_" + strings.Repeat("b", 32)} {
		for _, tool := range tools {
			var args map[string]any
			if tool == tools[1] {
				args = map[string]any{"offset": 0, "length": 1}
			}
			result, err := call(tool, id, args)
			if err == nil && !result.IsError {
				t.Fatal("worker read foreign or legacy artifact", tool)
			}
		}
	}
	for _, bounds := range []map[string]any{{"offset": -1, "length": 1}, {"offset": 1, "length": 1048577}, {"offset": int64(1<<63 - 1), "length": 1}, {"offset": 0, "length": 25}} {
		if result, err := call(tools[1], worker.Ref.Result.Artifact.ID, bounds); err == nil && !result.IsError {
			t.Fatal("worker range accepted invalid/overflow bounds")
		}
	}
	if result, err := connection.CallTool(context.Background(), &mcp.CallToolParams{Name: "orchestration_spawn", Arguments: map[string]any{}}); err == nil && !result.IsError {
		t.Fatal("worker acquired scheduler tool")
	}
	allowed.Store(false)
	if result, err := call(tools[0], worker.Ref.Result.Artifact.ID, nil); err == nil && !result.IsError {
		t.Fatal("worker reader survived grant revocation")
	}
}

// Exercise registered RPC handlers as well as the recovery callback. A real
// protected snapshot is issued to this admitted primary; bytes must never be
// delivered after its Core/turn authority is lost during recovery.
func TestAdmittedPrimaryProtectedReadersRecheckAuthority(t *testing.T) {
	for _, tool := range []string{"orchestration_artifact_read", "orchestration_artifact_read_range"} {
		for _, mode := range []string{"valid", "closed_before", "stale_before", "identity_missing", "closed_during", "replaced_during", "host_revoked_during", "core_revoked_during"} {
			t.Run(tool+"/"+mode, func(t *testing.T) {
				g, _, _, request, _ := protectedReaderFixture(t)
				s := admittedPrimaryServer(t)
				request.Owner = workingcontext.Ownership{SessionID: s.SessionID, TaskID: "inspect"}
				packet, err := g.Retrieve(context.Background(), request, func(context.Context) (evidence.Document, error) {
					return evidence.Document{Raw: []byte("exact protected snapshot"), MediaType: "text/plain", Basis: evidence.Extracted, SourceComplete: true, Sensitivity: workingcontext.SensitivityRestricted}, nil
				})
				if err != nil {
					t.Fatal(err)
				}
				hostAllowed := true
				s.AuthorizePrimaryTurn = func(context.Context) error {
					if !hostAllowed {
						return errors.New("host revoked")
					}
					return nil
				}
				if mode == "identity_missing" {
					s.PrimaryTurnIdentity = nil
				}
				mutate := func() {
					_, err := s.Store.Update(s.SessionID, func(v *session.Session) error {
						switch mode {
						case "closed_before", "closed_during":
							v.PrimaryTurn.Mode = core.PrimaryClosed
						case "stale_before", "replaced_during":
							v.PrimaryTurn.ID = "replacement_receipt"
						case "core_revoked_during":
							v.ProviderExecution = true
						}
						return nil
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				if strings.HasSuffix(mode, "_before") {
					mutate()
				}
				calls := 0
				s.RecoverEvidence = func(ctx context.Context, owner workingcontext.Ownership, id string) ([]byte, workingcontext.ArtifactRef, error) {
					calls++
					raw, ref, err := g.RecoverBound(ctx, owner, id)
					if strings.HasSuffix(mode, "_during") {
						mutate()
						if mode == "host_revoked_during" {
							hostAllowed = false
						}
					}
					return raw, ref, err
				}
				st, ct := mcp.NewInMemoryTransports()
				ss, err := s.protocolServer().Connect(context.Background(), st, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer ss.Close()
				cs, err := mcp.NewClient(&mcp.Implementation{Name: "primary-reader-test", Version: "1"}, nil).Connect(context.Background(), ct, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer cs.Close()
				args := map[string]any{"artifact_id": packet.Ref.Result.Artifact.ID}
				if tool == "orchestration_artifact_read_range" {
					args["offset"], args["length"] = 1, 3
				}
				result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
				if mode == "valid" {
					if err != nil || result.IsError || calls != 1 {
						t.Fatalf("authorized recovery unavailable: calls=%d err=%v result=%+v", calls, err, result)
					}
					expected := "exact protected snapshot"
					if tool == "orchestration_artifact_read_range" {
						expected = "xac"
					}
					if !strings.Contains(resultText(t, result), base64.StdEncoding.EncodeToString([]byte(expected))) {
						t.Fatal("exact protected bytes unavailable")
					}
				} else {
					if err == nil && !result.IsError {
						t.Fatal("revoked primary received protected evidence")
					}
					want := 1
					if strings.HasSuffix(mode, "_before") || mode == "identity_missing" {
						want = 0
					}
					if calls != want {
						t.Fatalf("recovery calls=%d want=%d", calls, want)
					}
					if result != nil && strings.Contains(resultText(t, result), base64.StdEncoding.EncodeToString([]byte("xac"))) {
						t.Fatal("denial exposed protected bytes")
					}
				}
			})
		}
	}
}
