package opencodebridge

import (
	"encoding/json"

	"github.com/ivo-lopes/ivoai/internal/core"
)

// The CLI transport preserves numeric lexemes. Never round fractional counters
// through float64, even when a fraction is below that representation's precision.
func reportedCounter(value any) (int64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	n, err := number.Int64()
	if err != nil || n < 0 || n > 1<<53-1 {
		return 0, false
	}
	return n, true
}

func observeCodexUsage(trace *ExecutionTrace, value any) {
	if value == nil { // Missing usage remains unknown, never zero.
		return
	}
	usage, ok := value.(map[string]any)
	input, inputOK := reportedCounter(usage["input_tokens"])
	output, outputOK := reportedCounter(usage["output_tokens"])
	if !ok || !inputOK || !outputOK {
		trace.Usage, trace.UsageStatus = nil, "invalid"
		return
	}
	var cache *int64
	if raw, present := usage["cached_input_tokens"]; present {
		n, valid := reportedCounter(raw)
		if !valid || n > input {
			trace.Usage, trace.UsageStatus = nil, "invalid"
			return
		}
		cache = &n
	}
	// A malformed or conflicting terminal report poisons this invocation. A
	// later apparently valid report must not silently repair its provenance.
	if trace.UsageStatus == "invalid" || trace.UsageStatus == "conflicting" {
		return
	}
	next := &core.ReportedTokenUsage{InputTokens: input, OutputTokens: output, CachedInputTokens: cache, Basis: "client_reported", Source: "codex_exec_turn_completed", Scope: "cli_invocation"}
	if previous := trace.Usage; previous != nil {
		equalCache := previous.CachedInputTokens == nil && cache == nil || previous.CachedInputTokens != nil && cache != nil && *previous.CachedInputTokens == *cache
		if previous.InputTokens != input || previous.OutputTokens != output || !equalCache {
			trace.Usage, trace.UsageStatus = nil, "conflicting"
		}
		return // Identical duplicates are one report, never added twice.
	}
	trace.Usage, trace.UsageStatus = next, "reported"
}

func copyReportedUsage(usage *core.ReportedTokenUsage) *core.ReportedTokenUsage {
	if usage == nil {
		return nil
	}
	copy := *usage
	if usage.CachedInputTokens != nil {
		cache := *usage.CachedInputTokens
		copy.CachedInputTokens = &cache
	}
	return &copy
}
