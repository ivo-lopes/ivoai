package components

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/codexresolver"
	"github.com/ivo-lopes/ivoai/internal/componentupdate"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/memory"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
	"golang.org/x/sys/unix"
)

//go:embed assets/package.json
var rufloPackageJSON []byte

//go:embed assets/package-lock.json
var rufloPackageLock []byte

//go:embed assets/braces-3.0.4-ivoai.1.tgz
var rufloBracesPatch []byte

//go:embed assets/ivoai-zod3-compat-3.25.76-ivoai.1.tgz
var rufloZod3Patch []byte

//go:embed assets/headroom-amd64.lock
var headroomAMD64Lock []byte

//go:embed assets/headroom-arm64.lock
var headroomARM64Lock []byte

type Strategy string

const (
	StrategyBinary      Strategy = "verified-archive"
	StrategyUVIsolated  Strategy = "isolated-uv-tool"
	StrategyNPMIsolated Strategy = "isolated-npm-prefix"
	StrategySupplyChain Strategy = "managed-supply-chain"
)

type Asset struct{ URL, SHA256 string }
type Spec struct {
	Name, Executable, Version, Package string
	PackageURL, Integrity              string
	Strategy                           Strategy
	Assets                             map[string]Asset
	RequiresManaged                    string
	NoVersionProbe                     bool
	Revision, DefaultBranch, License   string
	PayloadFormat, PayloadPath         string
	SignatureStatus, AttestationStatus string
	TrustLevel                         string
}

const (
	managedUVVersion     = "0.13.0"
	managedPythonVersion = "3.14.8"
	managedNodeVersion   = "26.11.1"
	dependencyCutoff     = "2026-10-12T00:00:00Z"
)

