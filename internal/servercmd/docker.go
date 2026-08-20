package servercmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	dockerComposeVersion     = "5.5.0"
	dockerComposeInstallPath = "/usr/local/lib/docker/cli-plugins/docker-compose"
	maxComposeBinarySize     = 96 << 20
	composeDownloadTimeout   = 30 * time.Minute
	composeProgressInterval  = 10 * time.Second
)

var dockerAPTInstallArgs = []string{"install", "-y", "ca-certificates", "docker.io"}

type composeAsset struct {
	URL    string
	SHA256 string
}

func dockerComposeAsset(architecture string) (composeAsset, error) {
	base := "https://github.com/docker/compose/releases/download/v" + dockerComposeVersion + "/"
	switch architecture {
	case "amd64":
		return composeAsset{
			URL:    base + "docker-compose-linux-x86_64",
			SHA256: "c57ab918abd5b05ca7e7d0f275875dd1330a695074f309dc9eab1b49efafcd4b",
		}, nil
	case "arm64":
		return composeAsset{
			URL:    base + "docker-compose-linux-aarch64",
			SHA256: "ff42489f5a9b879d5d117c5ffea6defc27390b3286da8ad52cbc9c6ab5df590e",
		}, nil
	default:
		return composeAsset{}, fmt.Errorf("Docker Compose does not have a reviewed linux/%s asset", architecture)
	}
}

func ensureDocker(ctx context.Context, out, errOut io.Writer) error {
	if dockerComposeAvailable(ctx) {
		return nil
	}
	if _, err := exec.LookPath("docker"); err != nil {
		apt, lookupErr := exec.LookPath("apt-get")
		if lookupErr != nil {
			return errors.New("Docker is required and automatic installation supports apt-based systems")
		}
		if err := runAPT(ctx, apt, out, errOut, "update"); err != nil {
			return fmt.Errorf("update apt package metadata: %w", err)
		}
		if err := runAPT(ctx, apt, out, errOut, dockerAPTInstallArgs...); err != nil {
			return fmt.Errorf("install Docker Engine from the operating-system repository: %w", err)
		}
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("Docker installation completed without a docker CLI")
	}
	if dockerComposeAvailable(ctx) {
		return nil
	}

	asset, err := dockerComposeAsset(runtime.GOARCH)
	if err != nil {
		return err
	}
	if err := ensureRootPluginDirectory(); err != nil {
		return err
	}
	client := &http.Client{
		Timeout: composeDownloadTimeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many Docker Compose download redirects")
			}
			if request.URL.Scheme != "https" || request.URL.User != nil {
				return errors.New("unsafe Docker Compose download redirect")
			}
			return nil
		},
	}
	fmt.Fprintf(out, "Docker Compose plugin %s is absent; installing the verified official plugin for linux/%s\n", dockerComposeVersion, runtime.GOARCH)
	if err := installVerifiedComposePlugin(ctx, client, asset, dockerComposeInstallPath, out); err != nil {
		return fmt.Errorf("install Docker Compose plugin %s: %w", dockerComposeVersion, err)
	}
	if !dockerComposeAvailable(ctx) {
		return errors.New("verified Docker Compose plugin was installed, but 'docker compose version' still fails; remove any incompatible per-user Docker CLI plugin and rerun setup")
	}
	return nil
}

func runAPT(ctx context.Context, apt string, out, errOut io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, apt, args...)
	cmd.Stdout, cmd.Stderr = out, errOut
	cmd.Env = []string{
		"DEBIAN_FRONTEND=noninteractive",
		"HOME=/root",
		"LANG=C.UTF-8",
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	}
	return cmd.Run()
}

