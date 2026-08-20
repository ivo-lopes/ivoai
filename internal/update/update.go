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
	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return Release{}, false, err
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
	version := strings.TrimSpace(release.Version)
	if !validVersion(version) {
		return fmt.Errorf("invalid release version %q", version)
	}
	if runtime.GOOS != "linux" || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		return fmt.Errorf("automatic update unsupported on %s/%s", runtime.GOOS, runtime.GOARCH)
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
		return fmt.Errorf("download update checksums: %w", err)
	}
	expected := checksumFor(checksums, asset)
	if expected == "" {
		return errors.New("release checksum does not list platform archive")
	}
	archive, err := c.download(ctx, base+"/"+asset, 256<<20)
	if err != nil {
		return fmt.Errorf("download update: %w", err)
	}
	sum := sha256.Sum256(archive)
	if fmt.Sprintf("%x", sum[:]) != strings.ToLower(expected) {
		return errors.New("update archive checksum mismatch")
	}
	candidateBytes, err := extractCandidate(archive)
	if err != nil {
		return err
	}
	dir := filepath.Dir(executable)
	info, err := os.Lstat(executable)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("refusing to update non-regular executable")
	}
	temp, err := os.CreateTemp(dir, ".ivoai-update-*")
	if err != nil {
		return fmt.Errorf("create update beside executable: %w", err)
	}
	candidate := temp.Name()
	defer os.Remove(candidate)
	if err = temp.Chmod(0o700); err != nil {
		temp.Close()
		return err
	}
	if _, err = temp.Write(candidateBytes); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	output, err := exec.CommandContext(probeCtx, candidate, "version").CombinedOutput()
	if err != nil || normalize(string(output)) != normalize(version) {
		return fmt.Errorf("downloaded binary probe failed for %s", version)
	}
	current, err := os.ReadFile(executable)
	if err != nil {
		return err
	}
	if err := platform.AtomicWritePrivate(current, rollbackPath); err != nil {
		return fmt.Errorf("save rollback binary: %w", err)
	}
	if err := os.Chmod(rollbackPath, 0o700); err != nil {
		return err
	}
	if err := os.Rename(candidate, executable); err != nil {
		return fmt.Errorf("replace ivoai binary: %w", err)
	}
	return os.Chmod(executable, 0o755)
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
	rollback, err := os.ReadFile(rollbackPath)
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
	current, err := os.ReadFile(executable)
	if err != nil {
		return err
	}
	newerPath := rollbackPath + ".newer"
	if err := platform.AtomicWritePrivate(current, newerPath); err != nil {
		return fmt.Errorf("retain replaced binary: %w", err)
	}
	if err := os.Chmod(newerPath, 0o700); err != nil {
		return err
	}
	if err := os.Rename(candidate, executable); err != nil {
		return fmt.Errorf("restore rollback binary: %w", err)
	}
	return os.Chmod(executable, 0o755)
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