// DefaultCatalog mirrors manifest/components.yaml. Release CI compares the
// validated versions before publishing, while keeping the binary self-contained.
func DefaultCatalog() []Spec {
	catalog := []Spec{
		{Name: "codex", Executable: "codex", Version: "0.162.1", Strategy: StrategyBinary, Assets: map[string]Asset{
			"linux/amd64": {URL: "https://github.com/openai/codex/releases/download/rust-v0.162.1/codex-x86_64-unknown-linux-musl.tar.gz", SHA256: "86f268d81b898f3e144c5ecff2ad2fda2c5802e042d6195b72538fe0fa125057"},
			"linux/arm64": {URL: "https://github.com/openai/codex/releases/download/rust-v0.162.1/codex-aarch64-unknown-linux-musl.tar.gz", SHA256: "bf3ca2719f1143b1440fbd85c149379e696c782c50ad7991f8bfcec60b2600c1"},
		}},
		{Name: "codex-code-mode-host", Executable: "codex-code-mode-host", Version: "0.162.1", Strategy: StrategyBinary, RequiresManaged: "codex", NoVersionProbe: true, Assets: map[string]Asset{
			"linux/amd64": {URL: "https://github.com/openai/codex/releases/download/rust-v0.162.1/codex-code-mode-host-x86_64-unknown-linux-musl.tar.gz", SHA256: "458239ea45c1fc508d89695f180ab352f2834709a8a46929a4466f4f04399ada"},
			"linux/arm64": {URL: "https://github.com/openai/codex/releases/download/rust-v0.162.1/codex-code-mode-host-aarch64-unknown-linux-musl.tar.gz", SHA256: "1395ce9367f8acd280674dff083edf25bf221d908b8351d5bd4f3fdde9181e9a"},
		}},
		{Name: "claude-code", Executable: "claude", Version: "2.1.296", Strategy: StrategyBinary, Assets: map[string]Asset{
			"linux/amd64": {URL: "https://github.com/anthropics/claude-code/releases/download/v2.1.296/claude-linux-x64.tar.gz", SHA256: "45fe2b33f5cbdd114309618970bcd90b8b163e34252d66cf100d7e068d9b831d"},
			"linux/arm64": {URL: "https://github.com/anthropics/claude-code/releases/download/v2.1.296/claude-linux-arm64.tar.gz", SHA256: "c644ad3e5ca3367e75d198194189d13180f33c491304c2d19557f7c1e8d5e0a6"},
		}},
		{Name: "headroom", Executable: "headroom", Version: "0.40.0", Package: "headroom-ai[proxy]", Strategy: StrategyUVIsolated, Assets: map[string]Asset{
			"linux/amd64": {URL: "https://github.com/headroomlabs-ai/headroom/releases/download/v0.40.0/headroom_ai-0.40.0-cp310-abi3-manylinux_2_28_x86_64.whl", SHA256: "49b6d8a01c8eceb5b9e9a675720ed5e4cb24e467282642653aac7f2fd8faf47f"},
			"linux/arm64": {URL: "https://github.com/headroomlabs-ai/headroom/releases/download/v0.40.0/headroom_ai-0.40.0-cp310-abi3-manylinux_2_28_aarch64.whl", SHA256: "140811066bfb7afe888f471be4a587375779af9de8dd76bb9a8126b52b8e930f"},
		}},
		{Name: "caveman", Executable: "caveman-proxy", Version: "2.1.0", Strategy: StrategySupplyChain,
			Revision: "e20f07e8152a0c0360f58c09e79d30ac94329991", DefaultBranch: "main", License: "Apache-2.0", PayloadFormat: "raw", PayloadPath: "bin/caveman-proxy", NoVersionProbe: true,
			SignatureStatus: "keysig_published_unverified", AttestationStatus: "not_exposed", TrustLevel: "upstream_checksum", Assets: map[string]Asset{
				"linux/amd64": {URL: "https://github.com/JuliusBrussee/caveman/releases/download/bin-v2.1.0/caveman-proxy_linux_amd64", SHA256: "44436d69705ad9968563c181cd11d396d6ae5eeccd8dbf228f6225d92dfadcb1"},
				"linux/arm64": {URL: "https://github.com/JuliusBrussee/caveman/releases/download/bin-v2.1.0/caveman-proxy_linux_arm64", SHA256: "5a881979934d51822ccf9ea13c519769fea18c52c061e14e6bd0d9f950ce760f"},
			}},
		{Name: "caveman-mcp", Executable: "caveman-mcp", Version: "2.1.0", Strategy: StrategySupplyChain, RequiresManaged: "caveman",
			Revision: "e20f07e8152a0c0360f58c09e79d30ac94329991", DefaultBranch: "main", License: "Apache-2.0", PayloadFormat: "raw", PayloadPath: "bin/caveman-mcp", NoVersionProbe: true,
			SignatureStatus: "keysig_published_unverified", AttestationStatus: "not_exposed", TrustLevel: "upstream_checksum", Assets: map[string]Asset{
				"linux/amd64": {URL: "https://github.com/JuliusBrussee/caveman/releases/download/bin-v2.1.0/caveman-mcp_linux_amd64", SHA256: "24339e645da66fb3580e3d9410743499845748238f01d0dc8097330fcb0f9bde"},
				"linux/arm64": {URL: "https://github.com/JuliusBrussee/caveman/releases/download/bin-v2.1.0/caveman-mcp_linux_arm64", SHA256: "fb49b996eee5a297a674a067979931d9a28d59cbe735be2b9104e7519ef7042a"},
			}},
		{Name: "opencode", Executable: "opencode", Version: "1.19.0", Strategy: StrategySupplyChain,
			Revision: "733b2c627e5aec39cb33cc22373bdce33053ef2c", DefaultBranch: "dev", License: "MIT", PayloadFormat: "tar_gzip", PayloadPath: "opencode",
			SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "upstream_checksum", Assets: map[string]Asset{
				"linux/amd64": {URL: "https://github.com/anomalyco/opencode/releases/download/v1.19.0/opencode-linux-x64.tar.gz", SHA256: "7bb9e61ad6feacf2e89c10a6a2c1840e3de4220bb50ba1fc7110f31806076aec"},
				"linux/arm64": {URL: "https://github.com/anomalyco/opencode/releases/download/v1.19.0/opencode-linux-arm64.tar.gz", SHA256: "e9841d43d3e1a8f36ed2691592d3b8b4fb097e606c8b559a277a1b845840e26b"},
			}},
		{Name: "ai-memory", Executable: "ai-memory", Version: "2.6.3", Strategy: StrategyBinary, Assets: map[string]Asset{
			"linux/amd64": {URL: "https://github.com/akitaonrails/ai-memory/releases/download/v2.6.3/ai-memory-linux-x86_64.tar.gz", SHA256: "933be9bb8fe6930336933f0b8adf0695007e644f945898f9f030fd312326f0f3"},
			"linux/arm64": {URL: "https://github.com/akitaonrails/ai-memory/releases/download/v2.6.3/ai-memory-linux-aarch64.tar.gz", SHA256: "e4e01409fe80148a4d561aef98ac5d526c488c4a621faa759f9d0b3ed2606088"},
		}},
		{Name: "ruflo", Executable: "ruflo", Version: "3.56.5", Package: "ruflo", PackageURL: "https://registry.npmjs.org/ruflo/-/ruflo-3.56.5.tgz", Integrity: "sha512-t80LC6aqtdRLDgv0wSrsV4VjBeYsnvKNUy8rH9tMxBZTVM83IDh3uKNgg5jA3hBixWz8k+3Eq6eUHO15MslMNQ==", Strategy: StrategyNPMIsolated},
	}
	if managedOpenCodeBuild != "" {
		for index := range catalog {
			if catalog[index].Name != "opencode" {
				continue
			}
			projected, err := managedOpenCodeSpec(catalog[index], managedOpenCodeBuild, runtime.GOOS+"/"+runtime.GOARCH)
			if err != nil {
				panic("invalid release-managed OpenCode build metadata")
			}
			catalog[index] = projected
		}
	}
	if ManagedGraftAvailable() {
		spec, err := managedGraftSpec(managedGraftBuild, runtime.GOOS+"/"+runtime.GOARCH)
		if err != nil {
			panic("invalid release-managed Graft build metadata")
		}
		catalog = append(catalog, spec)
	}
	return catalog
}

// ClientSetupCatalog leaves the reviewed legacy catalog available for explicit
// compatibility setup. Default client setup/update no longer installs, repairs,
// probes or adopts OpenCode, and does not remove its existing state or files.
func ClientSetupCatalog(includeLegacyOpenCode bool) []Spec {
	catalog := DefaultCatalog()
	if includeLegacyOpenCode {
		return catalog
	}
	result := make([]Spec, 0, len(catalog))
	for _, spec := range catalog {
		if spec.Name != "opencode" {
			result = append(result, spec)
		}
	}
	return result
}

