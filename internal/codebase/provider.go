// Package codebase is an optional, host-injected reader of a previously built
// Codebase Memory MCP index. It owns no transport, daemon, indexer or database.
package codebase

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/evidence"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

const (
	Version       = "0.12.1"
	SourceCommit  = "a7158ac2f48ca646b4527341b76f930f2aeff87e"
	ArchiveSHA256 = "8c3d0341064301e78ecc84cf7348d4e2c6562a0f57448243ea560aaa34de36c1"
	BinarySHA256  = "77745491f0a9c346d6da23eed4d92b5feaa31a27a883c9faa4fa9f47c8183705"
	MaxReplyBytes = 65536
)

var (
	ErrUnavailable = errors.New("optional codebase capability unavailable")
	ErrDenied      = errors.New("codebase host receipt no longer authorized or current")
	ErrResponse    = errors.New("invalid or unsupported codebase response")
	digestPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	identifier     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)
	projectPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]{0,127}$`)
)

// Runtime is host-verified provenance, not values copied from MCP initialize.
type Runtime struct{ Version, Commit, ArchiveSHA256, BinarySHA256 string }

func PinnedRuntime() Runtime { return Runtime{Version, SourceCommit, ArchiveSHA256, BinarySHA256} }

// Receipt is a value copied at construction. The host must have separately
// authorized indexing and validated this source/index/runtime/scope binding.
// Complete concerns the admitted index inventory, not semantic correctness.
// No receipt is an authorization token: live callbacks remain mandatory.
type Receipt struct {
	Owner            workingcontext.Ownership
	Scope            evidence.Scope
	Project          string
	SourceGeneration string
	IndexDigest      string
	Runtime          Runtime
	Complete         bool
}

// Caller uses an already-owned, isolated read-only MCP transport. It must bound
// ingress and serialize/multiplex its protocol safely. The returned bytes are a
// tools/call RESULT envelope (content/structuredContent/isError), not JSON-RPC.
// Only search_graph and trace_path can be selected here. Neither callback nor
// this adapter may use a read query as permission to build/rebuild an index.
// Cancellation is cooperative: a caller ignoring ctx can delay the return; late
// results are discarded, not forcibly terminated by this in-process boundary.
type Caller func(context.Context, string, json.RawMessage) (json.RawMessage, error)
type Options struct {
	Enabled   bool // zero/default never calls the transport or host callbacks
	Receipt   Receipt
	Call      Caller
	Authorize func(context.Context, Receipt) error
	// CurrentGeneration MUST observe source additions/edits/deletions, the index,
	// runtime and scope/permission binding, using host evidence, not MCP claims.
	CurrentGeneration func(context.Context) (Receipt, error)
}
type Provider struct {
	enabled   bool
	receipt   Receipt
	call      Caller
	authorize func(context.Context, Receipt) error
	current   func(context.Context) (Receipt, error)
	busy      chan struct{}
}

func New(o Options) (*Provider, error) {
	p := &Provider{enabled: o.Enabled, receipt: o.Receipt, call: o.Call, authorize: o.Authorize, current: o.CurrentGeneration, busy: make(chan struct{}, 1)}
	if o.Enabled && (!validReceipt(o.Receipt) || o.Call == nil || o.Authorize == nil || o.CurrentGeneration == nil) {
		return nil, ErrUnavailable
	}
	return p, nil
}
func validReceipt(r Receipt) bool {
	if r.Owner.Validate() != nil || !projectPattern.MatchString(r.Project) || r.Runtime != PinnedRuntime() {
		return false
	}
	for _, d := range []string{r.Scope.Organization, r.Scope.Repository, r.Scope.Worktree, r.Scope.Permissions, r.SourceGeneration, r.IndexDigest} {
		if !digestPattern.MatchString(d) {
			return false
		}
	}
	return true
}
func (p *Provider) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || !p.enabled || p.call == nil || p.authorize == nil || p.current == nil || !validReceipt(p.receipt) {
		return ErrUnavailable
	}
	if p.authorize(ctx, p.receipt) != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := p.current(ctx)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || current != p.receipt {
		return ErrDenied
	}
	return nil
}
func (p *Provider) ID() core.ComponentID { return "code_intelligence" }
func (p *Provider) Probe(ctx context.Context) core.ComponentStatus {
	available := p.check(ctx) == nil
	// Installed is the host's valid runtime/index attestation, not local probing
	// or an assertion that this adapter distributes/manages the external binary.
	installed := p != nil && p.enabled && validReceipt(p.receipt) && p.call != nil
	health := core.HealthUnavailable
	if available {
		health = core.HealthUnknown
	}
	return core.ComponentStatus{ID: p.ID(), Implementation: "codebase_memory", Installed: installed, Managed: false, Available: available, Health: health, Lifecycle: core.LifecycleUnknown, Provenance: core.Provenance{Source: "https://github.com/DeusData/codebase-memory-mcp", Version: Version + "@" + SourceCommit}, Capabilities: core.CapabilitySet{"code.symbol.exact": core.SupportSupported, "code.callers.direct": core.SupportSupported, "code.api": core.SupportNotExposed, "code.impact": core.SupportNotExposed}, Compatibility: core.Compatibility{State: core.CompatibilityUnknown, Reason: "host-attested isolated transport and prior index; no runtime parity certification"}, Fallback: core.Fallback{Allowed: true, Reason: "host may select existing Graft or permitted direct inspection"}}
}

func normalize(q core.CodeQuery) (core.CodeQuery, error) {
	if q.Operation != "ask" && q.Operation != "callers" {
		return q, ErrUnavailable
	}
	if q.File != "" || q.In != "" || q.IgnoreCase || q.StartLine != 0 || q.EndLine != 0 {
		return q, ErrUnavailable
	}
	symbol := q.Query
	if q.Operation == "ask" {
		if q.Symbol != "" || q.Direction != "" || q.Depth != 0 {
			return q, ErrUnavailable
		}
	} else {
		if q.Query != "" {
			return q, ErrUnavailable
		}
		symbol = q.Symbol
		if q.Direction == "" {
			q.Direction = "in"
		}
		if q.Depth == 0 {
			q.Depth = 1
		}
		if q.Direction != "in" || q.Depth != 1 {
			return q, ErrUnavailable
		}
	}
	if len(symbol) == 0 || len(symbol) > 256 || !identifier.MatchString(symbol) {
		return q, ErrUnavailable
	}
	if q.Limit == 0 {
		q.Limit = 5
	}
	if q.Limit < 1 || q.Limit > 20 {
		return q, ErrUnavailable
	}
	if q.BudgetBytes == 0 {
		q.BudgetBytes = 8192
	}
	if q.BudgetBytes < 512 || q.BudgetBytes > 32768 {
		return q, ErrUnavailable
	}
	return q, nil
}
func (p *Provider) invoke(ctx context.Context, name string, args any) (json.RawMessage, error) {
	if err := p.check(ctx); err != nil {
		return nil, err
	}
	if name != "search_graph" && name != "trace_path" {
		return nil, ErrUnavailable
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, ErrUnavailable
	}
	body, callErr := p.call(ctx, name, encoded)
	oversized := len(body) > MaxReplyBytes
	if !oversized {
		body = bytes.Clone(body)
	}
	// Recheck even failures and malformed replies, before interpreting or exposing
	// any external data. The host owns revocation and immutable index publication.
	if err := p.check(ctx); err != nil {
		return nil, err
	}
	if callErr != nil {
		return nil, ErrUnavailable
	}
	if oversized {
		return nil, ErrResponse
	}
	return toolPayload(body)
}
func (p *Provider) search(ctx context.Context, symbol string, limit, budget int) (symbolPage, error) {
	args := map[string]any{"project": p.receipt.Project, "limit": limit, "format": "json", "max_output_tokens": budget / 4}
	key := "name_pattern"
	if strings.Contains(symbol, ".") {
		key = "qn_pattern"
	}
	args[key] = "^" + regexp.QuoteMeta(symbol) + "$"
	body, err := p.invoke(ctx, "search_graph", args)
	if err != nil {
		return symbolPage{}, err
	}
	return parseSymbols(body, symbol, limit)
}
func (p *Provider) Query(ctx context.Context, q core.CodeQuery) (core.CodeResult, error) {
	start := time.Now()
	var result core.CodeResult
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if p == nil || !p.enabled {
		return result, ErrUnavailable
	}
	q, err := normalize(q)
	if err != nil {
		return result, err
	}
	select {
	case p.busy <- struct{}{}:
		defer func() { <-p.busy }()
	default:
		return result, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := p.check(ctx); err != nil {
		return result, err
	}
	symbol := q.Query
	limit := q.Limit
	if q.Operation == "callers" {
		symbol = q.Symbol
		limit = 2
	}
	symbols, err := p.search(ctx, symbol, limit, q.BudgetBytes)
	if err != nil {
		return result, err
	}
	lookupPartial := symbols.Partial
	symbols.Partial = symbols.Partial || !p.receipt.Complete
	var output any = symbols
	partial := symbols.Partial
	if q.Operation == "callers" {
		// Resolve exactly one indexed symbol before traversing. A union of ambiguous
		// short names must not masquerade as callers of one implementation.
		if lookupPartial || symbols.Total != 1 || len(symbols.Symbols) != 1 {
			return result, ErrUnavailable
		}
		target := symbols.Symbols[0].QualifiedName
		body, e := p.invoke(ctx, "trace_path", map[string]any{"project": p.receipt.Project, "function_name": target, "direction": "inbound", "depth": 1, "limit": q.Limit, "include_tests": true, "include_evidence": true, "mode": "calls", "max_output_tokens": q.BudgetBytes / 4, "format": "json"})
		if e != nil {
			return result, e
		}
		trace, e := parseTrace(body, target, q.Limit)
		if e != nil {
			return result, e
		}
		partial = partial || trace.Partial
		trace.Partial = partial
		output = trace
	}
	text, err := json.Marshal(output)
	if err != nil {
		return result, ErrResponse
	}
	if err = p.check(ctx); err != nil {
		return result, err
	}
	// Never byte-slice a structural relation or an authoritative symbol. A host
	// can narrow the query/use the existing artifact path under its own policy.
	if len(text) > q.BudgetBytes {
		return result, ErrUnavailable
	}
	result = core.CodeResult{Operation: q.Operation, Text: string(text), SourceDigest: p.receipt.SourceGeneration, ProviderVersion: Version + "@" + SourceCommit, Fresh: true, Untrusted: true, Truncated: partial, DurationMS: time.Since(start).Milliseconds()}
	return result, nil
}

var _ core.CodeIntelligenceProvider = (*Provider)(nil)
