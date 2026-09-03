package servercmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

const (
	dockerDebianKeyURL      = "https://download.docker.com/linux/debian/gpg"
	dockerDebianKeyPath     = "/etc/apt/keyrings/docker.asc"
	dockerDebianSourcesPath = "/etc/apt/sources.list.d/docker.sources"
	dockerKeyFingerprint    = "9DC858229FC7DD38854AE2D88D81803C0EBFCD88"
	maxDockerKeySize        = 128 << 10
	dockerInstallTimeout    = 30 * time.Minute
)

type serverOSRelease struct {
	ID       string
	Version  string
	Codename string
}

func readServerOSRelease(path string) (serverOSRelease, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return serverOSRelease{}, err
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found {
			values[key] = strings.Trim(value, "\"")
		}
	}
	return serverOSRelease{ID: values["ID"], Version: values["VERSION_ID"], Codename: values["VERSION_CODENAME"]}, nil
}

func installDockerEngineDebian(ctx context.Context, out io.Writer) error {
	release, err := readServerOSRelease("/etc/os-release")
	if err != nil || release.ID != "debian" || release.Codename == "" {
		return errors.New("automatic Docker Engine provisioning is limited to supported Debian releases")
	}
	if os.Geteuid() != 0 {
		return errors.New("Docker Engine installation requires root")
	}
	for _, command := range []string{"apt-get", "apt-cache", "dpkg", "systemctl"} {
		if _, err := exec.LookPath(command); err != nil {
			return fmt.Errorf("Docker Engine provisioning requires %s", command)
		}
	}
	fmt.Fprintln(out, "Docker Engine is absent; provisioning from Docker's official signed Debian repository")
	if err := runAPT(ctx, "update"); err != nil {
		return err
	}
	if err := runAPT(ctx, "install", "-y", "--no-install-recommends", "ca-certificates", "curl", "gnupg"); err != nil {
		return err
	}
	if _, err := exec.LookPath("gpg"); err != nil {
		return errors.New("Docker Engine provisioning installed gnupg but gpg is unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(dockerDebianKeyPath), 0o755); err != nil {
		return err
	}
	key, err := downloadDockerKey(ctx)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(dockerDebianKeyPath), ".ivoai-docker-key-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(key); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	fingerprint, err := commandOutput(ctx, "gpg", "--batch", "--show-keys", "--with-colons", temporaryPath)
	if err != nil || !containsFingerprint(fingerprint, dockerKeyFingerprint) {
		return errors.New("Docker repository signing key fingerprint does not match the reviewed official key")
	}
	if err := installRootFile(temporaryPath, dockerDebianKeyPath, 0o644); err != nil {
		return err
	}
	architecture, err := commandOutput(ctx, "dpkg", "--print-architecture")
	if err != nil {
		return err
	}
	if architecture != "amd64" && architecture != "arm64" {
		return fmt.Errorf("unsupported Docker package architecture %s", architecture)
	}
	sources := fmt.Sprintf("Types: deb\nURIs: https://download.docker.com/linux/debian\nSuites: %s\nComponents: stable\nArchitectures: %s\nSigned-By: %s\n", release.Codename, architecture, dockerDebianKeyPath)
	if err := writeRootManagedFile(dockerDebianSourcesPath, []byte(sources), 0o644); err != nil {
		return err
	}
	if err := runAPT(ctx, "update"); err != nil {
		return err
	}
	return installOfficialDockerPackages(ctx)
}

func installOfficialDockerPackages(ctx context.Context) error {
	packages := []string{"docker-ce", "docker-ce-cli", "containerd.io", "docker-buildx-plugin", "docker-compose-plugin"}
	arguments := []string{"install", "-y", "--no-install-recommends"}
	for _, packageName := range packages {
		candidate, err := aptCandidate(ctx, packageName)
		if err != nil {
			return err
		}
		arguments = append(arguments, packageName+"="+candidate)
	}
	if err := runAPT(ctx, arguments...); err != nil {
		return err
	}
	return runInstallCommand(ctx, "systemctl", "enable", "--now", "docker.service")
}

func aptCandidate(ctx context.Context, packageName string) (string, error) {
	output, err := commandOutput(ctx, "apt-cache", "policy", packageName)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(output, "\n") {
		if value, found := strings.CutPrefix(strings.TrimSpace(line), "Candidate:"); found {
			value = strings.TrimSpace(value)
			if value == "" || value == "(none)" || strings.ContainsAny(value, " \t\r\n") {
				break
			}
			return value, nil
		}
	}
	return "", fmt.Errorf("Docker official repository has no candidate for %s", packageName)
}

func downloadDockerKey(ctx context.Context) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, dockerDebianKeyURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 || request.URL.Scheme != "https" || request.URL.Host != "download.docker.com" {
			return errors.New("unsafe Docker key redirect")
		}
		return nil
	}}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > maxDockerKeySize {
		return nil, fmt.Errorf("Docker signing key download returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxDockerKeySize+1))
	if err != nil || len(data) == 0 || len(data) > maxDockerKeySize {
		return nil, errors.New("Docker signing key download is empty or oversized")
	}
	return data, nil
}

func containsFingerprint(output, expected string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, ":")
		if len(fields) > 9 && fields[0] == "fpr" && strings.EqualFold(fields[9], expected) {
			return true
		}
	}
	return false
}