func dockerComposeAvailable(ctx context.Context) bool {
	docker, err := exec.LookPath("docker")
	if err != nil {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, docker, "compose", "version")
	cmd.Env = []string{"HOME=/root", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	return cmd.Run() == nil
}

func ensureRootPluginDirectory() error {
	for _, path := range []string{
		"/usr/local",
		"/usr/local/lib",
		"/usr/local/lib/docker",
		"/usr/local/lib/docker/cli-plugins",
	} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(path, 0o755); err != nil {
				return fmt.Errorf("create Docker CLI plugin directory %s: %w", path, err)
			}
			info, err = os.Lstat(path)
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("unsafe Docker CLI plugin directory %s", path)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || info.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("Docker CLI plugin directory %s must be root-owned and not group/world writable", path)
		}
	}
	return nil
}

func installVerifiedComposePlugin(ctx context.Context, client *http.Client, asset composeAsset, destination string, progress io.Writer) error {
	parsed, err := url.Parse(asset.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return errors.New("invalid Docker Compose asset URL")
	}
	expected, err := hex.DecodeString(asset.SHA256)
	if err != nil || len(expected) != sha256.Size {
		return errors.New("invalid Docker Compose asset checksum")
	}
	if info, err := os.Lstat(destination); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("refusing unsafe Docker Compose plugin path %s", destination)
		}
		matches, hashErr := regularFileMatchesSHA256(destination, asset.SHA256)
		if hashErr != nil {
			return hashErr
		}
		if matches {
			return nil
		}
		return fmt.Errorf("refusing to replace pre-existing Docker Compose plugin %s; move it aside and rerun setup", destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxComposeBinarySize {
		return errors.New("Docker Compose asset exceeds the size limit")
	}
	if progress == nil {
		progress = io.Discard
	}

	directory := filepath.Dir(destination)
	temporary, err := os.CreateTemp(directory, ".ivoai-docker-compose-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o755); err != nil {
		temporary.Close()
		return err
	}
	hasher := sha256.New()
	counter := &atomicByteCounter{}
	stopProgress := startComposeDownloadProgress(progress, counter, response.ContentLength)
	written, copyErr := io.Copy(io.MultiWriter(temporary, hasher, counter), io.LimitReader(response.Body, maxComposeBinarySize+1))
	stopProgress()
	if copyErr != nil {
		temporary.Close()
		return copyErr
	}
	if written > maxComposeBinarySize {
		temporary.Close()
		return errors.New("Docker Compose asset exceeds the size limit")
	}
	if !strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), asset.SHA256) {
		temporary.Close()
		return errors.New("Docker Compose asset checksum mismatch")
	}
	fmt.Fprintf(progress, "Docker Compose download complete: %s\n", formatByteCount(written))
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return err
	}
	return os.Chmod(destination, 0o755)
}

type atomicByteCounter struct {
	written atomic.Int64
}

func (counter *atomicByteCounter) Write(data []byte) (int, error) {
	counter.written.Add(int64(len(data)))
	return len(data), nil
}

func startComposeDownloadProgress(out io.Writer, counter *atomicByteCounter, total int64) func() {
	done := make(chan struct{})
	stopped := make(chan struct{})
	fmt.Fprintf(out, "Downloading Docker Compose %s (%s); progress updates every %s\n", dockerComposeVersion, formatDownloadTotal(total), composeProgressInterval)
	ticker := time.NewTicker(composeProgressInterval)
	go func() {
		defer close(stopped)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				written := counter.written.Load()
				if total > 0 {
					percent := float64(written) * 100 / float64(total)
					fmt.Fprintf(out, "Docker Compose download: %s of %s (%.0f%%)\n", formatByteCount(written), formatByteCount(total), percent)
				} else {
					fmt.Fprintf(out, "Docker Compose download: %s received\n", formatByteCount(written))
				}
			case <-done:
				return
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}

func formatDownloadTotal(total int64) string {
	if total <= 0 {
		return "size not reported by server"
	}
	return formatByteCount(total)
}

func formatByteCount(bytes int64) string {
	const mebibyte = 1024 * 1024
	return fmt.Sprintf("%.1f MiB", float64(bytes)/mebibyte)
}

func regularFileMatchesSHA256(path, expected string) (bool, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return false, errors.New("open Docker Compose plugin")
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, io.LimitReader(file, maxComposeBinarySize+1)); err != nil {
		return false, err
	}
	return strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), expected), nil
}
