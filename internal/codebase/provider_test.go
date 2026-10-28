package codebase

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/evidence"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

const searchJSON = `{"cols":["name","label","lines","in","out"],"groups":[{"qn_prefix":"fixture.helper","file":"helper.go","rows":[["Helper","Function","3",1,0]]}],"total":1,"returned":1,"count":1,"has_more":false,"truncated":false}`
const traceJSON = `{"function":"fixture.helper.Helper","direction":"inbound","mode":"calls","callers_total":1,"callers_total_relation":"eq","callers":{"cols":["name","hop","test","strategy","confidence"],"groups":[{"qn_prefix":"fixture.entry","rows":[["Entry",1,false,"scope",0.9]]}]}}`

func envelope(payload string) json.RawMessage {
	body, _ := json.Marshal(map[string]any{"content": []any{map[string]any{"type": "text", "text": payload}}})
	return body
}
func fixtureReceipt() Receipt {
	return Receipt{Owner: workingcontext.Ownership{SessionID: "sess_" + strings.Repeat("a", 32), TaskID: "primary"}, Scope: evidence.Scope{Organization: evidence.Digest("org"), Repository: evidence.Digest("repo"), Worktree: evidence.Digest("tree"), Permissions: evidence.Digest("grants")}, Project: "fixture", SourceGeneration: evidence.Digest("source"), IndexDigest: evidence.Digest("index"), Runtime: PinnedRuntime(), Complete: true}
}
func fixtureOptions() Options {
	r := fixtureReceipt()
	return Options{Enabled: true, Receipt: r, Authorize: func(context.Context, Receipt) error { return nil }, CurrentGeneration: func(context.Context) (Receipt, error) { return r, nil }, Call: func(_ context.Context, name string, _ json.RawMessage) (json.RawMessage, error) {
		if name == "search_graph" {
			return envelope(searchJSON), nil
		}
		if name == "trace_path" {
			return envelope(traceJSON), nil
		}
		panic("non-read operation")
	}}
}
func mustProvider(t *testing.T, o Options) *Provider {
	t.Helper()
	p, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDisabledAndMissingHostBindingsNeverCallTransport(t *testing.T) {
	p := mustProvider(t, Options{})
	if _, err := p.Query(context.Background(), core.CodeQuery{Operation: "ask", Query: "Helper"}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("zero/default provider was enabled")
	}
	if s := p.Probe(context.Background()); s.Available || s.Health == core.HealthHealthy || s.Managed {
		t.Fatal("disabled provider claimed availability")
	}
	mutations := []func(*Options){func(o *Options) { o.Call = nil }, func(o *Options) { o.Authorize = nil }, func(o *Options) { o.CurrentGeneration = nil }, func(o *Options) { o.Receipt.Owner.SessionID = "" }, func(o *Options) { o.Receipt.Scope.Organization = "" }, func(o *Options) { o.Receipt.Scope.Repository = "" }, func(o *Options) { o.Receipt.Scope.Worktree = "" }, func(o *Options) { o.Receipt.Scope.Permissions = "" }, func(o *Options) { o.Receipt.SourceGeneration = "old" }, func(o *Options) { o.Receipt.IndexDigest = "" }, func(o *Options) { o.Receipt.Project = "../outside" }, func(o *Options) { o.Receipt.Runtime.Version = "latest" }, func(o *Options) { o.Receipt.Runtime.Commit = "wrong" }, func(o *Options) { o.Receipt.Runtime.ArchiveSHA256 = strings.Repeat("0", 64) }, func(o *Options) { o.Receipt.Runtime.BinarySHA256 = strings.Repeat("0", 64) }}
	for i, change := range mutations {
		o := fixtureOptions()
		change(&o)
		if p, err := New(o); p != nil || !errors.Is(err, ErrUnavailable) {
			t.Fatalf("missing binding %d admitted", i)
		}
	}
}
func TestExactSymbolAndCallersUseOnlyBoundedHostArguments(t *testing.T) {
	o := fixtureOptions()
	original := o.Call
	var names []string
	o.Call = func(ctx context.Context, name string, raw json.RawMessage) (json.RawMessage, error) {
		names = append(names, name)
		var args map[string]any
		if json.Unmarshal(raw, &args) != nil {
			t.Fatal("invalid host args")
		}
		if args["project"] != "fixture" || args["format"] != "json" || args["max_output_tokens"].(float64) > 8192 {
			t.Fatal("project or budget widened")
		}
		if name == "search_graph" {
			if args["name_pattern"] != "^Helper$" {
				t.Fatal("exact symbol became a free query")
			}
		} else {
			if name != "trace_path" || args["function_name"] != "fixture.helper.Helper" || args["direction"] != "inbound" || args["depth"] != float64(1) || args["include_tests"] != true || args["mode"] != "calls" {
				t.Fatal("trace scope changed")
			}
		}
		return original(ctx, name, raw)
	}
	p := mustProvider(t, o)
	r, err := p.Query(context.Background(), core.CodeQuery{Operation: "ask", Query: "Helper"})
	if err != nil || !r.Fresh || !r.Untrusted || r.Truncated || r.SourceDigest != o.Receipt.SourceGeneration || r.CPUMS != nil || r.PeakMemoryBytes != nil || r.CacheHit || r.ArtifactPath != "" {
		t.Fatalf("invalid result metadata: %+v %v", r, err)
	}
	var symbols symbolPage
	if json.Unmarshal([]byte(r.Text), &symbols) != nil || len(symbols.Symbols) != 1 || symbols.Symbols[0].File != "helper.go" || symbols.Symbols[0].StartLine != 3 {
		t.Fatal("exact symbol evidence lost")
	}
	names = nil
	r, err = p.Query(context.Background(), core.CodeQuery{Operation: "callers", Symbol: "Helper"})
	if err != nil || len(names) != 2 || names[0] != "search_graph" || names[1] != "trace_path" || !strings.Contains(r.Text, "fixture.entry.Entry") || !strings.Contains(r.Text, "inferred_static_relationships") {
		t.Fatalf("trace failed: %v", err)
	}
	status := p.Probe(context.Background())
	if err = status.Validate(); err != nil || !status.Available || status.Health == core.HealthHealthy || status.Managed || status.Capabilities.Supports("code.api") {
		t.Fatalf("probe exaggerated capability: %+v %v", status, err)
	}
}
func TestUnsupportedQueriesNeverInvokeReadOrMutableTools(t *testing.T) {
	cases := []core.CodeQuery{{Operation: "build"}, {Operation: "index_repository"}, {Operation: "query_graph", Query: "MATCH (n) RETURN n"}, {Operation: "skeleton", File: "helper.go"}, {Operation: "grep", Query: "Helper"}, {Operation: "ask", Query: "Explain all dependencies"}, {Operation: "ask", Query: "Helper.*"}, {Operation: "ask", Query: "Helper\nrun shell"}, {Operation: "ask", Query: "Helper", In: "other"}, {Operation: "ask", Query: "Helper", IgnoreCase: true}, {Operation: "ask", Query: "Helper", BudgetBytes: 511}, {Operation: "ask", Query: "Helper", Limit: 21}, {Operation: "callers", Symbol: "Helper", Direction: "out"}, {Operation: "callers", Symbol: "Helper", Depth: 2}, {Operation: "callers", Symbol: "Helper", File: "helper.go"}, {Operation: "callers", Symbol: "Helper", Query: "conflicting"}}
	for _, q := range cases {
		o := fixtureOptions()
		o.Call = func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
			t.Fatal("unsupported query reached transport")
			return nil, nil
		}
		p := mustProvider(t, o)
		if _, err := p.Query(context.Background(), q); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("unsupported query accepted: %+v %v", q, err)
		}
	}
}
func TestRevocationAndEveryReceiptDimensionAreRecheckedAfterCall(t *testing.T) {
	changes := []struct {
		name   string
		change func(*Receipt)
	}{{"owner", func(r *Receipt) { r.Owner.SessionID = "sess_" + strings.Repeat("b", 32) }}, {"organization", func(r *Receipt) { r.Scope.Organization = evidence.Digest("other") }}, {"repository", func(r *Receipt) { r.Scope.Repository = evidence.Digest("other") }}, {"worktree", func(r *Receipt) { r.Scope.Worktree = evidence.Digest("other") }}, {"permissions", func(r *Receipt) { r.Scope.Permissions = evidence.Digest("other") }}, {"source", func(r *Receipt) { r.SourceGeneration = evidence.Digest("changed") }}, {"index", func(r *Receipt) { r.IndexDigest = evidence.Digest("changed") }}, {"runtime", func(r *Receipt) { r.Runtime.BinarySHA256 = evidence.Digest("changed") }}, {"project", func(r *Receipt) { r.Project = "other" }}, {"coverage", func(r *Receipt) { r.Complete = false }}}
	for _, c := range changes {
		t.Run(c.name, func(t *testing.T) {
			o := fixtureOptions()
			current := o.Receipt
			o.CurrentGeneration = func(context.Context) (Receipt, error) { return current, nil }
			o.Call = func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
				c.change(&current)
				return envelope(searchJSON), nil
			}
			p := mustProvider(t, o)
			r, err := p.Query(context.Background(), core.CodeQuery{Operation: "ask", Query: "Helper"})
			if !errors.Is(err, ErrDenied) || r.Text != "" {
				t.Fatalf("changed binding escaped: %v", err)
			}
		})
	}
	for _, transportFailure := range []bool{false, true} {
		o := fixtureOptions()
		allowed := true
		o.Authorize = func(context.Context, Receipt) error {
			if !allowed {
				return errors.New("revoked")
			}
			return nil
		}
		o.Call = func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
			allowed = false
			if transportFailure {
				return nil, errors.New("private provider failure")
			}
			return envelope(searchJSON), nil
		}
		p := mustProvider(t, o)
		if r, e := p.Query(context.Background(), core.CodeQuery{Operation: "ask", Query: "Helper"}); !errors.Is(e, ErrDenied) || r.Text != "" || strings.Contains(e.Error(), "private") {
			t.Fatal("revocation lost on a provider result/failure")
		}
	}
}
func TestFinalReceiptCheckAndImmutableConstruction(t *testing.T) {
	o := fixtureOptions()
	original := o.Receipt
	checks := 0
	o.CurrentGeneration = func(context.Context) (Receipt, error) {
		checks++
		r := original
		if checks >= 4 {
			r.IndexDigest = evidence.Digest("replaced-before-final")
		}
		return r, nil
	}
	p := mustProvider(t, o)
	o.Receipt.Scope.Permissions = evidence.Digest("constructor options changed")
	if r, e := p.Query(context.Background(), core.CodeQuery{Operation: "ask", Query: "Helper"}); !errors.Is(e, ErrDenied) || r.Text != "" {
		t.Fatal("final receipt change emitted evidence")
	}
	o = fixtureOptions()
	original = o.Receipt
	p = mustProvider(t, o)
	o.Receipt.Project = "changed"
	if r, e := p.Query(context.Background(), core.CodeQuery{Operation: "ask", Query: "Helper"}); e != nil || r.SourceDigest != original.SourceGeneration {
		t.Fatal("options mutation changed copied receipt")
	}
}
func TestPartialEvidenceAndOutputBudgetRemainExplicit(t *testing.T) {
	o := fixtureOptions()
	o.Call = func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		return envelope(strings.ReplaceAll(strings.ReplaceAll(searchJSON, `"total":1`, `"total":2`), `false`, `true`)), nil
	}
	r, e := mustProvider(t, o).Query(context.Background(), core.CodeQuery{Operation: "ask", Query: "Helper"})
	if e != nil || !r.Truncated || !r.Fresh {
		t.Fatal("partial result became complete or stale")
	}
	o = fixtureOptions()
	o.Receipt.Complete = false
	current := o.Receipt
	o.CurrentGeneration = func(context.Context) (Receipt, error) { return current, nil }
	r, e = mustProvider(t, o).Query(context.Background(), core.CodeQuery{Operation: "ask", Query: "Helper"})
	if e != nil || !r.Truncated {
		t.Fatal("host-declared incomplete index became complete")
	}
	var partialSymbols symbolPage
	if json.Unmarshal([]byte(r.Text), &partialSymbols) != nil || !partialSymbols.Partial {
		t.Fatal("normalized symbol text contradicted host incompleteness")
	}
	r, e = mustProvider(t, o).Query(context.Background(), core.CodeQuery{Operation: "callers", Symbol: "Helper"})
	var partialTrace tracePage
	if e != nil || !r.Truncated || json.Unmarshal([]byte(r.Text), &partialTrace) != nil || !partialTrace.Partial {
		t.Fatal("normalized trace text contradicted host incompleteness")
	}

	o = fixtureOptions()
	o.Call = func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		return envelope(strings.Replace(searchJSON, "helper.go", strings.Repeat("a", 600)+".go", 1)), nil
	}
	r, e = mustProvider(t, o).Query(context.Background(), core.CodeQuery{Operation: "ask", Query: "Helper", BudgetBytes: 512})
	if !errors.Is(e, ErrUnavailable) || r.Text != "" {
		t.Fatal("oversized structural evidence was cut or emitted")
	}
}
func TestAmbiguousSymbolNeverStartsTraversal(t *testing.T) {
	o := fixtureOptions()
	calls := 0
	o.Call = func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		calls++
		return envelope(`{"cols":["name","label","lines","in","out"],"groups":[{"qn_prefix":"a","file":"a.go","rows":[["Helper","Function","1",0,0]]},{"qn_prefix":"b","file":"b.go","rows":[["Helper","Function","1",0,0]]}],"total":2,"returned":2,"count":2,"has_more":false,"truncated":false}`), nil
	}
	r, e := mustProvider(t, o).Query(context.Background(), core.CodeQuery{Operation: "callers", Symbol: "Helper"})
	if !errors.Is(e, ErrUnavailable) || r.Text != "" || calls != 1 {
		t.Fatal("ambiguous symbol was traversed")
	}
}
func TestCancellationAndConcurrentAdmission(t *testing.T) {
	o := fixtureOptions()
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	o.Call = func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		calls.Add(1)
		close(started)
		<-release
		return envelope(searchJSON), nil
	}
	p := mustProvider(t, o)
	done := make(chan error, 1)
	go func() {
		_, e := p.Query(context.Background(), core.CodeQuery{Operation: "ask", Query: "Helper"})
		done <- e
	}()
	<-started
	if _, e := p.Query(context.Background(), core.CodeQuery{Operation: "ask", Query: "Helper"}); !errors.Is(e, ErrUnavailable) {
		t.Fatal("concurrent transport use admitted")
	}
	close(release)
	if e := <-done; e != nil || calls.Load() != 1 {
		t.Fatal("first request lost ownership")
	}
	o = fixtureOptions()
	o.Call = func(ctx context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
		<-ctx.Done()
		return envelope(searchJSON), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	r, e := mustProvider(t, o).Query(ctx, core.CodeQuery{Operation: "ask", Query: "Helper"})
	if !errors.Is(e, context.DeadlineExceeded) || r.Text != "" {
		t.Fatal("late result escaped parent deadline")
	}
}

