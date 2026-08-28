package components

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

type absentRunner struct{}

func (absentRunner) LookPath(string) (string, error) { return "", errors.New("missing") }
func (absentRunner) Run(ctx context.Context, command string, args []string, options platform.RunOptions) (platform.Result, error) {
	return (platform.ExecRunner{}).Run(ctx, command, args, options)
}

type upgradeRunner struct{ lookups atomic.Int32 }

func (r *upgradeRunner) LookPath(string) (string, error) {
	r.lookups.Add(1)
	return "/old/managed/tool", nil
}
func (r *upgradeRunner) Run(context.Context, string, []string, platform.RunOptions) (platform.Result, error) {
	return platform.Result{Stdout: "tool 2.0.0\n"}, nil
}

func TestVerifiedBinaryInstallIsIdempotent(t *testing.T) {
	archive := testArchive(t, "tool", []byte("#!/bin/sh\necho tool 1.0.0\n"))
	sum := sha256.Sum256(archive)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.Write(archive) }))
	defer server.Close()
	root := t.TempDir()
	paths := config.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), BinDir: filepath.Join(root, "bin"), Config: filepath.Join(root, "config", "config.toml"), State: filepath.Join(root, "state", "state.toml"), Ownership: filepath.Join(root, "state", "ownership.toml"), HooksDir: filepath.Join(root, "data", "hooks")}
	store := config.NewStore(paths)
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	spec := Spec{Name: "tool", Executable: "tool", Version: "1.0.0", Strategy: StrategyBinary, Assets: map[string]Asset{runtime.GOOS + "/" + runtime.GOARCH: {URL: server.URL, SHA256: hex.EncodeToString(sum[:])}}}
	installer := Installer{Runner: absentRunner{}, Store: store, Catalog: []Spec{spec}, Client: server.Client()}
	if err := installer.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := installer.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("downloaded %d times", requests.Load())
	}
	info, err := os.Stat(filepath.Join(root, "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
}

func TestManagedSupplyChainRawComponentIsPrivateAndDoesNotTouchAgentConfig(t *testing.T) {
	payload := []byte("reviewed caveman proxy fixture")
	sum := sha256.Sum256(payload)
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	paths := config.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), BinDir: filepath.Join(root, "data", "bin"), Config: filepath.Join(root, "config", "config.toml"), State: filepath.Join(root, "state", "state.toml"), Ownership: filepath.Join(root, "state", "ownership.toml"), HooksDir: filepath.Join(root, "data", "hooks")}
	store := config.NewStore(paths)
	spec := Spec{Name: "caveman", Executable: "caveman-proxy", Version: "1.1.3", Strategy: StrategySupplyChain, Revision: strings.Repeat("a", 40), DefaultBranch: "main", License: "BSL-1.1", PayloadFormat: "raw", PayloadPath: "bin/caveman-proxy", NoVersionProbe: true, SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "upstream_checksum", Assets: map[string]Asset{runtime.GOOS + "/" + runtime.GOARCH: {URL: server.URL, SHA256: hex.EncodeToString(sum[:])}}}
	installer := Installer{Runner: absentRunner{}, Store: store, Catalog: []Spec{spec}, Client: server.Client()}
	if err := installer.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := installer.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("downloads=%d", requests.Load())
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	component := state.Components["caveman"]
	if !component.Managed || !strings.Contains(component.Path, filepath.Join("supply-chain", "objects", "caveman")) {
		t.Fatalf("component=%+v", component)
	}
	for _, path := range []string{filepath.Join(root, "home", ".codex"), filepath.Join(root, "home", ".claude"), filepath.Join(root, "home", ".config", "opencode")} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("installer modified agent config %s: %v", path, err)
		}
	}
}

