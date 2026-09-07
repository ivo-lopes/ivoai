package components

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/codexresolver"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

type codexTransport func(*http.Request) (*http.Response, error)

func (t codexTransport) RoundTrip(r *http.Request) (*http.Response, error) { return t(r) }

func TestCodexManagedUpgradeRollbackReapplyAndOffline(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), BinDir: filepath.Join(root, "bin"), Config: filepath.Join(root, "config", "config.toml"), State: filepath.Join(root, "state", "state.toml"), Ownership: filepath.Join(root, "state", "ownership.toml"), HooksDir: filepath.Join(root, "data", "hooks"), Secrets: filepath.Join(root, "config", "secrets.json"), SessionsDir: filepath.Join(root, "state", "sessions"), QuotaDir: filepath.Join(root, "state", "quota")}
	store := config.NewStore(paths)
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	script := func(version string) []byte {
		return []byte("#!/bin/sh\ncase \"$*\" in\n--version) echo 'codex-cli " + version + "';;\n*) echo 'Usage: codex';;\nesac\n")
	}
	old := codexPair{Codex: config.ComponentState{Installed: true, Managed: true, Version: "0.148.0", Path: filepath.Join(root, "old", "codex")}, Host: config.ComponentState{Installed: true, Managed: true, Version: "0.148.0", Path: filepath.Join(root, "old", "codex-code-mode-host")}}
	if err := os.MkdirAll(filepath.Join(root, "old"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{old.Codex.Path, old.Host.Path} {
		if err := os.WriteFile(path, script("0.148.0"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	i := Installer{Store: store, Runner: platform.ExecRunner{}}
	if err := i.activateCodexPair(old); err != nil {
		t.Fatal(err)
	}
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	archives := map[string][]byte{}
	assets := []map[string]string{}
	for _, name := range []string{"codex", "codex-code-mode-host"} {
		filename := name + "-" + arch + "-unknown-linux-musl.tar.gz"
		url := "https://github.com/openai/codex/releases/download/rust-v0.153.4/" + filename
		body := testArchive(t, name, script("0.153.4"))
		archives[url] = body
		assets = append(assets, map[string]string{"name": filename, "browser_download_url": url, "digest": fmt.Sprintf("sha256:%x", sha256.Sum256(body))})
	}
	metadata, _ := json.Marshal(map[string]any{"tag_name": "rust-v0.153.4", "assets": assets})
	offline := false
	corrupt := false
	requests := 0
	i.Client = &http.Client{Transport: codexTransport(func(req *http.Request) (*http.Response, error) {
		requests++
		if offline {
			return nil, errors.New("offline")
		}
		body := metadata
		if req.URL.Host != "api.github.com" {
			body = archives[req.URL.String()]
			if corrupt {
				body = []byte("tampered")
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}
	corrupt = true
	if err := i.UpdateCodex(context.Background(), false); err == nil {
		t.Fatal("accepted tampered archive")
	}
	state, _ := store.LoadState()
	if state.Components["codex"].Path != old.Codex.Path {
		t.Fatal("failed staging replaced previous")
	}
	corrupt = false
	if err := i.UpdateCodex(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	state, _ = store.LoadState()
	newPath := state.Components["codex"].Path
	if newPath == old.Codex.Path || state.Components["codex"].Version != "0.153.4" {
		t.Fatalf("not upgraded: %+v", state.Components)
	}
	oldHash, _ := codexresolver.Fingerprint(old.Codex.Path)
	if oldHash == "" {
		t.Fatal("old executable deleted")
	}
	offline = true
	before := requests
	if err := i.UpdateCodex(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if requests != before {
		t.Fatal("rollback required upstream")
	}
	state, _ = store.LoadState()
	if state.Components["codex"].Path != old.Codex.Path {
		t.Fatal("rollback did not restore original")
	}
	offline = false
	if err := i.UpdateCodex(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	state, _ = store.LoadState()
	if state.Components["codex"].Path != newPath {
		t.Fatal("reapply changed immutable path")
	}
	offline = true
	if err := i.UpdateCodex(context.Background(), false); err == nil || !strings.Contains(err.Error(), "preserved") {
		t.Fatalf("offline error: %v", err)
	}
	state, _ = store.LoadState()
	if state.Components["codex"].Path != newPath {
		t.Fatal("offline update mutated installation")
	}
	// Tampering with the previous binary must fail closed on rollback.
	if err := os.WriteFile(old.Host.Path, []byte("changed"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := i.UpdateCodex(context.Background(), true); err == nil {
		t.Fatal("rollback accepted modified previous companion")
	}
}
