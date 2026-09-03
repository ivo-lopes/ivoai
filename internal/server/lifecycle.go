package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var ManagedServices = []string{"ivoai-dependencies.service", "ivoai-context.service", "ivoai-gateway.service", "ivoai-docs.service"}

type ServiceState struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
	Detail string `json:"detail,omitempty"`
}

type Controller interface {
	DaemonReload(context.Context) error
	Enable(context.Context, []string) error
	Start(context.Context, []string) error
	Stop(context.Context, []string) error
	Restart(context.Context, []string) error
	Status(context.Context, []string) ([]ServiceState, error)
	Logs(context.Context, string, int) (string, error)
}

type SystemdController struct {
	Systemctl  string
	Journalctl string
	Timeout    time.Duration
}

func (c SystemdController) run(ctx context.Context, binary string, args ...string) ([]byte, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		// First start may need to pull pinned dependency images and the local
		// embedding model on a slow server connection. systemd itself retains
		// the tighter per-unit stop timeout.
		timeout = 15 * time.Minute
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(cmdCtx, binary, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w: %s", filepath.Base(binary), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func (c SystemdController) ctl() string {
	if c.Systemctl != "" {
		return c.Systemctl
	}
	return "systemctl"
}
func (c SystemdController) journal() string {
	if c.Journalctl != "" {
		return c.Journalctl
	}
	return "journalctl"
}
func (c SystemdController) DaemonReload(ctx context.Context) error {
	_, err := c.run(ctx, c.ctl(), "daemon-reload")
	return err
}
func (c SystemdController) Enable(ctx context.Context, names []string) error {
	_, err := c.run(ctx, c.ctl(), append([]string{"enable"}, names...)...)
	return err
}
func (c SystemdController) Start(ctx context.Context, names []string) error {
	_, err := c.run(ctx, c.ctl(), append([]string{"start"}, names...)...)
	return err
}
func (c SystemdController) Stop(ctx context.Context, names []string) error {
	_, err := c.run(ctx, c.ctl(), append([]string{"stop"}, names...)...)
	return err
}
func (c SystemdController) Restart(ctx context.Context, names []string) error {
	_, err := c.run(ctx, c.ctl(), append([]string{"restart"}, names...)...)
	return err
}
func (c SystemdController) Status(ctx context.Context, names []string) ([]ServiceState, error) {
	states := make([]ServiceState, 0, len(names))
	for _, name := range names {
		output, err := c.run(ctx, c.ctl(), "is-active", name)
		detail := strings.TrimSpace(string(output))
		states = append(states, ServiceState{Name: name, Active: err == nil && detail == "active", Detail: detail})
	}
	return states, nil
}
func (c SystemdController) Logs(ctx context.Context, name string, lines int) (string, error) {
	managed := false
	for _, candidate := range ManagedServices {
		managed = managed || candidate == name
	}
	if !managed {
		return "", errors.New("logs are limited to managed ivoai services")
	}
	if lines <= 0 || lines > 10000 {
		lines = 200
	}
	output, err := c.run(ctx, c.journal(), "--unit", name, "--no-pager", "--output", "short-iso", "--lines", fmt.Sprint(lines))
	return string(output), err
}

type Manager struct {
	Layout        Layout
	Controller    Controller
	Architecture  string
	ContainerUser string
}

func (m Manager) Setup(ctx context.Context) error {
	if err := m.Layout.Ensure(); err != nil {
		return err
	}
	arch := m.Architecture
	if arch == "" {
		arch = runtime.GOARCH
	}
	compose := ComposeYAML
	if m.ContainerUser != "" {
		if !validContainerUser(m.ContainerUser) {
			return errors.New("container user must be non-root numeric UID:GID")
		}
		compose = strings.ReplaceAll(compose, `user: "1000:1000"`, `user: "`+m.ContainerUser+`"`)
	}
	dependenciesUnit := DependenciesUnit
	if arch == "arm64" {
		dependenciesUnit = strings.ReplaceAll(dependenciesUnit, "-f /etc/ivoai/compose.yaml", "-f /etc/ivoai/compose.yaml -f /etc/ivoai/compose.arm64.yaml")
	}
	assets := map[string]struct {
		content string
		mode    os.FileMode
	}{
		filepath.Join(m.Layout.ConfigDir, "compose.yaml"):                {compose, 0o600},
		filepath.Join(m.Layout.ConfigDir, "server.toml"):                 {DefaultServerConfig, 0o600},
		filepath.Join(m.Layout.SystemdDir, "ivoai-gateway.service"):      {GatewayUnit, 0o644},
		filepath.Join(m.Layout.SystemdDir, "ivoai-context.service"):      {ContextUnit, 0o644},
		filepath.Join(m.Layout.SystemdDir, "ivoai-docs.service"):         {DocsUnit, 0o644},
		filepath.Join(m.Layout.SystemdDir, "ivoai-dependencies.service"): {dependenciesUnit, 0o644},
	}
	if err := EnsureDocsConfig(m.Layout); err != nil {
		return err
	}
	if arch == "arm64" {
		assets[filepath.Join(m.Layout.ConfigDir, "compose.arm64.yaml")] = struct {
			content string
			mode    os.FileMode
		}{ARM64ComposeOverride, 0o600}
	}
	for path, asset := range assets {
		if err := writeManagedFile(path, []byte(asset.content), asset.mode); err != nil {
			return err
		}
	}
	if m.Controller != nil {
		if err := m.Controller.DaemonReload(ctx); err != nil {
			return err
		}
		if err := m.Controller.Enable(ctx, ManagedServices); err != nil {
			return err
		}
	}
	return nil
}

func validContainerUser(value string) bool {
	uidText, gidText, found := strings.Cut(value, ":")
	if !found || uidText == "" || gidText == "" {
		return false
	}
	uid, uidErr := strconv.ParseUint(uidText, 10, 32)
	_, gidErr := strconv.ParseUint(gidText, 10, 32)
	return uidErr == nil && gidErr == nil && uid != 0
}

func writeManagedFile(path string, content []byte, mode os.FileMode) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to overwrite symlink %s", path)
		}
		existing, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Equal(existing, content) {
			return os.Chmod(path, mode)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".ivoai-managed-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
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
	return os.Rename(name, path)
}

func (m Manager) Start(ctx context.Context) error {
	if m.Controller == nil {
		return errors.New("service controller unavailable")
	}
	return m.Controller.Start(ctx, ManagedServices)
}
func (m Manager) Stop(ctx context.Context) error {
	if m.Controller == nil {
		return errors.New("service controller unavailable")
	}
	names := []string{"ivoai-docs.service", "ivoai-gateway.service", "ivoai-context.service", "ivoai-dependencies.service"}
	return m.Controller.Stop(ctx, names)
}
func (m Manager) Restart(ctx context.Context) error {
	if m.Controller == nil {
		return errors.New("service controller unavailable")
	}
	return m.Controller.Restart(ctx, ManagedServices)
}
func (m Manager) Status(ctx context.Context) ([]ServiceState, error) {
	if m.Controller == nil {
		return nil, errors.New("service controller unavailable")
	}
	return m.Controller.Status(ctx, ManagedServices)
}
func (m Manager) Logs(ctx context.Context, service string, lines int) (string, error) {
	if m.Controller == nil {
		return "", errors.New("service controller unavailable")
	}
	return m.Controller.Logs(ctx, service, lines)
}
