package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const newFilePatch = "diff --git a/feature.txt b/feature.txt\nnew file mode 100644\n--- /dev/null\n+++ b/feature.txt\n@@ -0,0 +1 @@\n+implemented\n"

func TestSequentialPatchNonGitScopeAndNoConcurrentWriter(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "protected.txt"), []byte("keep\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewSequentialPatch(root)
	release, err := s.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if second, err := s.Acquire(ctx); err == nil {
		second()
		t.Fatal("concurrent patch writer admitted")
	}
	if err := s.Apply(context.Background(), newFilePatch, []string{"other.txt"}); err == nil {
		t.Fatal("scope escaped")
	}
	if _, err := os.Stat(filepath.Join(root, "feature.txt")); !os.IsNotExist(err) {
		t.Fatal("denied patch mutated tree")
	}
	if err := s.Apply(context.Background(), newFilePatch, []string{"feature.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Apply(context.Background(), newFilePatch, []string{"feature.txt"}); err == nil {
		t.Fatal("conflicting patch reapplied")
	}
	release()
	release()
	if _, err := os.Stat(filepath.Join(root, ".git")); !os.IsNotExist(err) {
		t.Fatal("created Git repository")
	}
	if body, _ := os.ReadFile(filepath.Join(root, "protected.txt")); string(body) != "keep\n" {
		t.Fatal("protected file changed")
	}
	if body, _ := os.ReadFile(filepath.Join(root, "feature.txt")); string(body) != "implemented\n" {
		t.Fatal("patch not applied")
	}
}

func TestSequentialPatchRefusesSensitivePathsLinksAndModeChanges(t *testing.T) {
	for _, patch := range []string{
		strings.ReplaceAll(newFilePatch, "feature.txt", ".env"),
		strings.Replace(newFilePatch, "100644", "120000", 1),
		strings.ReplaceAll(newFilePatch, "feature.txt", "../escape.txt"),
		strings.ReplaceAll(newFilePatch, "feature.txt", "link/file.txt"),
	} {
		root := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
		if err := NewSequentialPatch(root).Apply(context.Background(), patch, []string{".env", "feature.txt", "../escape.txt", "link/"}); err == nil {
			t.Fatal("unsafe patch accepted")
		}
		entries, _ := os.ReadDir(outside)
		if len(entries) != 0 {
			t.Fatal("symlink escaped workspace")
		}
	}
}