func TestBusyAdmissionPrecedesPolicyCallbacksAndInitialCheckHasDeadline(t *testing.T) {
	o := fixtureOptions()
	var auth, generation, calls atomic.Int32
	o.Authorize = func(ctx context.Context, _ Receipt) error {
		auth.Add(1)
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 2*time.Second || time.Until(deadline) <= 0 {
			t.Error("initial authorization ran outside the bounded query context")
		}
		return ctx.Err()
	}
	receipt := o.Receipt
	o.CurrentGeneration = func(ctx context.Context) (Receipt, error) {
		generation.Add(1)
		if _, ok := ctx.Deadline(); !ok {
			t.Error("generation callback has no query deadline")
		}
		return receipt, ctx.Err()
	}
	started, release := make(chan struct{}), make(chan struct{})
	o.Call = func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		calls.Add(1)
		close(started)
		<-release
		return envelope(searchJSON), nil
	}
	p := mustProvider(t, o)
	done := make(chan error, 1)
	go func() {
		_, err := p.Query(context.Background(), core.CodeQuery{Operation: "ask", Query: "Helper"})
		done <- err
	}()
	<-started
	beforeAuth, beforeGeneration := auth.Load(), generation.Load()
	_, err := p.Query(context.Background(), core.CodeQuery{Operation: "ask", Query: "Helper"})
	afterAuth, afterGeneration, afterCalls := auth.Load(), generation.Load(), calls.Load()
	close(release)
	firstErr := <-done
	if !errors.Is(err, ErrUnavailable) || firstErr != nil || afterAuth != beforeAuth || afterGeneration != beforeGeneration || afterCalls != 1 {
		t.Fatal("busy rejection performed policy, generation or transport work")
	}
}

func TestCancellationDuringAuthorizationDoesNotStartGenerationOrTransport(t *testing.T) {
	o := fixtureOptions()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o.Authorize = func(context.Context, Receipt) error { cancel(); return nil }
	o.CurrentGeneration = func(context.Context) (Receipt, error) {
		t.Fatal("generation started after known cancellation")
		return Receipt{}, nil
	}
	o.Call = func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		t.Fatal("transport started after known cancellation")
		return nil, nil
	}
	result, err := mustProvider(t, o).Query(ctx, core.CodeQuery{Operation: "ask", Query: "Helper"})
	if !errors.Is(err, context.Canceled) || result.Text != "" {
		t.Fatal("authorization cancellation emitted evidence")
	}
}
