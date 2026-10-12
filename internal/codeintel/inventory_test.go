package codeintel

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestInventoryPreservesGitScopeAndExcludesSecrets(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("git", "init", "-q", root)
	if e := cmd.Run(); e != nil {
		t.Fatal(e)
	}
	for name, body := range map[string]string{"main.go": "package main", "go.mod": "module example", ".env": "SECRET=value", "password.key": "secret", "README.md": "docs", ".gitignore": "ignored.go\n", "ignored.go": "package ignored"} {
		os.WriteFile(filepath.Join(root, name), []byte(body), 0600)
	}
	files, e := Inventory(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	if !slices.Contains(files, "main.go") || !slices.Contains(files, "go.mod") {
		t.Fatal(files)
	}
	for _, name := range []string{".env", "password.key", "ignored.go", "README.md"} {
		if slices.Contains(files, name) {
			t.Fatalf("included %s", name)
		}
	}
}
