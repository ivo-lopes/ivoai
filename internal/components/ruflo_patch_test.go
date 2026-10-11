package components

import (
	"context"
	"errors"
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
func newRufloRuntimeFixture(t *testing.T) managedInstallerFixture {
	t.Helper()
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
	return fixture
}

func TestRufloInstallUsesStagedWorkingDirectory(t *testing.T) {
	fixture := newRufloRuntimeFixture(t)
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
	t                  *testing.T
	prefix             string
	missingNodeLibrary bool
}

func (r rufloWorkingDirectoryRunner) LookPath(string) (string, error) { return "", os.ErrNotExist }
func (r rufloWorkingDirectoryRunner) Run(_ context.Context, _ string, args []string, options platform.RunOptions) (platform.Result, error) {
	if r.missingNodeLibrary {
		return platform.Result{ExitCode: 127, Stderr: "node: error while loading shared libraries: libatomic.so.1: cannot open shared object file"}, errors.New("node exited with status 127")
	}
	if len(args) == 1 && args[0] == "--version" {
		return platform.Result{Stdout: "v" + managedNodeVersion + "\n"}, nil
	}
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

func TestRufloMissingNodeLibraryHasActionableDiagnostic(t *testing.T) {
	fixture := newRufloRuntimeFixture(t)
	fixture.installer.Runner = rufloWorkingDirectoryRunner{t: t, missingNodeLibrary: true}
	for _, spec := range DefaultCatalog() {
		if spec.Name == "ruflo" {
			_, err := fixture.installer.installNPMTool(context.Background(), spec)
			if err == nil || !strings.Contains(err.Error(), "libatomic1") || !strings.Contains(err.Error(), "apt-get install") {
				t.Fatalf("missing runtime library diagnostic: %v", err)
			}
			return
		}
	}
	t.Fatal("missing Ruflo spec")
}
