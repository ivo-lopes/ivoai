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

func TestInventorySkipsOversizedGeneratedSources(t *testing.T) {
	p, _ := testProvider(t)
	file, err := os.Create(filepath.Join(p.Root, "large.js"))
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(MaxFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	files, err := Inventory(context.Background(), p.Root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if name == "large.js" {
			t.Fatal("oversized source admitted")
		}
	}
	if _, err = Snapshot(p.Root, files); err != nil {
		t.Fatal(err)
	}
}

func TestInventoryExcludesGeneratedBundlesButKeepsSource(t *testing.T) {
	p, _ := testProvider(t)
	for _, name := range []string{"main.a3bcaeff.js", "1253.929985a4.js", "vendor.min.js", "source.js"} {
		os.WriteFile(filepath.Join(p.Root, name), []byte("export function validSource(){}"), 0600)
	}
	files, err := Inventory(context.Background(), p.Root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("generated bundles admitted: %v", files)
	}
}

func TestMonorepoModuleIdentityIsPreserved(t *testing.T) {
	p, _ := testProvider(t)
	os.Mkdir(filepath.Join(p.Root, "backend"), 0700)
	os.WriteFile(filepath.Join(p.Root, "backend", "go.mod"), []byte("module example.invalid/backend\ngo 1.27\n"), 0600)
	os.WriteFile(filepath.Join(p.Root, "backend", "main.go"), []byte("package backend\nfunc Run(){}"), 0600)
	files, e := Inventory(context.Background(), p.Root)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, f := range files {
		if f == "backend/go.mod" {
			found = true
		}
	}
	if !found {
		t.Fatal("monorepo module identity discarded")
	}
}
