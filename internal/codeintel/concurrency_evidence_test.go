package codeintel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
)

// Real local artifacts, synthetic repositories; no model or external targets.
func TestStructuralConcurrencyEvidence(t *testing.T) {
	if os.Getenv("IVOAI_GRAFT_PERFORMANCE") != "1" {
		t.Skip("reviewed artifact evidence opt-in")
	}
	cache := t.TempDir()
	var calls, active, maximum atomic.Int32
	providers := make([]*Provider, 2)
	for i := range providers {
		p, _ := testProvider(t)
		p.Node = os.Getenv("IVOAI_GRAFT_TEST_NODE")
		p.Distribution = os.Getenv("IVOAI_GRAFT_TEST_DIST")
		if p.Node == "" || p.Distribution == "" {
			t.Fatal("reviewed runtime required")
		}
		p.CacheRoot = cache
		p.Executable = os.Args[0]
		p.Scope = []string{"organization_a", "organization_b"}[i]
		if err := os.WriteFile(filepath.Join(p.Root, "main.go"), []byte("package fixture\nfunc Shared() int { return "+[]string{"17", "29"}[i]+" }\n"), 0600); err != nil {
			t.Fatal(err)
		}
		p.run = func(ctx context.Context, r adapterRequest) (adapterResponse, error) {
			calls.Add(1)
			n := active.Add(1)
			defer active.Add(-1)
			for old := maximum.Load(); n > old; old = maximum.Load() {
				if maximum.CompareAndSwap(old, n) {
					break
				}
			}
			return p.runProcess(ctx, r)
		}
		providers[i] = p
	}
	type row struct {
		Project    int
		WallMS     int64
		CacheHit   bool
		CPUMS, RSS *int64
		Correct    bool
	}
	done := make(chan row, 8)
	start := make(chan struct{})
	for i, p := range providers {
		for range 4 {
			go func() {
				<-start
				begin := time.Now()
				r, err := p.Query(context.Background(), core.CodeQuery{Operation: "ask", Query: "Shared", BudgetBytes: 4096})
				want := []string{"17", "29"}[i]
				other := []string{"29", "17"}[i]
				done <- row{i, time.Since(begin).Milliseconds(), r.CacheHit, r.CPUMS, r.PeakMemoryBytes, err == nil && strings.Contains(r.Text, "return "+want) && !strings.Contains(r.Text, "return "+other)}
			}()
		}
	}
	close(start)
	rows := make([]row, 0, 8)
	hits := 0
	for range 8 {
		r := <-done
		rows = append(rows, r)
		if !r.Correct {
			t.Error("concurrent source isolation/operation failed")
		}
		if r.CacheHit {
			hits++
		}
	}
	if calls.Load() != 2 || maximum.Load() != 1 || hits != 6 {
		t.Errorf("build/process dedup: calls=%d max=%d hits=%d", calls.Load(), maximum.Load(), hits)
	}
	report := struct {
		Rows                                         []row
		StructuralProcesses, PeakConcurrentProcesses int32
		CacheHits                                    int
	}{rows, calls.Load(), maximum.Load(), hits}
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if output := os.Getenv("IVOAI_GRAFT_CONCURRENCY_OUTPUT"); output != "" {
		if err := os.WriteFile(output, append(body, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Log(string(body))
}