func TestManagedOpenCodeArchiveUsesPinnedSupplyChainAndOwnedAuthIsUntouched(t *testing.T) {
	archive := testArchive(t, "opencode", []byte("#!/bin/sh\nprintf '1.18.25\\n'\n"))
	sum := sha256.Sum256(archive)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(archive) }))
	defer server.Close()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	paths := config.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), BinDir: filepath.Join(root, "data", "bin"), Config: filepath.Join(root, "config", "config.toml"), State: filepath.Join(root, "state", "state.toml"), Ownership: filepath.Join(root, "state", "ownership.toml"), HooksDir: filepath.Join(root, "data", "hooks")}
	store := config.NewStore(paths)
	spec := Spec{Name: "opencode", Executable: "opencode", Version: "1.18.25", Strategy: StrategySupplyChain, Revision: strings.Repeat("b", 40), DefaultBranch: "dev", License: "MIT", PayloadFormat: "tar_gzip", PayloadPath: "opencode", SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "upstream_checksum", Assets: map[string]Asset{runtime.GOOS + "/" + runtime.GOARCH: {URL: server.URL, SHA256: hex.EncodeToString(sum[:])}}}
	installer := Installer{Runner: absentRunner{}, Store: store, Catalog: []Spec{spec}, Client: server.Client()}
	if err := installer.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err := store.LoadState()
	if err != nil || !state.Components["opencode"].Managed || state.Components["opencode"].Version != "1.18.25" {
		t.Fatalf("state=%+v err=%v", state.Components["opencode"], err)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "share", "opencode", "auth.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("IVOAI touched OpenCode auth ownership: %v", err)
	}
}

func TestVersionlessManagedCompanionIsInstalledOnlyWithManagedParent(t *testing.T) {
	archive := testArchive(t, "tool-host", []byte("#!/bin/sh\nexit 0\n"))
	sum := sha256.Sum256(archive)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	root := t.TempDir()
	paths := config.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), BinDir: filepath.Join(root, "bin"), Config: filepath.Join(root, "config", "config.toml"), State: filepath.Join(root, "state", "state.toml"), Ownership: filepath.Join(root, "state", "ownership.toml"), HooksDir: filepath.Join(root, "data", "hooks")}
	store := config.NewStore(paths)
	if err := store.SaveState(config.State{Schema: config.SchemaVersion, Components: map[string]config.ComponentState{"tool": {Installed: true, Managed: false, Path: "/external/tool"}}}); err != nil {
		t.Fatal(err)
	}
	companion := Spec{Name: "tool-host", Executable: "tool-host", Version: "1.0.0", Strategy: StrategyBinary, RequiresManaged: "tool", NoVersionProbe: true, Assets: map[string]Asset{runtime.GOOS + "/" + runtime.GOARCH: {URL: server.URL, SHA256: hex.EncodeToString(sum[:])}}}
	installer := Installer{Runner: absentRunner{}, Store: store, Catalog: []Spec{companion}, Client: server.Client()}
	if err := installer.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatal("companion was installed for an externally managed parent")
	}
	state, _ := store.LoadState()
	state.Components["tool"] = config.ComponentState{Installed: true, Managed: true, Path: filepath.Join(root, "bin", "tool")}
	if err := store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	if err := installer.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("managed companion downloads=%d", requests.Load())
	}
	state, _ = store.LoadState()
	installed := state.Components["tool-host"]
	if !installed.Managed || installed.Version != "1.0.0" || filepath.Base(installed.Path) != "tool-host" {
		t.Fatalf("companion state=%+v", installed)
	}
}

func TestVersionCompatibility(t *testing.T) {
	for _, test := range []struct {
		actual, minimum string
		want            bool
	}{
		{"codex-cli 0.148.0", "0.148.0", true},
		{"2.1.231 (Claude Code)", "2.1.228", true},
		{"headroom, version 0.35.0", "0.36.0", false},
		{"not a version", "1.0.0", false},
		{"10.0.0", "2.99.99", true},
	} {
		if got := versionAtLeast(test.actual, test.minimum); got != test.want {
			t.Errorf("versionAtLeast(%q, %q) = %t, want %t", test.actual, test.minimum, got, test.want)
		}
	}
}

