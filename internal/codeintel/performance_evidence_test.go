package codeintel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
)

// Explicit artifact benchmark, never an activation switch in the product. The
// 100-ms warm target follows the measured 8–12-ms baseline with scheduling margin.
// Timing targets are reported rather than made flaky correctness assertions.
func TestStructuralPerformanceEvidence(t *testing.T) {
	if os.Getenv("IVOAI_GRAFT_PERFORMANCE") != "1" {
		t.Skip("opt-in reviewed artifact performance measurement")
	}
	p, _ := testProvider(t)
	p.Node, p.Distribution = os.Getenv("IVOAI_GRAFT_TEST_NODE"), os.Getenv("IVOAI_GRAFT_TEST_DIST")
	if p.Node == "" || p.Distribution == "" {
		t.Fatal("reviewed runtime required")
	}
	p.Executable, p.run = os.Args[0], p.runProcess
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(p.Root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package fixture\nfunc Shared(v int) int { return v*2 }\n")
	for i := range 120 {
		write(fmt.Sprintf("module%03d.go", i), fmt.Sprintf("package fixture\nfunc Module%d() int { return Shared(%d) }\n", i, i))
	}
	type sample struct {
		Phase                      string
		WallMS                     int64
		Bytes                      int
		CacheHit                   bool
		CPUTimeMS, PeakMemoryBytes *int64
		Correct                    bool
	}
	rows := []sample{}
	query := core.CodeQuery{Operation: "skeleton", File: "main.go", BudgetBytes: 4096}
	run := func(phase string, q core.CodeQuery, want string, cache bool) core.CodeResult {
		t.Helper()
		start := time.Now()
		r, err := p.Query(context.Background(), q)
		correct := err == nil && strings.Contains(r.Text, want) && r.CacheHit == cache && len(r.Text) <= q.BudgetBytes
		rows = append(rows, sample{phase, time.Since(start).Milliseconds(), len(r.Text), r.CacheHit, r.CPUMS, r.PeakMemoryBytes, correct})
		if !correct {
			t.Fatalf("%s: incorrect evidence/cache contract: %v", phase, err)
		}
		if r.PeakMemoryBytes != nil && *r.PeakMemoryBytes > 512<<20 {
			t.Fatalf("%s: monitored RSS budget exceeded", phase)
		}
		return r
	}
	first := run("cold", query, "Shared", false)
	warm := make([]int64, 0, 10)
	for range 10 {
		run("warm", query, "Shared", true)
		warm = append(warm, rows[len(rows)-1].WallMS)
	}
	write("main.go", "package fixture\nfunc Shared(v int) int { return v*4 }\nfunc AddedSignature(s string) string { return s }\n")
	changed := run("incremental", query, "AddedSignature", false)
	if changed.SourceDigest == first.SourceDigest {
		t.Fatal("source change failed to invalidate receipt")
	}
	run("incremental-warm", query, "AddedSignature", true)
	if err := os.Rename(filepath.Join(p.Root, "main.go"), filepath.Join(p.Root, "renamed.go")); err != nil {
		t.Fatal(err)
	}
	query.File = "renamed.go"
	run("rename", query, "AddedSignature", false)
	if err := os.Remove(filepath.Join(p.Root, "module000.go")); err != nil {
		t.Fatal(err)
	}
	run("delete", query, "AddedSignature", false)
	slices.Sort(warm)
	report := struct {
		Samples                                                  []sample
		WarmP95MS, WarmTargetMS                                  int64
		WarmTargetMet                                            bool
		ColdTargetMS, ResultBudgetBytes, MonitoredRSSBudgetBytes int64
		ProviderTokens                                           any
	}{rows, warm[len(warm)-1], 100, warm[len(warm)-1] <= 100, 90000, 4096, 512 << 20, nil}
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("IVOAI_GRAFT_PERFORMANCE_OUTPUT"); path != "" {
		if err := os.WriteFile(path, append(body, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Log(string(body))
}