type Installer struct {
	Runner  platform.Runner
	Store   *config.Store
	Catalog []Spec
	Out     io.Writer
	Client  *http.Client
}

func (i *Installer) Setup(ctx context.Context) error {
	if i.Runner == nil {
		i.Runner = platform.ExecRunner{}
	}
	if i.Client == nil {
		i.Client = &http.Client{Timeout: 15 * time.Minute}
	}
	if len(i.Catalog) == 0 {
		i.Catalog = DefaultCatalog()
		if !ManagedGraftAvailable() && i.Out != nil {
			fmt.Fprintln(i.Out, "Graft unavailable: this build has no reviewed managed runtime metadata.")
		}
	}
	state, err := i.Store.LoadState()
	if err != nil {
		return err
	}
	ownership, err := i.Store.LoadOwnership()
	if err != nil {
		return err
	}
	var failures []string
	for _, spec := range i.Catalog {
		if spec.RequiresManaged != "" && !state.Components[spec.RequiresManaged].Managed {
			continue
		}
		previous := state.Components[spec.Name]
		component, installErr := i.ensure(ctx, spec, previous)
		if installErr == nil {
			state.Components[spec.Name] = component
			ownership.Components[spec.Name] = config.OwnedItem{Managed: component.Managed, Path: component.Path}
		}
		if saveErr := i.Store.SaveState(state); saveErr != nil {
			return saveErr
		}
		if saveErr := i.Store.SaveOwnership(ownership); saveErr != nil {
			return saveErr
		}
		if installErr != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", spec.Name, installErr))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("component setup failed:\n  %s", strings.Join(failures, "\n  "))
	}
	return nil
}

