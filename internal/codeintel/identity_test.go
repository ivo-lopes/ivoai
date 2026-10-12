package codeintel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityBindsWorktreeOrganizationAndPermissions(t *testing.T) {
	root := t.TempDir()
	a, err := Identity(root, "organization-a", "read-code")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][3]string{{t.TempDir(), "organization-a", "read-code"}, {root, "organization-b", "read-code"}, {root, "organization-a", "read-subtree"}} {
		b, err := Identity(args[0], args[1], args[2])
		if err != nil || a == b {
			t.Fatalf("identity collision %v", err)
		}
	}
	if _, err := Identity(root, "", "read-code"); err == nil {
		t.Fatal("missing organization scope accepted")
	}
}
func TestSourceSnapshotRejectsEscapesAndInvalidatesSameSizeChanges(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package a\n"), 0600)
	os.WriteFile(filepath.Join(outside, "secret.go"), []byte("private"), 0600)
	os.Symlink(filepath.Join(outside, "secret.go"), filepath.Join(root, "escape.go"))
	a, err := Snapshot(root, []string{"main.go"})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package b\n"), 0600)
	b, err := Snapshot(root, []string{"main.go"})
	if err != nil || a.Digest == b.Digest {
		t.Fatal("content change undetected")
	}
	for _, path := range []string{"../secret.go", "escape.go", filepath.Join(outside, "secret.go")} {
		if _, err := Snapshot(root, []string{path}); err == nil {
			t.Fatalf("accepted unsafe source %q", path)
		}
	}
}
func TestSourceSnapshotBoundedAndDeterministic(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.go"), []byte("a"), 0600)
	os.WriteFile(filepath.Join(root, "b.go"), []byte("b"), 0600)
	a, e := Snapshot(root, []string{"b.go", "a.go"})
	if e != nil {
		t.Fatal(e)
	}
	b, e := Snapshot(root, []string{"a.go", "b.go"})
	if e != nil || a.Digest != b.Digest {
		t.Fatal("unstable order")
	}
	if _, e := Snapshot(root, []string{"a.go", "a.go"}); e == nil {
		t.Fatal("duplicate accepted")
	}
	os.WriteFile(filepath.Join(root, "large.go"), make([]byte, MaxFileBytes+1), 0600)
	if _, e := Snapshot(root, []string{"large.go"}); e == nil {
		t.Fatal("oversized accepted")
	}
}
