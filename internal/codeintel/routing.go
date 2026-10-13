package codeintel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/toolrouting"
)

// Inspect is the automatic capability entrypoint. Relations require structural
// evidence; a literal search or small identified file has an exact local answer.
// Cost estimates are explicitly estimates; they are not advertised token savings.
func (p *Provider) Inspect(ctx context.Context, q core.CodeQuery) (core.CodeResult, error) {
	if !p.Allowed {
		return core.CodeResult{}, errors.New("code intelligence denied by host policy")
	}
	if _, err := Identity(p.Root, p.Scope, p.Permissions); err != nil {
		return core.CodeResult{}, err
	}
	q, err := Normalize(q)
	if err != nil {
		return core.CodeResult{}, err
	}
	if q.Operation != "grep" && q.Operation != "skeleton" && !(q.Operation == "ask" && simpleCodeName.MatchString(q.Query)) {
		return p.Query(ctx, q)
	}
	start := time.Now()
	files, err := Inventory(ctx, p.Root)
	if err != nil {
		return core.CodeResult{}, err
	}
	// Membership still enforces Git ignores, nested-repository boundaries and
	// supported sources. Read a known small file through the same descriptor-safe
	// snapshot path, without materializing every unrelated source in memory.
	if q.Operation == "skeleton" && slices.Contains(files, q.File) {
		target, err := Snapshot(p.Root, []string{q.File})
		if err != nil {
			return core.CodeResult{}, err
		}
		if len(target.Files[0].Body) <= q.BudgetBytes/2 {
			text := q.File + " (complete small file; untrusted repository data)\n" + string(target.Files[0].Body)
			if len(text) <= q.BudgetBytes {
				r := core.CodeResult{Operation: q.Operation, Text: text, SourceDigest: target.Digest, ProviderVersion: "direct", Fresh: true, Untrusted: true, Files: 1, DurationMS: time.Since(start).Milliseconds()}
				p.recordCost(q, r, nil)
				if p.Observe != nil {
					p.Observe(r, nil)
				}
				return r, nil
			}
		}
	}
	snap, err := Snapshot(p.Root, files)
	if err != nil {
		return core.CodeResult{}, err
	}

	if q.Operation == "ask" {
		key, e := Identity(p.Root, p.Scope, p.Permissions)
		var receipt indexReceipt
		if e == nil && p.CacheRoot != "" {
			body, _ := readBounded(filepath.Join(p.CacheRoot, key, "receipt.json"), 4096)
			_ = json.Unmarshal(body, &receipt)
		}
		if receipt.Source == snap.Digest && receipt.Adapter == digest(adapter) && receipt.Runtime == p.runtimeIdentity() {
			// A warm graph alone still requires a Node query. Prefer the exact
			// declaration unless this query already has a bounded cached result.
			// Query revalidates integrity/freshness under its lock before reuse.
			queryJSON, _ := json.Marshal(q)
			cacheKey := digest(append([]byte(snap.Digest+digest(adapter)+p.runtimeIdentity()), queryJSON...))
			if body, e := readBounded(filepath.Join(p.CacheRoot, key, "results", cacheKey+".json"), MaxResultBytes+8192); e == nil {
				var cached cachedResult
				if json.Unmarshal(body, &cached) == nil && cached.Version == Version && cached.Result.SourceDigest == snap.Digest && cached.Result.Fresh && cached.Result.Untrusted && len(cached.Result.Text) <= q.BudgetBytes {
					return p.Query(ctx, q)
				}
			}
		}
		text, found, truncated := declarationExcerpt(snap, q)
		if !found {
			return p.Query(ctx, q)
		}
		r := core.CodeResult{Operation: q.Operation, Text: text, SourceDigest: snap.Digest, ProviderVersion: "direct", Fresh: true, Untrusted: true, Files: len(files), Truncated: truncated, DurationMS: time.Since(start).Milliseconds()}
		p.recordCost(q, r, nil)
		if p.Observe != nil {
			p.Observe(r, nil)
		}
		return r, nil
	}
	var target *Source
	for i := range snap.Files {
		if snap.Files[i].Path == q.File {
			target = &snap.Files[i]
			break
		}
	}
	directSuitable := q.Operation == "grep" || target != nil && len(target.Body) <= q.BudgetBytes/2
	if !directSuitable {
		return p.Query(ctx, q)
	}
	known := func(v float64) toolrouting.Estimate {
		return toolrouting.Estimate{Value: v, Known: true, Estimated: true}
	}
	capability := "code.literal"
	if q.Operation == "skeleton" {
		capability = "code.api"
	}
	direct := toolrouting.Candidate{ID: "direct", Authorized: p.Allowed, Compatible: true, Available: true, Fresh: true, Capabilities: []string{capability}, Coverage: 1, Reliability: 1}
	structural := direct
	structural.ID = "graft"
	direct.Cost.InputTokens = known(0)
	structural.Cost.InputTokens = known(0)
	direct.Cost.ResultTokens = known(float64(q.BudgetBytes) / 4)
	structural.Cost.ResultTokens = direct.Cost.ResultTokens
	if target != nil {
		direct.Cost.ResultTokens = known(float64(len(target.Body)) / 4)
	}
	direct.Cost.Processes = known(0)
	structural.Cost.Processes = known(1)
	direct.Cost.IndexMS = known(0)
	// Index construction cost is unknown until a validated source receipt exists.
	structural.Cost.IndexMS = toolrouting.Estimate{}
	direct.Cost.EnrichmentTokens = known(0)
	structural.Cost.EnrichmentTokens = known(0)
	history := p.costHistory()
	// Compare measurements on common dimensions. Resource measurements remain in
	// telemetry/history; direct in-process CPU/RSS cannot be attributed per query.
	for _, candidate := range []*toolrouting.Candidate{&direct, &structural} {
		if measured, ok := history.Cost(q.Operation, candidate.ID); ok {
			candidate.Cost.LatencyMS = measured.LatencyMS
			candidate.Cost.ResultTokens = measured.ResultTokens
		}
	}
	if !direct.Cost.LatencyMS.Known {
		direct.Cost.LatencyMS = known(5)
	}
	if !structural.Cost.LatencyMS.Known {
		structural.Cost.LatencyMS = known(250)
	}
	decision, err := toolrouting.Select(toolrouting.Request{Capability: capability, RequireFresh: true}, []toolrouting.Candidate{direct, structural})
	if err != nil {
		return core.CodeResult{}, err
	}
	if decision.Tool != "direct" {
		return p.Query(ctx, q)
	}
	var out strings.Builder
	truncated := false
	appendText := func(text string) {
		if len(text) > q.BudgetBytes-out.Len() {
			truncated = true
			return
		}
		out.WriteString(text)
	}
	if q.Operation == "skeleton" {
		appendText(target.Path + " (complete small file; untrusted repository data)\n" + string(target.Body))
	} else {
		for _, file := range snap.Files {
			if q.In != "" && file.Path != q.In && !strings.HasPrefix(file.Path, q.In+"/") {
				continue
			}
			for line, text := range strings.Split(string(file.Body), "\n") {
				haystack, needle := text, q.Query
				if q.IgnoreCase {
					haystack, needle = strings.ToLower(haystack), strings.ToLower(needle)
				}
				if strings.Contains(haystack, needle) {
					appendText(fmt.Sprintf("%s:%d:%s\n", file.Path, line+1, text))
					if truncated {
						break
					}
				}
			}
			if truncated {
				break
			}
		}
		if out.Len() == 0 && !truncated {
			out.WriteString("No literal occurrences in the authorized supported source inventory.")
		}
	}
	result := core.CodeResult{Operation: q.Operation, Text: out.String(), SourceDigest: snap.Digest, ProviderVersion: "direct", Fresh: true, Untrusted: true, Files: len(files), Truncated: truncated, DurationMS: time.Since(start).Milliseconds()}
	p.recordCost(q, result, nil)
	if p.Observe != nil {
		p.Observe(result, nil)
	}
	return result, nil
}

