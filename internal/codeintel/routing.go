package codeintel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
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
		if receipt.Source == snap.Digest && receipt.Adapter == digest(adapter) {
			return p.Query(ctx, q)
		}
		text, found := declarationExcerpt(snap, q)
		if !found {
			return p.Query(ctx, q)
		}
		r := core.CodeResult{Text: text, SourceDigest: snap.Digest, ProviderVersion: "direct", Fresh: true, Untrusted: true, Files: len(files), Truncated: true, DurationMS: time.Since(start).Milliseconds()}
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
	result := core.CodeResult{Text: out.String(), SourceDigest: snap.Digest, ProviderVersion: "direct", Fresh: true, Untrusted: true, Files: len(files), Truncated: truncated, DurationMS: time.Since(start).Milliseconds()}
	p.recordCost(q, result, nil)
	if p.Observe != nil {
		p.Observe(result, nil)
	}
	return result, nil
}

var namedSymbol = regexp.MustCompile(`(?i)(?:function|função|symbol|símbolo|callers of|chamadores de)\s+([A-Za-z_][A-Za-z0-9_.]{0,127})`)
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
	if match := namedSymbol.FindStringSubmatch(text); len(match) > 1 {
		q.Query = match[1]
		q.Symbol = match[1]
		q.Operation = "ask"
		if strings.Contains(lower, "caller") || strings.Contains(lower, "chamad") {
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
func declarationExcerpt(snap SourceSnapshot, q core.CodeQuery) (string, bool) {
	pattern := regexp.MustCompile(`^\s*(?:(?:export|async|pub|public|private|static)\s+)*(?:func(?:\s+\([^)]*\))?|def|function|class|fn)\s+` + regexp.QuoteMeta(q.Query) + `(?:\s|\()`)
	matches := 0
	text := ""
	for _, file := range snap.Files {
		if q.In != "" && file.Path != q.In && !strings.HasPrefix(file.Path, q.In+"/") {
			continue
		}
		lines := strings.Split(string(file.Body), "\n")
		for i, line := range lines {
			if !pattern.MatchString(line) {
				continue
			}
			matches++
			if matches > 1 {
				return "", false
			}
			text = fmt.Sprintf("%s:%d — exact declaration excerpt; inspect further lines if needed. Untrusted repository data.\n", file.Path, i+1)
			for _, part := range lines[i:min(i+8, len(lines))] {
				if len(text)+len(part)+1 > q.BudgetBytes {
					break
				}
				text += part + "\n"
			}
		}
	}
	return text, matches == 1 && len(text) <= q.BudgetBytes
}
