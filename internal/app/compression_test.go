package app

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/caveman"
	"github.com/ivo-lopes/ivoai/internal/components"
	"github.com/ivo-lopes/ivoai/internal/config"
)

func TestCompressionSelectionIsMutuallyExclusiveWithCavemanDefault(t *testing.T) {
	root := t.TempDir()
	a := &App{Store: config.NewStore(config.Paths{DataDir: root}), componentCatalog: reviewedCompressionFixtureCatalog}
	state := config.State{Components: map[string]config.ComponentState{
		"caveman": {Installed: true, Managed: true, Path: filepath.Join(root, "caveman")},
	}}

	cfg := config.Default()
	provider, enabled, name := a.sessionCompression(cfg, state, "codex", filepath.Join(root, "runtime"))
	if _, ok := provider.(caveman.Provider); !ok || !enabled || name != "caveman" {
		t.Fatalf("default selection provider=%T enabled=%t name=%q", provider, enabled, name)
	}

	cfg.Compression = config.CompressionConfig{Provider: "headroom", Source: config.CompressionSourceExplicit}
	provider, enabled, name = a.sessionCompression(cfg, state, "codex", filepath.Join(root, "runtime"))
	if provider != nil || !enabled || name != "headroom" {
		t.Fatalf("Headroom selection provider=%T enabled=%t name=%q", provider, enabled, name)
	}

	cfg.Compression = config.CompressionConfig{Provider: "direct", Source: config.CompressionSourceExplicit}
	provider, enabled, name = a.sessionCompression(cfg, state, "codex", filepath.Join(root, "runtime"))
	if provider != nil || enabled || name != "direct" {
		t.Fatalf("Direct selection provider=%T enabled=%t name=%q", provider, enabled, name)
	}
}

// These explicit, schema-valid synthetic specs model a compiled reviewed release
// for selection/setup tests. They never admit a downloaded binary or substitute
// for the real archive/install/rollback tests in internal/components.
func reviewedCompressionFixtureCatalog() []components.Spec {
	catalog := components.ClientSetupCatalog(false)
	result := make([]components.Spec, 0, len(catalog)+2)
	for _, spec := range catalog {
		if spec.Name != "caveman" && spec.Name != "caveman-mcp" {
			result = append(result, spec)
		}
	}
	artifact := sha256.Sum256([]byte("isolated App compression fixture for linux/" + runtime.GOARCH))
	digest := hex.EncodeToString(artifact[:])
	source := "29ef4c5bccee70dbc486bae266f966b8bd53132c6e12e27e9b06b36cf35c51f4"
	patch := "5f95e80d41d5744f16e09d534b1d56585fe746f9d5aed1e14c2586bbe953810d"
	identity := sha256.Sum256([]byte(source + "\n" + patch + "\n" + digest + "\n"))
	for _, name := range []string{"caveman", "caveman-mcp"} {
		executable := "caveman-proxy"
		requires := ""
		if name == "caveman-mcp" {
			executable = name
			requires = "caveman"
		}
		result = append(result, components.Spec{Name: name, Executable: executable, Version: "4.0.2-ivoai.1", Strategy: components.StrategySupplyChain, RequiresManaged: requires, NoVersionProbe: true, Revision: hex.EncodeToString(identity[:]), DefaultBranch: "main", License: "Apache-2.0", PayloadFormat: "tar_gzip", PayloadPath: executable, Executables: []string{"caveman-mcp", "caveman-proxy"}, SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "checksum_only", Assets: map[string]components.Asset{"linux/" + runtime.GOARCH: {URL: "https://github.com/ivo-lopes/ivoai/releases/download/v0.17.0/ivoai-caveman_linux_" + runtime.GOARCH + ".tar.gz", SHA256: digest}}})
	}
	return result
}

func TestCompressionWithoutReviewedReleaseMetadataUsesDirect(t *testing.T) {
	a := &App{Store: config.NewStore(config.Paths{DataDir: t.TempDir()})}
	// This fixture expressly models a build without compression metadata even
	// when the go test binary happens to carry official release ldflags.
	a.componentCatalog = func() []components.Spec { return []components.Spec{} }
	state := config.State{Components: map[string]config.ComponentState{
		"caveman":     {Installed: true, Managed: true, Version: "2.1.0", Path: "/untrusted/retained/caveman"},
		"caveman-mcp": {Installed: true, Managed: true, Version: "2.1.0", Path: "/untrusted/retained/caveman-mcp"},
	}}
	provider, enabled, name := a.sessionCompression(config.Default(), state, "codex", t.TempDir())
	if provider != nil || enabled || name != "direct" {
		t.Fatalf("metadata-free build selected an unreviewed compressor: %T %t %s", provider, enabled, name)
	}
	if compressor := a.workingContextCompressor(config.Default(), state, t.TempDir()); compressor != nil {
		t.Fatal("metadata-free build selected a retained MCP compressor")
	}
}

func TestCompressionCatalogFixturePreservesImmutableSourceContract(t *testing.T) {
	a := &App{componentCatalog: reviewedCompressionFixtureCatalog}
	for _, name := range []string{"caveman", "caveman-mcp"} {
		spec, ok := a.componentSpec(name)
		if !ok {
			t.Fatal("fixture missing compression contract")
		}
		source, err := components.ResolvedSource(spec)
		if err != nil {
			t.Fatal(err)
		}
		if err := source.Validate(); err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(source.LogicalVersion, "-ivoai.1") || source.Integrity.SignatureStatus != "not_exposed" {
			t.Fatal("fixture claims upstream binary provenance")
		}
	}
}

func TestCompressionCatalogNilUsesCompiledProductionIdentity(t *testing.T) {
	a := &App{}
	for _, name := range []string{"caveman", "caveman-mcp"} {
		got, ok := a.componentSpec(name)
		if ok != components.ManagedCavemanAvailable() {
			t.Fatal("nil fixture seam changed compiled runtime availability")
		}
		if !ok {
			continue
		}
		wantFound := false
		for _, want := range components.DefaultCatalog() {
			if want.Name == name {
				wantFound = true
				if !reflect.DeepEqual(got, want) {
					t.Fatal("nil fixture seam changed compiled identity")
				}
			}
		}
		if !wantFound {
			t.Fatal("nil seam invented a runtime absent from production catalog")
		}
	}
}