func (i *Installer) ensure(ctx context.Context, spec Spec, previous config.ComponentState) (config.ComponentState, error) {
	if os.Getenv("IVOAI_TEST_MODE") == "1" {
		return i.fixture(spec)
	}
	if spec.Strategy == StrategySupplyChain {
		return i.ensureSupplyChain(ctx, spec, previous)
	}
	// The Ruflo version alone does not identify its transitive dependency pins.
	// Legacy flat prefixes must migrate even when the top-level version matches.
	ruflo := spec.Strategy == StrategyNPMIsolated && spec.Name == "ruflo"
	if ruflo && previous.Installed && previous.Managed && previous.Version == spec.Version && i.currentRufloProfile(spec, previous) {
		return previous, nil
	}
	// Existing compatible Codex pairs change only through the atomic updater,
	// never by overwriting their files during idempotent setup. The catalog
	// pin below is a verified fresh-install bootstrap, not launch policy.
	if !ruflo && previous.Installed && previous.Managed && (previous.Version == spec.Version || (spec.Name == "codex" || spec.Name == "codex-code-mode-host") && versionAtLeast(previous.Version, codexresolver.MinimumSupported)) {
		if info, err := os.Stat(previous.Path); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o077 == 0 {
			return previous, nil
		}
	}
	if !previous.Managed {
		if existing, err := i.Runner.LookPath(spec.Executable); err == nil {
			actual := detectVersion(ctx, i.Runner, existing)
			if versionAtLeast(actual, spec.Version) {
				return config.ComponentState{Installed: true, Managed: false, Version: actual, Path: existing}, nil
			}
		}
	}
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return config.ComponentState{}, fmt.Errorf("unsupported client platform %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	path, err := i.install(ctx, spec)
	if err != nil {
		return config.ComponentState{}, err
	}
	// Ruflo is probed before its atomic wrapper promotion. A post-promotion
	// failure must never remove the last working wrapper or its rollback target.
	if !spec.NoVersionProbe && !ruflo {
		actual := detectVersion(ctx, i.Runner, path)
		if !sameVersion(actual, spec.Version) {
			_ = os.Remove(path)
			return config.ComponentState{}, fmt.Errorf("installed version mismatch: expected %s, got %q", spec.Version, actual)
		}
	}
	return config.ComponentState{Installed: true, Managed: true, Version: spec.Version, Path: path}, nil
}

func (i *Installer) ensureSupplyChain(ctx context.Context, spec Spec, previous config.ComponentState) (config.ComponentState, error) {
	source, err := ResolvedSource(spec)
	if err != nil {
		return config.ComponentState{}, err
	}
	runner := i.Runner
	if spec.Name == "graft" {
		if _, _, err := i.ensureNode(ctx); err != nil {
			return config.ComponentState{}, fmt.Errorf("prepare managed Graft Node runtime: %w", err)
		}
		runner = managedGraftRunner{Runner: runner}
	}
	manager := componentupdate.Manager{
		Supply:     supplychain.Manager{Root: filepath.Join(i.Store.Paths.DataDir, "supply-chain"), Limits: componentSupplyChainLimits()},
		Discoverer: componentupdate.StaticDiscoverer{Source: source}, Fetcher: componentupdate.HTTPFetcher{Client: i.Client},
		Store: i.Store, Runner: runner, Executable: spec.Executable, VersionArg: []string{"--version"}, NoVersionProbe: spec.NoVersionProbe,
	}
	result, err := manager.Update(ctx, supplychain.Reference{ID: spec.Name, Kind: supplychain.KindComponent, Source: source.Source, Version: spec.Version})
	return result.State, err
}

// ResolvedSource returns the immutable, architecture-specific supply-chain
// identity used by both installation and runtime integrity preflight.
func ResolvedSource(spec Spec) (supplychain.ResolvedSource, error) {
	asset, ok := spec.Assets[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return supplychain.ResolvedSource{}, errors.New("no asset for current platform")
	}
	return supplychain.ResolvedSource{
		ID: spec.Name, Kind: supplychain.KindComponent, Source: asset.URL, Revision: spec.Revision,
		LogicalVersion: spec.Version, DefaultBranch: spec.DefaultBranch, PayloadFormat: spec.PayloadFormat,
		PayloadPath: spec.PayloadPath, License: spec.License, Executables: []string{spec.PayloadPath},
		Integrity: supplychain.Integrity{Algorithm: "sha256", Digest: asset.SHA256, SignatureStatus: spec.SignatureStatus, AttestationStatus: spec.AttestationStatus, TrustLevel: spec.TrustLevel},
	}, nil
}

func componentSupplyChainLimits() supplychain.Limits {
	return supplychain.DefaultLimits()
}

func versionAtLeast(actual, minimum string) bool {
	a, ok := semanticTriple(actual)
	if !ok {
		return false
	}
	b, ok := semanticTriple(minimum)
	if !ok {
		return false
	}
	for idx := range a {
		if a[idx] != b[idx] {
			return a[idx] > b[idx]
		}
	}
	return true
}

func sameVersion(actual, expected string) bool {
	a, aOK := semanticTriple(actual)
	b, bOK := semanticTriple(expected)
	return aOK && bOK && a == b
}

func semanticTriple(value string) ([3]int, bool) {
	for _, field := range strings.Fields(value) {
		candidate := strings.Trim(field, "vV(),[]{}")
		parts := strings.Split(candidate, ".")
		if len(parts) != 3 {
			continue
		}
		var result [3]int
		valid := true
		for idx, part := range parts {
			parsed, err := strconv.Atoi(part)
			if err != nil || parsed < 0 {
				valid = false
				break
			}
			result[idx] = parsed
		}
		if valid {
			return result, true
		}
	}
	return [3]int{}, false
}

func (i *Installer) fixture(spec Spec) (config.ComponentState, error) {
	fixtureDir := filepath.Join(i.Store.Paths.CacheDir, "test-fixtures")
	if err := platform.EnsurePrivateDir(fixtureDir); err != nil {
		return config.ComponentState{}, err
	}
	path := filepath.Join(fixtureDir, spec.Executable)
	body := []byte("#!/bin/sh\nprintf '%s\\n' '" + spec.Name + " " + spec.Version + " (ivoai test fixture)'\n")
	if err := platform.AtomicWritePrivate(body, path); err != nil {
		return config.ComponentState{}, err
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return config.ComponentState{}, err
	}
	return config.ComponentState{Installed: true, Managed: true, Version: spec.Version + "-fixture", Path: path}, nil
}

func (i *Installer) install(ctx context.Context, spec Spec) (string, error) {
	switch spec.Strategy {
	case StrategyBinary:
		return i.installBinary(ctx, spec)
	case StrategyUVIsolated:
		return i.installUVTool(ctx, spec)
	case StrategyNPMIsolated:
		return i.installNPMTool(ctx, spec)
	default:
		return "", fmt.Errorf("unsupported install strategy %q", spec.Strategy)
	}
}

func (i *Installer) installBinary(ctx context.Context, spec Spec) (string, error) {
	asset, ok := spec.Assets[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return "", errors.New("no asset for current platform")
	}
	archive, err := i.downloadVerified(ctx, asset)
	if err != nil {
		return "", err
	}
	defer os.Remove(archive)
	binary, err := extractSingleExecutable(archive, spec.Executable)
	if err != nil {
		return "", err
	}
	defer os.Remove(binary)
	if spec.Name == "ai-memory" {
		stagedHooks, err := os.MkdirTemp("", "ivoai-verified-hooks-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(stagedHooks)
		if err := extractArchivePrefix(archive, "hooks", stagedHooks); err != nil {
			return "", fmt.Errorf("stage ai-memory hook assets: %w", err)
		}
		if err := memory.InstallHookAssets(stagedHooks, i.Store.Paths.HooksDir); err != nil {
			return "", fmt.Errorf("install ai-memory hook assets: %w", err)
		}
	}

	return i.installManagedExecutable(binary, spec.Executable)
}

func extractArchivePrefix(archive, prefix, destination string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	if err := platform.EnsurePrivateDir(destination); err != nil {
		return err
	}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		clean := filepath.Clean(strings.TrimPrefix(h.Name, "./"))
		if clean == prefix || !strings.HasPrefix(clean, prefix+"/") {
			continue
		}
		rel := strings.TrimPrefix(clean, prefix+"/")
		if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe hook archive path %q", h.Name)
		}
		target := filepath.Join(destination, rel)
		if !strings.HasPrefix(target, destination+string(filepath.Separator)) {
			return fmt.Errorf("hook archive traversal %q", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := platform.EnsurePrivateDir(target); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if h.Size < 0 || h.Size > 4<<20 {
				return errors.New("invalid hook asset size")
			}
			data, err := io.ReadAll(io.LimitReader(tr, h.Size))
			if err != nil {
				return err
			}
			if err := platform.AtomicWritePrivate(data, target); err != nil {
				return err
			}
			if h.FileInfo().Mode()&0o111 != 0 {
				if err := os.Chmod(target, 0o700); err != nil {
					return err
				}
			}
		}
	}
}

func (i *Installer) installUVTool(ctx context.Context, spec Spec) (string, error) {
	uv, err := i.ensureUV(ctx)
	if err != nil {
		return "", err
	}
	asset := spec.Assets[runtime.GOOS+"/"+runtime.GOARCH]
	toolDir := filepath.Join(i.Store.Paths.DataDir, "components", "headroom")
	binDir := filepath.Join(toolDir, "bin")
	if err := platform.EnsurePrivateDir(toolDir); err != nil {
		return "", err
	}
	sandboxHome := filepath.Join(i.Store.Paths.DataDir, "installer-home")
	if err := platform.EnsurePrivateDir(sandboxHome); err != nil {
		return "", err
	}
	argument := spec.Package + " @ " + asset.URL + "#sha256=" + asset.SHA256
	lock := headroomAMD64Lock
	if runtime.GOARCH == "arm64" {
		lock = headroomARM64Lock
	}
	if !bytes.Contains(lock, []byte(asset.URL)) || !bytes.Contains(lock, []byte(asset.SHA256)) {
		return "", errors.New("embedded Headroom dependency lock does not match reviewed wheel")
	}
	lockPath := filepath.Join(toolDir, "requirements.lock")
	if err := platform.AtomicWritePrivate(lock, lockPath); err != nil {
		return "", err
	}
	uvEnv := []string{"HOME=" + sandboxHome, "PATH=" + filepath.Dir(uv) + ":/usr/local/bin:/usr/bin:/bin", "UV_TOOL_DIR=" + toolDir, "UV_TOOL_BIN_DIR=" + binDir, "UV_PYTHON_INSTALL_DIR=" + filepath.Join(i.Store.Paths.DataDir, "components", "python"), "UV_PYTHON_PREFERENCE=only-managed", "UV_NO_PROGRESS=1"}
	_, err = i.Runner.Run(ctx, uv, []string{"tool", "install", "--force", "--python", managedPythonVersion, "--constraints", lockPath, "--no-build", "--exclude-newer", dependencyCutoff, argument}, platform.RunOptions{Env: uvEnv, CleanEnv: true, Stdout: i.Out, Stderr: i.Out, Timeout: 15 * time.Minute})
	if err != nil {
		return "", err
	}
	return i.installWrapper(spec.Executable, filepath.Join(binDir, spec.Executable), "")
}

// rufloProfilePath identifies the reviewed dependency graph, independently of
// the unchanged top-level Ruflo version. Prior profiles remain rollback targets.
func (i *Installer) rufloProfilePath() string {
	hash := sha256.New()
	_, _ = hash.Write(rufloPackageJSON)
	_, _ = hash.Write(rufloPackageLock)
	return filepath.Join(i.Store.Paths.DataDir, "components", "ruflo-runtime-"+hex.EncodeToString(hash.Sum(nil)))
}

// rufloSafePath rejects symlinks in managed roots before any write. npm's .bin
// link is handled separately: only its resolved regular in-profile leaf runs.
func rufloSafePath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("Ruflo runtime requires a specific absolute managed path")
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Ruflo runtime path contains a symlink")
		}
		if current == filepath.Dir(current) {
			return nil
		}
	}
}

