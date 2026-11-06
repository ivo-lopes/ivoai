package components

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

type headroomEnvironmentRunner struct {
	options platform.RunOptions
	args    []string
}

var errHeadroomEnvironmentStop = errors.New("stop before installation")

func (*headroomEnvironmentRunner) LookPath(string) (string, error) { return "", os.ErrNotExist }
func (r *headroomEnvironmentRunner) Run(_ context.Context, _ string, args []string, options platform.RunOptions) (platform.Result, error) {
	r.options, r.args = options, append([]string(nil), args...)
	return platform.Result{}, errHeadroomEnvironmentStop
}

func TestHeadroomOfflineOptInDoesNotInheritInstallerConfiguration(t *testing.T) {
	for _, value := range []string{"", "0", "true", "1", "1\n"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("UV_OFFLINE", value)
			for _, variable := range []string{"UV_INDEX_URL", "UV_EXTRA_INDEX_URL", "UV_CONFIG_FILE", "UV_PYTHON", "PYTHONPATH", "OPENAI_API_KEY", "HTTPS_PROXY"} {
				t.Setenv(variable, "must-not-reach-installer")
			}
			root := t.TempDir()
			paths := config.Paths{DataDir: filepath.Join(root, "data"), BinDir: filepath.Join(root, "bin")}
			uv := filepath.Join(paths.DataDir, "components", "uv-"+managedUVVersion, "uv")
			if err := os.MkdirAll(filepath.Dir(uv), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(uv, []byte("fixture"), 0o700); err != nil {
				t.Fatal(err)
			}
			runner := &headroomEnvironmentRunner{}
			installer := Installer{Runner: runner, Store: config.NewStore(paths), Out: io.Discard}
			var spec Spec
			for _, candidate := range DefaultCatalog() {
				if candidate.Name == "headroom" {
					spec = candidate
				}
			}
			if _, err := installer.installUVTool(context.Background(), spec); !errors.Is(err, errHeadroomEnvironmentStop) {
				t.Fatalf("installer failure was not preserved: %v", err)
			}
			if !runner.options.CleanEnv {
				t.Fatal("installer inherited the parent environment")
			}
			offline := false
			for _, field := range runner.options.Env {
				if field == "UV_OFFLINE=1" {
					offline = true
					continue
				}
				key, _, _ := strings.Cut(field, "=")
				switch key {
				case "HOME", "PATH", "UV_TOOL_DIR", "UV_TOOL_BIN_DIR", "UV_PYTHON_INSTALL_DIR", "UV_PYTHON_PREFERENCE", "UV_NO_PROGRESS":
				default:
					t.Fatalf("unexpected inherited setting: %s", key)
				}
			}
			if offline != (value == "1") {
				t.Fatal("offline mode did not require the literal opt-in")
			}
			args := runner.args
			if len(args) != 11 || args[0] != "tool" || args[1] != "install" || args[2] != "--force" || args[3] != "--python" || args[4] != managedPythonVersion || args[5] != "--constraints" || args[7] != "--no-build" || args[8] != "--exclude-newer" || args[9] != dependencyCutoff {
				t.Fatalf("reviewed install contract changed: %v", args)
			}
			asset := spec.Assets[runtime.GOOS+"/"+runtime.GOARCH]
			if args[len(args)-1] != spec.Package+" @ "+asset.URL+"#sha256="+asset.SHA256 {
				t.Fatal("reviewed wheel hash was not preserved")
			}
			if _, err := os.Lstat(filepath.Join(paths.BinDir, spec.Executable)); !os.IsNotExist(err) {
				t.Fatal("failed installation published a launcher")
			}
		})
	}
}

// This separate negative executes the production installer with the official
// reviewed UV binary and an empty private cache. Offline is never a bypass.
func TestLiveHeadroomOfflineCacheMiss(t *testing.T) {
	archive := os.Getenv("IVOAI_TEST_HEADROOM_UV_ARCHIVE")
	if archive == "" {
		t.Skip("requires the reviewed UV archive; no provider inference")
	}
	t.Setenv("UV_OFFLINE", "1")
	body, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != managedUVAsset().SHA256 {
		t.Fatal("UV archive does not match its reviewed pin")
	}
	extracted, err := extractSingleExecutable(archive, "uv")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(extracted)
	body, err = os.ReadFile(extracted)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	paths := config.Paths{DataDir: filepath.Join(root, "data"), BinDir: filepath.Join(root, "bin")}
	uv := filepath.Join(paths.DataDir, "components", "uv-"+managedUVVersion, "uv")
	if err := os.MkdirAll(filepath.Dir(uv), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(uv, body, 0o700); err != nil {
		t.Fatal(err)
	}
	var spec Spec
	for _, candidate := range DefaultCatalog() {
		if candidate.Name == "headroom" {
			spec = candidate
		}
	}
	var diagnostic bytes.Buffer
	installer := Installer{Runner: absentRunner{}, Store: config.NewStore(paths), Out: &diagnostic}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := installer.installUVTool(ctx, spec); err == nil {
		t.Fatal("empty offline cache was treated as installed")
	}
	if !strings.Contains(strings.ToLower(diagnostic.String()), "offline") {
		t.Fatal("official runtime did not report an offline cache denial")
	}
	if _, err := os.Lstat(filepath.Join(paths.BinDir, spec.Executable)); !os.IsNotExist(err) {
		t.Fatal("cache miss published a launcher")
	}
	t.Log("official UV denied an empty offline cache; production installer published no launcher")
}

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
