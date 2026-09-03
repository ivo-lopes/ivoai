package servercmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/server"
)

type serverPreflight struct {
	OSSupported           bool
	ArchitectureSupported bool
	RunningInContainer    bool
	LXCDetected           bool
	DockerCLIPresent      bool
	DockerEngineVersion   string
	DockerDaemonReachable bool
	DockerComposeV2       bool
	DockerComposeVersion  string
	SystemdAvailable      bool
	PrivilegesOK          bool
	ServerState           string
}

func inspectServerPreflight(ctx context.Context, layout server.Layout) serverPreflight {
	release, _ := readServerOSRelease("/etc/os-release")
	value := serverPreflight{
		OSSupported:           supportedServerOS(release.ID, release.Version),
		ArchitectureSupported: supportedServerArchitecture(runtime.GOARCH),
		LXCDetected:           lxcDetected(),
		SystemdAvailable:      directoryExists("/run/systemd/system"),
		PrivilegesOK:          os.Geteuid() == 0,
		ServerState:           serverSetupState(layout),
	}
	value.RunningInContainer = value.LXCDetected || containerDetected()
	docker, err := exec.LookPath("docker")
	if err != nil {
		return value
	}
	value.DockerCLIPresent = true
	if version, err := dockerEngineVersion(ctx, docker); err == nil {
		value.DockerDaemonReachable = true
		value.DockerEngineVersion = version
	}
	if version, err := dockerComposeVersion(ctx, docker); err == nil {
		value.DockerComposeVersion = version
		value.DockerComposeV2, _ = runtimeVersionAtLeast(version, dockerComposeMinimumVersion)
	}
	return value
}

func (p serverPreflight) summary() string {
	return fmt.Sprintf("OS_SUPPORTED=%t\nARCH_SUPPORTED=%t\nRUNNING_IN_CONTAINER=%t\nLXC_DETECTED=%t\nDOCKER_CLI_PRESENT=%t\nDOCKER_ENGINE_VERSION=%s\nDOCKER_DAEMON_REACHABLE=%t\nDOCKER_COMPOSE_V2_PRESENT=%t\nSYSTEMD_AVAILABLE=%t\nPRIVILEGES_OK=%t\nSERVER_STATE=%s",
		p.OSSupported, p.ArchitectureSupported, p.RunningInContainer, p.LXCDetected, p.DockerCLIPresent, emptyAs(p.DockerEngineVersion, "unavailable"), p.DockerDaemonReachable, p.DockerComposeV2, p.SystemdAvailable, p.PrivilegesOK, p.ServerState)
}

func (p serverPreflight) rootCause() string {
	if !p.OSSupported {
		return "unsupported operating system"
	}
	if !p.ArchitectureSupported {
		return "unsupported architecture"
	}
	if !p.SystemdAvailable {
		return "systemd is unavailable"
	}
	if !p.DockerCLIPresent {
		return "Docker Engine is not installed"
	}
	if !p.DockerDaemonReachable {
		if p.LXCDetected {
			return "Docker daemon is unreachable inside LXC; enable host nesting/keyctl and compatible cgroup/device permissions"
		}
		return "Docker daemon is unreachable"
	}
	if p.DockerEngineVersion != "" {
		compatible, parsed := runtimeVersionAtLeast(p.DockerEngineVersion, dockerEngineMinimumVersion)
		if !parsed || !compatible {
			return fmt.Sprintf("Docker Engine %s is older than required %s", p.DockerEngineVersion, dockerEngineMinimumVersion)
		}
	}
	if !p.DockerComposeV2 {
		return fmt.Sprintf("Docker Compose v2 %s or newer is unavailable", dockerComposeMinimumVersion)
	}
	return "server setup was interrupted before managed state was committed"
}

func serverSetupState(layout server.Layout) string {
	for _, path := range []string{
		filepath.Join(layout.ConfigDir, "server.toml"), filepath.Join(layout.ConfigDir, "compose.yaml"), server.DocsConfigPath(layout),
		filepath.Join(layout.SecretsDir, "qdrant.env"), filepath.Join(layout.SecretsDir, "embeddings.env"), filepath.Join(layout.SecretsDir, "memory.env"),
		filepath.Join(layout.SystemdDir, "ivoai-dependencies.service"), filepath.Join(layout.SystemdDir, "ivoai-context.service"),
		filepath.Join(layout.SystemdDir, "ivoai-gateway.service"), filepath.Join(layout.SystemdDir, "ivoai-docs.service"),
	} {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "incomplete"
		}
	}
	return "configured"
}

func requireServerSetup(ctx context.Context, layout server.Layout) error {
	if serverSetupState(layout) == "configured" {
		return nil
	}
	preflight := inspectServerPreflight(ctx, layout)
	return fmt.Errorf("SERVER_SETUP=INCOMPLETE\nROOT_CAUSE=%s", preflight.rootCause())
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func containerDetected() bool {
	if data, err := os.ReadFile("/run/systemd/container"); err == nil && strings.TrimSpace(string(data)) != "" {
		return true
	}
	data, _ := os.ReadFile("/proc/1/cgroup")
	return strings.Contains(string(data), "/docker/") || strings.Contains(string(data), "/containerd/")
}

func emptyAs(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
