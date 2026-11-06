package doctor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/components"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/headroom"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

type noCompressionExecutionRunner struct{ calls int }

func (r *noCompressionExecutionRunner) LookPath(string) (string, error) {
	r.calls++
	return "", os.ErrNotExist
}
func (r *noCompressionExecutionRunner) Run(context.Context, string, []string, platform.RunOptions) (platform.Result, error) {
	r.calls++
	return platform.Result{}, os.ErrPermission
}

func stagedCompressionFixture(t *testing.T, version string) (Doctor, components.Spec, config.ComponentState, *noCompressionExecutionRunner) {
	t.Helper()
	root := t.TempDir()
	body := []byte("#!/bin/sh\n# Doctor must never execute this file.\nexit 97\n")
	digest := sha256.Sum256(body)
	spec := components.Spec{Name: "caveman", Executable: "caveman-proxy", Version: version, Strategy: components.StrategySupplyChain, Revision: strings.Repeat("a", 64), DefaultBranch: "main", License: "Apache-2.0", PayloadFormat: "raw", PayloadPath: "bin/caveman-proxy", NoVersionProbe: true, SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "checksum_only", Assets: map[string]components.Asset{runtime.GOOS + "/" + runtime.GOARCH: {URL: "https://github.com/ivo-lopes/ivoai/releases/download/v0.17.0/caveman-proxy_linux_" + runtime.GOARCH, SHA256: hex.EncodeToString(digest[:])}}}
	source, err := components.ResolvedSource(spec)
	if err != nil {
		t.Fatal(err)
	}
	accept := supplychain.ValidatorFunc(func(context.Context, supplychain.ResolvedSource, string) error { return nil })
	manager := supplychain.Manager{Root: filepath.Join(root, "supply-chain"), Structural: accept, Policy: accept, Health: accept}
	staged, err := manager.StageArchive(context.Background(), source, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Promote(staged); err != nil {
		t.Fatal(err)
	}
	_, path, err := manager.Active("caveman")
	if err != nil {
		t.Fatal(err)
	}
	state := config.ComponentState{Installed: true, Managed: true, Version: version, Path: filepath.Join(path, filepath.FromSlash(spec.PayloadPath))}
	runner := &noCompressionExecutionRunner{}
	return Doctor{Store: config.NewStore(config.Paths{DataDir: root}), Runner: runner}, spec, state, runner
}

func TestManagedCompressionDoctorRejectsLegacyIdentity(t *testing.T) {
	t.Setenv("IVOAI_TEST_MODE", "")
	d, _, state, runner := stagedCompressionFixture(t, "2.1.0")
	before, err := os.ReadFile(state.Path)
	if err != nil {
		t.Fatal(err)
	}
	got := d.managedComponent("caveman", state)
	if !got.Installed || got.Healthy {
		t.Fatalf("retained old identity reported as a healthy current compressor: %+v", got)
	}
	if provider, reason := effectiveCompression(config.Default(), headroom.Status{}, got); provider != "direct" || reason != "caveman_unavailable" {
		t.Fatal("legacy identity did not produce explicit Direct fallback")
	}
	after, err := os.ReadFile(state.Path)
	if err != nil || !bytes.Equal(before, after) || runner.calls != 0 {
		t.Fatal("Doctor executed or rewrote retained binary", err)
	}
}

func compressionTreeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		value := fmt.Sprintf("%v:%d", info.Mode(), info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value += fmt.Sprintf(":%x", sha256.Sum256(body))
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			value += ":" + target
		}
		result[relative] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestManagedCompressionDoctorRequiresCurrentIdentityAndIntegrity(t *testing.T) {
	t.Setenv("IVOAI_TEST_MODE", "")
	for _, tc := range []struct {
		name     string
		mutate   func(*components.Spec, *config.ComponentState)
		metadata bool
		healthy  bool
	}{
		{name: "current verified object", metadata: true, healthy: true},
		{name: "release metadata unavailable", metadata: false},
		{name: "historical state", metadata: true, mutate: func(_ *components.Spec, s *config.ComponentState) { s.Version = "2.1.0" }},
		{name: "unmanaged binary", metadata: true, mutate: func(_ *components.Spec, s *config.ComponentState) { s.Managed = false }},
		{name: "state path diverges", metadata: true, mutate: func(_ *components.Spec, s *config.ComponentState) { s.Path += ".outside" }},
		{name: "artifact identity differs", metadata: true, mutate: func(spec *components.Spec, _ *config.ComponentState) { spec.Revision = strings.Repeat("b", 64) }},
		{name: "artifact integrity differs", metadata: true, mutate: func(spec *components.Spec, _ *config.ComponentState) {
			key := runtime.GOOS + "/" + runtime.GOARCH
			asset := spec.Assets[key]
			asset.SHA256 = strings.Repeat("b", 64)
			spec.Assets = map[string]components.Asset{key: asset}
		}},
		{name: "publisher differs", metadata: true, mutate: func(spec *components.Spec, _ *config.ComponentState) {
			key := runtime.GOOS + "/" + runtime.GOARCH
			asset := spec.Assets[key]
			asset.URL = strings.Replace(asset.URL, "ivo-lopes/ivoai", "another/publisher", 1)
			spec.Assets = map[string]components.Asset{key: asset}
		}},
		{name: "platform asset unavailable", metadata: true, mutate: func(spec *components.Spec, _ *config.ComponentState) { spec.Assets = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, spec, state, runner := stagedCompressionFixture(t, "4.0.2-ivoai.1")
			var catalog []components.Spec
			if tc.mutate != nil {
				tc.mutate(&spec, &state)
			}
			if tc.metadata {
				catalog = []components.Spec{spec}
			}
			before := compressionTreeSnapshot(t, d.Store.Paths.DataDir)
			got := d.managedCatalogComponent("caveman", state, catalog)
			if got.Healthy != tc.healthy {
				t.Fatalf("current identity health=%v want=%v: %+v", got.Healthy, tc.healthy, got)
			}
			if tc.healthy && (got.Revision != spec.Revision || got.TrustLevel != "checksum_only") {
				t.Fatal("verified diagnostic lost exact provenance")
			}
			if !reflect.DeepEqual(before, compressionTreeSnapshot(t, d.Store.Paths.DataDir)) || runner.calls != 0 {
				t.Fatal("diagnostic executed a compressor or mutated the managed store")
			}
		})
	}
}

func TestManagedCompressionDoctorRejectsCorruptObjectsWithoutExecution(t *testing.T) {
	t.Setenv("IVOAI_TEST_MODE", "")
	for _, kind := range []string{"binary bytes", "binary permissions", "unlisted content", "active pointer missing"} {
		t.Run(kind, func(t *testing.T) {
			d, spec, state, runner := stagedCompressionFixture(t, "4.0.2-ivoai.1")
			switch kind {
			case "binary bytes":
				if err := os.WriteFile(state.Path, []byte("corrupted executable"), 0o700); err != nil {
					t.Fatal(err)
				}
			case "binary permissions":
				if err := os.Chmod(state.Path, 0o755); err != nil {
					t.Fatal(err)
				}
			case "unlisted content":
				if err := os.WriteFile(filepath.Join(filepath.Dir(state.Path), "unexpected-notice"), []byte("not in immutable manifest"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "active pointer missing":
				if err := os.Remove(filepath.Join(d.Store.Paths.DataDir, "supply-chain", "state", "caveman.json")); err != nil {
					t.Fatal(err)
				}
			}
			before := compressionTreeSnapshot(t, d.Store.Paths.DataDir)
			got := d.managedCatalogComponent("caveman", state, []components.Spec{spec})
			if got.Healthy {
				t.Fatal("corrupt component reported healthy")
			}
			if !reflect.DeepEqual(before, compressionTreeSnapshot(t, d.Store.Paths.DataDir)) || runner.calls != 0 {
				t.Fatal("diagnostic repaired, executed or mutated a rejected object")
			}
		})
	}
}

func TestManagedCompressionDoctorRetainsLegacyUnderCurrentCatalog(t *testing.T) {
	t.Setenv("IVOAI_TEST_MODE", "")
	d, current, state, runner := stagedCompressionFixture(t, "2.1.0")
	current.Version = "4.0.2-ivoai.1"
	before := compressionTreeSnapshot(t, d.Store.Paths.DataDir)
	got := d.managedCatalogComponent("caveman", state, []components.Spec{current})
	if got.Healthy || !got.Installed || got.Version != "2.1.0" || got.Path != state.Path {
		t.Fatalf("legacy metadata lost or accepted as current: %+v", got)
	}
	if !reflect.DeepEqual(before, compressionTreeSnapshot(t, d.Store.Paths.DataDir)) || runner.calls != 0 {
		t.Fatal("retained object changed during diagnosis")
	}
}

func TestManagedCompressionDoctorFixtureHealthRequiresExplicitTestMode(t *testing.T) {
	d := Doctor{}
	state := config.ComponentState{Installed: true, Managed: true, Version: "4.0.2-ivoai.1-fixture", Path: "/not-a-real-runtime"}
	t.Setenv("IVOAI_TEST_MODE", "")
	if got := d.managedCatalogComponent("caveman", state, nil); got.Healthy {
		t.Fatal("production trusted a synthetic fixture label")
	}
	t.Setenv("IVOAI_TEST_MODE", "1")
	if got := d.managedCatalogComponent("caveman", state, nil); !got.Healthy || !got.Fixture {
		t.Fatal("existing isolated fixture contract was lost")
	}
}