func TestManagedComponentUpgradeDoesNotBecomePreExisting(t *testing.T) {
	archive := testArchive(t, "tool", []byte("#!/bin/sh\necho tool 2.0.0\n"))
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(archive) }))
	defer server.Close()
	root := t.TempDir()
	paths := config.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), BinDir: filepath.Join(root, "bin"), Config: filepath.Join(root, "config", "config.toml"), State: filepath.Join(root, "state", "state.toml"), Ownership: filepath.Join(root, "state", "ownership.toml"), HooksDir: filepath.Join(root, "data", "hooks")}
	store := config.NewStore(paths)
	if err := store.SaveState(config.State{Schema: config.SchemaVersion, Components: map[string]config.ComponentState{"tool": {Installed: true, Managed: true, Version: "1.0.0", Path: filepath.Join(root, "bin", "tool")}}}); err != nil {
		t.Fatal(err)
	}
	runner := &upgradeRunner{}
	spec := Spec{Name: "tool", Executable: "tool", Version: "2.0.0", Strategy: StrategyBinary, Assets: map[string]Asset{runtime.GOOS + "/" + runtime.GOARCH: {URL: server.URL, SHA256: hex.EncodeToString(sum[:])}}}
	if err := (&Installer{Runner: runner, Store: store, Catalog: []Spec{spec}, Client: server.Client()}).Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runner.lookups.Load() != 0 {
		t.Fatal("managed upgrade was reclassified through PATH lookup")
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Components["tool"]; !got.Managed || got.Version != "2.0.0" {
		t.Fatalf("managed upgrade state = %+v", got)
	}
}

func TestChecksumMismatchLeavesNoBinary(t *testing.T) {
	archive := testArchive(t, "tool", []byte("x"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(archive) }))
	defer server.Close()
	root := t.TempDir()
	paths := config.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), BinDir: filepath.Join(root, "bin"), Config: filepath.Join(root, "config", "config.toml"), State: filepath.Join(root, "state", "state.toml"), Ownership: filepath.Join(root, "state", "ownership.toml"), HooksDir: filepath.Join(root, "data", "hooks")}
	store := config.NewStore(paths)
	_ = store.Ensure()
	spec := Spec{Name: "tool", Executable: "tool", Version: "1", Strategy: StrategyBinary, Assets: map[string]Asset{runtime.GOOS + "/" + runtime.GOARCH: {URL: server.URL, SHA256: strings.Repeat("0", 64)}}}
	installer := Installer{Runner: absentRunner{}, Store: store, Catalog: []Spec{spec}, Client: server.Client()}
	err := installer.Setup(context.Background())
	if err == nil {
		t.Fatal("accepted bad checksum")
	}
	if _, statErr := os.Stat(filepath.Join(root, "bin", "tool")); !os.IsNotExist(statErr) {
		t.Fatal("binary created")
	}
}

func TestReviewedNPMIntegrityIsEnforced(t *testing.T) {
	body := []byte("reviewed npm tarball")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()
	cache := t.TempDir()
	installer := Installer{Client: server.Client(), Store: config.NewStore(config.Paths{CacheDir: cache})}
	digest := sha512.Sum512(body)
	integrity := "sha512-" + base64.StdEncoding.EncodeToString(digest[:])
	archive, err := installer.downloadVerifiedNPM(context.Background(), server.URL, integrity)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(archive)
	installed, err := os.ReadFile(archive)
	if err != nil || !bytes.Equal(installed, body) {
		t.Fatalf("verified npm payload mismatch: %v", err)
	}
	if _, err := installer.downloadVerifiedNPM(context.Background(), server.URL, "sha512-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="); err == nil {
		t.Fatal("mismatched npm integrity was accepted")
	}
}

func TestEmbeddedRufloLockPinsEveryDependency(t *testing.T) {
	var ruflo Spec
	for _, spec := range DefaultCatalog() {
		if spec.Name == "ruflo" {
			ruflo = spec
			break
		}
	}
	if err := validateRufloLock(ruflo); err != nil {
		t.Fatal(err)
	}
}

func TestEmbeddedHeadroomLocksContainReviewedWheelsAndHashes(t *testing.T) {
	var headroom Spec
	for _, spec := range DefaultCatalog() {
		if spec.Name == "headroom" {
			headroom = spec
			break
		}
	}
	for platformName, lock := range map[string][]byte{"linux/amd64": headroomAMD64Lock, "linux/arm64": headroomARM64Lock} {
		asset := headroom.Assets[platformName]
		if len(lock) == 0 || !bytes.Contains(lock, []byte(asset.URL)) || !bytes.Contains(lock, []byte(asset.SHA256)) {
			t.Errorf("%s Headroom lock is not tied to reviewed wheel", platformName)
		}
	}
}

func testArchive(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
