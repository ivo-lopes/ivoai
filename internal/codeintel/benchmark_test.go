package codeintel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
)

// TestEfficiencyEvidence is opt-in, deterministic tool-layer evidence. It does
// not manufacture provider token counts or claim equivalence to a model A/B run.
func TestEfficiencyEvidence(t *testing.T) {
	if os.Getenv("IVOAI_GRAFT_BENCHMARK") == "" {
		t.Skip("opt-in reviewed artifact benchmark")
	}
	node, dist := os.Getenv("IVOAI_GRAFT_TEST_NODE"), os.Getenv("IVOAI_GRAFT_TEST_DIST")
	if node == "" || dist == "" {
		t.Fatal("reviewed runtime required")
	}
	p, _ := testProvider(t)
	p.Node = node
	p.Distribution = dist
	p.Executable = os.Args[0]
	p.run = p.runProcess
	for i := range 120 {
		body := fmt.Sprintf("package fixture\n// Module %d.\nfunc Module%d() int { return Shared(3) }\n", i, i)
		if err := os.WriteFile(filepath.Join(p.Root, fmt.Sprintf("module%03d.go", i)), []byte(body+strings.Repeat("// deterministic filler without private content\n", 50)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(p.Root, "main.go"), []byte("package fixture\nfunc Shared(v int) int { return v*2 }\nfunc Entry() int { return Shared(4) }\n"), 0600)
	os.WriteFile(filepath.Join(p.Root, "helper.py"), []byte("def helper_value():\n    return 42\n"), 0600)
	type evidence struct {
		Scenario, Arm, Tool string
		WallMS              int64
		Bytes               int
		Useful              bool
		Cache               bool
		CPU                 *int64
		RSS                 *int64
		Tokens              any
	}
	rows := []evidence{}
	cases := []struct {
		name   string
		q      core.CodeQuery
		needle string
	}{
		{"implementation", core.CodeQuery{Operation: "ask", Query: "Shared", Limit: 2}, "Shared"},
		{"dependencies", core.CodeQuery{Operation: "callers", Symbol: "Shared"}, "Module"},
		{"cross_module_change", core.CodeQuery{Operation: "blast", Symbol: "Shared"}, "Module"},
		{"cross_file_refactor", core.CodeQuery{Operation: "blast", File: "main.go", StartLine: 2, EndLine: 2}, "Module"},
		{"locate_error", core.CodeQuery{Operation: "ask", Query: "helper_value"}, "helper_value"},
		{"api", core.CodeQuery{Operation: "skeleton", File: "module000.go"}, "Module0"},
		{"orientation", core.CodeQuery{Operation: "map"}, "module"},
		{"exact_string", core.CodeQuery{Operation: "grep", Query: "return 42"}, "42"},
		{"small_known_file", core.CodeQuery{Operation: "skeleton", File: "main.go"}, "Shared"},
		{"multiple_languages", core.CodeQuery{Operation: "ask", Query: "helper_value"}, "helper_value"},
	}
	for _, c := range cases {
		q := c.q
		q.BudgetBytes = 4096
		// Baseline is bounded text discovery over the same authorized inventory. It
		// cannot prove call relationships; record that quality limitation explicitly.
		start := time.Now()
		files, e := Inventory(context.Background(), p.Root)
		if e != nil {
			t.Fatal(e)
		}
		snap, e := Snapshot(p.Root, files)
		if e != nil {
			t.Fatal(e)
		}
		var out strings.Builder
		needle := q.Query
		if needle == "" {
			needle = q.Symbol
		}
		if needle == "" {
			needle = c.needle
		}
		for _, f := range snap.Files {
			for i, line := range strings.Split(string(f.Body), "\n") {
				if strings.Contains(line, needle) && out.Len()+len(line)+len(f.Path)+20 <= 4096 {
					fmt.Fprintf(&out, "%s:%d:%s\n", f.Path, i+1, line)
				}
			}
		}
		exact := q.Operation != "callers" && q.Operation != "blast" && q.Operation != "map"
		rows = append(rows, evidence{Scenario: c.name, Arm: "A", Tool: "bounded_text", WallMS: time.Since(start).Milliseconds(), Bytes: out.Len(), Useful: exact && strings.Contains(out.String(), c.needle)})
		for _, arm := range []string{"B-cold-query", "B-repeat"} {
			start = time.Now()
			r, e := p.Inspect(context.Background(), q)
			if e != nil {
				t.Fatalf("%s: %v", c.name, e)
			}
			useful := strings.Contains(r.Text, c.needle)
			rows = append(rows, evidence{Scenario: c.name, Arm: arm, Tool: r.ProviderVersion, WallMS: time.Since(start).Milliseconds(), Bytes: len(r.Text), Useful: useful, Cache: r.CacheHit, CPU: r.CPUMS, RSS: r.PeakMemoryBytes})
			if !useful {
				t.Errorf("%s missing expected evidence: %s", c.name, r.Text)
			}
		}
	}
	for _, text := range []string{"Consult institutional documentation in Context", "Read historical ai-memory decisions"} {
		if _, ok := ForTask(text); ok {
			t.Fatal("structural tool selected for institutional source")
		}
		rows = append(rows, evidence{Scenario: "institutional_documentation", Arm: "B", Tool: "institutional_route", Useful: true})
	}
	if root := os.Getenv("IVOAI_GRAFT_BENCHMARK_REPO"); root != "" {
		p.Root = root
		start := time.Now()
		r, e := p.Inspect(context.Background(), core.CodeQuery{Operation: "ask", Query: "prepareWorkerAccess", Limit: 2, BudgetBytes: 4096})
		row := evidence{Scenario: "ivoai_repository", Arm: "B", Tool: r.ProviderVersion, WallMS: time.Since(start).Milliseconds(), Bytes: len(r.Text), Useful: e == nil && strings.Contains(r.Text, "prepareWorkerAccess"), Cache: r.CacheHit, CPU: r.CPUMS, RSS: r.PeakMemoryBytes}
		rows = append(rows, row)
		if e != nil {
			t.Errorf("real IVOAI: %v", e)
		}
	}
	body, _ := json.MarshalIndent(rows, "", "  ")
	if path := os.Getenv("IVOAI_GRAFT_BENCHMARK_OUTPUT"); path != "" {
		if e := os.WriteFile(path, body, 0600); e != nil {
			t.Fatal(e)
		}
	}
	t.Log(string(body))
}
