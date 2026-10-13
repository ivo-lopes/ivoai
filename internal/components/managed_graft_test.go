package components

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/componentupdate"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

func graftMetadata(t *testing.T, arch string, payload []byte) map[string]string {
	t.Helper()
	sum := sha256.Sum256(payload)
	value := map[string]string{"version": "0.21.1-ivoai.1", "platform": "linux/" + arch, "sha256": hex.EncodeToString(sum[:]), "upstream_revision": "fe30ead39d5e6f0c921018d364da2bdbc9d4b3ad", "source_sha256": "ab27f9123eeb59b3987a342a24b7e357ad3412f41c556fde3a76ca9e250d2db1", "patch_sha256": strings.Repeat("c", 64), "archive": "ivoai-graft_linux_" + arch + ".tar.gz"}
	value["url"] = "https://github.com/ivo-lopes/ivoai/releases/download/v0.12.0-rc.1/" + value["archive"]
	identity := sha256.Sum256([]byte(value["source_sha256"] + "\n" + value["patch_sha256"] + "\n" + value["sha256"] + "\n"))
	value["revision"] = hex.EncodeToString(identity[:])
	return value
}
func encodedGraft(t *testing.T, value map[string]string) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(body)
}

func TestManagedGraftBuildIdentity(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		value := graftMetadata(t, arch, []byte("payload"))
		spec, err := managedGraftSpec(encodedGraft(t, value), "linux/"+arch)
		if err != nil {
			t.Fatal(err)
		}
		if spec.Name != "graft" || spec.Executable != "graft-runtime" || spec.PayloadPath != "graft-runtime" || spec.Strategy != StrategySupplyChain || spec.PayloadFormat != "tar_gzip" || spec.NoVersionProbe || spec.License != "MIT" || spec.TrustLevel != "checksum_only" || spec.SignatureStatus != "not_exposed" || spec.AttestationStatus != "not_exposed" {
			t.Fatalf("invalid managed contract: %+v", spec)
		}
	}
	for name, mutate := range map[string]func(map[string]string){
		"platform":  func(m map[string]string) { m["platform"] = "linux/arm64" },
		"version":   func(m map[string]string) { m["version"] = "0.21.1" },
		"upstream":  func(m map[string]string) { m["upstream_revision"] = strings.Repeat("a", 40) },
		"source":    func(m map[string]string) { m["source_sha256"] = strings.Repeat("a", 64) },
		"archive":   func(m map[string]string) { m["archive"] = "graft.tar.gz" },
		"digest":    func(m map[string]string) { m["sha256"] = strings.Repeat("f", 64) },
		"identity":  func(m map[string]string) { m["revision"] = strings.Repeat("d", 64) },
		"publisher": func(m map[string]string) { m["url"] = strings.Replace(m["url"], "ivo-lopes/ivoai", "other/repo", 1) },
		"userinfo":  func(m map[string]string) { m["url"] = strings.Replace(m["url"], "https://", "https://secret@", 1) },
		"mutable":   func(m map[string]string) { m["url"] = strings.Replace(m["url"], "v0.12.0-rc.1", "main", 1) },
		"query":     func(m map[string]string) { m["url"] += "?token=private" },
	} {
		t.Run(name, func(t *testing.T) {
			value := graftMetadata(t, "amd64", []byte("payload"))
			mutate(value)
			if _, err := managedGraftSpec(encodedGraft(t, value), "linux/amd64"); err == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
}

func TestManagedGraftReviewedRepackIdentity(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		for _, source := range []string{
			"ab27f9123eeb59b3987a342a24b7e357ad3412f41c556fde3a76ca9e250d2db1",
			"d39598390324ad0076a88ab593b63ad2ba352370ea07c073e31f11327979f8e1",
			strings.Repeat("a", 64),
		} {
			value := graftMetadata(t, arch, []byte("payload"))
			original := value["revision"]
			value["source_sha256"] = source
			identity := sha256.Sum256([]byte(source + "\n" + value["patch_sha256"] + "\n" + value["sha256"] + "\n"))
			value["revision"] = hex.EncodeToString(identity[:])
			_, err := managedGraftSpec(encodedGraft(t, value), "linux/"+arch)
			if accepted := source != strings.Repeat("a", 64); (err == nil) != accepted {
				t.Fatalf("source %s on %s: acceptance mismatch: %v", source, arch, err)
			}
			if source == "d39598390324ad0076a88ab593b63ad2ba352370ea07c073e31f11327979f8e1" {
				value["revision"] = original
				if _, err := managedGraftSpec(encodedGraft(t, value), "linux/"+arch); err == nil {
					t.Fatal("repack accepted an identity bound to the previous archive")
				}
			}
		}
	}
}

func TestManagedGraftDevelopmentCatalogDoesNotUseUpstream(t *testing.T) {
	old := managedGraftBuild
	managedGraftBuild = ""
	t.Cleanup(func() { managedGraftBuild = old })
	if ManagedGraftAvailable() {
		t.Fatal("development build claims managed Graft")
	}
	for _, spec := range DefaultCatalog() {
		if spec.Name == "graft" {
			t.Fatal("development build offers unreviewed Graft")
		}
	}
}

func TestManagedGraftInstallerPromotesIdempotentlyWithoutExternalAdoption(t *testing.T) {
	t.Setenv("IVOAI_TEST_MODE", "")
	archive := graftTestArchive(t, "graft-runtime", []byte("#!/bin/sh\n[ \"$#\" = 1 ] && [ \"$1\" = --version ] || exit 1\nprintf '0.21.1-ivoai.1\\n'\n"))
	metadata := graftMetadata(t, runtime.GOARCH, archive)
	testManagedGraftInstallation(t, archive, metadata)
}

func testManagedGraftInstallation(t *testing.T, archive []byte, metadata map[string]string) {
	t.Helper()
	t.Setenv("IVOAI_TEST_MODE", "")
	old := managedGraftBuild
	managedGraftBuild = encodedGraft(t, metadata)
	t.Cleanup(func() { managedGraftBuild = old })
	var spec Spec
	for _, candidate := range DefaultCatalog() {
		if candidate.Name == "graft" {
			spec = candidate
		}
	}
	if !ManagedGraftAvailable() || spec.Name == "" {
		t.Fatal("release catalog lacks managed Graft")
	}
	root := t.TempDir()
	paths := config.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), BinDir: filepath.Join(root, "bin"), Config: filepath.Join(root, "config", "config.toml"), State: filepath.Join(root, "state", "state.toml"), Ownership: filepath.Join(root, "state", "ownership.toml")}
	store := config.NewStore(paths)
	node := ManagedNodePath(paths.DataDir)
	npm := filepath.Join(filepath.Dir(filepath.Dir(node)), "lib", "node_modules", "npm", "bin", "npm-cli.js")
	for _, path := range []string{node, npm} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	requests := 0
	client := &http.Client{Transport: managedReleaseTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != metadata["url"] {
			t.Fatalf("unexpected download: %s", r.URL)
		}
		requests++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(archive)), Request: r}, nil
	})}
	runner := &graftProbeRunner{t: t, node: node}
	installer := Installer{Runner: runner, Store: store, Catalog: []Spec{spec}, Client: client}
	for range 2 {
		if err := installer.Setup(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	got := state.Components["graft"]
	if !got.Managed || got.Version != "0.21.1-ivoai.1" || !strings.Contains(got.Path, filepath.Join("supply-chain", "objects", "graft")) || requests != 1 || runner.probes == 0 {
		t.Fatalf("component=%+v requests=%d probes=%d", got, requests, runner.probes)
	}
	if filepath.Base(got.Path) != "graft-runtime" {
		t.Fatal("runtime root cannot be resolved")
	}

	for _, name := range []string{"dist", "node_modules", "LICENSE", "provenance.json"} {
		if _, err := os.Stat(filepath.Join(filepath.Dir(got.Path), filepath.FromSlash(name))); err != nil {
			t.Fatalf("runtime object lost %s: %v", name, err)
		}
	}
	for _, fixture := range []struct{ name, path, body string }{
		{"traversal", "../escaped", "unsafe"},
		{"absolute", "/escaped", "unsafe"},
		{"health", "graft-runtime", "#!/bin/sh\necho wrong-version\n"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			archive = graftTestArchive(t, fixture.path, []byte(fixture.body))
			metadata = graftMetadata(t, runtime.GOARCH, archive)
			candidate, err := managedGraftSpec(encodedGraft(t, metadata), "linux/"+runtime.GOARCH)
			if err != nil {
				t.Fatal(err)
			}
			installer.Catalog = []Spec{candidate}
			if err := installer.Setup(context.Background()); err == nil {
				t.Fatal("unsafe Graft candidate promoted")
			}
			current, err := store.LoadState()
			if err != nil || current.Components["graft"] != got {
				t.Fatalf("failed candidate changed active runtime: %+v %v", current.Components["graft"], err)
			}
		})
	}
	// A second reviewed object exercises real promotion and pointer/state rollback.
	// For the opt-in artifact test, the previous object is the distributed archive.
	archive = graftTestArchive(t, "graft-runtime", []byte("#!/bin/sh\n# second reviewed fixture\n[ \"$#\" = 1 ] && [ \"$1\" = --version ] || exit 1\nprintf '0.21.1-ivoai.1\\n'\n"))
	metadata = graftMetadata(t, runtime.GOARCH, archive)
	next, err := managedGraftSpec(encodedGraft(t, metadata), "linux/"+runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	installer.Catalog = []Spec{next}
	if err := installer.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	updated, err := store.LoadState()
	if err != nil || updated.Components["graft"].Path == got.Path {
		t.Fatal("second object was not activated", err)
	}
	source, err := ResolvedSource(next)
	if err != nil {
		t.Fatal(err)
	}
	rollback := componentupdate.Manager{Supply: supplychain.Manager{Root: filepath.Join(paths.DataDir, "supply-chain")}, Store: store, Runner: managedGraftRunner{Runner: runner}, Executable: "graft-runtime", VersionArg: []string{"--version"}, Discoverer: componentupdate.StaticDiscoverer{Source: source}, Fetcher: componentupdate.HTTPFetcher{Client: client}}
	if restored, err := rollback.Rollback(context.Background(), "graft"); err != nil || !restored {
		t.Fatal("Graft rollback failed", err)
	}
	restored, err := store.LoadState()
	if err != nil || restored.Components["graft"].Path != got.Path || restored.Components["graft"].Version != got.Version {
		t.Fatal("Graft rollback did not restore prior state", err)
	}
	if again, err := rollback.Rollback(context.Background(), "graft"); err != nil || again {
		t.Fatal("rollback not idempotent", err)
	}
}

type graftProbeRunner struct {
	t      *testing.T
	node   string
	probes int
}

func (r *graftProbeRunner) LookPath(string) (string, error) { return "/unmanaged/graft-runtime", nil }
func (r *graftProbeRunner) Run(ctx context.Context, path string, args []string, options platform.RunOptions) (platform.Result, error) {
	r.t.Helper()
	if path == "/unmanaged/graft-runtime" {
		r.t.Fatal("probed unreviewed external runtime")
	}
	if _, err := os.Stat(r.node); err != nil {
		r.t.Fatal("managed Node not present before promotion")
	}
	if len(args) != 1 || args[0] != "--version" || !options.CleanEnv {
		r.t.Fatalf("unsafe health probe: %v %+v", args, options)
	}
	r.probes++
	return (platform.ExecRunner{}).Run(ctx, path, args, options)
}

func graftTestArchive(t *testing.T, executable string, body []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	files := []struct {
		name string
		body []byte
		mode int64
	}{{executable, body, 0700}, {"dist/runtime.mjs", []byte("fixture"), 0600}, {"node_modules/reviewed/index.js", []byte("fixture"), 0600}, {"LICENSE", []byte("fixture"), 0600}, {"provenance.json", []byte("fixture"), 0600}}
	for _, file := range files {
		if err := tw.WriteHeader(&tar.Header{Name: file.name, Mode: file.mode, Size: int64(len(file.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(file.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestManagedGraftRequiresPinnedNodeBeforePromotion(t *testing.T) {
	t.Setenv("IVOAI_TEST_MODE", "")
	root := t.TempDir()
	paths := config.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), BinDir: filepath.Join(root, "bin"), Config: filepath.Join(root, "config", "config.toml"), State: filepath.Join(root, "state", "state.toml"), Ownership: filepath.Join(root, "state", "ownership.toml")}
	store := config.NewStore(paths)
	spec, err := managedGraftSpec(encodedGraft(t, graftMetadata(t, runtime.GOARCH, []byte("payload"))), "linux/"+runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	client := &http.Client{Transport: managedReleaseTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if !strings.HasPrefix(r.URL.String(), "https://nodejs.org/dist/v26.11.1/node-v26.11.1-linux-") {
			t.Fatalf("Graft downloaded before Node: %s", r.URL)
		}
		return nil, errors.New("fixture Node unavailable")
	})}
	installer := Installer{Store: store, Runner: absentRunner{}, Client: client, Catalog: []Spec{spec}}
	if err := installer.Setup(context.Background()); err == nil || !strings.Contains(err.Error(), "prepare managed Graft Node runtime") {
		t.Fatalf("Node failure not propagated: %v", err)
	}
	state, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Components["graft"].Installed || requests != 1 {
		t.Fatalf("runtime promoted despite Node failure: %+v downloads=%d", state.Components["graft"], requests)
	}
}

// Opt-in artifact integration uses an already-built local archive, never a network download.
func TestReviewedManagedGraftArtifact(t *testing.T) {
	archivePath := os.Getenv("IVOAI_GRAFT_TEST_ARCHIVE")
	if archivePath == "" {
		t.Skip("set IVOAI_GRAFT_TEST_ARCHIVE and IVOAI_GRAFT_TEST_METADATA to reviewed local build outputs")
	}
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(os.Getenv("IVOAI_GRAFT_TEST_METADATA"))
	if err != nil {
		t.Fatal(err)
	}
	metadata := map[string]string{}
	if err := json.Unmarshal(body, &metadata); err != nil {
		t.Fatal(err)
	}
	metadata["url"] = "https://github.com/ivo-lopes/ivoai/releases/download/v0.12.0-rc.1/" + metadata["archive"]
	testManagedGraftInstallation(t, archive, metadata)
}
