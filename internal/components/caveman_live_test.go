package components

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/caveman"
	"github.com/ivo-lopes/ivoai/internal/cavemaneval"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

// The caller supplies only an archive already built by the reviewed script.
// Its source/patch identity is fixed below; transport serves those exact bytes
// locally to exercise the real immutable installer without publishing a fixture.
func TestLiveManagedCavemanInstallation(t *testing.T) {
	path := os.Getenv("IVOAI_TEST_CAVEMAN_ARCHIVE")
	if path == "" {
		t.Skip("requires locally built reviewed Caveman archive; no inference")
	}
	t.Setenv("IVOAI_TEST_MODE", "")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var sidecar managedGraftMetadata
	encoded, err := os.ReadFile(filepath.Join(filepath.Dir(path), "ivoai-caveman_linux_"+runtime.GOARCH+".metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(encoded, &sidecar) != nil {
		t.Fatal("malformed build sidecar")
	}
	metadata := cavemanMetadata(t, "linux/"+runtime.GOARCH, body)
	if sidecar.Version != metadata["version"] || sidecar.Platform != metadata["platform"] || sidecar.Revision != metadata["revision"] || sidecar.SHA256 != metadata["sha256"] || sidecar.SourceSHA256 != managedCavemanSource || sidecar.PatchSHA256 != managedCavemanPatch || sidecar.UpstreamRevision != managedCavemanUpstream {
		t.Fatal("archive differs from reviewed build sidecar")
	}
	specs, err := managedCavemanSpecs(encodedGraft(t, metadata), "linux/"+runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	paths := config.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), BinDir: filepath.Join(root, "bin"), Config: filepath.Join(root, "config", "config.toml"), State: filepath.Join(root, "state", "state.toml"), Ownership: filepath.Join(root, "state", "ownership.toml")}
	store := config.NewStore(paths)
	requests := 0
	client := &http.Client{Transport: managedReleaseTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != metadata["url"] {
			t.Fatal("unexpected archive request")
		}
		requests++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	})}
	installer := Installer{Runner: absentRunner{}, Store: store, Catalog: specs, Client: client}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for range 2 {
		if err := installer.Setup(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if requests != 2 {
		t.Fatalf("expected one immutable download per component, got%d", requests)
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	manager := supplychain.Manager{Root: filepath.Join(paths.DataDir, "supply-chain")}
	for _, spec := range specs {
		got := state.Components[spec.Name]
		if !got.Managed || got.Version != managedCavemanVersion {
			t.Fatalf("unexpected managed state: %+v", got)
		}
		info, err := os.Stat(got.Path)
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("unsafe installed binary: %v %+v", err, info)
		}
		for _, name := range []string{"LICENSE", "NOTICE", "files.json", "provenance.json", "third-party/modules.json"} {
			if _, err := os.Stat(filepath.Join(filepath.Dir(got.Path), name)); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, err := manager.Active(spec.Name); err != nil {
			t.Fatal(err)
		}
		result, err := (platform.ExecRunner{}).Run(ctx, got.Path, []string{"version", "--json"}, platform.RunOptions{CleanEnv: true, Env: []string{"HOME=" + root, "CAVEMAN_HOME=" + root, "CAVEMAN_MCP_EPHEMERAL=1"}, Timeout: 10 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		var v struct {
			Version string `json:"version"`
		}
		if json.Unmarshal([]byte(result.Stdout), &v) != nil || v.Version != "bin-v"+managedCavemanVersion {
			t.Fatal("installed version identity mismatch")
		}
	}
	proxySource, _, err := manager.Active("caveman")
	if err != nil {
		t.Fatal(err)
	}
	status := (caveman.Provider{Binary: state.Components["caveman"].Path, SupplyRoot: manager.Root, Expected: proxySource, Managed: true}).Probe(ctx)
	if !status.Available || status.Health != core.HealthHealthy {
		t.Fatalf("promoted proxy unavailable: %+v", status)
	}
	mcpSource, _, err := manager.Active("caveman-mcp")
	if err != nil {
		t.Fatal(err)
	}
	compressor := caveman.MCPCompressor{Binary: state.Components["caveman-mcp"].Path, SupplyRoot: manager.Root, Expected: mcpSource, Managed: true, RuntimeDir: filepath.Join(root, "session")}
	metrics, err := cavemaneval.Run(ctx, cavemaneval.Options{Root: filepath.Join(root, "canary"), Provider: "caveman", Compressor: compressor, ContextBudget: 8 << 10, RuntimeSHA256: metadata["sha256"]})
	if err != nil || !metrics.QualityPassed() || !metrics.OptimizationVerified() {
		t.Fatalf("installed compressor failed quality: %+v %v", metrics, err)
	}
	t.Logf("actual rebuilt archive installed and admitted twice; native structured probes and22-scenario fidelity canary passed; no inference")
}
