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

	"github.com/ivo-lopes/ivoai/internal/componentupdate"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

//go:embed assets/package.json
var rufloPackageJSON []byte

//go:embed assets/package-lock.json
var rufloPackageLock []byte

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
	managedUVVersion     = "0.12.5"
	managedPythonVersion = "3.13.15"
	managedNodeVersion   = "22.18.0"
	dependencyCutoff     = "2026-08-20T00:00:00Z"
)

// DefaultCatalog mirrors manifest/components.yaml. Release CI compares the
// validated versions before publishing, while keeping the binary self-contained.
func DefaultCatalog() []Spec {
	return []Spec{
		{Name: "codex", Executable: "codex", Version: "0.148.0", Strategy: StrategyBinary, Assets: map[string]Asset{
			"linux/amd64": {URL: "https://github.com/openai/codex/releases/download/rust-v0.148.0/codex-x86_64-unknown-linux-musl.tar.gz", SHA256: "1a36f762f6b3bef533bb86345ad9517661c2d84d53996a250cf2ca89d2cfee5a"},
			"linux/arm64": {URL: "https://github.com/openai/codex/releases/download/rust-v0.148.0/codex-aarch64-unknown-linux-musl.tar.gz", SHA256: "410c6ae0c763eb39c6da17665e63f9aa4a98e6ee663d81f8e8b779c97cb175ac"},
		}},
		{Name: "codex-code-mode-host", Executable: "codex-code-mode-host", Version: "0.148.0", Strategy: StrategyBinary, RequiresManaged: "codex", NoVersionProbe: true, Assets: map[string]Asset{
			"linux/amd64": {URL: "https://github.com/openai/codex/releases/download/rust-v0.148.0/codex-code-mode-host-x86_64-unknown-linux-musl.tar.gz", SHA256: "8e6e559b228fa61b18fb2c28c31ec02068751025bcce3f00cf63c79499d59829"},
			"linux/arm64": {URL: "https://github.com/openai/codex/releases/download/rust-v0.148.0/codex-code-mode-host-aarch64-unknown-linux-musl.tar.gz", SHA256: "1c410fe4bb174949649efe05c150b1512fb4775d5874eb54b2edb624cf7513a4"},
		}},
		{Name: "claude-code", Executable: "claude", Version: "2.1.228", Strategy: StrategyBinary, Assets: map[string]Asset{
			"linux/amd64": {URL: "https://github.com/anthropics/claude-code/releases/download/v2.1.228/claude-linux-x64.tar.gz", SHA256: "9050d667bcc3940b7ceee65e3e5c4439d2b7161a71d940fdf60192302243f960"},
			"linux/arm64": {URL: "https://github.com/anthropics/claude-code/releases/download/v2.1.228/claude-linux-arm64.tar.gz", SHA256: "877d423c35e6d059752f86399352837df5bf1af2a9dbcda5753d898629a439f4"},
		}},
		{Name: "headroom", Executable: "headroom", Version: "0.36.0", Package: "headroom-ai[proxy]", Strategy: StrategyUVIsolated, Assets: map[string]Asset{
			"linux/amd64": {URL: "https://github.com/headroomlabs-ai/headroom/releases/download/v0.36.0/headroom_ai-0.36.0-cp310-abi3-manylinux_2_28_x86_64.whl", SHA256: "fab6af014363c5a9a6bb41913a84df6f1daaecb56edf005925940e4501937f42"},
			"linux/arm64": {URL: "https://github.com/headroomlabs-ai/headroom/releases/download/v0.36.0/headroom_ai-0.36.0-cp310-abi3-manylinux_2_28_aarch64.whl", SHA256: "a00f3d7a705e15bc52a529c1476a775badb8d0e2e3fb72ec96f952814121697c"},
		}},
		{Name: "caveman", Executable: "caveman-proxy", Version: "1.1.3", Strategy: StrategySupplyChain,
			Revision: "0d2f052babfd613ec9b4186c86ec6f133cdfd4d7", DefaultBranch: "main", License: "BSL-1.1", PayloadFormat: "raw", PayloadPath: "bin/caveman-proxy", NoVersionProbe: true,
			SignatureStatus: "keysig_published_unverified", AttestationStatus: "not_exposed", TrustLevel: "upstream_checksum", Assets: map[string]Asset{
				"linux/amd64": {URL: "https://github.com/JuliusBrussee/caveman/releases/download/bin-v1.1.3/caveman-proxy_linux_amd64", SHA256: "d883b9ab4b559e0c1935335c0e24400deb5c61d5e247f1ca239c4149f57885b0"},
				"linux/arm64": {URL: "https://github.com/JuliusBrussee/caveman/releases/download/bin-v1.1.3/caveman-proxy_linux_arm64", SHA256: "2d6c1950bbce1a70c910a03bde88883817a8380f03f5ec25dd80186fed434ce7"},
			}},
		{Name: "opencode", Executable: "opencode", Version: "1.18.25", Strategy: StrategySupplyChain,
			Revision: "cb7d8b2f5e44876ef98b661dc10590c915af3a9f", DefaultBranch: "dev", License: "MIT", PayloadFormat: "tar_gzip", PayloadPath: "opencode",
			SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "upstream_checksum", Assets: map[string]Asset{
				"linux/amd64": {URL: "https://github.com/anomalyco/opencode/releases/download/v1.18.25/opencode-linux-x64.tar.gz", SHA256: "58a3729a6f3432dd6d2917fcc4a949788891a035818646ad480e12c947f56e78"},
				"linux/arm64": {URL: "https://github.com/anomalyco/opencode/releases/download/v1.18.25/opencode-linux-arm64.tar.gz", SHA256: "35ef77897425e41b5183a2c21ac4fb1d4d944d82a94e3c920f57b5490af11ac5"},
			}},
		{Name: "ai-memory", Executable: "ai-memory", Version: "1.29.0", Strategy: StrategyBinary, Assets: map[string]Asset{
			"linux/amd64": {URL: "https://github.com/akitaonrails/ai-memory/releases/download/v1.29.0/ai-memory-linux-x86_64.tar.gz", SHA256: "c666fa4ec778673ae995cd8aa4489b6184c7a3dc220a2c4e1c18792eda1321f1"},
			"linux/arm64": {URL: "https://github.com/akitaonrails/ai-memory/releases/download/v1.29.0/ai-memory-linux-aarch64.tar.gz", SHA256: "828cb63f697f8b773d4e6c41c38d0d850310afed04f37479bb48e3a11969d689"},
		}},
		{Name: "ruflo", Executable: "ruflo", Version: "3.38.12", Package: "ruflo", PackageURL: "https://registry.npmjs.org/ruflo/-/ruflo-3.38.12.tgz", Integrity: "sha512-NOQnhI/fKok9aM0c+NR/6r6K81LJIO9ZwX7UZUJqIZFU/jg8edMX2qvQVaJfh+wdGCWP7oCdZyxjnNn/5MVr0Q==", Strategy: StrategyNPMIsolated},
	}
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
		component, installErr := i.ensure(ctx, spec, state.Components[spec.Name])
		state.Components[spec.Name] = component
		ownership.Components[spec.Name] = config.OwnedItem{Managed: component.Managed, Path: component.Path}
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
	if previous.Installed && previous.Managed && previous.Version == spec.Version {
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
	if !spec.NoVersionProbe {
		actual := detectVersion(ctx, i.Runner, path)
		if !sameVersion(actual, spec.Version) {
			_ = os.Remove(path)
			return config.ComponentState{}, fmt.Errorf("installed version mismatch: expected %s, got %q", spec.Version, actual)
		}
	}
	return config.ComponentState{Installed: true, Managed: true, Version: spec.Version, Path: path}, nil
}

func (i *Installer) ensureSupplyChain(ctx context.Context, spec Spec, previous config.ComponentState) (config.ComponentState, error) {
	if previous.Installed && previous.Managed && previous.Version == spec.Version {
		if info, err := os.Lstat(previous.Path); err == nil && info.Mode()&os.ModeSymlink == 0 && info.Mode().IsRegular() && info.Mode().Perm() == 0o700 {
			return previous, nil
		}
	}
	asset, ok := spec.Assets[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return config.ComponentState{}, errors.New("no asset for current platform")
	}
	executables := []string{spec.PayloadPath}
	source := supplychain.ResolvedSource{
		ID: spec.Name, Kind: supplychain.KindComponent, Source: asset.URL, Revision: spec.Revision,
		LogicalVersion: spec.Version, DefaultBranch: spec.DefaultBranch, PayloadFormat: spec.PayloadFormat,
		PayloadPath: spec.PayloadPath, License: spec.License, Executables: executables,
		Integrity: supplychain.Integrity{Algorithm: "sha256", Digest: asset.SHA256, SignatureStatus: spec.SignatureStatus, AttestationStatus: spec.AttestationStatus, TrustLevel: spec.TrustLevel},
	}
	manager := componentupdate.Manager{
		Supply:     supplychain.Manager{Root: filepath.Join(i.Store.Paths.DataDir, "supply-chain"), Limits: supplychain.Limits{ArchiveBytes: 128 << 20, ExpandedBytes: 512 << 20, FileBytes: 128 << 20, Files: 4096}},
		Discoverer: componentupdate.StaticDiscoverer{Source: source}, Fetcher: componentupdate.HTTPFetcher{Client: i.Client},
		Store: i.Store, Runner: i.Runner, Executable: spec.Executable, VersionArg: []string{"--version"}, NoVersionProbe: spec.NoVersionProbe,
	}
	result, err := manager.Update(ctx, supplychain.Reference{ID: spec.Name, Kind: supplychain.KindComponent, Source: asset.URL, Version: spec.Version})
	return result.State, err
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
		if err := extractArchivePrefix(archive, "hooks", i.Store.Paths.HooksDir); err != nil {
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

func (i *Installer) installNPMTool(ctx context.Context, spec Spec) (string, error) {
	node, npmCLI, err := i.ensureNode(ctx)
	if err != nil {
		return "", err
	}
	prefix := filepath.Join(i.Store.Paths.DataDir, "components", "ruflo")
	if err := platform.EnsurePrivateDir(prefix); err != nil {
		return "", err
	}
	sandboxHome := filepath.Join(i.Store.Paths.DataDir, "installer-home")
	if err := platform.EnsurePrivateDir(sandboxHome); err != nil {
		return "", err
	}
	npmCache := filepath.Join(i.Store.Paths.CacheDir, "npm")
	if err := platform.EnsurePrivateDir(npmCache); err != nil {
		return "", err
	}
	if err := validateRufloLock(spec); err != nil {
		return "", err
	}
	if err := platform.AtomicWritePrivate(rufloPackageJSON, filepath.Join(prefix, "package.json")); err != nil {
		return "", err
	}
	if err := platform.AtomicWritePrivate(rufloPackageLock, filepath.Join(prefix, "package-lock.json")); err != nil {
		return "", err
	}
	_, err = i.Runner.Run(ctx, node, []string{npmCLI, "ci", "--prefix", prefix, "--ignore-scripts", "--no-audit", "--no-fund"}, platform.RunOptions{Env: []string{"HOME=" + sandboxHome, "PATH=" + filepath.Dir(node) + ":/usr/local/bin:/usr/bin:/bin", "NPM_CONFIG_CACHE=" + npmCache}, CleanEnv: true, Stdout: i.Out, Stderr: i.Out, Timeout: 15 * time.Minute})
	if err != nil {
		return "", err
	}
	return i.installWrapper(spec.Executable, filepath.Join(prefix, "node_modules", ".bin", spec.Executable), filepath.Dir(node))
}

func validateRufloLock(spec Spec) error {
	var lock struct {
		LockfileVersion int `json:"lockfileVersion"`
		Packages        map[string]struct {
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
	line := "exec " + shellQuote(target) + " \"$@\""
	if prependPath != "" {
		line = "PATH=" + shellQuote(prependPath) + ":\"$PATH\"\nexport PATH\n" + line
	}
	body := []byte("#!/bin/sh\n" + line + "\n")
	destination := filepath.Join(i.Store.Paths.BinDir, name)
	if err := platform.AtomicWritePrivate(body, destination); err != nil {
		return "", err
	}
	if err := os.Chmod(destination, 0o700); err != nil {
		return "", err
	}
	return destination, nil
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
	arch, sha := "x86_64", "68a509da24b06b4223a1c0175fb5eb5bc79342b76cbeff0cfe51ac3f5b17b6b2"
	if runtime.GOARCH == "arm64" {
		arch, sha = "aarch64", "9bf43b4d1a07665bf64d4c4e710930b382321a785e0eb10aac07f46471f86a31"
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