var namedSymbol = regexp.MustCompile(`(?i)(?:function|função|symbol|símbolo|callers of|chamadores de)\s+([A-Za-z_][A-Za-z0-9_.]{0,127})`)
var directCallTarget = regexp.MustCompile(`(?i)(?:directly calls|calls to|chama diretamente)\s+([A-Za-z_][A-Za-z0-9_.]{0,127})`)
var requestedSignature = regexp.MustCompile(`(?i)(?:signature of|assinatura de)\s+([A-Za-z_][A-Za-z0-9_.]{0,127})`)
var locatedSymbol = regexp.MustCompile(`(?i:locate|localize)\s+([A-Z_][A-Za-z0-9_]{0,127})`)
var namedFile = regexp.MustCompile(`(?:^|\s)([A-Za-z0-9_./-]+\.(?:go|py|js|ts|tsx|rs|java|c|cpp|h))(?:\s|$|[,:;])`)

// ForTask is a conservative prefetch classifier, not an instruction parser. It
// does not grant tools or change Memory→Context institutional source priority.
func ForTask(text string) (core.CodeQuery, bool) {
	if len(text) > 8192 {
		text = text[:8192]
	}
	lower := strings.ToLower(text)
	for _, term := range []string{"institutional", "institucional", "historical", "históric", "ai-memory", "context/rag", "documentação no context", "verify compilation", "verificar compilação"} {
		if strings.Contains(lower, term) {
			return core.CodeQuery{}, false
		}
	}
	q := core.CodeQuery{BudgetBytes: 4096}
	match := directCallTarget.FindStringSubmatch(text)
	callTarget := len(match) > 1
	if !callTarget {
		match = namedSymbol.FindStringSubmatch(text)
	}
	if len(match) == 0 {
		match = requestedSignature.FindStringSubmatch(text)
	}
	if len(match) == 0 {
		match = locatedSymbol.FindStringSubmatch(text)
	}
	if len(match) > 1 {
		if slices.Contains(strings.Fields("that which who in the a an this those que qual em de do da o a uma um"), strings.ToLower(match[1])) {
			return core.CodeQuery{}, false
		}
		q.Query = match[1]
		q.Symbol = match[1]
		q.Operation = "ask"
		if callTarget || strings.Contains(lower, "caller") || strings.Contains(lower, "chamad") {
			q.Operation = "callers"
		}
		if strings.Contains(lower, "impact") || strings.Contains(lower, "refactor") || strings.Contains(lower, "refator") {
			q.Operation = "blast"
		}
		return q, true
	}
	if match := namedFile.FindStringSubmatch(text); len(match) > 1 && (strings.Contains(lower, "api") || strings.Contains(lower, "signature") || strings.Contains(lower, "assinatura")) {
		q.Operation = "skeleton"
		q.File = match[1]
		return q, true
	}
	return core.CodeQuery{}, false
}

