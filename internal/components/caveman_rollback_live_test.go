package components

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/caveman"
	"github.com/ivo-lopes/ivoai/internal/cavemaneval"
	"github.com/ivo-lopes/ivoai/internal/componentupdate"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

// This separate opt-in path preserves the published2.1 baseline pins and tests
// a real raw-object to rebuilt-archive migration and back in a temporary store.
func TestLiveManagedCavemanPublishedUpgradeRollback(t *testing.T) {
	archivePath, baselineDir := os.Getenv("IVOAI_TEST_CAVEMAN_ARCHIVE"), os.Getenv("IVOAI_TEST_CAVEMAN_BASELINE_DIR")
	if archivePath == "" || baselineDir == "" {
		t.Skip("requires rebuilt archive and reviewed historical binaries; no inference")
	}
	t.Setenv("IVOAI_TEST_MODE", "")
	body, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cavemanMetadata(t, "linux/"+runtime.GOARCH, body)
	specs, err := managedCavemanSpecs(encodedGraft(t, metadata), "linux/"+runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	pins := map[string]map[string]string{
		"amd64": {"caveman": "44436d69705ad9968563c181cd11d396d6ae5eeccd8dbf228f6225d92dfadcb1", "caveman-mcp": "24339e645da66fb3580e3d9410743499845748238f01d0dc8097330fcb0f9bde"},
		"arm64": {"caveman": "5a881979934d51822ccf9ea13c519769fea18c52c061e14e6bd0d9f950ce760f", "caveman-mcp": "fb49b996eee5a297a674a067979931d9a28d59cbe735be2b9104e7519ef7042a"},
	}
	responses := map[string][]byte{metadata["url"]: body}
	var baseline []Spec
	for _, spec := range specs {
		binary, err := os.ReadFile(filepath.Join(baselineDir, spec.Executable+"_linux_"+runtime.GOARCH))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(binary)
		if hex.EncodeToString(sum[:]) != pins[runtime.GOARCH][spec.Name] {
			t.Fatal("historical binary differs from published immutable pin")
		}
		url := "https://github.com/JuliusBrussee/caveman/releases/download/bin-v2.1.0/" + spec.Executable + "_linux_" + runtime.GOARCH
		responses[url] = binary
		baseline = append(baseline, Spec{Name: spec.Name, Executable: spec.Executable, Version: "2.1.0", Strategy: StrategySupplyChain, Revision: "e20f07e8152a0c0360f58c09e79d30ac94329991", DefaultBranch: "main", License: "Apache-2.0", PayloadFormat: "raw", PayloadPath: "bin/" + spec.Executable, NoVersionProbe: true, SignatureStatus: "keysig_published_unverified", AttestationStatus: "not_exposed", TrustLevel: "upstream_checksum", Assets: map[string]Asset{"linux/" + runtime.GOARCH: {URL: url, SHA256: pins[runtime.GOARCH][spec.Name]}}})
	}
	root := t.TempDir()
	paths := config.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), BinDir: filepath.Join(root, "bin"), Config: filepath.Join(root, "config", "config.toml"), State: filepath.Join(root, "state", "state.toml"), Ownership: filepath.Join(root, "state", "ownership.toml")}
	store := config.NewStore(paths)
	client := &http.Client{Transport: managedReleaseTransport(func(r *http.Request) (*http.Response, error) {
		data, ok := responses[r.URL.String()]
		if !ok {
			t.Fatal("unexpected rollback fixture request")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data)), Request: r}, nil
	})}
	installer := Installer{Runner: absentRunner{}, Store: store, Catalog: baseline, Client: client}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := installer.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	previous, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	installer.Catalog = specs
	if err := installer.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range specs {
		if current.Components[spec.Name].Version != managedCavemanVersion || current.Components[spec.Name].Path == previous.Components[spec.Name].Path {
			t.Fatal("real archive upgrade was not promoted")
		}
	}
	supply := supplychain.Manager{Root: filepath.Join(paths.DataDir, "supply-chain")}
	for i := len(specs) - 1; i >= 0; i-- {
		spec := specs[i]
		source, err := ResolvedSource(spec)
		if err != nil {
			t.Fatal(err)
		}
		manager := componentupdate.Manager{Supply: supply, Store: store, Runner: absentRunner{}, Executable: spec.Executable, NoVersionProbe: true, Discoverer: componentupdate.StaticDiscoverer{Source: source}, Fetcher: componentupdate.HTTPFetcher{Client: client}}
		if ok, err := manager.Rollback(ctx, spec.Name); err != nil || !ok {
			t.Fatalf("rollback %s: %v %v", spec.Name, ok, err)
		}
		if ok, err := manager.Rollback(ctx, spec.Name); err != nil || ok {
			t.Fatalf("rollback not idempotent %s: %v %v", spec.Name, ok, err)
		}
	}
	restored, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range specs {
		if restored.Components[spec.Name] != previous.Components[spec.Name] {
			t.Fatal("rollback failed to restore exact historical state")
		}
	}
	source, _, err := supply.Active("caveman-mcp")
	if err != nil {
		t.Fatal(err)
	}
	compressor := caveman.MCPCompressor{Binary: restored.Components["caveman-mcp"].Path, SupplyRoot: supply.Root, Expected: source, Managed: true, RuntimeDir: filepath.Join(root, "session")}
	metrics, err := cavemaneval.Run(ctx, cavemaneval.Options{Root: filepath.Join(root, "restored-canary"), Provider: "caveman", Compressor: compressor, ContextBudget: 8 << 10, RuntimeSHA256: pins[runtime.GOARCH]["caveman-mcp"]})
	if err != nil || !metrics.QualityPassed() || !metrics.OptimizationVerified() {
		t.Fatalf("restored historical MCP failed fidelity: %+v %v", metrics, err)
	}
	t.Log("actual published2.1 raw binaries -> reviewed4.0.2-ivoai.1 archive -> exact published2.1 objects; idempotent rollback and restored22-scenario fidelity PASS; no inference")
}
