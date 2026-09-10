package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureRepository(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	for _, args := range [][]string{{"init"}, {"config", "user.name", "Fixture"}, {"config", "user.email", "fixture@example.invalid"}} {
		if _, err := gitOutput(ctx, dir, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "protected.txt"), []byte("protected\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "--", "protected.txt"}, {"-c", "commit.gpgsign=false", "commit", "-m", "fixture baseline"}} {
		if _, err := gitOutput(ctx, dir, args...); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestWorktreesIsolatedWritersIntegrationAndCleanup(t *testing.T) {
	ctx := context.Background()
	repo := fixtureRepository(t)
	m, err := NewWorktrees(ctx, repo, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a, err := m.Create(ctx, "feature_a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Create(ctx, "feature_b")
	if err != nil {
		t.Fatal(err)
	}
	if a.Path == b.Path || a.Branch == b.Branch {
		t.Fatal("writers share a checkout")
	}
	errors := make(chan error, 2)
	for _, w := range []Worktree{a, b} {
		go func(w Worktree) {
			errors <- os.WriteFile(filepath.Join(w.Path, w.TaskID+".txt"), []byte("implemented\n"), 0600)
		}(w)
	}
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	for _, w := range []Worktree{a, b} {
		if _, err := os.Stat(filepath.Join(repo, w.TaskID+".txt")); !os.IsNotExist(err) {
			t.Fatal("worker modified primary checkout")
		}
		if _, err := m.Collect(ctx, w.TaskID, []string{w.TaskID + ".txt"}); err != nil {
			t.Fatal(err)
		}
	}
	sha, err := m.Integrate(ctx, []string{a.TaskID, b.TaskID})
	if err != nil {
		t.Fatal(err)
	}
	if sha == m.base {
		t.Fatal("no integration")
	}
	for _, w := range []Worktree{a, b} {
		if _, err := os.Stat(filepath.Join(repo, w.TaskID+".txt")); err != nil {
			t.Fatal(err)
		}
		if err := m.Cleanup(ctx, w.TaskID); err != nil {
			t.Fatal(err)
		}
	}
	protected, err := os.ReadFile(filepath.Join(repo, "protected.txt"))
	if err != nil || string(protected) != "protected\n" {
		t.Fatal("protected file changed")
	}
	if err := cleanTree(ctx, repo); err != nil {
		t.Fatal(err)
	}
}

func TestWorktreeConflictNeverMergesIntoPrimary(t *testing.T) {
	ctx := context.Background()
	repo := fixtureRepository(t)
	m, err := NewWorktrees(ctx, repo, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		w, err := m.Create(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(w.Path, "protected.txt"), []byte(id+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = m.Collect(ctx, id, []string{"protected.txt"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = m.Integrate(ctx, []string{"a", "b"}); err == nil || !strings.Contains(err.Error(), "INTEGRATION_CONFLICT") {
		t.Fatalf("expected conflict: %v", err)
	}
	head, err := gitOutput(ctx, repo, "rev-parse", "HEAD")
	if err != nil || head != m.base {
		t.Fatal("primary history changed on conflict")
	}
	if err = cleanTree(ctx, repo); err != nil {
		t.Fatal(err)
	}
}

func TestWorktreesRefuseDirtyUnknownAndOutOfScope(t *testing.T) {
	ctx := context.Background()
	repo := fixtureRepository(t)
	m, err := NewWorktrees(ctx, repo, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Create(ctx, "../escape"); err == nil {
		t.Fatal("accepted path traversal")
	}
	w, err := m.Create(ctx, "docs")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(w.Path, "unexpected.txt"), []byte("private fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Collect(ctx, "docs", []string{"docs/"}); err == nil {
		t.Fatal("scope widened")
	}
	if err = m.Cleanup(ctx, "docs"); err == nil {
		t.Fatal("uncollected work removed")
	}
	if err = m.Cleanup(ctx, "unknown"); err == nil {
		t.Fatal("unknown path cleanup")
	}
	if _, err = NewWorktrees(ctx, t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("non-Git silently initialized")
	}
}
