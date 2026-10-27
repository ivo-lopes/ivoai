package components

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

// This opt-in test installs the real locked graph with lifecycle scripts disabled
// in a temporary managed root. It never invokes inference or personal runtimes.
func TestLiveRufloProfileInstallation(t *testing.T) {
	archive := os.Getenv("IVOAI_TEST_RUFLO_NODE_ARCHIVE")
	if archive == "" {
		t.Skip("real Ruflo installation requires a reviewed Node archive; not provider evidence")
	}
	t.Setenv("IVOAI_TEST_MODE", "")
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		t.Fatal("live managed runtime supports Linux amd64/arm64")
	}
	body, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	want := "15fc043a51a5783f2f4b634561091658f5a7f0e3db03f997dca3a8033a33e45b"
	if runtime.GOARCH == "arm64" {
		want = "170454fb022f5e9b5f6cc76c25fa32ef5100895be44eb04e92f9780591491bbf"
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != want {
		t.Fatal("Node archive differs from the reviewed architecture pin")
	}
	root := t.TempDir()
	paths := config.Paths{
		ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"),
		StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"),
		BinDir: filepath.Join(root, "bin"),
	}
	node := ManagedNodePath(paths.DataDir)
	nodeRoot := filepath.Dir(filepath.Dir(node))
	if err := os.MkdirAll(nodeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := extractNodeArchive(archive, nodeRoot); err != nil {
		t.Fatal(err)
	}
	var spec Spec
	for _, candidate := range DefaultCatalog() {
		if candidate.Name == "ruflo" {
			spec = candidate
		}
	}
	if spec.Name == "" {
		t.Fatal("missing reviewed Ruflo spec")
	}
	// Suppress discovery/adoption of personal executables; Run remains the real
	// process runner. The managed Node and graph execute in this temporary root.
	installer := Installer{Runner: absentRunner{}, Store: config.NewStore(paths), Out: io.Discard}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	installed, err := installer.ensure(ctx, spec, config.ComponentState{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := installer.Runner.Run(ctx, installed.Path, []string{"--version"}, platform.RunOptions{
		Dir: root, CleanEnv: true, Timeout: 15 * time.Second,
		Env: []string{"HOME=" + filepath.Join(paths.DataDir, "installer-home"), "PATH=/usr/bin:/bin", "RUFLO_PROVIDER=ivoai-disabled", "CLAUDE_FLOW_MEMORY_BACKEND=memory"},
	})
	if err != nil || !sameVersion(strings.TrimSpace(result.Stdout), spec.Version) {
		t.Fatal("promoted real Ruflo launcher did not report the reviewed version")
	}
	before, err := os.ReadFile(installed.Path)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := installer.ensure(ctx, spec, installed)
	if err != nil || repeated != installed {
		t.Fatal("real profile failed idempotent admission")
	}
	after, err := os.ReadFile(installed.Path)
	if err != nil || string(before) != string(after) {
		t.Fatal("idempotent admission changed the real launcher")
	}
	t.Logf("real locked Ruflo %s installed, promoted and admitted idempotently; no inference", spec.Version)
}