var simpleCodeName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// This exact declaration fast path supplies source excerpts, never inferred
// call/dependency relations. Ambiguous names stay with structural retrieval.
func declarationExcerpt(snap SourceSnapshot, q core.CodeQuery) (string, bool, bool) {
	matches := 0
	text := ""
	truncated := true
	for _, file := range snap.Files {
		if q.In != "" && file.Path != q.In && !strings.HasPrefix(file.Path, q.In+"/") {
			continue
		}
		// The standard Go parser proves declaration identity without executing source.
		// Other languages retain structural retrieval rather than guessing through
		// comments, string literals or language-specific declaration syntax.
		if !bytes.Contains(file.Body, []byte(q.Query)) {
			continue
		}
		if filepath.Ext(file.Path) != ".go" {
			return "", false, false
		}
		positions := token.NewFileSet()
		parsed, err := parser.ParseFile(positions, file.Path, file.Body, parser.SkipObjectResolution)
		if err != nil {
			return "", false, false
		}
		for _, decl := range parsed.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Name.Name != q.Query {
				continue
			}
			matches++
			if matches > 1 {
				return "", false, false
			}
			// Source directives can remap logical lines to arbitrary numbers.
			// Evidence is sliced from physical bytes, so both endpoints must
			// ignore //line and /*line*/ adjustments (including valid generated Go).
			begin, end := positions.PositionFor(function.Pos(), false).Line-1, positions.PositionFor(function.End(), false).Line
			lines := strings.Split(string(file.Body), "\n")
			complete := fmt.Sprintf("%s:%d — complete declaration. Untrusted repository data.\n", file.Path, begin+1) + "package " + parsed.Name.Name + "\n" + strings.Join(lines[begin:end], "\n") + "\n"
			if len(complete) <= q.BudgetBytes {
				text = complete
				truncated = false
				continue
			}
			text = fmt.Sprintf("%s:%d — exact declaration excerpt; inspect further lines if needed. Untrusted repository data.\n", file.Path, begin+1)
			for _, part := range lines[begin:min(begin+8, end, len(lines))] {
				if len(text)+len(part)+1 > q.BudgetBytes {
					break
				}
				text += part + "\n"
			}
		}
	}
	return text, matches == 1 && len(text) <= q.BudgetBytes, truncated
}