func commandOutput(ctx context.Context, name string, args ...string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(commandCtx, name, args...)
	command.Env = []string{"DEBIAN_FRONTEND=noninteractive", "HOME=/root", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(platform.Redact(string(output))))
	}
	return strings.TrimSpace(string(output)), nil
}

func runInstallCommand(ctx context.Context, name string, args ...string) error {
	commandCtx, cancel := context.WithTimeout(ctx, dockerInstallTimeout)
	defer cancel()
	command := exec.Command(name, args...)
	command.Env = []string{"DEBIAN_FRONTEND=noninteractive", "HOME=/root", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-commandCtx.Done():
		// apt and package hooks start helpers. Killing only the direct process
		// can leave a downloader holding dpkg/apt locks and break an idempotent
		// rerun, so the managed process group is the cancellation boundary.
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		<-done
		err = commandCtx.Err()
	}
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(platform.Redact(output.String())))
	}
	return nil
}

func runAPT(ctx context.Context, args ...string) error {
	// Docker's official Debian repository and the supported Debian mirrors are
	// reachable over IPv4. LXC guests frequently inherit an IPv6 route without
	// usable upstream connectivity; forcing IPv4 avoids a long apt stall while
	// keeping the package source and TLS validation unchanged.
	arguments := append([]string{"-o", "Acquire::ForceIPv4=true", "-o", "Acquire::Retries=3"}, args...)
	return runInstallCommand(ctx, "apt-get", arguments...)
}

func officialDockerInstalled(ctx context.Context) bool {
	output, err := commandOutput(ctx, "dpkg-query", "-W", "-f=${Status}", "docker-ce")
	return err == nil && output == "install ok installed"
}

func writeRootManagedFile(path string, content []byte, mode os.FileMode) error {
	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, content) {
			return os.Chmod(path, mode)
		}
		return fmt.Errorf("refusing to replace existing package repository configuration %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return platform.AtomicWriteFile(content, path, mode)
}

func installRootFile(source, destination string, mode os.FileMode) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if existing, err := os.ReadFile(destination); err == nil && bytes.Equal(existing, data) {
		return os.Chmod(destination, mode)
	} else if err == nil {
		return fmt.Errorf("refusing to replace existing repository key %s", destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return platform.AtomicWriteFile(data, destination, mode)
}

func lxcDetected() bool {
	systemdContainer, _ := os.ReadFile("/run/systemd/container")
	cgroup, _ := os.ReadFile("/proc/1/cgroup")
	return isLXC(systemdContainer, cgroup)
}

func isLXC(systemdContainer, cgroup []byte) bool {
	return strings.Contains(strings.ToLower(string(systemdContainer)), "lxc") || strings.Contains(strings.ToLower(string(cgroup)), "lxc")
}

func dockerLXCError(err error) error {
	if !lxcDetected() {
		return err
	}
	return fmt.Errorf("%w; LXC detected: Docker requires host-enabled nesting/keyctl and compatible cgroup/device permissions; change these on the Proxmox/LXC host, then rerun setup", err)
}
