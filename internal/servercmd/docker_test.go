package servercmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerRuntimeVersionsRequireGatewayPrioritySupport(t *testing.T) {
	tests := []struct {
		name, engine, compose, errorContains string
	}{
		{name: "minimum versions", engine: "28.0.0", compose: "2.33.1"},
		{name: "newer versions", engine: "29.7.2", compose: "5.5.0"},
		{name: "distribution suffixes", engine: "28.0.4+dfsg1", compose: "v2.33.1-desktop.1"},
		{name: "old engine", engine: "20.10.24+dfsg1", compose: "5.5.0", errorContains: "Docker Engine 28.0.0 or newer"},
		{name: "old compose", engine: "28.0.0", compose: "2.32.4", errorContains: "Docker Compose 2.33.1 or newer"},
		{name: "unparseable engine", engine: "unknown", compose: "5.5.0", errorContains: "determine Docker Engine server version"},
		{name: "unparseable compose", engine: "28.0.0", compose: "unknown", errorContains: "determine Docker Compose version"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateDockerRuntimeVersions(test.engine, test.compose)
			if test.errorContains == "" && err != nil {
				t.Fatalf("compatible runtime rejected: %v", err)
			}
			if test.errorContains != "" && (err == nil || !strings.Contains(err.Error(), test.errorContains)) {
				t.Fatalf("validateDockerRuntimeVersions(%q, %q) = %v, want error containing %q", test.engine, test.compose, err, test.errorContains)
			}
		})
	}
}

func TestEnsureDockerChecksTheDaemonVersionBeforeProvisioning(t *testing.T) {
	for _, test := range []struct {
		name, engine, compose, errorContains string
	}{
		{name: "compatible", engine: "29.7.2", compose: "5.5.0"},
		{name: "old daemon", engine: "20.10.24+dfsg1", compose: "5.5.0", errorContains: "Docker Engine 28.0.0 or newer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			docker := filepath.Join(directory, "docker")
			script := "#!/bin/sh\nif [ \"$1\" = version ]; then printf '%s\\n' '" + test.engine + "'; exit 0; fi\nif [ \"$1\" = compose ]; then printf '%s\\n' '" + test.compose + "'; exit 0; fi\nexit 1\n"
			if err := os.WriteFile(docker, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", directory)
			err := ensureDocker(context.Background(), io.Discard)
			if test.errorContains == "" && err != nil {
				t.Fatalf("compatible Docker runtime rejected: %v", err)
			}
			if test.errorContains != "" && (err == nil || !strings.Contains(err.Error(), test.errorContains)) {
				t.Fatalf("ensureDocker() = %v, want error containing %q", err, test.errorContains)
			}
		})
	}
}

func TestDockerComposePinsMatchReviewedManifest(t *testing.T) {
	manifest, err := os.ReadFile("../../manifest/components.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{dockerEngineMinimumVersion, dockerComposeMinimumVersion} {
		if !strings.Contains(string(manifest), value) {
			t.Errorf("Docker runtime minimum is absent from manifest: %s", value)
		}
	}
	for _, architecture := range []string{"amd64", "arm64"} {
		asset, err := dockerComposeAsset(architecture)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range []string{pinnedDockerComposeVersion, asset.URL, asset.SHA256} {
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
	var progress bytes.Buffer
	if err := installVerifiedComposePlugin(context.Background(), server.Client(), asset, destination, &progress); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Downloading Docker Compose", "download complete"} {
		if !strings.Contains(progress.String(), expected) {
			t.Errorf("download progress omitted %q: %s", expected, progress.String())
		}
	}
	installed, err := os.ReadFile(destination)
	if err != nil || string(installed) != string(payload) {
		t.Fatalf("installed plugin mismatch: %q / %v", installed, err)
	}
	info, err := os.Stat(destination)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("installed plugin mode: %v / %v", info, err)
	}
	if err := installVerifiedComposePlugin(context.Background(), server.Client(), asset, destination, io.Discard); err != nil {
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
	if err := installVerifiedComposePlugin(context.Background(), server.Client(), asset, destination, io.Discard); err == nil {
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
	if err := installVerifiedComposePlugin(context.Background(), server.Client(), asset, link, io.Discard); err == nil {
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
	if err := installVerifiedComposePlugin(context.Background(), server.Client(), asset, destination, io.Discard); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("corrupt download was not rejected safely: %v", err)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("corrupt plugin became visible at destination: %v", err)
	}
}
