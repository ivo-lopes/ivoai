package servercmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDockerAPTInstallUsesBookwormPackagesOnly(t *testing.T) {
	for _, unavailable := range []string{"docker-compose-v2", "docker-compose-plugin", "docker-compose"} {
		if slices.Contains(dockerAPTInstallArgs, unavailable) {
			t.Fatalf("Debian Bookworm-incompatible package remains in APT install: %s", unavailable)
		}
	}
	for _, required := range []string{"ca-certificates", "docker.io"} {
		if !slices.Contains(dockerAPTInstallArgs, required) {
			t.Fatalf("required operating-system package missing: %s", required)
		}
	}
}

func TestDockerComposePinsMatchReviewedManifest(t *testing.T) {
	manifest, err := os.ReadFile("../../manifest/components.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, architecture := range []string{"amd64", "arm64"} {
		asset, err := dockerComposeAsset(architecture)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range []string{dockerComposeVersion, asset.URL, asset.SHA256} {
			if !strings.Contains(string(manifest), value) {
				t.Errorf("Docker Compose %s pin is absent from manifest: %s", architecture, value)
			}
		}
	}
}

func TestInstallVerifiedComposePlugin(t *testing.T) {
	payload := []byte("verified compose fixture\n")
	digest := sha256.Sum256(payload)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "cli-plugins", "docker-compose")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	asset := composeAsset{URL: server.URL, SHA256: hex.EncodeToString(digest[:])}
	if err := installVerifiedComposePlugin(context.Background(), server.Client(), asset, destination); err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(destination)
	if err != nil || string(installed) != string(payload) {
		t.Fatalf("installed plugin mismatch: %q / %v", installed, err)
	}
	info, err := os.Stat(destination)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("installed plugin mode: %v / %v", info, err)
	}
	if err := installVerifiedComposePlugin(context.Background(), server.Client(), asset, destination); err != nil {
		t.Fatalf("exact pinned plugin was not idempotent: %v", err)
	}
}

func TestInstallVerifiedComposePluginPreservesPreExistingPaths(t *testing.T) {
	payload := []byte("downloaded fixture\n")
	digest := sha256.Sum256(payload)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	asset := composeAsset{URL: server.URL, SHA256: hex.EncodeToString(digest[:])}

	directory := t.TempDir()
	destination := filepath.Join(directory, "docker-compose")
	if err := os.WriteFile(destination, []byte("user-owned plugin\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installVerifiedComposePlugin(context.Background(), server.Client(), asset, destination); err == nil {
		t.Fatal("pre-existing plugin was replaced")
	}
	content, _ := os.ReadFile(destination)
	if string(content) != "user-owned plugin\n" {
		t.Fatalf("pre-existing plugin changed: %q", content)
	}

	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("safe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "compose-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := installVerifiedComposePlugin(context.Background(), server.Client(), asset, link); err == nil {
		t.Fatal("symlink plugin path was accepted")
	}
	content, _ = os.ReadFile(target)
	if string(content) != "safe\n" {
		t.Fatalf("symlink target changed: %q", content)
	}
}

func TestInstallVerifiedComposePluginRejectsCorruptDownload(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("partial or corrupted download\n"))
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "docker-compose")
	asset := composeAsset{
		URL:    server.URL,
		SHA256: strings.Repeat("0", sha256.Size*2),
	}
	if err := installVerifiedComposePlugin(context.Background(), server.Client(), asset, destination); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("corrupt download was not rejected safely: %v", err)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("corrupt plugin became visible at destination: %v", err)
	}
}
