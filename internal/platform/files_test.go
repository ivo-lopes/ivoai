package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFilePreservesExistingParentPermissions(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "public-bin")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "ivoai")
	if err := AtomicWriteFile([]byte("candidate"), path, 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("managed write changed parent mode to %o", info.Mode().Perm())
	}
}

func TestAtomicWritePrivateSecuresPrivateParent(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "state")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "journal.json")
	if err := AtomicWritePrivate([]byte("{}"), path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("private write left parent mode %o", info.Mode().Perm())
	}
}
