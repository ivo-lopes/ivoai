package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoveryWorktreesCollectReopenIntegrate(t *testing.T) {
	ctx := context.Background()
	repo, root := fixtureRepository(t), t.TempDir()
	manager, err := NewWorktrees(ctx, repo, root)
	if err != nil {
		t.Fatal(err)
	}
	w, err := manager.Create(ctx, "worker_fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Path, "feature.txt"), []byte("finished\n"), 0600); err != nil {
		t.Fatal(err)
	}
	collected, err := manager.Collect(ctx, w.TaskID, []string{"feature.txt"})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreWorktrees(ctx, repo, root, w.Base, []Worktree{collected})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Integrate(ctx, []string{w.TaskID}); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(filepath.Join(repo, "feature.txt")); string(body) != "finished\n" {
		t.Fatal("recovered integration lost work")
	}
	if _, err := RestoreWorktrees(ctx, repo, root, w.Base, []Worktree{collected}); err == nil {
		t.Fatal("changed repository accepted")
	}
}

func TestRecoveryWorktreeRejectsUncollectedChanges(t *testing.T) {
	ctx := context.Background()
	repo, root := fixtureRepository(t), t.TempDir()
	manager, err := NewWorktrees(ctx, repo, root)
	if err != nil {
		t.Fatal(err)
	}
	w, err := manager.Create(ctx, "worker_fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Path, "feature.txt"), []byte("uncertain"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreWorktrees(ctx, repo, root, w.Base, []Worktree{w}); err == nil {
		t.Fatal("uncollected writer recovered")
	}
}
