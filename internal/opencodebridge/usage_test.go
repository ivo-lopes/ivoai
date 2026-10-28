package opencodebridge

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func usageFixture(input, output any) map[string]any {
	return map[string]any{"input_tokens": input, "output_tokens": output}
}

func TestUsageScannerPreservesPublicNumericContract(t *testing.T) {
	const event = "{\"type\":\"fixture\",\"counter\":9007199254740990.1}\n"
	if err := ScanJSONLines(strings.NewReader(event), func(value map[string]any) error {
		if _, ok := value["counter"].(float64); !ok {
			t.Fatal("public scanner numeric callback contract changed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := scanJSONLines(strings.NewReader(event), func(value map[string]any) error {
		if value["counter"] != json.Number("9007199254740990.1") {
			t.Fatal("CLI scanner rounded the numeric lexeme")
		}
		return nil
	}, true); err != nil {
		t.Fatal(err)
	}
	if err := scanJSONLines(strings.NewReader("{\"type\":\"fixture\"} {}\n"), func(map[string]any) error { return nil }, true); err == nil {
		t.Fatal("CLI scanner accepted trailing JSON")
	}
}

func TestReportedUsagePreservesUnknownCacheAndRejectsInvalidCounters(t *testing.T) {
	for _, value := range []any{json.Number("-1"), json.Number("1.5"), json.Number("9007199254740992"), json.Number("9007199254740990.1"), json.Number("1e2"), float64(100), math.Inf(1), math.NaN(), "100", true, nil} {
		trace := ExecutionTrace{}
		observeCodexUsage(&trace, usageFixture(value, json.Number("2")))
		if trace.Usage != nil || trace.UsageStatus != "invalid" {
			t.Fatal("invalid counter was accepted")
		}
		observeCodexUsage(&trace, usageFixture(json.Number("100"), json.Number("2")))
		if trace.Usage != nil || trace.UsageStatus != "invalid" {
			t.Fatal("later valid report repaired invalid provenance")
		}
	}
	for _, value := range []any{"unexpected", []any{}, map[string]any{}, usageFixture(json.Number("100"), "2")} {
		trace := ExecutionTrace{}
		observeCodexUsage(&trace, value)
		if trace.Usage != nil || trace.UsageStatus != "invalid" {
			t.Fatal("invalid usage shape was accepted")
		}
	}
	trace := ExecutionTrace{}
	observeCodexUsage(&trace, nil)
	if trace.Usage != nil || trace.UsageStatus != "" {
		t.Fatal("missing usage must remain unknown and backward compatible")
	}
	observeCodexUsage(&trace, usageFixture(json.Number("100"), json.Number("2")))
	if trace.Usage == nil || trace.Usage.CachedInputTokens != nil || trace.Usage.InputTokens != 100 || trace.Usage.OutputTokens != 2 || trace.UsageStatus != "reported" {
		t.Fatal("unknown cache became a measured zero")
	}
}

func TestReportedUsageCacheSubsetDuplicatesAndConflicts(t *testing.T) {
	for _, cache := range []any{json.Number("-1"), json.Number("101"), json.Number("1.5"), nil, "0"} {
		trace := ExecutionTrace{}
		usage := usageFixture(json.Number("100"), json.Number("2"))
		usage["cached_input_tokens"] = cache
		observeCodexUsage(&trace, usage)
		if trace.Usage != nil || trace.UsageStatus != "invalid" {
			t.Fatal("invalid cache subset was accepted")
		}
	}
	trace := ExecutionTrace{}
	usage := usageFixture(json.Number("100"), json.Number("2"))
	usage["cached_input_tokens"] = json.Number("60")
	observeCodexUsage(&trace, usage)
	observeCodexUsage(&trace, usage)
	if trace.Usage == nil || trace.Usage.InputTokens != 100 || *trace.Usage.CachedInputTokens != 60 {
		t.Fatal("identical reports were summed or cache was charged again")
	}
	copy := copyReportedUsage(trace.Usage)
	copy.InputTokens, *copy.CachedInputTokens = 0, 0
	if trace.Usage.InputTokens != 100 || *trace.Usage.CachedInputTokens != 60 {
		t.Fatal("progress copy can mutate retained usage")
	}
	observeCodexUsage(&trace, usageFixture(json.Number("100"), json.Number("2")))
	if trace.Usage != nil || trace.UsageStatus != "conflicting" {
		t.Fatal("conflicting partial report was silently merged")
	}
	observeCodexUsage(&trace, usage)
	if trace.Usage != nil || trace.UsageStatus != "conflicting" {
		t.Fatal("later report repaired conflicting provenance")
	}
}

func TestCodexRunnerReportsBoundedUsageWithoutPrivateFields(t *testing.T) {
	root := t.TempDir()
	path := fixtureExecutable(t, root, "codex", `#!/bin/sh
printf '%s\n' '{"type":"thread.started","thread_id":"thread_fixture"}'
printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"fixture answer"}}'
printf '%s\n' '{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":60,"output_tokens":2,"private_field":"private-usage-payload"}}'
printf '%s\n' '{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":60,"output_tokens":2}}'
`)
	progressReports := 0
	result, err := (CLIRunner{Codex: ExecutorSpec{Path: path, Dir: root, ObserveProgress: func(trace ExecutionTrace) {
		if trace.Usage != nil {
			progressReports++
			trace.Usage.InputTokens = 0
			*trace.Usage.CachedInputTokens = 0
		}
	}}}).Run(context.Background(), ExecutorRequest{Executor: "codex"}, func(string) error { return nil })
	if err != nil || result.Trace == nil || result.Trace.Usage == nil || progressReports != 2 {
		t.Fatalf("missing usage/progress: %v", err)
	}
	usage := result.Trace.Usage
	if usage.InputTokens != 100 || usage.OutputTokens != 2 || *usage.CachedInputTokens != 60 || usage.Basis != "client_reported" || usage.Source != "codex_exec_turn_completed" || usage.Scope != "cli_invocation" {
		t.Fatal("usage semantics changed")
	}
	encoded, err := json.Marshal(result.Trace)
	if err != nil || strings.Contains(string(encoded), "private-") || strings.Contains(string(encoded), "fixture answer") {
		t.Fatal("usage trace leaked an unallowlisted field or answer")
	}
}