func rufloProfileLauncher(prefix string, spec Spec) (string, error) {
	launcher, err := rufloProfileLeaf(prefix, spec)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(launcher)
	if err != nil || info.Mode()&0o100 == 0 || info.Mode().Perm()&0o022 != 0 {
		return "", errors.New("Ruflo runtime launcher must be a contained regular executable")
	}
	return launcher, nil
}

func rufloProfileLeaf(prefix string, spec Spec) (string, error) {
	if err := rufloSafePath(prefix); err != nil {
		return "", err
	}
	for name, expected := range map[string][]byte{"package.json": rufloPackageJSON, "package-lock.json": rufloPackageLock} {
		body, err := platform.ReadRegularFile(filepath.Join(prefix, name), int64(len(expected)+1))
		if err != nil || !bytes.Equal(body, expected) {
			return "", errors.New("Ruflo runtime manifest or lock differs from the reviewed profile")
		}
	}
	launcher, err := filepath.EvalSymlinks(filepath.Join(prefix, "node_modules", ".bin", spec.Executable))
	if err != nil {
		return "", errors.New("Ruflo runtime launcher is missing")
	}
	relative, err := filepath.Rel(prefix, launcher)
	if err != nil || relative == "." || !filepath.IsLocal(relative) {
		return "", errors.New("Ruflo runtime launcher escapes the reviewed profile")
	}
	if err := rufloSafePath(launcher); err != nil {
		return "", err
	}
	info, err := os.Lstat(launcher)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("Ruflo runtime launcher must be a contained regular executable")
	}
	return launcher, nil
}

