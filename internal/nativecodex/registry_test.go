package nativecodex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/evidence"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

type viewFunc func(context.Context, string, []byte) ([]byte, error)

func (f viewFunc) Transform(ctx context.Context, filter string, raw []byte) ([]byte, error) {
	return f(ctx, filter, raw)
}

type evidenceFixture struct {
	owner      workingcontext.Ownership
	binding    Binding
	registry   *Registry
	raw        []byte
	allowed    atomic.Bool
	generation atomic.Value
	viewCalls  atomic.Int32
}

func fixtureEvidence(t *testing.T) *evidenceFixture {
	t.Helper()
	f := &evidenceFixture{owner: workingcontext.Ownership{SessionID: "sess_" + strings.Repeat("a", 32), TaskID: "primary_read"}, raw: []byte(strings.Repeat("PRIVATE_RAW_SENTINEL src/example.go:12: value\n", 20))}
	f.allowed.Store(true)
	f.generation.Store(evidence.Digest("generation-one"))
	store, err := workingcontext.NewLocalStore(filepath.Join(t.TempDir(), "artifacts"), workingcontext.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	g, err := evidence.New(store)
	if err != nil {
		t.Fatal(err)
	}
	request := evidence.Request{Owner: f.owner, Scope: evidence.Scope{Organization: evidence.Digest("org"), Repository: evidence.Digest("repo"), Worktree: evidence.Digest("worktree"), Permissions: evidence.Digest("readonly")}, Source: "tool_read", QueryDigest: evidence.Digest("existing-inventory"), ReadOnly: true, BudgetBytes: 4096, Authorize: func(ctx context.Context) error {
		if !f.allowed.Load() {
			return errors.New("private grant detail")
		}
		return ctx.Err()
	}, CurrentGeneration: func(ctx context.Context) (string, error) { return f.generation.Load().(string), ctx.Err() }}
	packet, err := g.Retrieve(context.Background(), request, func(context.Context) (evidence.Document, error) {
		return evidence.Document{Raw: f.raw, MediaType: "text/plain", Basis: evidence.Extracted, SourceComplete: true, Sensitivity: workingcontext.SensitivityRestricted}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f.binding = Binding{Gateway: g, Request: request, Ref: packet.Ref, ViewOptions: evidence.OutputViewOptions{Enabled: true, AllowDerived: true, Kind: "inventory", Filter: "grep"}, ViewProvider: viewFunc(func(_ context.Context, filter string, raw []byte) ([]byte, error) {
		f.viewCalls.Add(1)
		if filter != "grep" || !bytes.Equal(raw, f.raw) {
			return nil, errors.New("unexpected view input")
		}
		return []byte("VIEW_ONLY src/example.go:12\n"), nil
	})}
	f.registry, err = NewRegistry(context.Background(), f.owner, []Binding{f.binding})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestEvidenceRegistryKeepsExactRawAndImmutableManifest(t *testing.T) {
	f := fixtureEvidence(t)
	bindings := []Binding{f.binding}
	r, err := NewRegistry(context.Background(), f.owner, bindings)
	if err != nil {
		t.Fatal(err)
	}
	id := r.IDs()[0]
	ids := r.IDs()
	ids[0] = "changed"
	bindings[0].Request.ReadOnly = false
	bindings[0].Ref.Seal = "forged"
	bindings[0].ViewOptions.Enabled = false
	prepared, err := r.prepare(context.Background(), id, "view")
	if err != nil || !strings.Contains(prepared.text, "VIEW_ONLY") || strings.Contains(prepared.text, "PRIVATE_RAW_SENTINEL") || strings.Contains(prepared.text, "seal") || strings.Contains(prepared.text, "permissions") || !strings.Contains(prepared.text, `"derived":true`) || !strings.Contains(prepared.text, `"raw_fidelity":"exact_required"`) || !strings.Contains(prepared.text, `"raw_basis":"extracted"`) || !strings.Contains(prepared.text, f.binding.Ref.Result.Artifact.ID) {
		t.Fatalf("unexpected prepared view: %q %v", prepared.text, err)
	}
	raw, err := f.binding.Gateway.Recover(context.Background(), f.binding.Request, f.binding.Ref)
	if err != nil || !bytes.Equal(raw, f.raw) {
		t.Fatal("view changed original artifact")
	}
	if err := prepared.validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.allowed.Store(false)
	if prepared.validate(context.Background()) == nil {
		t.Fatal("revoked prepared view remained valid")
	}
}

func TestEvidenceRegistryRejectsWrongRecipientAndForgedRefs(t *testing.T) {
	f := fixtureEvidence(t)
	for _, kind := range []string{"session", "task", "worker", "mutable", "forged", "empty"} {
		t.Run(kind, func(t *testing.T) {
			owner, binding := f.owner, f.binding
			switch kind {
			case "session":
				owner.SessionID = "sess_" + strings.Repeat("b", 32)
			case "task":
				owner.TaskID = "other_task"
			case "worker":
				owner.WorkerID = "worker_" + strings.Repeat("b", 32)
				binding.Request.Owner = owner
			case "mutable":
				binding.Request.ReadOnly = false
			case "forged":
				binding.Ref.Seal = "forged"
			case "empty":
				owner = workingcontext.Ownership{}
			}
			if _, err := NewRegistry(context.Background(), owner, []Binding{binding}); err == nil {
				t.Fatal("foreign/invalid binding admitted")
			}
		})
	}
}

func TestEvidenceArgumentsAreTwoBoundedEnumFields(t *testing.T) {
	f := fixtureEvidence(t)
	id := f.registry.IDs()[0]
	for _, raw := range []string{`[]`, `null`, `{}`, `{"evidence_id":1}`, `{"evidence_id":"` + id + `"}`, `{"evidence_id":"` + id + `","representation":"unknown"}`, `{"evidence_id":"` + id + `","representation":"view","command":"pwd"}`, `{"evidence_id":"` + id + `","evidence_id":"` + id + `","representation":"view"}`, `{"evidence_id":"` + id + `","representation":"view","representation":"exact"}`, `{"evidence_id":"` + id + `","representation":"view"} {}`, strings.Repeat(" ", 513)} {
		if _, err := evidenceArguments([]byte(raw)); err == nil {
			t.Fatalf("malformed args admitted: %.100s", raw)
		}
	}
	if got, err := evidenceArguments([]byte(`{"representation":"view","evidence_id":"` + id + `"}`)); err != nil || got.id != id || got.representation != "view" {
		t.Fatal("valid enum argument failed")
	}
	if _, err := f.registry.start(context.Background(), "ev_"+strings.Repeat("f", 32), "view"); err == nil {
		t.Fatal("unknown enum value dispatched")
	}
}

func TestEvidenceExactIsOnDemandBudgetedAndReauthorized(t *testing.T) {
	f := fixtureEvidence(t)
	view, err := f.registry.prepare(context.Background(), f.registry.IDs()[0], "view")
	if err != nil || strings.Contains(view.text, "PRIVATE_RAW_SENTINEL") {
		t.Fatal("derived view unexpectedly disclosed raw")
	}
	exact, err := f.registry.prepare(context.Background(), f.registry.IDs()[0], "exact")
	var body struct {
		Text           string   `json:"text"`
		Representation string   `json:"representation"`
		Derived        bool     `json:"derived"`
		Gaps           []string `json:"gaps"`
	}
	if err != nil || json.Unmarshal([]byte(exact.text), &body) != nil || !bytes.Equal([]byte(body.Text), f.raw) || body.Representation != "exact" || body.Derived || f.viewCalls.Load() != 1 {
		t.Fatal("explicit exact did not recover exact bytes independently of provider")
	}
	b := f.binding
	b.Request.BudgetBytes = 16
	r, err := NewRegistry(context.Background(), f.owner, []Binding{b})
	if err != nil {
		t.Fatal(err)
	}
	limited, err := r.prepare(context.Background(), r.IDs()[0], "exact")
	body.Text = ""
	if err != nil || json.Unmarshal([]byte(limited.text), &body) != nil || body.Text != "" || body.Representation != "reference_only" || len(body.Gaps) == 0 || !strings.Contains(limited.text, "exact_exceeds_budget") || f.viewCalls.Load() != 1 {
		t.Fatal("oversized exact bypassed budget or lost explicit gap")
	}
	f.allowed.Store(false)
	if exact.validate(context.Background()) == nil {
		t.Fatal("exact response remained valid after revocation")
	}
	if _, err := r.prepare(context.Background(), r.IDs()[0], "exact"); err == nil {
		t.Fatal("exact recovery bypassed current admission")
	}
}

func TestEvidenceRegistryRevocationChangeAndProviderFailure(t *testing.T) {
	for _, kind := range []string{"revoked", "changed", "changed_in_provider", "timeout", "panic"} {
		t.Run(kind, func(t *testing.T) {
			f := fixtureEvidence(t)
			b := f.binding
			if kind == "changed_in_provider" {
				b.ViewProvider = viewFunc(func(context.Context, string, []byte) ([]byte, error) {
					f.generation.Store(evidence.Digest("changed"))
					return []byte("view"), nil
				})
			}
			if kind == "timeout" {
				b.Request.BudgetBytes = 16
				b.ViewProvider = viewFunc(func(context.Context, string, []byte) ([]byte, error) { return nil, context.DeadlineExceeded })
			}
			if kind == "panic" {
				b.ViewProvider = viewFunc(func(context.Context, string, []byte) ([]byte, error) { panic("private panic") })
			}
			r, err := NewRegistry(context.Background(), f.owner, []Binding{b})
			if err != nil {
				t.Fatal(err)
			}
			if kind == "revoked" {
				f.allowed.Store(false)
			}
			if kind == "changed" {
				f.generation.Store(evidence.Digest("changed"))
			}
			pending, err := r.start(context.Background(), r.IDs()[0], "view")
			if err != nil {
				t.Fatal(err)
			}
			prepared := <-pending
			if kind == "timeout" {
				if prepared.err != nil || strings.Contains(prepared.value.text, "PRIVATE_RAW") || !strings.Contains(prepared.value.text, "reference_only") || !strings.Contains(prepared.value.text, "provider_timeout") {
					t.Fatalf("unsafe fallback: %+v", prepared)
				}
			} else if prepared.err == nil || prepared.value.text != "" {
				t.Fatal("failed evidence preparation emitted data")
			}
		})
	}
}

func TestEvidenceRegistryCancellationBoundsLateCooperativeState(t *testing.T) {
	f := fixtureEvidence(t)
	started, release := make(chan struct{}), make(chan struct{})
	b := f.binding
	b.ViewProvider = viewFunc(func(context.Context, string, []byte) ([]byte, error) {
		close(started)
		<-release
		return []byte("late view"), nil
	})
	r, err := NewRegistry(context.Background(), f.owner, []Binding{b})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	pending, err := r.start(ctx, r.IDs()[0], "view")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	cancel()
	if _, err := r.start(context.Background(), r.IDs()[0], "view"); err == nil {
		t.Fatal("canceled noncooperative callback accumulated another operation")
	}
	close(release)
	result := <-pending
	if !errors.Is(result.err, context.Canceled) || result.value.text != "" {
		t.Fatal("late provider output escaped cancellation")
	}
}
