//go:build linux

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/caveman"
	"github.com/ivo-lopes/ivoai/internal/components"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

type adaptiveCavemanArchiveTransport func(*http.Request) (*http.Response, error)

func (f adaptiveCavemanArchiveTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type adaptiveCavemanInstallerRunner struct{ platform.ExecRunner }

func (adaptiveCavemanInstallerRunner) LookPath(string) (string, error) { return "", os.ErrNotExist }

// The public Core and test must carry the same compiled reviewed identity. No
// environment-provided version/digest broadens either production catalog.
func adaptiveFullCLIInstallCaveman(t *testing.T, production string, store *config.Store, row *adaptiveFullCLIReceipt) {
	t.Helper()
	t.Setenv("IVOAI_TEST_MODE", "")
	if !components.ManagedCavemanAvailable() {
		t.Fatal("CAVEMAN_REVIEWED_BUILD_METADATA_REQUIRED")
	}
	var specs []components.Spec
	for _, spec := range components.DefaultCatalog() {
		if spec.Name == "caveman" || spec.Name == "caveman-mcp" {
			specs = append(specs, spec)
		}
	}
	if len(specs) != 2 {
		t.Fatal("CAVEMAN_REVIEWED_PAIR_REQUIRED")
	}
	info, err := buildinfo.ReadFile(production)
	if err != nil {
		t.Fatal("CAVEMAN_PRODUCTION_METADATA_UNAVAILABLE")
	}
	const prefix = "github.com/ivo-lopes/ivoai/internal/components.managedCavemanBuild="
	var metadata map[string]string
	for _, setting := range info.Settings {
		if setting.Key != "-ldflags" {
			continue
		}
		for _, field := range strings.Fields(setting.Value) {
			field = strings.TrimPrefix(field, "-X=")
			if !strings.HasPrefix(field, prefix) {
				continue
			}
			encoded := strings.TrimPrefix(field, prefix)
			if len(encoded) > 8192 {
				t.Fatal("CAVEMAN_PRODUCTION_METADATA_INVALID")
			}
			body, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil || json.Unmarshal(body, &metadata) != nil {
				t.Fatal("CAVEMAN_PRODUCTION_METADATA_INVALID")
			}
		}
	}
	key := runtime.GOOS + "/" + runtime.GOARCH
	asset := specs[0].Assets[key]
	if metadata["version"] != specs[0].Version || metadata["revision"] != specs[0].Revision || metadata["platform"] != key || metadata["sha256"] != asset.SHA256 || metadata["url"] != asset.URL {
		t.Fatal("CAVEMAN_TEST_AND_PRODUCTION_IDENTITIES_DIFFER")
	}
	archive := os.Getenv("IVOAI_TEST_CAVEMAN_ARCHIVE")
	if !filepath.IsAbs(archive) {
		t.Fatal("CAVEMAN_REVIEWED_LOCAL_ARCHIVE_REQUIRED")
	}
	body, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal("CAVEMAN_REVIEWED_ARCHIVE_UNAVAILABLE")
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != asset.SHA256 {
		t.Fatal("CAVEMAN_ARCHIVE_DIFFERS_FROM_COMPILED_PIN")
	}
	requests := 0
	client := &http.Client{Transport: adaptiveCavemanArchiveTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != asset.URL {
			t.Fatal("CAVEMAN_UNREVIEWED_DOWNLOAD_REFUSED")
		}
		requests++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	})}
	ctx := context.Background()
	installer := components.Installer{Runner: adaptiveCavemanInstallerRunner{}, Store: store, Catalog: specs, Client: client, Out: io.Discard}
	for range 2 {
		if err := installer.Setup(ctx); err != nil {
			t.Fatal("CAVEMAN_REAL_INSTALLATION_FAILED")
		}
	}
	if requests != 2 {
		t.Fatal("CAVEMAN_IDEMPOTENT_INSTALLATION_FAILED")
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatal("CAVEMAN_MANAGED_STATE_UNAVAILABLE")
	}
	manager := supplychain.Manager{Root: filepath.Join(store.Paths.DataDir, "supply-chain")}
	for _, spec := range specs {
		expected, err := components.ResolvedSource(spec)
		if err != nil {
			t.Fatal("CAVEMAN_COMPILED_SOURCE_INVALID")
		}
		source, root, err := manager.Active(spec.Name)
		if err != nil || !reflect.DeepEqual(source, expected) || state.Components[spec.Name].Path != filepath.Join(root, spec.PayloadPath) || !state.Components[spec.Name].Managed || state.Components[spec.Name].Version != spec.Version {
			t.Fatal("CAVEMAN_REAL_OBJECT_IDENTITY_FAILED")
		}
	}
	source, _, err := manager.Active("caveman")
	if err != nil {
		t.Fatal("CAVEMAN_PROXY_IDENTITY_UNAVAILABLE")
	}
	provider := caveman.Provider{Binary: state.Components["caveman"].Path, SupplyRoot: manager.Root, Expected: source, Managed: true}
	lease, err := provider.Prepare(ctx, core.CompressionRequest{Executor: core.ComponentCodex, DirectPath: "/bin/true", Environment: []string{"PATH=/usr/bin:/bin"}, RuntimeDir: t.TempDir(), Fidelity: core.CompressionCompressible})
	if err != nil || lease == nil || !lease.Decision().Used || lease.Decision().Provider != "caveman" {
		t.Fatal("CAVEMAN_REAL_PROXY_NOT_READY")
	}
	if err := lease.Close(ctx); err != nil {
		t.Fatal("CAVEMAN_REAL_PROXY_CLEANUP_FAILED")
	}
	row.CavemanInstalled = true
	row.CavemanWrapReady = true
	row.CavemanVersion = specs[0].Version
	row.CavemanArchiveSHA256 = asset.SHA256
	row.ProductionCavemanMetadata = true
}

func TestLiveAdaptiveCavemanPairSetupWithoutInference(t *testing.T) {
	if os.Getenv("IVOAI_ADAPTIVE_CAVEMAN_INSTALL_LIVE") != "1" {
		t.Skip("requires compiled reviewed Caveman metadata and local archive; no inference")
	}
	root := t.TempDir()
	for key, directory := range map[string]string{"XDG_CONFIG_HOME": "config", "XDG_DATA_HOME": "data", "XDG_STATE_HOME": "state", "XDG_CACHE_HOME": "cache"} {
		t.Setenv(key, filepath.Join(root, directory))
	}
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	store := config.NewStore(paths)
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(config.Default()); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveState(config.State{Schema: config.StateSchemaVersion, Components: map[string]config.ComponentState{}}); err != nil {
		t.Fatal(err)
	}
	row := adaptiveFullCLIReceipt{}
	adaptiveFullCLIInstallCaveman(t, os.Getenv("IVOAI_ADAPTIVE_FULL_CLI_BINARY"), store, &row)
	if !row.CavemanInstalled || !row.CavemanWrapReady || !row.ProductionCavemanMetadata {
		t.Fatal("real pair installation not verified")
	}
	t.Logf("CAVEMAN_FULL_CLI_SETUP=PASS version=%s archive=%s real_install=true proxy_ready=true production_metadata_matched=true inference=false", row.CavemanVersion, row.CavemanArchiveSHA256)
}
