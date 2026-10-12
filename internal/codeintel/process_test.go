package codeintel

import (
	"context"
	"github.com/ivo-lopes/ivoai/internal/core"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxInvocationRejectsUnsafeLayout(t *testing.T) {
	for _, args := range [][]string{nil, {"relative", "/dist", "/source", "/cache", "/entry"}, {"/node", "/dist", "/same", "/same", "/entry"}} {
		if e := SandboxHelper(args); e == nil {
			t.Fatal("unsafe helper invocation accepted")
		}
	}
}
func TestProcessDoesNotAcceptMissingManagedRuntime(t *testing.T) {
	p := &Provider{Executable: filepath.Join(t.TempDir(), "absent"), Node: "/absent", Distribution: t.TempDir()}
	_, e := p.runProcess(context.Background(), adapterRequest{Root: t.TempDir(), Cache: t.TempDir()})
	if e == nil {
		t.Fatal("missing runtime accepted")
	}
	_ = os.ErrNotExist
}
func init() {
	if len(os.Args) > 1 && os.Args[1] == "_code-sandbox" {
		if e := SandboxHelper(os.Args[2:]); e != nil {
			os.Stderr.WriteString(e.Error())
			os.Exit(1)
		}
		os.Exit(0)
	}
}
func TestReviewedGraftSandbox(t *testing.T) {
	node, dist := os.Getenv("IVOAI_GRAFT_TEST_NODE"), os.Getenv("IVOAI_GRAFT_TEST_DIST")
	if node == "" || dist == "" {
		t.Skip("reviewed Graft artifact required")
	}
	p, _ := testProvider(t)
	p.Node = node
	p.Distribution = dist
	p.Executable = os.Args[0]
	p.run = p.runProcess
	os.WriteFile(filepath.Join(p.Root, "main.go"), []byte("package main\nfunc Multiply(a, b int) int { return a*b }\nfunc Caller() int { return Multiply(2,3) }\n"), 0600)
	r, e := p.Query(context.Background(), core.CodeQuery{Operation: "ask", Query: "Multiply"})
	if e != nil || !strings.Contains(r.Text, "Multiply") {
		t.Fatalf("real Graft: %+v %v", r, e)
	}
	if r.PeakMemoryBytes == nil || *r.PeakMemoryBytes <= 0 {
		t.Fatal("missing real resource observation")
	}
}
func TestReviewedGraftOperationsAndIncremental(t *testing.T) {
	node, dist := os.Getenv("IVOAI_GRAFT_TEST_NODE"), os.Getenv("IVOAI_GRAFT_TEST_DIST")
	if node == "" || dist == "" {
		t.Skip("reviewed artifact required")
	}
	p, _ := testProvider(t)
	p.Node = node
	p.Distribution = dist
	p.Executable = os.Args[0]
	p.run = p.runProcess
	os.WriteFile(filepath.Join(p.Root, "main.go"), []byte("package main\nfunc Multiply(a, b int) int { return a*b }\nfunc Caller() int { return Multiply(2,3) }\n"), 0600)
	for _, q := range []core.CodeQuery{{Operation: "build"}, {Operation: "skeleton", File: "main.go"}, {Operation: "callers", Symbol: "Multiply"}, {Operation: "grep", Query: "Multiply"}, {Operation: "map"}, {Operation: "blast", Symbol: "Multiply"}, {Operation: "blast", File: "main.go", StartLine: 2, EndLine: 2}, {Operation: "check"}} {
		r, e := p.Query(context.Background(), q)
		if e != nil || r.Text == "" {
			t.Fatalf("%s: %+v %v", q.Operation, r, e)
		}
	}
	os.WriteFile(filepath.Join(p.Root, "new.py"), []byte("def new_symbol():\n    return 42\n"), 0600)
	r, e := p.Query(context.Background(), core.CodeQuery{Operation: "ask", Query: "new_symbol"})
	if e != nil || !strings.Contains(r.Text, "new_symbol") {
		t.Fatalf("incremental addition %+v %v", r, e)
	}
	os.Remove(filepath.Join(p.Root, "new.py"))
	r, e = p.Query(context.Background(), core.CodeQuery{Operation: "ask", Query: "new_symbol"})
	if e != nil || strings.Contains(r.Text, "new.py:") {
		t.Fatalf("incremental deletion %+v %v", r, e)
	}
}
