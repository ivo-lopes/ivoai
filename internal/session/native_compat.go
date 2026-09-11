package session

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/quota"
)

const nativeDirectory = ".native-opencode"
const frontendDirectory = ".orchestrated-frontends"

type diskSession struct {
	Session
	NativeQuota *quota.ProviderQuota `json:"native_opencode_quota,omitempty"`
}

func nativeSession(value Session) bool {
	for _, executor := range []string{value.InitialPlanner, value.CurrentPrimary, value.RequestedExecutor, value.EffectiveExecutor} {
		if executor == "opencode" {
			return true
		}
	}
	for _, mapping := range value.ExecutorSessions {
		if mapping.Executor == "opencode" {
			return true
		}
	}
	for _, worker := range value.Workers {
		if worker.Executor == "opencode" || worker.RequestedExecutor == "opencode" {
			return true
		}
	}
	return false
}

func (s Store) nativeDir() string           { return filepath.Join(s.Root, nativeDirectory) }
func (s Store) nativePath(id string) string { return filepath.Join(s.nativeDir(), id+".json") }
func (s Store) validateNativeDir() error {
	return s.validateSessionNamespace(s.nativeDir())
}

func (s Store) frontendDir() string { return filepath.Join(s.Root, frontendDirectory) }
func (s Store) validateSessionNamespace(directory string) error {
	if directory != s.nativeDir() && directory != s.frontendDir() {
		return errors.New("invalid session namespace")
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return errors.New("unsafe native session directory")
	}
	return nil
}

// ReconcileNativeMetadata upgrades only the pre-release native runtime format.
// Records are preserved, not downgraded or rewritten as another executor.
// Older binaries ignore the private namespace; reapply reads it automatically.
func (s Store) ReconcileNativeMetadata() error {
	return s.withLock(func() error {
		entries, err := os.ReadDir(s.Root)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			id := strings.TrimSuffix(entry.Name(), ".json")
			if ValidateID(id) != nil {
				continue
			}
			value, err := s.read(filepath.Join(s.Root, entry.Name()))
			if err != nil {
				return err
			}
			body, readErr := platform.ReadRegularFile(filepath.Join(s.Root, entry.Name()), maxStateBytes)
			if readErr != nil {
				return readErr
			}
			var legacy struct {
				Quota map[quota.Provider]json.RawMessage `json:"quota"`
			}
			if err := json.Unmarshal(body, &legacy); err != nil {
				return err
			}
			_, hasLegacyNativeQuota := legacy.Quota[quota.ProviderOpenCode]
			if nativeSession(value) || hasLegacyNativeQuota {
				if err := s.write(value); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s Store) path(id string) string {
	// v0.9.9 does not enumerate this namespace and cannot rewrite new frontend
	// metadata during rollback. Reapply uses the same records, not a new store.
	frontendPath := filepath.Join(s.frontendDir(), id+".json")
	if _, err := os.Lstat(frontendPath); !errors.Is(err, fs.ErrNotExist) {
		return frontendPath
	}
	path := s.nativePath(id)
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		return path
	}
	return filepath.Join(s.Root, id+".json")
}
