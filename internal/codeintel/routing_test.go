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

func TestColdExactDeclarationAvoidsIndexConstruction(t *testing.T) {
	p, calls := testProvider(t)
	os.WriteFile(filepath.Join(p.Root, "main.go"), []byte("package fixture\nfunc SmallKnown() int { return 42 }\n"), 0600)
	q := core.CodeQuery{Operation: "ask", Query: "SmallKnown"}
	r, e := p.Inspect(context.Background(), q)
	if e != nil || calls.Load() != 0 || r.ProviderVersion != "direct" || !strings.Contains(r.Text, "return 42") {
		t.Fatalf("cold exact lookup built graph: %+v %v calls=%d", r, e, calls.Load())
	}
	if _, e = p.Query(context.Background(), q); e != nil {
		t.Fatal(e)
	}
	r, e = p.Inspect(context.Background(), q)
	if e != nil || r.ProviderVersion == "direct" {
		t.Fatalf("valid structural cache not reused: %+v %v", r, e)
	}
}

func TestDirectInspectionRequiresTrustedScope(t *testing.T) {
	for _, q := range []core.CodeQuery{{Operation: "grep", Query: "package"}, {Operation: "skeleton", File: "main.go"}, {Operation: "ask", Query: "SmallKnown"}} {
		p, _ := testProvider(t)
		p.Scope = ""
		os.WriteFile(filepath.Join(p.Root, "main.go"), []byte("package fixture\nfunc SmallKnown() int { return 42 }\n"), 0600)
		if _, err := p.Inspect(context.Background(), q); err == nil {
			t.Errorf("%s accepted absent organization scope", q.Operation)
		}
	}
}

func TestWarmIndexAloneDoesNotForceExactLookupProcess(t *testing.T) {
	p, calls := testProvider(t)
	os.WriteFile(filepath.Join(p.Root, "main.go"), []byte("package fixture\nfunc SmallKnown() int {return 42}\n"), 0600)
	if _, err := p.Query(context.Background(), core.CodeQuery{Operation: "map"}); err != nil {
		t.Fatal(err)
	}
	q := core.CodeQuery{Operation: "ask", Query: "SmallKnown"}
	r, err := p.Inspect(context.Background(), q)
	if err != nil || r.ProviderVersion != "direct" || calls.Load() != 1 {
		t.Fatalf("warm index unnecessarily launched query: %+v %v calls=%d", r, err, calls.Load())
	}
	if _, err := p.Query(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	r, err = p.Inspect(context.Background(), q)
	if err != nil || !r.CacheHit || r.ProviderVersion != Version || calls.Load() != 2 {
		t.Fatalf("exact query cache not reused: %+v %v calls=%d", r, err, calls.Load())
	}
}

func TestExactLookupCannotMistakeCommentsForDeclarations(t *testing.T) {
	p, calls := testProvider(t)
	os.WriteFile(filepath.Join(p.Root, "main.go"), []byte("package fixture\n/*\nfunc Pretend() int {return 42}\n*/\n"), 0600)
	r, err := p.Inspect(context.Background(), core.CodeQuery{Operation: "ask", Query: "Pretend"})
	if err != nil || r.ProviderVersion == "direct" || calls.Load() != 1 {
		t.Fatalf("comment falsely asserted as declaration: %+v %v", r, err)
	}
}

func TestExactLookupRejectsAmbiguityAcrossLanguagesAndMalformedGo(t *testing.T) {
	for _, other := range []struct{ file, body string }{{"other.py", "def SharedName():\n    return 7\n"}, {"other.go", "package fixture\nfunc SharedName( broken"}, {"other.go", "package fixture\nfunc SharedName() int {return 7}\n"}} {
		t.Run(other.file+other.body[:7], func(t *testing.T) {
			p, calls := testProvider(t)
			os.WriteFile(filepath.Join(p.Root, "main.go"), []byte("package fixture\nfunc SharedName() int {return 42}\n"), 0600)
			os.WriteFile(filepath.Join(p.Root, other.file), []byte(other.body), 0600)
			r, err := p.Inspect(context.Background(), core.CodeQuery{Operation: "ask", Query: "SharedName"})
			if err != nil || r.ProviderVersion == "direct" || calls.Load() != 1 {
				t.Fatalf("ambiguous declaration bypassed structure: %+v %v", r, err)
			}
		})
	}
}
