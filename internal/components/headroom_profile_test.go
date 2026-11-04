package components

import (
	"bytes"
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

func TestHeadroomProfilesPreservePreviousGraph(t *testing.T) {
	installer := Installer{Store: config.NewStore(config.Paths{DataDir: t.TempDir()})}
	spec := Spec{Version: "1", Package: "headroom-ai[proxy]", Assets: map[string]Asset{runtime.GOOS + "/" + runtime.GOARCH: {URL: "https://example.invalid/a.whl", SHA256: strings.Repeat("a", 64)}}}
	lock := []byte("reviewed original graph")
	previous := installer.headroomProfilePath(spec, lock)
	if err := os.MkdirAll(previous, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(previous, "rollback-marker")
	if err := os.WriteFile(marker, lock, 0o600); err != nil {
		t.Fatal(err)
	}
	if previous == installer.headroomProfilePath(spec, []byte("changed transitive graph")) {
		t.Fatal("changing locked dependencies would overwrite the prior profile")
	}
	spec.Version = "2"
	if previous == installer.headroomProfilePath(spec, lock) {
		t.Fatal("changing the reviewed component would overwrite the prior profile")
	}
	got, err := os.ReadFile(marker)
	if err != nil || !bytes.Equal(got, lock) {
		t.Fatal("previous optional environment was changed")
	}
}

// This opt-in test runs the production installer with the official wheel graph
// in a new private root. It performs no inference and touches no client config.
func TestLiveHeadroomProfileInstallation(t *testing.T) {
	archive := os.Getenv("IVOAI_TEST_HEADROOM_UV_ARCHIVE")
	if archive == "" {
		t.Skip("real Headroom installation requires the reviewed uv archive; not provider evidence")
	}
	t.Setenv("IVOAI_TEST_MODE", "")
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		t.Fatal("live managed runtime supports Linux amd64/arm64")
	}
	body, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	want := "c5221ff41b7892bfc223543883600125f6a47fe845093e0f5bdff4f359196d83"
	if runtime.GOARCH == "arm64" {
		want = "71279f8d3c67241e807b9d8db872d1bf541579fb9f08b7d1eb1682196d1e57dc"
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != want {
		t.Fatal("uv archive differs from the reviewed architecture pin")
	}
	root := t.TempDir()
	paths := config.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), BinDir: filepath.Join(root, "bin")}
	uvRoot := filepath.Join(paths.DataDir, "components", "uv-"+managedUVVersion)
	if err := os.MkdirAll(uvRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	extracted, err := extractSingleExecutable(archive, "uv")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(extracted)
	data, err := os.ReadFile(extracted)
	if err != nil {
		t.Fatal(err)
	}
	uv := filepath.Join(uvRoot, "uv")
	if err := os.WriteFile(uv, data, 0o700); err != nil {
		t.Fatal(err)
	}
	var spec Spec
	for _, candidate := range DefaultCatalog() {
		if candidate.Name == "headroom" {
			spec = candidate
		}
	}
	if spec.Name == "" {
		t.Fatal("reviewed Headroom spec is missing")
	}
	installer := Installer{Runner: absentRunner{}, Store: config.NewStore(paths), Out: io.Discard}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	installed, err := installer.ensure(ctx, spec, config.ComponentState{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := installer.Runner.Run(ctx, installed.Path, []string{"--version"}, platform.RunOptions{Dir: root, CleanEnv: true, Timeout: 20 * time.Second, Env: []string{"HOME=" + filepath.Join(paths.DataDir, "installer-home"), "PATH=/usr/bin:/bin"}})
	if err != nil || !sameVersion(strings.TrimSpace(result.Stdout), spec.Version) {
		t.Fatal("real launcher does not report the reviewed Headroom version")
	}
	before, err := os.ReadFile(installed.Path)
	if err != nil || !bytes.Contains(before, []byte("headroom-runtime-")) {
		t.Fatal("launcher does not select the isolated locked profile")
	}
	again, err := installer.ensure(ctx, spec, installed)
	if err != nil || again != installed {
		t.Fatal("real optional profile failed idempotent admission")
	}
	after, err := os.ReadFile(installed.Path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("idempotent admission changed the real launcher")
	}
	t.Logf("real locked Headroom%s installed and admitted idempotently on CPython%s; no inference or managed wrapping", spec.Version, managedPythonVersion)
}
