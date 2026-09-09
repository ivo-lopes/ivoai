package connections

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/serverpool"
	"golang.org/x/sys/unix"
)

// EnrollmentUpsert preserves the legacy connect-server contract. Public Add
// uses Create; replacement is a distinct, explicit operation in CLI and TUI.
type EnrollmentMode uint8

const (
	EnrollmentUpsert EnrollmentMode = iota
	EnrollmentCreate
	EnrollmentReplace
)

// ProfileMetadata cannot change the stable identity, connection, or credential.
type ProfileMetadata struct {
	Purpose         string
	RedundancyGroup string
	Priority        int
}

func removeProfileBindings(cfg *config.Config, profile config.ServerProfile) {
	if profile.Alias != "default" {
		return
	}
	for name, endpoint := range map[string]string{"ivoai-context": profile.ContextMCPURL, "ivoai-memory": profile.MemoryMCPURL} {
		binding, exists := cfg.MCP.Servers[name]
		kind := "context"
		if name == "ivoai-memory" {
			kind = "memory"
		}
		if exists && endpoint != "" && binding.URL == endpoint && binding.Kind == kind {
			delete(cfg.MCP.Servers, name)
		}
	}
}

func (s ServerConnector) SetEnabled(alias string, enabled bool) error {
	return s.editProfile(alias, func(profile *config.ServerProfile) error {
		profile.Enabled = enabled
		return nil
	})
}

func (s ServerConnector) EditMetadata(alias string, metadata ProfileMetadata) error {
	if err := serverpool.ValidateLabel("server purpose", metadata.Purpose); err != nil {
		return err
	}
	if metadata.RedundancyGroup != "" {
		if err := serverpool.ValidateLabel("redundancy group", metadata.RedundancyGroup); err != nil {
			return err
		}
	}
	return s.editProfile(alias, func(profile *config.ServerProfile) error {
		profile.Purpose, profile.RedundancyGroup, profile.Priority = metadata.Purpose, metadata.RedundancyGroup, metadata.Priority
		return nil
	})
}

func (s ServerConnector) editProfile(alias string, edit func(*config.ServerProfile) error) error {
	if err := serverpool.ValidateAlias(alias); err != nil {
		return err
	}
	unlock, err := s.lockProfiles()
	if err != nil {
		return err
	}
	defer unlock()
	cfg, err := s.Store.Load()
	if err != nil {
		return err
	}
	if _, err := serverpool.New(cfg.Connections.Servers); err != nil {
		return err
	}
	profile, exists := cfg.Connections.Servers[alias]
	if !exists {
		return fmt.Errorf("server profile %q was not found", alias)
	}
	if err := edit(&profile); err != nil {
		return err
	}
	cfg.Connections.Servers[alias] = profile
	return s.saveConfig(cfg)
}

// Serialize profile mutations across CLI/TUI processes, including the enrollment
// transaction. Fail busy before consuming a one-time code; never lose another
// profile by committing a stale whole-config snapshot after network I/O.
func (s ServerConnector) lockProfiles() (func(), error) {
	if err := s.Store.Ensure(); err != nil {
		return nil, err
	}
	path := filepath.Join(filepath.Dir(s.Store.Paths.Config), ".server-connections.lock")
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, errors.New("cannot open server connections lock")
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		file.Close()
		return nil, errors.New("unsafe server connections lock")
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("server connections are being changed by another process; retry when it finishes")
	}
	return func() { _ = unix.Flock(fd, unix.LOCK_UN); _ = file.Close() }, nil
}
