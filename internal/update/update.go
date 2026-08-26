package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

type Release struct {
	Version string `json:"tag_name"`
	URL     string `json:"html_url"`
	Notes   string `json:"body"`
}
type Checker struct {
	Client      *http.Client
	Endpoint    string
	ReleaseBase string
}

// PreparedCandidate is a checksum-verified, version-probed executable staged
// beside the installed binary. Call Close if it is not promoted.
type PreparedCandidate struct {
	Path    string
	Version string
}

func (p *PreparedCandidate) Close() error {
	if p == nil || p.Path == "" {
		return nil
	}
	err := os.Remove(p.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (c Checker) Check(ctx context.Context, current string) (Release, bool, error) {
	if c.Client == nil {
		c.Client = &http.Client{Timeout: 20 * time.Second}
	}
	if c.Endpoint == "" {
		c.Endpoint = "https://api.github.com/repos/ivo-lopes/ivoai/releases/latest"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Endpoint, nil)
	if err != nil {
		return Release{}, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.Client.Do(req)
	if err != nil {
		return Release{}, false, fmt.Errorf("check update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Release{}, false, errors.New("no published ivoai release is available")
	}
	if resp.StatusCode != http.StatusOK {
		return Release{}, false, fmt.Errorf("GitHub returned HTTP %d", resp.StatusCode)
	}
	const releaseMetadataLimit = 1 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, releaseMetadataLimit+1))
	if err != nil {
		return Release{}, false, err
	}
	if len(data) > releaseMetadataLimit {
		return Release{}, false, errors.New("release metadata exceeds size limit")
	}
	var release Release
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&release); err != nil {
		return Release{}, false, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Release{}, false, errors.New("release metadata has trailing data")
	}
	if !validVersion(release.Version) {
		return Release{}, false, fmt.Errorf("invalid release version %q", release.Version)
	}
	if !validVersion(current) {
		return release, true, nil
	}
	return release, compareVersions(release.Version, current) > 0, nil
}
func normalize(v string) string { return strings.TrimPrefix(strings.TrimSpace(v), "v") }

func compareVersions(left, right string) int {
	l := versionParts(left)
	r := versionParts(right)
	for idx := range l {
		if l[idx] < r[idx] {
			return -1
		}
		if l[idx] > r[idx] {
			return 1
		}
	}
	return 0
}

func versionParts(value string) [3]int {
	var result [3]int
	for idx, part := range strings.Split(normalize(value), ".") {
		if idx >= len(result) {
			break
		}
		result[idx], _ = strconv.Atoi(part)
	}
	return result
}

// Apply downloads the platform archive and checksum from an ivoai GitHub
// release, validates and probes the candidate, then atomically replaces the
// running executable. The previous binary is retained as rollbackPath.
func (c Checker) Apply(ctx context.Context, release Release, executable, rollbackPath string) error {
	candidate, err := c.Prepare(ctx, release, executable)
	if err != nil {
		return err
	}
	defer candidate.Close()
	return c.Promote(candidate, executable, rollbackPath)
}

// Prepare downloads, verifies, extracts, and probes a release without changing
// the installed executable. This is the compatibility-preflight boundary used
// by the transactional updater.
func (c Checker) Prepare(ctx context.Context, release Release, executable string) (*PreparedCandidate, error) {
	version := strings.TrimSpace(release.Version)
	if !validVersion(version) {
		return nil, fmt.Errorf("invalid release version %q", version)
	}
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return nil, fmt.Errorf("automatic update unsupported on %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if c.Client == nil {
		c.Client = &http.Client{Timeout: 5 * time.Minute}
	}
	base := strings.TrimRight(c.ReleaseBase, "/")
	if base == "" {
		base = "https://github.com/ivo-lopes/ivoai/releases/download/" + version
	}
	asset := "ivoai_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	checksums, err := c.download(ctx, base+"/checksums.txt", 1<<20)
	if err != nil {
		return nil, fmt.Errorf("download update checksums: %w", err)
	}
	expected := checksumFor(checksums, asset)
	if expected == "" {
		return nil, errors.New("release checksum does not list platform archive")
	}
	archive, err := c.download(ctx, base+"/"+asset, 256<<20)
	if err != nil {
		return nil, fmt.Errorf("download update: %w", err)
	}
	sum := sha256.Sum256(archive)
	if fmt.Sprintf("%x", sum[:]) != strings.ToLower(expected) {
		return nil, errors.New("update archive checksum mismatch")
	}
	candidateBytes, err := extractCandidate(archive)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(executable)
	info, err := os.Lstat(executable)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("refusing to update non-regular executable")
	}
	temp, err := os.CreateTemp(dir, ".ivoai-update-*")
	if err != nil {
		return nil, fmt.Errorf("create update beside executable: %w", err)
	}
	candidate := temp.Name()
	keepCandidate := false
	defer func() {
		if !keepCandidate {
			_ = os.Remove(candidate)
		}
	}()
	if err = temp.Chmod(0o700); err != nil {
		temp.Close()
		return nil, err
	}
	if _, err = temp.Write(candidateBytes); err != nil {
		temp.Close()
		return nil, err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return nil, err
	}
	if err = temp.Close(); err != nil {
		return nil, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(probeCtx, candidate, "version").CombinedOutput()
	if err != nil || normalize(string(output)) != normalize(version) {
		return nil, fmt.Errorf("downloaded binary probe failed for %s", version)
	}
	keepCandidate = true
	return &PreparedCandidate{Path: candidate, Version: version}, nil
}

// Promote atomically installs a prepared candidate and retains the previous
// executable for compatibility with the v0.5.0 rollback contract.
func (c Checker) Promote(prepared *PreparedCandidate, executable, rollbackPath string) error {
	if prepared == nil || prepared.Path == "" || prepared.Version == "" {
		return errors.New("invalid prepared update candidate")
	}
	info, err := os.Lstat(prepared.Path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("prepared update candidate is not a regular file")
	}
	currentInfo, err := os.Lstat(executable)
	if err != nil || currentInfo.Mode()&os.ModeSymlink != 0 || !currentInfo.Mode().IsRegular() {
		return errors.New("installed executable is not a regular file")
	}
	current, err := platform.ReadRegularFile(executable, 256<<20)
	if err != nil {
		return err
	}
	if err := platform.AtomicWriteFile(current, rollbackPath, 0o700); err != nil {
		return fmt.Errorf("save rollback binary: %w", err)
	}
	if err := os.Chmod(prepared.Path, 0o755); err != nil {
		return err
	}
	if err := os.Rename(prepared.Path, executable); err != nil {
		return fmt.Errorf("replace ivoai binary: %w", err)
	}
	prepared.Path = ""
	return platform.SyncDir(filepath.Dir(executable))
}

// Rollback restores the last binary retained by Apply. The currently running
// binary is retained beside it so an operator can recover from an accidental
// rollback without downloading another release.
func (c Checker) Rollback(ctx context.Context, executable, rollbackPath string) error {
	currentInfo, err := os.Lstat(executable)
	if err != nil {
		return fmt.Errorf("inspect current executable: %w", err)
	}
	rollbackInfo, err := os.Lstat(rollbackPath)
	if err != nil {
		return fmt.Errorf("inspect rollback binary: %w", err)
	}
	if currentInfo.Mode()&os.ModeSymlink != 0 || !currentInfo.Mode().IsRegular() {
		return errors.New("refusing to replace non-regular executable")
	}
	if rollbackInfo.Mode()&os.ModeSymlink != 0 || !rollbackInfo.Mode().IsRegular() {
		return errors.New("refusing non-regular rollback binary")
	}
	if rollbackInfo.Size() <= 0 || rollbackInfo.Size() > 256<<20 {
		return errors.New("invalid rollback binary size")
	}
	rollback, err := platform.ReadRegularFile(rollbackPath, 256<<20)
	if err != nil {
		return err
	}
	dir := filepath.Dir(executable)
	temp, err := os.CreateTemp(dir, ".ivoai-rollback-*")
	if err != nil {
		return fmt.Errorf("create rollback candidate: %w", err)
	}
	candidate := temp.Name()
	defer os.Remove(candidate)
	if err := temp.Chmod(0o700); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(rollback); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(probeCtx, candidate, "version").CombinedOutput(); err != nil || !validVersion(strings.TrimSpace(string(output))) {
		return errors.New("rollback binary probe failed")
	}
	current, err := platform.ReadRegularFile(executable, 256<<20)
	if err != nil {
		return err
	}
	newerPath := rollbackPath + ".newer"
	if err := platform.AtomicWriteFile(current, newerPath, 0o700); err != nil {
		return fmt.Errorf("retain replaced binary: %w", err)
	}
	if err := os.Chmod(candidate, 0o755); err != nil {
		return err
	}
	if err := os.Rename(candidate, executable); err != nil {
		return fmt.Errorf("restore rollback binary: %w", err)
	}
	return platform.SyncDir(filepath.Dir(executable))
}

func (c Checker) download(ctx context.Context, endpoint string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	reader := io.LimitReader(resp.Body, limit+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("download exceeds size limit")
	}
	return data, nil
}
func validVersion(v string) bool {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}
func checksumFor(data []byte, asset string) string {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset && len(fields[0]) == 64 {
			return fields[0]
		}
	}
	return ""
}
func extractCandidate(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var candidate []byte
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Clean(header.Name) != "ivoai" || header.Typeflag != tar.TypeReg {
			continue
		}
		if header.Size <= 0 || header.Size > 256<<20 {
			return nil, errors.New("invalid ivoai binary size")
		}
		if candidate != nil {
			return nil, errors.New("archive contains duplicate ivoai binaries")
		}
		candidate, err = io.ReadAll(io.LimitReader(tr, header.Size))
		if err != nil {
			return nil, err
		}
	}
	if candidate == nil {
		return nil, errors.New("archive does not contain ivoai binary")
	}
	return candidate, nil
}
