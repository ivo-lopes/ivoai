package codeintel

import (
	"context"
	"github.com/ivo-lopes/ivoai/internal/core"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEconomicRoutingUsesDirectLiteralAndSmallKnownFile(t *testing.T) {
	p, calls := testProvider(t)
	os.WriteFile(filepath.Join(p.Root, "main.go"), []byte("package a\nfunc ExactNeedle() {}\n"), 0600)
	for _, q := range []core.CodeQuery{{Operation: "grep", Query: "ExactNeedle"}, {Operation: "skeleton", File: "main.go"}} {
		r, e := p.Inspect(context.Background(), q)
		if e != nil || !strings.Contains(r.Text, "ExactNeedle") || r.ProviderVersion != "direct" {
			t.Fatalf("%+v %v", r, e)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("unnecessary structural process")
	}
}
func TestEconomicRoutingPreservesStructuralRelations(t *testing.T) {
	p, calls := testProvider(t)
	r, e := p.Inspect(context.Background(), core.CodeQuery{Operation: "callers", Symbol: "Run"})
	if e != nil || calls.Load() != 1 || r.ProviderVersion != Version {
		t.Fatalf("%+v %v calls %d", r, e, calls.Load())
	}
}
func TestTaskRoutingKeepsKnowledgeAndCompilationSeparate(t *testing.T) {
	for _, text := range []string{"Read institutional documentation for deployment", "Consultar decisões históricas no ai-memory", "Run tests to verify compilation", "Leia documentação no Context"} {
		if _, ok := ForTask(text); ok {
			t.Fatalf("unnecessary structural retrieval for %q", text)
		}
	}
	for _, text := range []string{"Locate implementation of function Multiply", "Find callers of Multiply before refactoring", "Inspect API in main.go"} {
		if _, ok := ForTask(text); !ok {
			t.Fatalf("missing structural retrieval for %q", text)
		}
	}
}
