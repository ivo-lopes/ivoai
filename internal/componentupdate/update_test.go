package componentupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

type fakeDiscoverer struct{ source supplychain.ResolvedSource }

func (f *fakeDiscoverer) Resolve(context.Context, supplychain.Reference) (supplychain.ResolvedSource, error) {
	return f.source, nil
}

type fakeFetcher struct {
	payloads map[string][]byte
	calls    int
}

func (f *fakeFetcher) Fetch(_ context.Context, source supplychain.ResolvedSource) (io.ReadCloser, error) {
	f.calls++
	payload, ok := f.payloads[source.Revision]
	if !ok {
		return nil, errors.New("missing fixture")
	}
	return io.NopCloser(bytes.NewReader(payload)), nil
}

type fakeRunner struct {
	external string
	fail     bool
	runs     int
	options  []platform.RunOptions
}

func (f *fakeRunner) LookPath(string) (string, error) {
	if f.external == "" {
		return "", errors.New("missing")
	}
	return f.external, nil
}

func (f *fakeRunner) Run(_ context.Context, command string, _ []string, options platform.RunOptions) (platform.Result, error) {
	f.runs++
	f.options = append(f.options, options)
	if f.fail {
		return platform.Result{}, errors.New("health failed")
	}
	payload, err := os.ReadFile(command)
	return platform.Result{Stdout: string(payload)}, err
}

func TestManagedComponentUpdateNoChangeRollbackAndRecoveryState(t *testing.T) {
	root := t.TempDir()
	payloadA := []byte("component 1.0.0")
	payloadB := []byte("component 2.0.0")
	discoverer := &fakeDiscoverer{source: componentSource("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "1.0.0", payloadA)}
	fetcher := &fakeFetcher{payloads: map[string][]byte{discoverer.source.Revision: payloadA, strings.Repeat("b", 40): payloadB}}
	runner := &fakeRunner{}
	manager := testComponentManager(t, root, discoverer, fetcher, runner)

	first, err := manager.Update(context.Background(), componentReference("1.0.0"))
	if err != nil || !first.Changed || !first.State.Managed {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	discoverer.source = componentSource(strings.Repeat("b", 40), "2.0.0", payloadB)
	second, err := manager.Update(context.Background(), componentReference("2.0.0"))
	if err != nil || !second.Changed || second.State.Version != "2.0.0" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	fetchCalls := fetcher.calls
	unchanged, err := manager.Update(context.Background(), componentReference("2.0.0"))
	if err != nil || unchanged.Changed || fetcher.calls != fetchCalls {
		t.Fatalf("unchanged=%+v err=%v fetches=%d", unchanged, err, fetcher.calls)
	}
	rolled, err := manager.Rollback(context.Background(), "component")
	if err != nil || !rolled {
		t.Fatalf("rollback=%t err=%v", rolled, err)
	}
	state, err := manager.Store.LoadState()
	if err != nil || state.Components["component"].Version != "1.0.0" {
		t.Fatalf("state=%+v err=%v", state.Components["component"], err)
	}
	if info, err := os.Stat(manager.Supply.Root); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("supply root mode=%v err=%v", info.Mode(), err)
	}
	for _, options := range runner.options {
		if !options.CleanEnv || len(options.Env) != 1 || options.Env[0] != "PATH=/usr/bin:/bin" {
			t.Fatalf("health probe inherited provider environment: %+v", options)
		}
	}
}

func TestUnmanagedComponentIsPreservedWithoutDownload(t *testing.T) {
	root := t.TempDir()
	external := filepath.Join(root, "external")
	if err := os.WriteFile(external, []byte("component 9.0.0"), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("component 1.0.0")
	discoverer := &fakeDiscoverer{source: componentSource(strings.Repeat("a", 40), "1.0.0", payload)}
	fetcher := &fakeFetcher{payloads: map[string][]byte{discoverer.source.Revision: payload}}
	manager := testComponentManager(t, root, discoverer, fetcher, &fakeRunner{external: external})
	result, err := manager.Update(context.Background(), componentReference("1.0.0"))
	if err != nil || result.State.Managed || result.State.Path != external || fetcher.calls != 0 {
		t.Fatalf("result=%+v err=%v fetches=%d", result, err, fetcher.calls)
	}
}

func TestHealthFailurePreservesPreviousActiveAndAuthoritativeState(t *testing.T) {
	root := t.TempDir()
	payloadA := []byte("component 1.0.0")
	payloadB := []byte("component 2.0.0")
	discoverer := &fakeDiscoverer{source: componentSource(strings.Repeat("a", 40), "1.0.0", payloadA)}
	fetcher := &fakeFetcher{payloads: map[string][]byte{strings.Repeat("a", 40): payloadA, strings.Repeat("b", 40): payloadB}}
	runner := &fakeRunner{}
	manager := testComponentManager(t, root, discoverer, fetcher, runner)
	if _, err := manager.Update(context.Background(), componentReference("1.0.0")); err != nil {
		t.Fatal(err)
	}
	discoverer.source = componentSource(strings.Repeat("b", 40), "2.0.0", payloadB)
	runner.fail = true
	if _, err := manager.Update(context.Background(), componentReference("2.0.0")); err == nil {
		t.Fatal("health failure promoted candidate")
	}
	active, _, err := manager.Supply.Active("component")
	if err != nil || active.Revision != strings.Repeat("a", 40) {
		t.Fatalf("active=%+v err=%v", active, err)
	}
	state, _ := manager.Store.LoadState()
	if state.Components["component"].Version != "1.0.0" {
		t.Fatalf("state=%+v", state.Components["component"])
	}
}

func testComponentManager(t *testing.T, root string, discoverer *fakeDiscoverer, fetcher *fakeFetcher, runner *fakeRunner) Manager {
	t.Helper()
	paths := config.Paths{
		ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), BinDir: filepath.Join(root, "data", "bin"),
		Config: filepath.Join(root, "config", "config.toml"), State: filepath.Join(root, "state", "state.toml"), Ownership: filepath.Join(root, "state", "ownership.toml"), HooksDir: filepath.Join(root, "data", "hooks"), SessionsDir: filepath.Join(root, "state", "sessions"), QuotaDir: filepath.Join(root, "state", "quota"),
	}
	store := config.NewStore(paths)
	if err := store.SaveState(config.State{Schema: config.StateSchemaVersion, Components: map[string]config.ComponentState{}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveOwnership(config.Ownership{Schema: config.OwnershipSchemaVersion, Components: map[string]config.OwnedItem{}}); err != nil {
		t.Fatal(err)
	}
	return Manager{Supply: supplychain.Manager{Root: filepath.Join(root, "data", "supply-chain")}, Discoverer: discoverer, Fetcher: fetcher, Store: store, Runner: runner, Executable: "component", VersionArg: []string{"--version"}}
}

func componentReference(version string) supplychain.Reference {
	return supplychain.Reference{ID: "component", Kind: supplychain.KindComponent, Source: "https://example.invalid/component", Version: version}
}

func componentSource(revision, version string, payload []byte) supplychain.ResolvedSource {
	digest := sha256.Sum256(payload)
	return supplychain.ResolvedSource{
		ID: "component", Kind: supplychain.KindComponent, Source: "https://example.invalid/component", Revision: revision, LogicalVersion: version,
		PayloadFormat: "raw", PayloadPath: "bin/component", License: "MIT", Executables: []string{"bin/component"},
		Integrity: supplychain.Integrity{Algorithm: "sha256", Digest: fmt.Sprintf("%x", digest[:]), SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "upstream_checksum"},
	}
}
