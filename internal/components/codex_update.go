package components

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/codexresolver"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"golang.org/x/sys/unix"
)

type codexPair struct {
	Codex     config.ComponentState `json:"codex"`
	Host      config.ComponentState `json:"host"`
	CodexHash string                `json:"codex_sha256"`
	HostHash  string                `json:"host_sha256"`
}
type codexUpdateJournal struct {
	Phase     string    `json:"phase"`
	Previous  codexPair `json:"previous"`
	Candidate codexPair `json:"candidate"`
}

// UpdateCodex is deliberately explicit. Launch never calls this operation.
// Both official, digest-verified executables are staged, smoke-tested and
// promoted as one immutable directory. Existing managed files stay untouched.
func (i *Installer) UpdateCodex(ctx context.Context, rollback bool) error {
	if i.Runner == nil {
		i.Runner = platform.ExecRunner{}
	}
	if i.Client == nil {
		i.Client = &http.Client{Timeout: 10 * time.Minute}
	}
	if err := i.Store.Ensure(); err != nil {
		return err
	}
	root := filepath.Join(i.Store.Paths.DataDir, "codex-releases")
	if err := platform.EnsurePrivateDir(root); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(root, "update.lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.New("Codex update already active")
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	journalPath := filepath.Join(root, "transaction.json")
	var journal codexUpdateJournal
	if body, err := platform.ReadRegularFile(journalPath, 64<<10); err == nil {
		if json.Unmarshal(body, &journal) != nil {
			return errors.New("invalid Codex update journal")
		}
		if journal.Phase == "promoting" {
			if journal.Previous.Codex.Path != "" || journal.Previous.Host.Path != "" {
				if err := validateCodexPair(journal.Previous); err != nil {
					return fmt.Errorf("recovery integrity: %w", err)
				}
			}
			if err := i.activateCodexPair(journal.Previous); err != nil {
				return fmt.Errorf("recover Codex update: %w", err)
			}
			journal.Phase = "rolled_back"
			if err := saveCodexJournal(journalPath, journal); err != nil {
				return err
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if rollback {
		if journal.Phase != "committed" || journal.Previous.Codex.Path == "" {
			return errors.New("no previous Codex installation available")
		}
		if err := validateCodexPair(journal.Previous); err != nil {
			return fmt.Errorf("rollback integrity: %w", err)
		}
		if err := i.smokeCodexPair(ctx, journal.Previous); err != nil {
			return err
		}
		journal.Phase = "promoting"
		if err := saveCodexJournal(journalPath, journal); err != nil {
			return err
		}
		if err := i.activateCodexPair(journal.Previous); err != nil {
			return err
		}
		journal.Phase = "rolled_back"
		return saveCodexJournal(journalPath, journal)
	}
	version, assets, err := i.discoverCodexStable(ctx)
	if err != nil {
		return fmt.Errorf("Codex upstream unavailable; installed clients preserved: %w", err)
	}
	state, err := i.Store.LoadState()
	if err != nil {
		return err
	}
	previous := codexPair{Codex: state.Components["codex"], Host: state.Components["codex-code-mode-host"]}
	previous.CodexHash, _ = codexresolver.Fingerprint(previous.Codex.Path)
	previous.HostHash, _ = codexresolver.Fingerprint(previous.Host.Path)
	if old, ok := codexresolver.Version(previous.Codex.Version); ok && codexresolver.Compare(old, version) > 0 {
		return errors.New("refusing implicit Codex downgrade")
	}
	destination := filepath.Join(root, version)
	pair := codexPair{Codex: config.ComponentState{Installed: true, Managed: true, Version: version, Path: filepath.Join(destination, "codex")}, Host: config.ComponentState{Installed: true, Managed: true, Version: version, Path: filepath.Join(destination, "codex-code-mode-host")}}
	if info, err := os.Lstat(destination); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe Codex object directory")
		}
		body, readErr := platform.ReadRegularFile(filepath.Join(destination, "verified-assets.json"), 64<<10)
		var saved struct {
			Version string
			Assets  map[string]Asset
			Pair    codexPair
		}
		if readErr != nil || json.Unmarshal(body, &saved) != nil || saved.Version != version || saved.Assets["codex"] != assets["codex"] || saved.Assets["codex-code-mode-host"] != assets["codex-code-mode-host"] || saved.Pair.Codex.Path != pair.Codex.Path || saved.Pair.Host.Path != pair.Host.Path {
			return errors.New("existing Codex object provenance mismatch")
		}
		pair = saved.Pair
		if err := validateCodexPair(pair); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else {
		staging, err := os.MkdirTemp(root, ".staging-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(staging)
		for _, name := range []string{"codex", "codex-code-mode-host"} {
			archive, err := i.downloadVerified(ctx, assets[name])
			if err != nil {
				return err
			}
			binary, extractErr := extractSingleExecutable(archive, name)
			_ = os.Remove(archive)
			if extractErr != nil {
				return extractErr
			}
			if err := copyCodexStaged(binary, filepath.Join(staging, name)); err != nil {
				_ = os.Remove(binary)
				return err
			}
			_ = os.Remove(binary)
			if err := os.Chmod(filepath.Join(staging, name), 0700); err != nil {
				return err
			}
		}
		staged := pair
		staged.Codex.Path = filepath.Join(staging, "codex")
		staged.Host.Path = filepath.Join(staging, "codex-code-mode-host")
		if err := i.smokeCodexPair(ctx, staged); err != nil {
			return err
		}
		pair.CodexHash, err = codexresolver.Fingerprint(staged.Codex.Path)
		if err != nil {
			return err
		}
		pair.HostHash, err = codexresolver.Fingerprint(staged.Host.Path)
		if err != nil {
			return err
		}
		body, _ := json.Marshal(struct {
			Version string
			Assets  map[string]Asset
			Pair    codexPair
		}{version, assets, pair})
		if err := platform.AtomicWritePrivate(body, filepath.Join(staging, "verified-assets.json")); err != nil {
			return err
		}
		if err := os.Rename(staging, destination); err != nil {
			return err
		}
	}
	if err := i.smokeCodexPair(ctx, pair); err != nil {
		return err
	}
	if previous.Codex.Path == pair.Codex.Path && previous.Host.Path == pair.Host.Path {
		return nil
	}
	journal = codexUpdateJournal{Phase: "promoting", Previous: previous, Candidate: pair}
	if err := saveCodexJournal(journalPath, journal); err != nil {
		return err
	}
	if err := i.activateCodexPair(pair); err != nil {
		return errors.Join(err, i.activateCodexPair(previous))
	}
	journal.Phase = "committed"
	return saveCodexJournal(journalPath, journal)
}

func saveCodexJournal(path string, j codexUpdateJournal) error {
	body, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return platform.AtomicWritePrivate(body, path)
}
func validateCodexPair(p codexPair) error {
	for path, expected := range map[string]string{p.Codex.Path: p.CodexHash, p.Host.Path: p.HostHash} {
		got, err := codexresolver.Fingerprint(path)
		if err != nil || expected == "" || got != expected {
			return errors.New("Codex pair fingerprint mismatch")
		}
	}
	return nil
}
func (i *Installer) smokeCodexPair(ctx context.Context, p codexPair) error {
	options := platform.RunOptions{Timeout: 15 * time.Second, CleanEnv: true, Env: []string{"PATH=/usr/bin:/bin"}}
	result, err := i.Runner.Run(ctx, p.Codex.Path, []string{"--version"}, options)
	version, ok := codexresolver.Version(result.Stdout)
	if err != nil || !ok || version != p.Codex.Version || version != p.Host.Version || codexresolver.Compare(version, codexresolver.MinimumSupported) < 0 {
		return errors.New("Codex staged version incompatible")
	}
	for _, args := range [][]string{{"exec", "--help"}, {"app-server", "--help"}} {
		out, err := i.Runner.Run(ctx, p.Codex.Path, args, options)
		if err != nil || !strings.Contains(out.Stdout, "Usage:") {
			return errors.New("Codex staged capability smoke failed")
		}
	}
	info, err := os.Stat(p.Host.Path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return errors.New("Codex companion missing")
	}
	return nil
}
func (i *Installer) activateCodexPair(p codexPair) error {
	state, err := i.Store.LoadState()
	if err != nil {
		return err
	}
	owned, err := i.Store.LoadOwnership()
	if err != nil {
		return err
	}
	state.Components["codex"], state.Components["codex-code-mode-host"] = p.Codex, p.Host
	owned.Components["codex"] = config.OwnedItem{Path: p.Codex.Path, Managed: p.Codex.Managed}
	owned.Components["codex-code-mode-host"] = config.OwnedItem{Path: p.Host.Path, Managed: p.Host.Managed}
	if err := i.Store.SaveState(state); err != nil {
		return err
	}
	return i.Store.SaveOwnership(owned)
}

func (i *Installer) discoverCodexStable(parent context.Context) (string, map[string]Asset, error) {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/openai/codex/releases/latest", nil)
	response, err := i.Client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return "", nil, fmt.Errorf("upstream HTTP %d", response.StatusCode)
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&release); err != nil {
		return "", nil, err
	}
	version, ok := codexresolver.Version(strings.TrimPrefix(release.Tag, "rust-"))
	if !ok || !strings.HasPrefix(release.Tag, "rust-v") || release.Draft || release.Prerelease || codexresolver.Compare(version, codexresolver.MinimumSupported) < 0 {
		return "", nil, errors.New("upstream did not identify a compatible stable release")
	}
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	if runtime.GOOS != "linux" || arch == "" {
		return "", nil, errors.New("Codex platform unsupported")
	}
	assets := map[string]Asset{}
	for _, name := range []string{"codex", "codex-code-mode-host"} {
		want := name + "-" + arch + "-unknown-linux-musl.tar.gz"
		url := "https://github.com/openai/codex/releases/download/" + release.Tag + "/" + want
		for _, asset := range release.Assets {
			if asset.Name == want && asset.URL == url && strings.HasPrefix(asset.Digest, "sha256:") && len(asset.Digest) == 71 {
				assets[name] = Asset{URL: asset.URL, SHA256: strings.TrimPrefix(asset.Digest, "sha256:")}
			}
		}
	}
	if len(assets) != 2 {
		return "", nil, errors.New("official stable pair missing verified asset digests")
	}
	return version, assets, nil
}

func copyCodexStaged(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	return errors.Join(copyErr, syncErr, closeErr)
}
