package evidence

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/rtkview"
)

// These are real local output transformations, not provider inference or the
// 16-scenario economic certification matrix. Missing runtimes remain skipped.
func TestRealRTKGatewayABBA(t *testing.T) {
	binary, helper := os.Getenv("IVOAI_TEST_RTK_OSS"), os.Getenv("IVOAI_TEST_RTK_HELPER")
	if binary == "" || helper == "" {
		t.Skip("NOT_RUN: explicit pinned OSS binary and static helper required")
	}
	provider := rtkview.New(binary, helper)
	for _, scenario := range []struct {
		name, kind, filter, raw string
		derived                 bool
	}{
		{"grep_inventory", "inventory", "grep", strings.Repeat("src/example.go:12: referenced_symbol\n", 50), true},
		{"tiny_grep", "inventory", "grep", "a.go:1:x\n", false},
		{"diff_exact", "diff", "git-status", "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n", false},
		{"test_report_exact", "test_report", "grep", "FAIL: TestExample\n    expected 1; got 2\nFAIL\n", false},
		{"stack_error_exact", "error", "grep", "panic: failure\ngoroutine 1 [running]:\nexample/main.go:42\n", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newGatewayFixture(t)
			f.request.Source = "tool_read"
			f.request.BudgetBytes = 32768 // Both arms can present the entire raw fixture.
			doc := exactDocument()
			doc.Raw = []byte(scenario.raw)
			packet := mustRetrieve(t, f, f.request, doc)
			opts := inventoryOptions()
			opts.Kind, opts.Filter = scenario.kind, scenario.filter
			var elapsed [2]time.Duration
			var sizes [2]int
			for repetition := 0; repetition < 6; repetition++ {
				// AB/BA alternation keeps every unfavorable observation, too.
				for step := 0; step < 2; step++ {
					arm := (step + repetition) % 2
					options := opts
					options.Enabled = arm == 1
					start := time.Now()
					view, err := f.gateway.ViewOutput(context.Background(), f.request, packet.Ref, provider, options)
					duration := time.Since(start)
					elapsed[arm] += duration
					if err != nil {
						t.Fatalf("real arm %d failed: %v", arm, err)
					}
					if view.Derived != (arm == 1 && scenario.derived) {
						t.Fatalf("unexpected representation arm=%d derived=%v diagnostic=%s", arm, view.Derived, view.Diagnostic)
					}
					exact, err := f.gateway.Recover(context.Background(), f.request, view.Raw)
					if err != nil || !bytes.Equal(exact, doc.Raw) {
						t.Fatal("original output not exactly recoverable")
					}
					if !view.Derived && view.Text != scenario.raw {
						t.Fatal("exact-required output was transformed")
					}
					if view.Derived && !strings.Contains(view.Text, "example.go") {
						t.Fatal("inventory lost required path")
					}
					sizes[arm] += view.ViewBytes
					t.Logf("observation repetition=%d arm=%d duration_us=%d view_bytes=%d diagnostic=%s", repetition, arm, duration.Microseconds(), view.ViewBytes, view.Diagnostic)
				}
			}
			if f.store.puts.Load() != 1 {
				t.Fatal("view replaced or duplicated artifact storage")
			}
			t.Logf("repetitions=6 AB/BA raw_bytes=%d baseline_view_bytes=%d candidate_view_bytes=%d baseline_total_us=%d candidate_total_us=%d exact_recovery=true", len(doc.Raw), sizes[0], sizes[1], elapsed[0].Microseconds(), elapsed[1].Microseconds())
		})
	}
}
