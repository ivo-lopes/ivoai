package quota

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"golang.org/x/sys/unix"
)

const maxSnapshotBytes = 256 << 10

type Store struct{ Root string }

// Keep the legacy provider map readable by v0.9.2. Native telemetry is a
// reconstructible extension which older writers may safely discard.
type diskSnapshot struct {
	Snapshot
	Native *ProviderQuota `json:"native_opencode,omitempty"`
}

func (s Store) path() string { return filepath.Join(s.Root, "snapshot.json") }

func (s Store) lockPath() string { return filepath.Join(s.Root, "snapshot.lock") }

func (s Store) Load() (Snapshot, error) {
	result := Snapshot{Providers: map[Provider]ProviderQuota{}}
	fd, err := unix.Open(s.path(), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	file := os.NewFile(uintptr(fd), s.path())
	if file == nil {
		_ = unix.Close(fd)
		return result, errors.New("open quota snapshot")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > maxSnapshotBytes {
		return result, errors.New("unsafe quota snapshot")
	}
	body, err := io.ReadAll(io.LimitReader(file, maxSnapshotBytes+1))
	if err != nil || len(body) > maxSnapshotBytes {
		return result, errors.New("invalid quota snapshot")
	}
	var disk diskSnapshot
	if err := json.Unmarshal(body, &disk); err != nil {
		return result, err
	}
	result = disk.Snapshot
	if result.Providers == nil {
		result.Providers = map[Provider]ProviderQuota{}
	}
	if disk.Native != nil {
		if disk.Native.Provider != ProviderOpenCode {
			return result, errors.New("invalid native quota extension")
		}
		if _, duplicate := result.Providers[ProviderOpenCode]; duplicate {
			return result, errors.New("duplicate native quota metadata")
		}
		result.Providers[ProviderOpenCode] = *disk.Native
	}
	return result, validateSnapshot(result)
}

func (s Store) Save(value Snapshot) error {
	if err := validateSnapshot(value); err != nil {
		return err
	}
	if err := platform.EnsurePrivateDir(s.Root); err != nil {
		return err
	}
	disk := diskSnapshot{Snapshot: value}
	disk.Providers = map[Provider]ProviderQuota{}
	for provider, current := range value.Providers {
		if provider == ProviderOpenCode {
			copy := current
			disk.Native = &copy
		} else {
			disk.Providers[provider] = current
		}
	}
	body, err := json.MarshalIndent(disk, "", "  ")
	if err != nil || len(body) > maxSnapshotBytes {
		return errors.New("encode quota snapshot")
	}
	return platform.AtomicWritePrivate(append(body, '\n'), s.path())
}

func (s Store) Put(value ProviderQuota) error {
	return s.mutate(func(snapshot *Snapshot) {
		snapshot.Providers[value.Provider] = value
	})
}

// Invalidate removes only one provider's reconstructible quota telemetry.
// Explicit authentication transitions use it so stale hard limits from a
// previous account can never gate the newly authenticated account.
func (s Store) Invalidate(provider Provider) error {
	if !Supported(provider) {
		return errors.New("invalid quota provider")
	}
	return s.mutate(func(snapshot *Snapshot) { delete(snapshot.Providers, provider) })
}

func (s Store) mutate(change func(*Snapshot)) error {
	if err := platform.EnsurePrivateDir(s.Root); err != nil {
		return err
	}
	fd, err := unix.Open(s.lockPath(), unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	lock := os.NewFile(uintptr(fd), s.lockPath())
	if lock == nil {
		_ = unix.Close(fd)
		return errors.New("open quota lock")
	}
	defer lock.Close()
	if err := unix.Fchmod(fd, 0o600); err != nil {
		return err
	}
	info, err := lock.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("unsafe quota lock")
	}
	if err := unix.Flock(fd, unix.LOCK_EX); err != nil {
		return err
	}
	defer unix.Flock(fd, unix.LOCK_UN)

	snapshot, err := s.Load()
	if err != nil {
		return err
	}
	change(&snapshot)
	snapshot.UpdatedAt = time.Now().UTC()
	return s.Save(snapshot)
}

func validateSnapshot(value Snapshot) error {
	for provider, current := range value.Providers {
		if !Supported(provider) || current.Provider != provider {
			return errors.New("invalid quota provider")
		}
		if len(current.Windows) > 32 {
			return errors.New("too many quota windows")
		}
		if unsafeMetadata(current.Source, 128) || unsafeMetadata(current.Reason, 1024) || unsafeMetadata(current.Model, 128) {
			return errors.New("unsafe quota metadata")
		}
		for _, window := range current.Windows {
			if window.DurationMinutes < 0 || window.DurationMinutes > 525600 {
				return errors.New("invalid quota window duration")
			}
			switch window.TelemetryState() {
			case TelemetryPending, TelemetryAvailable, TelemetryNotExposed, TelemetryStale, TelemetryExhausted:
			default:
				return errors.New("invalid quota telemetry state")
			}
			if window.Available && (window.RemainingPercent < 0 || window.RemainingPercent > 100 || window.UsedPercent < 0 || window.UsedPercent > 100) {
				return errors.New("invalid quota percentage")
			}
			if unsafeMetadata(window.Source, 128) || unsafeMetadata(window.Model, 128) {
				return errors.New("unsafe quota window metadata")
			}
		}
	}
	return nil
}

func unsafeMetadata(value string, limit int) bool {
	return len(value) > limit || strings.ContainsAny(value, "\x00\x1b\r\n") || platform.Redact(value) != value
}
