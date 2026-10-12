package codeintel

import (
	"context"
	"github.com/ivo-lopes/ivoai/internal/core"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func testProvider(t *testing.T) (*Provider, *atomic.Int32) {
	t.Helper()
	root := t.TempDir()
	if e := exec.Command("git", "init", "-q", root).Run(); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package a"), 0600)
	calls := new(atomic.Int32)
	p := &Provider{Root: root, Scope: "org-a", Permissions: "filesystem.read", CacheRoot: t.TempDir(), Allowed: true}
	p.run = func(ctx context.Context, r adapterRequest) (adapterResponse, error) {
		calls.Add(1)
		if r.Build {
			os.MkdirAll(filepath.Join(r.Cache, ".graph"), 0700)
			os.WriteFile(filepath.Join(r.Cache, ".graph", "wiring.json"), []byte(`{"meta":{"version":1},"nodes":[]}`), 0600)
		}
		return adapterResponse{Text: "Run at main.go:1", Files: 1, Nodes: 1}, nil
	}
	return p, calls
}
func TestProviderDeduplicatesAndInvalidatesContent(t *testing.T) {
	p, calls := testProvider(t)
	q := core.CodeQuery{Operation: "ask", Query: "Run"}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if _, e := p.Query(context.Background(), q); e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("duplicate execution: %d", calls.Load())
	}
	r, e := p.Query(context.Background(), q)
	if e != nil || !r.CacheHit || !r.Untrusted || !r.Fresh {
		t.Fatalf("%+v %v", r, e)
	}
	os.WriteFile(filepath.Join(p.Root, "main.go"), []byte("package b"), 0600)
	r, e = p.Query(context.Background(), q)
	if e != nil || r.CacheHit || calls.Load() != 2 {
		t.Fatalf("stale cached response %+v %v", r, e)
	}
}
func TestProviderInvalidatesIndexAfterManagedObjectChange(t *testing.T) {
	p, calls := testProvider(t)
	p.Distribution = "/managed/objects/graft/revision-one"
	q := core.CodeQuery{Operation: "map"}
	if _, err := p.Query(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	run := p.run
	p.run = func(ctx context.Context, r adapterRequest) (adapterResponse, error) {
		if !r.Build {
			t.Error("new managed object reused the old structural index")
		}
		return run(ctx, r)
	}
	p.Distribution = "/managed/objects/graft/revision-two"
	r, err := p.Query(context.Background(), q)
	if err != nil || r.CacheHit || calls.Load() != 2 {
		t.Fatalf("managed revision failed to invalidate cache: %+v %v", r, err)
	}
}
func TestProviderRejectsPolicyAndCorruptCache(t *testing.T) {
	p, calls := testProvider(t)
	p.Allowed = false
	if _, e := p.Query(context.Background(), core.CodeQuery{Operation: "map"}); e == nil || calls.Load() != 0 {
		t.Fatal("policy bypass")
	}
	p.Allowed = true
	q := core.CodeQuery{Operation: "map"}
	if _, e := p.Query(context.Background(), q); e != nil {
		t.Fatal(e)
	}
	key, _ := Identity(p.Root, p.Scope, p.Permissions)
	os.WriteFile(filepath.Join(p.CacheRoot, key, "index", ".graph", "wiring.json"), []byte("corrupt"), 0600)
	if _, e := p.Query(context.Background(), q); e != nil || calls.Load() != 2 {
		t.Fatalf("did not recover corrupted index %v %d", e, calls.Load())
	}
}
func TestProviderOutputAndCancellationBounded(t *testing.T) {
	p, _ := testProvider(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := p.Query(ctx, core.CodeQuery{Operation: "map"}); e == nil {
		t.Fatal("ignored cancellation")
	}
	p.run = func(ctx context.Context, r adapterRequest) (adapterResponse, error) {
		return adapterResponse{Text: string(make([]byte, 10000))}, nil
	}
	if _, e := p.Query(context.Background(), core.CodeQuery{Operation: "map", BudgetBytes: 256}); e == nil {
		t.Fatal("oversized result accepted")
	}
}

func TestSidecarIntegrityAndCacheMeasurements(t *testing.T) {
	p, calls := testProvider(t)
	q := core.CodeQuery{Operation: "map"}
	if _, err := p.Query(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	key, _ := Identity(p.Root, p.Scope, p.Permissions)
	os.MkdirAll(filepath.Join(p.CacheRoot, key, "index", ".cache"), 0700)
	os.WriteFile(filepath.Join(p.CacheRoot, key, "index", ".cache", "extraction.json"), []byte(`{"untrusted":"injected"}`), 0600)
	r, err := p.Query(context.Background(), q)
	if err != nil || r.CacheHit || calls.Load() != 2 {
		t.Fatalf("sidecar integrity not checked: %+v %v", r, err)
	}
	r, err = p.Query(context.Background(), q)
	if err != nil || !r.CacheHit || r.PeakMemoryBytes != nil || r.CPUMS != nil {
		t.Fatalf("cache hit fabricated runtime measures: %+v %v", r, err)
	}
}

func TestLocalCostHistoryRetainsOnlyAggregateMeasurements(t *testing.T) {
	p, _ := testProvider(t)
	if _, e := p.Query(context.Background(), core.CodeQuery{Operation: "ask", Query: "private request text"}); e != nil {
		t.Fatal(e)
	}
	body, e := os.ReadFile(p.costPath())
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(body), "private") || strings.Contains(string(body), "main.go") {
		t.Fatal("source/query retained in cost history")
	}
	cost, ok := p.costHistory().Cost("ask", "graft")
	if !ok || !cost.LatencyMS.Known || cost.CPUMS.Known {
		t.Fatalf("invalid observed aggregate %+v", cost)
	}
}
