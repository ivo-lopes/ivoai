package codeintel

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/toolrouting"
)

func directCacheFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			out[rel] = digest(body)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDirectOnlyCannotReuseStructuralCacheOrExecute(t *testing.T) {
	p, calls := testProvider(t)
	p.Node, p.Distribution, p.Executable = "/fixture/node", "/fixture/runtime", "/fixture/helper"
	if err := os.WriteFile(filepath.Join(p.Root, "main.go"), []byte("package fixture\nfunc Run() int { return 42 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	q := core.CodeQuery{Operation: "ask", Query: "Run"}
	if _, err := p.Query(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	limited := p.WithoutStructuralRuntime()
	if p.ExecutionMode() != "managed" || limited.ExecutionMode() != "direct_only" || p.Node == "" || p.run == nil || limited.Node != "" || limited.Distribution != "" || limited.Executable != "" || limited.run != nil {
		t.Fatal("restriction mutated managed provider or retained an executable")
	}
	// Equal (even forged) runtime paths cannot collapse the explicit mode identity.
	limited.Node, limited.Distribution, limited.Executable = p.Node, p.Distribution, p.Executable
	limited.run = p.run // even a forged in-process runner cannot undo the restriction
	if limited.runtimeIdentity() == p.runtimeIdentity() {
		t.Fatal("execution modes share a cache identity")
	}
	before := directCacheFiles(t, p.CacheRoot)
	for _, operation := range []string{"ask", "grep", "skeleton", "callers", "map", "blast", "check", "build", "viz"} {
		_, err := limited.Query(context.Background(), core.CodeQuery{Operation: operation, Query: "Run", Symbol: "Run", File: "main.go"})
		if !errors.Is(err, ErrStructuralUnavailable) {
			t.Fatalf("%s: structural query was not refused", operation)
		}
	}
	if calls.Load() != 1 || !reflect.DeepEqual(before, directCacheFiles(t, p.CacheRoot)) {
		t.Fatal("unavailable runtime reused or changed structural cache")
	}
	for _, query := range []core.CodeQuery{q, {Operation: "grep", Query: "Run"}, {Operation: "skeleton", File: "main.go"}} {
		r, err := limited.Inspect(context.Background(), query)
		if err != nil || r.ProviderVersion != "direct" || r.CacheHit || !r.Fresh || !r.Untrusted || !strings.Contains(r.Text, "return 42") || calls.Load() != 1 {
			t.Fatal("Direct-only inspection preferred a managed result or lost exact source")
		}
	}
}

func TestDirectOnlyHistoryAndAmbiguityCannotEnableGraft(t *testing.T) {
	p, calls := testProvider(t)
	limited := p.WithoutStructuralRuntime()
	if err := os.WriteFile(filepath.Join(p.Root, "main.go"), []byte("package fixture\nfunc Run() int { return 42 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	h := toolrouting.History{}
	for range 6 {
		h.Observe("grep", "graft", toolrouting.Sample{LatencyMS: 0, ResultBytes: 1, Success: true})
		h.Observe("grep", "direct", toolrouting.Sample{LatencyMS: 90000, ResultBytes: 32768, Success: true})
	}
	body, _ := json.Marshal(h)
	if err := os.MkdirAll(filepath.Dir(limited.costPath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(limited.costPath(), body, 0600); err != nil {
		t.Fatal(err)
	}
	r, err := limited.Inspect(context.Background(), core.CodeQuery{Operation: "grep", Query: "Run"})
	if err != nil || r.ProviderVersion != "direct" || calls.Load() != 0 {
		t.Fatal("cost history enabled an unavailable structural provider")
	}
	if err := os.WriteFile(filepath.Join(p.Root, "other.go"), []byte("package other\nfunc Run() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, query := range []core.CodeQuery{{Operation: "ask", Query: "Run"}, {Operation: "callers", Symbol: "Run"}, {Operation: "skeleton", File: "main.go", BudgetBytes: 256}} {
		if query.Operation == "skeleton" {
			if err := os.WriteFile(filepath.Join(p.Root, "main.go"), []byte("package fixture\n"+strings.Repeat("// source bytes\n", 30)), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := limited.Inspect(context.Background(), query); !errors.Is(err, ErrStructuralUnavailable) || calls.Load() != 0 {
			t.Fatal("inexact or relational request silently degraded to guessed evidence")
		}
	}
	event := Observation(core.CodeResult{Operation: "callers"}, ErrStructuralUnavailable)
	if event.Provider != "" || event.State != observability.StateUnavailable || event.CacheHit != nil || event.IndexFresh != nil || event.CPUMilliseconds != nil {
		t.Fatal("unattempted structural operation fabricated provider availability")
	}
}
