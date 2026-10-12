package codeintel

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/core"
)

// TestReviewedGraftScenarios is deterministic tool-layer evidence, not model A/B
// or a token benchmark. All repositories, worktrees and compilation live in TempDir.
func TestReviewedGraftScenarios(t *testing.T) {
	node, dist := os.Getenv("IVOAI_GRAFT_TEST_NODE"), os.Getenv("IVOAI_GRAFT_TEST_DIST")
	if node == "" || dist == "" {
		t.Skip("reviewed Graft runtime required")
	}
	p, _ := testProvider(t)
	p.Node, p.Distribution, p.Executable, p.run = node, dist, os.Args[0], nil
	write := func(t *testing.T, root, name, body string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git := func(t *testing.T, root string, args ...string) {
		t.Helper()
		argv := append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "commit.gpgsign=false", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-C", root}, args...)
		cmd := exec.Command("/usr/bin/git", argv...)
		cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture git: %v: %s", err, out)
		}
	}
	write(t, p.Root, "go.mod", "module example.test/scenario\n\ngo 1.23\n\nrequire example.test/math v0.0.0\nreplace example.test/math => ./mathpkg\n")
	write(t, p.Root, "main.go", "package scenario\n// Scale is only mentioned here, not called.\nfunc Decoy() string { return \"Scale\" }\n")
	write(t, p.Root, "mathpkg/go.mod", "module example.test/math\n\ngo 1.23\n")
	original := "package mathpkg\nfunc Scale(v int) int { return v * 2 }\n"
	caller := "package app\nimport \"example.test/math\"\nfunc Entry() int { return mathpkg.Scale(3) }\n"
	write(t, p.Root, "mathpkg/scale.go", original)
	write(t, p.Root, "app/entry.go", caller)
	write(t, p.Root, "app/entry_test.go", "package app\nimport \"testing\"\nfunc TestEntry(t *testing.T) { if Entry() != 6 { t.Fatal(\"wrong result\") } }\n")
	git(t, p.Root, "add", ".")
	git(t, p.Root, "commit", "-qm", "deterministic fixture")

	type row struct {
		Scenario, Provider string
		Bytes              int
		Cache              bool
		DurationMS         int64
		CPU, RSS           *int64
		Tokens             any
	}
	var rows []row
	query := func(t *testing.T, name string, provider *Provider, q core.CodeQuery) core.CodeResult {
		t.Helper()
		q.BudgetBytes = 4096
		r, err := provider.Inspect(context.Background(), q)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !r.Fresh || !r.Untrusted || len(r.Text) > q.BudgetBytes {
			t.Fatalf("%s: result contract: %+v", name, r)
		}
		if r.PeakMemoryBytes != nil && *r.PeakMemoryBytes > maxProcessMemory {
			t.Fatalf("%s exceeded unchanged RSS budget", name)
		}
		rows = append(rows, row{name, r.ProviderVersion, len(r.Text), r.CacheHit, r.DurationMS, r.CPUMS, r.PeakMemoryBytes, nil})
		return r
	}
	q := core.CodeQuery{Operation: "callers", Symbol: "Scale"}
	assertGraph := func(t *testing.T, provider *Provider, symbol string) {
		t.Helper()
		key, err := Identity(provider.Root, provider.Scope, provider.Permissions)
		if err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(filepath.Join(provider.CacheRoot, key, "index", ".graph", "wiring.json"))
		if err != nil {
			t.Fatal(err)
		}
		var graph struct {
			Nodes []struct{ ID, Name, Path string }
			Edges []struct{ Source, Target, Relation string }
		}
		if err := json.Unmarshal(body, &graph); err != nil {
			t.Fatal(err)
		}
		names := map[string]string{}
		for _, n := range graph.Nodes {
			names[n.ID] = n.Path + ":" + n.Name
		}
		found := false
		for _, e := range graph.Edges {
			if e.Relation != "calls" || names[e.Target] != "mathpkg/scale.go:"+symbol {
				continue
			}
			if names[e.Source] == "main.go:Decoy" {
				t.Fatal("literal decoy became a call edge")
			}
			if names[e.Source] == "app/entry.go:Entry" {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing exact cross-package Entry -> %s call edge", symbol)
		}
	}

	hasSymbol := func(t *testing.T, provider *Provider, symbol string) bool {
		t.Helper()
		key, err := Identity(provider.Root, provider.Scope, provider.Permissions)
		if err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(filepath.Join(provider.CacheRoot, key, "index", ".graph", "wiring.json"))
		if err != nil {
			t.Fatal(err)
		}
		var graph struct{ Nodes []struct{ Name string } }
		if err := json.Unmarshal(body, &graph); err != nil {
			t.Fatal(err)
		}
		for _, n := range graph.Nodes {
			if n.Name == symbol {
				return true
			}
		}
		return false
	}

	t.Run("01_cross_package_relation", func(t *testing.T) {
		r := query(t, "cross_package_relation", p, q)
		if !strings.Contains(r.Text, "Entry") || strings.Contains(r.Text, "Decoy") {
			t.Fatalf("callers quality: %s", r.Text)
		}
		assertGraph(t, p, "Scale")
	})
	t.Run("02_literal_baseline", func(t *testing.T) {
		r := query(t, "literal_baseline", p, core.CodeQuery{Operation: "grep", Query: "Scale"})
		if r.ProviderVersion != "direct" || !strings.Contains(r.Text, "Decoy") || !strings.Contains(r.Text, "mathpkg.Scale") {
			t.Fatalf("literal baseline: %+v", r)
		}
	})
	t.Run("03_complete_small_file", func(t *testing.T) {
		r := query(t, "complete_small_file", p, core.CodeQuery{Operation: "skeleton", File: "mathpkg/scale.go"})
		if r.ProviderVersion != "direct" || !strings.Contains(r.Text, original) {
			t.Fatalf("small exact file: %+v", r)
		}
	})
	t.Run("04_cached_relations", func(t *testing.T) {
		r := query(t, "cached_relations", p, q)
		if !r.CacheHit || r.CPUMS != nil || r.PeakMemoryBytes != nil {
			t.Fatal("cache hit retained process measurements")
		}
	})
	var stagedDigest string
	t.Run("05_staged_change", func(t *testing.T) {
		write(t, p.Root, "mathpkg/scale.go", original+"func StagedOnly() int { return 7 }\n")
		git(t, p.Root, "add", "mathpkg/scale.go")
		r := query(t, "staged_change", p, core.CodeQuery{Operation: "callers", Symbol: "StagedOnly"})
		if r.CacheHit || !hasSymbol(t, p, "StagedOnly") {
			t.Fatal("staged content was not indexed")
		}
		stagedDigest = r.SourceDigest
	})
	t.Run("06_uncommitted_same_length", func(t *testing.T) {
		name := filepath.Join(p.Root, "mathpkg/scale.go")
		st, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		write(t, p.Root, "mathpkg/scale.go", original+"func EditedOnly() int { return 7 }\n")
		if err = os.Chtimes(name, st.ModTime(), st.ModTime()); err != nil {
			t.Fatal(err)
		}
		r := query(t, "uncommitted_same_length", p, core.CodeQuery{Operation: "callers", Symbol: "EditedOnly"})
		if r.CacheHit || r.SourceDigest == stagedDigest || !hasSymbol(t, p, "EditedOnly") {
			t.Fatal("same-size same-mtime content change was missed")
		}
	})
	t.Run("07_untracked_add_delete", func(t *testing.T) {
		write(t, p.Root, "mathpkg/transient.go", "package mathpkg\nfunc TransientOnly() int { return 9 }\n")
		r := query(t, "untracked_add", p, core.CodeQuery{Operation: "callers", Symbol: "TransientOnly"})
		if !hasSymbol(t, p, "TransientOnly") {
			t.Fatal("untracked addition absent")
		}
		if err := os.Remove(filepath.Join(p.Root, "mathpkg/transient.go")); err != nil {
			t.Fatal(err)
		}
		deleted := query(t, "untracked_delete", p, q)
		if deleted.CacheHit || deleted.SourceDigest == r.SourceDigest || hasSymbol(t, p, "TransientOnly") {
			t.Fatal("deleted source remained visible")
		}
	})
	t.Run("08_actual_git_worktree", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "worktree")
		git(t, p.Root, "worktree", "add", "--detach", root, "HEAD")
		other := *p
		other.Root = root
		r := query(t, "actual_git_worktree", &other, q)
		if r.CacheHit || hasSymbol(t, &other, "EditedOnly") {
			t.Fatalf("worktree leaked uncommitted symbol: %s", r.Text)
		}
		a, _ := Identity(p.Root, p.Scope, p.Permissions)
		b, _ := Identity(root, p.Scope, p.Permissions)
		if a == b {
			t.Fatal("real worktrees share identity")
		}
		assertGraph(t, &other, "Scale")
	})
	t.Run("09_organization_boundary", func(t *testing.T) {
		other := *p
		other.Scope = "organization-b"
		r := query(t, "organization_boundary", &other, q)
		if r.CacheHit {
			t.Fatal("organization inherited another cache")
		}
		assertGraph(t, &other, "Scale")
	})
	t.Run("10_permission_boundary", func(t *testing.T) {
		other := *p
		other.Permissions = "filesystem.read:restricted-fixture"
		r := query(t, "permission_boundary", &other, q)
		if r.CacheHit {
			t.Fatal("permission identity inherited another cache")
		}
		assertGraph(t, &other, "Scale")
	})
	t.Run("11_corrupt_graph_recovery", func(t *testing.T) {
		key, _ := Identity(p.Root, p.Scope, p.Permissions)
		write(t, p.CacheRoot, filepath.Join(key, "index", ".graph", "wiring.json"), "not valid graph JSON")
		r := query(t, "corrupt_graph_recovery", p, q)
		if r.CacheHit {
			t.Fatal("corrupt graph reused cached result")
		}
		assertGraph(t, p, "Scale")
	})
	t.Run("12_refactor_compile_and_impact", func(t *testing.T) {
		impact := query(t, "refactor_impact", p, core.CodeQuery{Operation: "blast", File: "mathpkg/scale.go", StartLine: 2, EndLine: 2})
		if !strings.Contains(impact.Text, "app/entry.go") {
			t.Fatalf("missing affected caller: %s", impact.Text)
		}
		home, cache := t.TempDir(), t.TempDir()
		compile := func() ([]byte, error) {
			cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "test", "-p=1", "./...")
			cmd.Dir = p.Root
			cmd.Env = []string{"HOME=" + home, "GOCACHE=" + cache, "GOPATH=" + home, "GOENV=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "GOWORK=off", "GOMAXPROCS=2", "CGO_ENABLED=0", "PATH=/usr/bin:/bin"}
			return cmd.CombinedOutput()
		}
		if out, err := compile(); err != nil {
			t.Fatalf("baseline fixture did not compile: %v %s", err, out)
		}
		write(t, p.Root, "mathpkg/scale.go", strings.ReplaceAll(original, "Scale", "Double"))
		if out, err := compile(); err == nil || !strings.Contains(string(out), "undefined: mathpkg.Scale") {
			t.Fatalf("incomplete refactor not detected: %v %s", err, out)
		}
		write(t, p.Root, "app/entry.go", strings.ReplaceAll(caller, "Scale", "Double"))
		if out, err := compile(); err != nil {
			t.Fatalf("completed refactor failed: %v %s", err, out)
		}
		r := query(t, "refactor_verified", p, core.CodeQuery{Operation: "callers", Symbol: "Double"})
		if r.CacheHit || !strings.Contains(r.Text, "Entry") {
			t.Fatal("refactored relation not refreshed")
		}
		assertGraph(t, p, "Double")
	})
	body, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("SCENARIO_EVIDENCE=%s", body)
}
