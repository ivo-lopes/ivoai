package components

import (
	"context"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRufloLocalPatchRequiresExactIntegrity(t *testing.T) {
	var spec Spec
	for _, item := range DefaultCatalog() {
		if item.Name == "ruflo" {
			spec = item
		}
	}
	if err := validateRufloLock(spec); err != nil {
		t.Fatal(err)
	}
	original := rufloPackageLock
	t.Cleanup(func() { rufloPackageLock = original })
	rufloPackageLock = []byte(strings.ReplaceAll(string(original), "file:braces-3.0.4-ivoai.1.tgz", "file:unreviewed.tgz"))
	if err := validateRufloLock(spec); err == nil {
		t.Fatal("unreviewed local package accepted")
	}
	rufloPackageLock = original
	patch := rufloBracesPatch
	t.Cleanup(func() { rufloBracesPatch = patch })
	rufloBracesPatch = []byte("tampered")
	if err := validateRufloLock(spec); err == nil {
		t.Fatal("modified embedded patch accepted")
	}
}

// npm resolves local file dependencies against its working directory even when
// --prefix is provided. The installer must be independent of the caller's cwd.
func TestRufloInstallUsesStagedWorkingDirectory(t *testing.T) {
	fixture := newManagedInstallerFixture(t)
	root := filepath.Join(fixture.store.Paths.DataDir, "components", "node-"+managedNodeVersion)
	for _, rel := range []string{"bin/node", "lib/node_modules/npm/bin/npm-cli.js"} {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	prefix := filepath.Join(fixture.store.Paths.DataDir, "components", "ruflo")
	fixture.installer.Runner = rufloWorkingDirectoryRunner{t: t, prefix: prefix}
	for _, spec := range DefaultCatalog() {
		if spec.Name == "ruflo" {
			if _, err := fixture.installer.installNPMTool(context.Background(), spec); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("missing Ruflo spec")
}

type rufloWorkingDirectoryRunner struct {
	t      *testing.T
	prefix string
}

func (r rufloWorkingDirectoryRunner) LookPath(string) (string, error) { return "", os.ErrNotExist }
func (r rufloWorkingDirectoryRunner) Run(_ context.Context, _ string, _ []string, options platform.RunOptions) (platform.Result, error) {
	if options.Dir != r.prefix {
		r.t.Fatalf("npm cwd=%q, want staged prefix %q", options.Dir, r.prefix)
	}
	if _, err := os.Stat(filepath.Join(options.Dir, "braces-3.0.4-ivoai.1.tgz")); err != nil {
		r.t.Fatal(err)
	}
	launcher := filepath.Join(r.prefix, "node_modules", ".bin", "ruflo")
	if err := os.MkdirAll(filepath.Dir(launcher), 0700); err != nil {
		return platform.Result{}, err
	}
	return platform.Result{}, os.WriteFile(launcher, []byte("fixture"), 0700)
}