func (i *Installer) currentRufloProfile(spec Spec, previous config.ComponentState) bool {
	wrapper := filepath.Join(i.Store.Paths.BinDir, spec.Executable)
	if previous.Path != wrapper || rufloSafePath(wrapper) != nil {
		return false
	}
	info, err := os.Lstat(wrapper)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o700 {
		return false
	}
	node := ManagedNodePath(i.Store.Paths.DataDir)
	if rufloSafePath(node) != nil {
		return false
	}
	if info, err := os.Lstat(node); err != nil || !info.Mode().IsRegular() || info.Mode()&0o100 == 0 {
		return false
	}
	launcher, err := rufloProfileLauncher(i.rufloProfilePath(), spec)
	if err != nil {
		return false
	}
	body, err := platform.ReadRegularFile(wrapper, 16<<10)
	return err == nil && bytes.Equal(body, wrapperScript(launcher, filepath.Dir(ManagedNodePath(i.Store.Paths.DataDir))))
}

func (i *Installer) installNPMTool(ctx context.Context, spec Spec) (string, error) {
	if spec.Name != "ruflo" || spec.Executable != "ruflo" {
		return "", errors.New("unsupported managed npm runtime")
	}
	if err := validateRufloLock(spec); err != nil {
		return "", err
	}
	if err := rufloSafePath(filepath.Join(i.Store.Paths.BinDir, spec.Executable)); err != nil {
		return "", err
	}
	root := filepath.Join(i.Store.Paths.DataDir, "components")
	if err := rufloSafePath(root); err != nil {
		return "", err
	}
	if err := platform.EnsurePrivateDir(root); err != nil {
		return "", err
	}
	// Serialize graph admission and wrapper promotion across setup processes.
	lock, err := os.OpenFile(filepath.Join(root, "ruflo-install.lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	if info, err := lock.Stat(); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("unsafe Ruflo installation lock")
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return "", errors.New("Ruflo installation already active")
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)

	if err := rufloSafePath(ManagedNodePath(i.Store.Paths.DataDir)); err != nil {
		return "", err
	}
	node, npmCLI, err := i.ensureNode(ctx)
	if err != nil {
		return "", err
	}
	if err := rufloSafePath(npmCLI); err != nil {
		return "", err
	}
	sandboxHome := filepath.Join(i.Store.Paths.DataDir, "installer-home")
	npmCache := filepath.Join(i.Store.Paths.CacheDir, "npm")
	for _, dir := range []string{sandboxHome, npmCache} {
		if err := rufloSafePath(dir); err != nil {
			return "", err
		}
		if err := platform.EnsurePrivateDir(dir); err != nil {
			return "", err
		}
	}
	environment := []string{"HOME=" + sandboxHome, "PATH=" + filepath.Dir(node) + ":/usr/local/bin:/usr/bin:/bin", "NPM_CONFIG_CACHE=" + npmCache, "RUFLO_PROVIDER=ivoai-disabled", "CLAUDE_FLOW_MEMORY_BACKEND=memory"}
	probe, err := i.Runner.Run(ctx, node, []string{"--version"}, platform.RunOptions{CleanEnv: true, Env: environment, Timeout: 10 * time.Second})
	if err != nil {
		if strings.Contains(probe.Stderr, "libatomic.so.1") {
			return "", errors.New("managed Node requires libatomic.so.1; on Debian/Ubuntu run sudo apt-get install libatomic1, then repeat ivoai setup")
		}
		return "", fmt.Errorf("managed Node runtime preflight: %w", err)
	}
	if !sameVersion(strings.TrimSpace(probe.Stdout), managedNodeVersion) {
		return "", errors.New("managed Node runtime version does not match the reviewed pin")
	}
	prefix := i.rufloProfilePath()
	if _, err := os.Lstat(prefix); errors.Is(err, os.ErrNotExist) {
		staging, err := os.MkdirTemp(root, ".ruflo-stage-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(staging)
		for name, body := range map[string][]byte{
			"package.json": rufloPackageJSON, "package-lock.json": rufloPackageLock,
			"braces-3.0.4-ivoai.1.tgz":              rufloBracesPatch,
			"ivoai-zod3-compat-3.25.76-ivoai.1.tgz": rufloZod3Patch,
		} {
			if err := platform.AtomicWritePrivate(body, filepath.Join(staging, name)); err != nil {
				return "", err
			}
		}
		_, err = i.Runner.Run(ctx, node, []string{npmCLI, "ci", "--prefix", staging, "--ignore-scripts", "--no-audit", "--no-fund"}, platform.RunOptions{Dir: staging, Env: environment, CleanEnv: true, Stdout: i.Out, Stderr: i.Out, Timeout: 15 * time.Minute})
		if err != nil {
			return "", err
		}
		// npm can preserve a group-writable executable mode from its package
		// extraction. Normalize only the contained leaf in this new private stage;
		// previously promoted profiles remain immutable and strictly validated.
		leaf, err := rufloProfileLeaf(staging, spec)
		if err != nil {
			return "", err
		}
		if err := os.Chmod(leaf, 0o700); err != nil {
			return "", err
		}
		if err := i.probeRufloProfile(ctx, node, staging, spec, environment); err != nil {
			return "", err
		}
		if err := platform.SyncDir(staging); err != nil {
			return "", err
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if err := os.Rename(staging, prefix); err != nil {
			return "", err
		}
		if err := platform.SyncDir(root); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	} else if err := i.probeRufloProfile(ctx, node, prefix, spec, environment); err != nil {
		// A named immutable profile is never repaired or replaced in place.
		return "", err
	}
	launcher, err := rufloProfileLauncher(prefix, spec)
	if err != nil {
		return "", err
	}
	wrapper := filepath.Join(i.Store.Paths.BinDir, spec.Executable)
	if err := rufloSafePath(wrapper); err != nil {
		return "", err
	}
	if err := ensureBinDir(i.Store.Paths.BinDir); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := platform.AtomicWriteFile(wrapperScript(launcher, filepath.Dir(node)), wrapper, 0o700); err != nil {
		return "", err
	}
	return wrapper, nil
}

func (i *Installer) probeRufloProfile(ctx context.Context, node, prefix string, spec Spec, environment []string) error {
	launcher, err := rufloProfileLauncher(prefix, spec)
	if err != nil {
		return err
	}
	result, err := i.Runner.Run(ctx, node, []string{launcher, "--version"}, platform.RunOptions{Dir: prefix, Env: environment, CleanEnv: true, Timeout: 10 * time.Second})
	if err != nil || !sameVersion(strings.TrimSpace(strings.Split(result.Stdout, "\n")[0]), spec.Version) {
		return errors.New("staged Ruflo version does not match the reviewed pin")
	}
	return nil
}

func validateRufloLock(spec Spec) error {
	var lock struct {
		LockfileVersion int `json:"lockfileVersion"`
		Packages        map[string]struct {
			Name      string `json:"name"`
			Version   string `json:"version"`
			Resolved  string `json:"resolved"`
			Integrity string `json:"integrity"`
			Link      bool   `json:"link"`
			InBundle  bool   `json:"inBundle"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(rufloPackageLock, &lock); err != nil || lock.LockfileVersion != 3 {
		return errors.New("embedded Ruflo package lock is invalid")
	}
	root, found := lock.Packages["node_modules/ruflo"]
	if !found || root.Version != spec.Version || root.Resolved != spec.PackageURL || root.Integrity != spec.Integrity {
		return errors.New("embedded Ruflo package lock does not match reviewed component")
	}
	for path, pkg := range lock.Packages {
		if path == "" || pkg.Link {
			continue
		}
		if path == "node_modules/braces" && pkg.Version == "3.0.4-ivoai.1" && pkg.Resolved == "file:braces-3.0.4-ivoai.1.tgz" {
			digest := sha512.Sum512(rufloBracesPatch)
			if pkg.Integrity != "sha512-"+base64.StdEncoding.EncodeToString(digest[:]) {
				return errors.New("embedded braces patch integrity mismatch")
			}
			continue
		}
		// This exact reviewed local fork is the only Zod file dependency admitted.
		// V4 dependencies must retain their official registry packages.
		if path == "node_modules/zod" && pkg.Name == "@ivoai/zod3-compat" && pkg.Version == "3.25.76-ivoai.1" && pkg.Resolved == "file:ivoai-zod3-compat-3.25.76-ivoai.1.tgz" {
			digest := sha512.Sum512(rufloZod3Patch)
			if pkg.Integrity != "sha512-"+base64.StdEncoding.EncodeToString(digest[:]) {
				return errors.New("embedded Zod V3 compatibility patch integrity mismatch")
			}
			continue
		}
		if pkg.Version == "" || (!pkg.InBundle && (pkg.Resolved == "" || pkg.Integrity == "" || !strings.HasPrefix(pkg.Resolved, "https://registry.npmjs.org/"))) {
			return fmt.Errorf("Ruflo lock contains unpinned dependency %s", path)
		}
	}
	return nil
}

func (i *Installer) downloadVerifiedNPM(ctx context.Context, endpoint, integrity string) (string, error) {
	const prefix = "sha512-"
	if !strings.HasPrefix(integrity, prefix) || endpoint == "" {
		return "", errors.New("npm package has no reviewed SHA-512 integrity")
	}
	expected, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(integrity, prefix))
	if err != nil || len(expected) != sha512.Size {
		return "", errors.New("npm package has invalid SHA-512 integrity")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := i.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download npm package: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download npm package: HTTP %d", resp.StatusCode)
	}
	file, err := os.CreateTemp(i.Store.Paths.CacheDir, "npm-package-*.tgz")
	if err != nil {
		return "", err
	}
	path := file.Name()
	failed := true
	defer func() {
		_ = file.Close()
		if failed {
			_ = os.Remove(path)
		}
	}()
	hash := sha512.New()
	written, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(resp.Body, (128<<20)+1))
	if err != nil {
		return "", err
	}
	if written > 128<<20 {
		return "", errors.New("npm package exceeds size limit")
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if !bytes.Equal(hash.Sum(nil), expected) {
		return "", errors.New("npm package integrity mismatch")
	}
	failed = false
	return path, nil
}

func detectVersion(ctx context.Context, runner platform.Runner, path string) string {
	r, err := runner.Run(ctx, path, []string{"--version"}, platform.RunOptions{Timeout: 10 * time.Second})
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(strings.Split(r.Stdout, "\n")[0])
}

func (i *Installer) downloadVerified(ctx context.Context, asset Asset) (string, error) {
	if len(asset.SHA256) != 64 {
		return "", errors.New("asset has no valid SHA-256")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := i.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download component: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download component: HTTP %d", resp.StatusCode)
	}
	f, err := os.CreateTemp(i.Store.Paths.CacheDir, "component-*.archive")
	if err != nil {
		return "", err
	}
	path := f.Name()
	failed := true
	defer func() {
		f.Close()
		if failed {
			os.Remove(path)
		}
	}()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return "", err
	}
	if written >= 512<<20 {
		return "", errors.New("component archive exceeds size limit")
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), asset.SHA256) {
		return "", errors.New("component checksum mismatch")
	}
	failed = false
	return path, nil
}

func extractSingleExecutable(archive, executable string) (string, error) {
	f, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var output string
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			continue
		}
		base := filepath.Base(filepath.Clean(h.Name))
		variant := strings.HasPrefix(base, executable+"-") && !strings.Contains(base, ".")
		if base != executable && !variant {
			continue
		}
		if h.Size <= 0 || h.Size > 512<<20 {
			return "", errors.New("invalid component binary size")
		}
		temp, err := os.CreateTemp("", "ivoai-component-*")
		if err != nil {
			return "", err
		}
		if _, err = io.CopyN(temp, tr, h.Size); err != nil {
			temp.Close()
			os.Remove(temp.Name())
			return "", err
		}
		if err = temp.Close(); err != nil {
			return "", err
		}
		if output != "" {
			os.Remove(temp.Name())
			return "", errors.New("archive contains multiple candidate executables")
		}
		output = temp.Name()
	}
	if output == "" {
		return "", fmt.Errorf("archive does not contain %s", executable)
	}
	return output, nil
}

func (i *Installer) installManagedExecutable(source, name string) (string, error) {
	if err := ensureBinDir(i.Store.Paths.BinDir); err != nil {
		return "", err
	}
	b, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}
	destination := filepath.Join(i.Store.Paths.BinDir, name)
	if err := platform.AtomicWritePrivate(b, destination); err != nil {
		return "", err
	}
	if err := os.Chmod(destination, 0o700); err != nil {
		return "", err
	}
	return destination, nil
}
func ensureBinDir(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return os.MkdirAll(path, 0o700)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("refusing unsafe bin directory %s", path)
	}
	return nil
}
func (i *Installer) installWrapper(name, target, prependPath string) (string, error) {
	if err := ensureBinDir(i.Store.Paths.BinDir); err != nil {
		return "", err
	}
	if info, err := os.Stat(target); err != nil || info.IsDir() {
		return "", fmt.Errorf("installed %s launcher is missing", name)
	}
	body := wrapperScript(target, prependPath)

	destination := filepath.Join(i.Store.Paths.BinDir, name)
	if err := platform.AtomicWritePrivate(body, destination); err != nil {
		return "", err
	}
	if err := os.Chmod(destination, 0o700); err != nil {
		return "", err
	}
	return destination, nil
}
func wrapperScript(target, prependPath string) []byte {
	line := "exec " + shellQuote(target) + " \"$@\""
	if prependPath != "" {
		line = "PATH=" + shellQuote(prependPath) + ":\"$PATH\"\nexport PATH\n" + line
	}
	return []byte("#!/bin/sh\n" + line + "\n")
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func (i *Installer) ensureUV(ctx context.Context) (string, error) {
	root := filepath.Join(i.Store.Paths.DataDir, "components", "uv-"+managedUVVersion)
	binary := filepath.Join(root, "uv")
	if info, err := os.Stat(binary); err == nil && info.Mode().IsRegular() {
		return binary, nil
	}
	if err := platform.EnsurePrivateDir(root); err != nil {
		return "", err
	}
	arch, sha := "x86_64", "1468ebd5a5541121837c5a2817b9972ba6090fa6caa3d142620850a47fb75154"
	if runtime.GOARCH == "arm64" {
		arch, sha = "aarch64", "3ccfb6af6e242433eb552f7d9676abd5412c6595c497e990d9c8cb7b5bd4d2c3"
	}
	asset := Asset{URL: "https://github.com/astral-sh/uv/releases/download/" + managedUVVersion + "/uv-" + arch + "-unknown-linux-gnu.tar.gz", SHA256: sha}
	archive, err := i.downloadVerified(ctx, asset)
	if err != nil {
		return "", err
	}
	defer os.Remove(archive)
	extracted, err := extractSingleExecutable(archive, "uv")
	if err != nil {
		return "", err
	}
	defer os.Remove(extracted)
	b, err := os.ReadFile(extracted)
	if err != nil {
		return "", err
	}
	if err = platform.AtomicWritePrivate(b, binary); err != nil {
		return "", err
	}
	if err = os.Chmod(binary, 0o700); err != nil {
		return "", err
	}
	return binary, nil
}
