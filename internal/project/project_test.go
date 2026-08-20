package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostIdentityDoesNotDependOnWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	one := filepath.Join(root, "etc")
	two := filepath.Join(root, "var", "lib")
	_ = os.MkdirAll(one, 0o700)
	_ = os.MkdirAll(two, 0o700)
	a, b := Identity(one), Identity(two)
	if a != b || !strings.HasPrefix(a, "host:") {
		t.Fatalf("identities %q %q", a, b)
	}
}

func TestProjectInitIsIdempotent(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("git", "init", root)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git unavailable: %v %s", err, output)
	}
	first, err := Init(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Init(root)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || Identity(root) != first.ID {
		t.Fatal("identity changed")
	}
	info, _ := os.Stat(filepath.Join(root, MarkerName))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
}
