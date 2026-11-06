package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/codeintel"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/evidence"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

func newAdaptiveCodeGatewayFixture(t *testing.T) (*localMetadataFixture, *adaptivePrimaryTurn, *codeintel.Provider) {
	t.Helper()
	f := newAdaptiveFixture(t)
	paths := f.a.Store.Paths
	paths.DataDir = filepath.Join(paths.StateDir, "data")
	f.a.Store = config.NewStore(paths)
	t.Setenv("IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY", "1")
	if err := os.WriteFile(filepath.Join(f.root, "auth.go"), []byte("package fixture\nfunc Authenticate() bool{return true}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("/usr/bin/git", "-c", "core.hooksPath=/dev/null", "init", "-q")
	command.Dir = f.root
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + f.a.Store.Paths.StateDir, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0"}
	if err := command.Run(); err != nil {
		t.Fatal("private code fixture Git initialization failed")
	}
	scope, err := conversationScopeID(f.root, f.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.update(t, func(v *session.Session) { v.KnowledgeScopeID = scope })
	if ok, err := f.store.ClaimFrontendRequest(context.Background(), f.id, "fresh_frontend", "code_query"); err != nil || !ok {
		t.Fatal(err)
	}
	start := opencodebridge.TurnAttempt{ID: "attempt_code", TurnID: "turn_code", Frontend: "codex", FrontendSessionID: "fresh_frontend", RequestedExecutor: "codex", EffectiveExecutor: "codex", State: "running"}
	if err := persistManagedTurnAttempt(context.Background(), f.store, f.id, start); err != nil {
		t.Fatal(err)
	}
	// Obtain the single-user transport observation from the real bridge, rather
	// than introducing a test setter for this admission boundary.
	var turn *adaptivePrimaryTurn
	var accepted bool
	catalog := opencodebridge.CatalogFromRegistry(routing.Registry{Providers: map[string]routing.ProviderCapability{"codex": {Authenticated: true, Models: []routing.ModelCapability{{Name: "fixture-observed-model", Source: routing.SourceRuntimeVerified, CapabilityTier: routing.TierStrong, SupportedEfforts: []string{"high"}, DefaultEffort: "high"}}}}})
	bridge, err := opencodebridge.Start(opencodebridge.Options{Frontend: "codex", Catalog: catalog, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() opencodebridge.Status { return opencodebridge.Status{Frontend: "codex"} }, Runner: turnRunnerFunc(func(ctx context.Context, r core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
		var admissionErr error
		turn, accepted, admissionErr = f.a.admitAdaptivePrimary(ctx, f.cfg, f.state, f.store, f.id, f.root, nil, r)
		if admissionErr != nil {
			return core.ExecutorResult{}, admissionErr
		}
		return core.ExecutorResult{}, emit("fixture transport admission only")
	})})
	if err != nil {
		t.Fatal(err)
	}
	_, err = bridge.SubmitTurn(context.Background(), "fresh_frontend", "transport_admission", "Locate Authenticate", "auto", "")
	_ = bridge.Close(context.Background())
	if err != nil || !accepted || turn == nil {
		t.Fatal("read-only receipt fixture denied", err)
	}
	provider, err := f.a.CodeIntelligenceProvider(context.Background(), f.root, turn.scopeID)
	if err != nil {
		t.Fatal(err)
	}
	return f, turn, provider
}
func adaptiveExactCodeResult() core.CodeResult {
	return core.CodeResult{Operation: "ask", Text: "auth.go: Authenticate", SourceDigest: evidence.Digest("fixture_source"), ProviderVersion: "direct", Fresh: true, Untrusted: true, Files: 1, DurationMS: 137}
}
func adaptiveCodeQueryFixture() core.CodeQuery {
	return core.CodeQuery{Operation: "ask", Query: "Authenticate", BudgetBytes: 1024}
}
func adaptiveGatewayEvents(v session.Session) []observability.Event {
	var out []observability.Event
	for _, e := range v.Observability {
		if e.Capability == "evidence.code" && strings.HasPrefix(e.TaskID, "primary_") {
			out = append(out, e)
		}
	}
	return out
}

func TestAdaptiveCodeGatewayExactReuseAndRecovery(t *testing.T) {
	f, turn, p := newAdaptiveCodeGatewayFixture(t)
	expected := adaptiveExactCodeResult()
	var calls atomic.Int32
	next := func(context.Context, core.CodeQuery) (core.CodeResult, error) { calls.Add(1); return expected, nil }
	for range 2 {
		got, err := f.a.adaptiveCodeQuery(context.Background(), turn, p, "inspect", adaptiveCodeQueryFixture(), next)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(expected)
		if !bytes.Equal(a, b) {
			t.Fatal("gateway changed typed payload bytes")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("repeated query executed provider again")
	}
	v, err := f.store.Get(f.id)
	if err != nil {
		t.Fatal(err)
	}
	events := adaptiveGatewayEvents(v)
	if len(events) != 2 || events[0].CacheHit == nil || *events[0].CacheHit || events[1].CacheHit == nil || !*events[1].CacheHit {
		t.Fatal("gateway cache observations unavailable")
	}
	owner := workingcontext.Ownership{SessionID: f.id, TaskID: "primary_" + turn.receipt.ID}
	raw, ref, err := f.a.recoverProtectedEvidence(context.Background(), owner, events[1].ArtifactID)
	want, _ := json.Marshal(expected)
	if err != nil || ref.Owner != owner || !bytes.Equal(raw, want) {
		t.Fatal("exact protected native result unavailable", err)
	}
	other := owner
	other.TaskID = "foreign_receipt"
	if _, _, err := f.a.recoverProtectedEvidence(context.Background(), other, events[1].ArtifactID); err == nil {
		t.Fatal("another receipt recovered evidence")
	}
	turn.close()
	if _, _, err := f.a.recoverProtectedEvidence(context.Background(), owner, events[1].ArtifactID); err == nil {
		t.Fatal("closed receipt recovered evidence")
	}
}

func TestAdaptiveCodeGatewaySourceChangesInvalidateAndRevokeRecovery(t *testing.T) {
	f, turn, p := newAdaptiveCodeGatewayFixture(t)
	var calls int
	next := func(context.Context, core.CodeQuery) (core.CodeResult, error) {
		calls++
		return adaptiveExactCodeResult(), nil
	}
	if _, err := f.a.adaptiveCodeQuery(context.Background(), turn, p, "inspect", adaptiveCodeQueryFixture(), next); err != nil {
		t.Fatal(err)
	}
	v, _ := f.store.Get(f.id)
	id := adaptiveGatewayEvents(v)[0].ArtifactID
	if err := os.WriteFile(filepath.Join(f.root, "auth.go"), []byte("package fixture\nfunc Authenticate() bool{return false}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	owner := workingcontext.Ownership{SessionID: f.id, TaskID: "primary_" + turn.receipt.ID}
	if _, _, err := f.a.recoverProtectedEvidence(context.Background(), owner, id); err == nil {
		t.Fatal("changed source kept old recovery grant")
	}
	if _, err := f.a.adaptiveCodeQuery(context.Background(), turn, p, "inspect", adaptiveCodeQueryFixture(), next); err != nil || calls != 2 {
		t.Fatal("source change did not retrieve again", err)
	}
}

func TestAdaptiveCodeGatewayOwnerScopesAndAuthorityDenyBeforeProvider(t *testing.T) {
	for _, kind := range []string{"scope", "worktree", "permissions", "provider_denied", "receipt", "config", "runtime", "gateway_flag"} {
		t.Run(kind, func(t *testing.T) {
			f, turn, p := newAdaptiveCodeGatewayFixture(t)
			var calls int
			next := func(context.Context, core.CodeQuery) (core.CodeResult, error) {
				calls++
				return adaptiveExactCodeResult(), nil
			}
			switch kind {
			case "scope":
				p.Scope = "foreign"
			case "worktree":
				p.Root = t.TempDir()
			case "permissions":
				p.Permissions = "filesystem.write"
			case "provider_denied":
				p.Allowed = false
			case "receipt":
				turn.close()
			case "config":
				f.cfg.Memory.Enabled = !f.cfg.Memory.Enabled
				if err := f.a.Store.Save(f.cfg); err != nil {
					t.Fatal(err)
				}
			case "gateway_flag":
				t.Setenv("IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY", "0")
			case "runtime":
				if err := os.WriteFile(turn.runtime.executable, []byte("changed"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.a.adaptiveCodeQuery(context.Background(), turn, p, "inspect", adaptiveCodeQueryFixture(), next); err == nil || calls != 0 {
				t.Fatal("denied scope/authority executed query", err)
			}
		})
	}
}

func TestAdaptiveCodeGatewayNeverCachesIndexOrDiagnosticActions(t *testing.T) {
	for _, op := range []string{"build", "viz", "check"} {
		t.Run(op, func(t *testing.T) {
			f, turn, p := newAdaptiveCodeGatewayFixture(t)
			var calls int
			next := func(context.Context, core.CodeQuery) (core.CodeResult, error) {
				calls++
				return adaptiveExactCodeResult(), nil
			}
			q := adaptiveCodeQueryFixture()
			q.Operation = op
			for range 2 {
				if _, err := f.a.adaptiveCodeQuery(context.Background(), turn, p, "query", q, next); err != nil {
					t.Fatal(err)
				}
			}
			if calls != 2 || f.a.evidenceGateway != nil {
				t.Fatal("diagnostic/mutable operation entered replay cache")
			}
		})
	}
}

func TestAdaptiveCodeGatewayChangedAuthorityOrSensitiveTextWithholds(t *testing.T) {
	for _, kind := range []string{"revoke", "source_change", "secret_json", "private_key", "upstream_error"} {
		t.Run(kind, func(t *testing.T) {
			f, turn, p := newAdaptiveCodeGatewayFixture(t)
			next := func(context.Context, core.CodeQuery) (core.CodeResult, error) {
				r := adaptiveExactCodeResult()
				switch kind {
				case "revoke":
					turn.close()
				case "source_change":
					if err := os.WriteFile(filepath.Join(f.root, "auth.go"), []byte("package changed\n"), 0600); err != nil {
						t.Fatal(err)
					}
				case "secret_json":
					r.Text = `{"password":"sensitive_fixture_value"}`
				case "private_key":
					r.Text = "-----BEGIN PRIVATE KEY-----"
				case "upstream_error":
					return core.CodeResult{}, errors.New("fixture_failure")
				}
				return r, nil
			}
			if _, err := f.a.adaptiveCodeQuery(context.Background(), turn, p, "inspect", adaptiveCodeQueryFixture(), next); err == nil {
				t.Fatal("unsafe or revoked evidence reached caller")
			}
			v, _ := f.store.Get(f.id)
			if len(adaptiveGatewayEvents(v)) != 0 {
				t.Fatal("failed query reported completed cached evidence")
			}
		})
	}
}

func TestAdaptiveCodeGatewayConcurrentQueriesDeduplicate(t *testing.T) {
	f, turn, p := newAdaptiveCodeGatewayFixture(t)
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	next := func(context.Context, core.CodeQuery) (core.CodeResult, error) {
		calls.Add(1)
		once.Do(func() { close(entered) })
		<-release
		return adaptiveExactCodeResult(), nil
	}
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := f.a.adaptiveCodeQuery(context.Background(), turn, p, "inspect", adaptiveCodeQueryFixture(), next)
			errs <- err
		}()
	}
	select {
	case <-entered:
	case err := <-errs:
		close(release)
		t.Fatal("query denied before loader", err)
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("query loader did not start")
	}
	close(release)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("concurrent reads were not deduplicated")
	}
}

func TestAdaptiveCodeGatewayDispatchesDoNotShareResults(t *testing.T) {
	f, turn, p := newAdaptiveCodeGatewayFixture(t)
	var calls int
	next := func(context.Context, core.CodeQuery) (core.CodeResult, error) {
		calls++
		return adaptiveExactCodeResult(), nil
	}
	for _, kind := range []string{"inspect", "query", "inspect", "query"} {
		if _, err := f.a.adaptiveCodeQuery(context.Background(), turn, p, kind, adaptiveCodeQueryFixture(), next); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatal("automatic and structural dispatch provenance conflated")
	}
}

func TestAdaptiveCodeGatewayCacheHitCannotHideRevocationOrRuntimeClaim(t *testing.T) {
	for _, kind := range []string{"closed", "gateway_flag", "claimed_runtime", "permissions"} {
		t.Run(kind, func(t *testing.T) {
			f, turn, p := newAdaptiveCodeGatewayFixture(t)
			var calls int
			next := func(context.Context, core.CodeQuery) (core.CodeResult, error) {
				calls++
				return adaptiveExactCodeResult(), nil
			}
			if _, err := f.a.adaptiveCodeQuery(context.Background(), turn, p, "inspect", adaptiveCodeQueryFixture(), next); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "closed":
				turn.close()
			case "gateway_flag":
				t.Setenv("IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY", "0")
			case "claimed_runtime":
				if err := os.MkdirAll(filepath.Join(f.a.Store.Paths.DataDir, "supply-chain"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), filepath.Join(f.a.Store.Paths.DataDir, "supply-chain", "state")); err != nil {
					t.Fatal(err)
				}
			case "permissions":
				p.Permissions = "filesystem.write"
			}
			if _, err := f.a.adaptiveCodeQuery(context.Background(), turn, p, "inspect", adaptiveCodeQueryFixture(), next); err == nil || calls != 1 {
				t.Fatal("cached content hid changed authority/runtime", err)
			}
		})
	}
}

func TestAdaptiveCodeGatewayRecoversReferenceOnlyJSONWithoutTruncation(t *testing.T) {
	f, turn, p := newAdaptiveCodeGatewayFixture(t)
	expected := adaptiveExactCodeResult()
	expected.Text = strings.Repeat("<", 32768)
	q := adaptiveCodeQueryFixture()
	q.BudgetBytes = 32768
	got, err := f.a.adaptiveCodeQuery(context.Background(), turn, p, "inspect", q, func(context.Context, core.CodeQuery) (core.CodeResult, error) { return expected, nil })
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := json.Marshal(got)
	original, _ := json.Marshal(expected)
	if len(original) <= 32768 || !bytes.Equal(actual, original) {
		t.Fatal("reference-only gateway packet truncated original typed JSON")
	}
}

func TestAdaptiveCodeGatewayRevokedWaitersCannotReuseInflightQuery(t *testing.T) {
	f, turn, p := newAdaptiveCodeGatewayFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	var once sync.Once
	next := func(context.Context, core.CodeQuery) (core.CodeResult, error) {
		calls.Add(1)
		once.Do(func() { close(entered) })
		<-release
		return adaptiveExactCodeResult(), nil
	}
	done := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := f.a.adaptiveCodeQuery(context.Background(), turn, p, "inspect", adaptiveCodeQueryFixture(), next)
			done <- err
		}()
	}
	select {
	case <-entered:
	case err := <-done:
		close(release)
		t.Fatal("inflight loader unavailable", err)
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("inflight loader deadline")
	}
	turn.close()
	close(release)
	for range 2 {
		if err := <-done; err == nil {
			t.Fatal("revoked waiter received original or cached content")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("revoked waiter reexecuted lookup")
	}
}
