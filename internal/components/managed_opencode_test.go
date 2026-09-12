package components

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
)

type managedReleaseTransport func(*http.Request) (*http.Response, error)

func (f managedReleaseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestManagedOpenCodeReleaseMetadataPromotesThroughInstaller(t *testing.T) {
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		t.Skip("managed release targets Linux amd64/arm64")
	}
	archive := testArchive(t, "opencode", []byte("#!/bin/sh\necho 1.18.25-ivoai.1\n"))
	digest := sha256.Sum256(archive)
	metadata := managedOpenCodeMetadata{
		Version: "1.18.25-ivoai.1", Platform: runtime.GOOS + "/" + runtime.GOARCH,
		SHA256: hex.EncodeToString(digest[:]), SourceSHA256: strings.Repeat("b", 64), PatchSHA256: strings.Repeat("c", 64),
		UpstreamRevision: "cb7d8b2f5e44876ef98b661dc10590c915af3a9f",
		Archive:          "ivoai-opencode_linux_" + runtime.GOARCH + ".tar.gz",
	}
	metadata.URL = "https://github.com/ivo-lopes/ivoai/releases/download/v0.10.1/" + metadata.Archive
	identity := sha256.Sum256([]byte(metadata.SourceSHA256 + "\n" + metadata.PatchSHA256 + "\n"))
	metadata.Revision = hex.EncodeToString(identity[:])
	body, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	previous := managedOpenCodeBuild
	managedOpenCodeBuild = base64.StdEncoding.EncodeToString(body)
	t.Cleanup(func() { managedOpenCodeBuild = previous })
	var spec Spec
	for _, candidate := range DefaultCatalog() {
		if candidate.Name == "opencode" {
			spec = candidate
		}
	}
	if spec.Version != metadata.Version || spec.TrustLevel != "checksum_only" || spec.SignatureStatus != "not_exposed" || spec.AttestationStatus != "not_exposed" {
		t.Fatal("release build must use the existing digest-only promotion contract without claiming upstream signatures")
	}
	root := t.TempDir()
	paths := config.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), BinDir: filepath.Join(root, "bin"), Config: filepath.Join(root, "config", "config.toml"), State: filepath.Join(root, "state", "state.toml"), Ownership: filepath.Join(root, "state", "ownership.toml")}
	store := config.NewStore(paths)
	requests := 0
	client := &http.Client{Transport: managedReleaseTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != metadata.URL {
			t.Fatalf("unexpected artifact URL: %s", r.URL)
		}
		requests++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(archive)), Request: r}, nil
	})}
	installer := Installer{Runner: absentRunner{}, Store: store, Catalog: []Spec{spec}, Client: client}
	for range 2 {
		if err := installer.Setup(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	state, err := store.LoadState()
	if err != nil || !state.Components["opencode"].Managed || state.Components["opencode"].Version != metadata.Version || requests != 1 {
		t.Fatalf("release setup did not promote idempotently: state=%+v requests=%d err=%v", state.Components["opencode"], requests, err)
	}
}

func TestManagedOpenCodeBuildIdentity(t *testing.T) {
	metadata := managedOpenCodeMetadata{
		Version: "1.18.25-ivoai.1", Platform: "linux/amd64",
		SHA256: strings.Repeat("a", 64), SourceSHA256: strings.Repeat("b", 64), PatchSHA256: strings.Repeat("c", 64),
		UpstreamRevision: "cb7d8b2f5e44876ef98b661dc10590c915af3a9f",
		Archive:          "ivoai-opencode_linux_amd64.tar.gz",
		URL:              "https://github.com/ivo-lopes/ivoai/releases/download/v0.10.1/ivoai-opencode_linux_amd64.tar.gz",
	}
	identity := sha256.Sum256([]byte(metadata.SourceSHA256 + "\n" + metadata.PatchSHA256 + "\n"))
	metadata.Revision = hex.EncodeToString(identity[:])
	encode := func(value managedOpenCodeMetadata) string {
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return base64.StdEncoding.EncodeToString(body)
	}
	original := Spec{Name: "opencode", Version: "1.18.25", Revision: metadata.UpstreamRevision,
		Strategy: StrategySupplyChain, PayloadPath: "opencode", License: "MIT",
		Assets: map[string]Asset{"linux/amd64": {URL: "https://example.invalid/upstream", SHA256: strings.Repeat("d", 64)}},
	}
	projected, err := managedOpenCodeSpec(original, encode(metadata), "linux/amd64")
	if err != nil {
		t.Fatal(err)
	}
	if projected.Version != metadata.Version || projected.Revision == original.Revision || projected.Assets["linux/amd64"].SHA256 != metadata.SHA256 || projected.Strategy != StrategySupplyChain || projected.PayloadPath != "opencode" {
		t.Fatal("patched build did not retain isolated, verified supply-chain identity")
	}
	if original.Version != "1.18.25" || original.Assets["linux/amd64"].URL != "https://example.invalid/upstream" {
		t.Fatal("upstream catalog was mutated")
	}
	preview := metadata
	preview.URL = strings.Replace(preview.URL, "v0.10.1", "v0.10.1-rc.1", 1)
	if _, err := managedOpenCodeSpec(original, encode(preview), "linux/amd64"); err != nil {
		t.Fatal("official prerelease tag contract rejected", err)
	}
	for name, mutate := range map[string]func(*managedOpenCodeMetadata){
		"different-platform": func(m *managedOpenCodeMetadata) { m.Platform = "linux/arm64" },
		"different-upstream": func(m *managedOpenCodeMetadata) { m.UpstreamRevision = strings.Repeat("f", 40) },
		"wrong-digest":       func(m *managedOpenCodeMetadata) { m.SHA256 = "invalid" },
		"wrong-identity":     func(m *managedOpenCodeMetadata) { m.Revision = strings.Repeat("e", 64) },
		"foreign-publisher":  func(m *managedOpenCodeMetadata) { m.URL = strings.Replace(m.URL, "ivo-lopes/ivoai", "other/repo", 1) },
		"credentials-in-url": func(m *managedOpenCodeMetadata) {
			m.URL = strings.Replace(m.URL, "https://", "https://user:fixture@", 1)
		},
		"mutable-branch-url": func(m *managedOpenCodeMetadata) { m.URL = strings.Replace(m.URL, "v0.10.1", "main", 1) },
		"wrong-archive":      func(m *managedOpenCodeMetadata) { m.Archive = "other.tar.gz" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := metadata
			mutate(&changed)
			if _, err := managedOpenCodeSpec(original, encode(changed), "linux/amd64"); err == nil {
				t.Fatal("invalid release metadata accepted")
			}
		})
	}
}
