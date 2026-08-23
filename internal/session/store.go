package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"golang.org/x/sys/unix"
)

const maxStateBytes = 1 << 20

type Store struct{ Root string }

func NewID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "sess_" + hex.EncodeToString(raw[:]), nil
}

func ValidateID(id string) error {
	if len(id) != 37 || !strings.HasPrefix(id, "sess_") {
		return errors.New("invalid session ID")
	}
	_, err := hex.DecodeString(strings.TrimPrefix(id, "sess_"))
	if err != nil {
		return errors.New("invalid session ID")
	}
	return nil
}

func (s Store) Create(value Session) error {
	if err := ValidateID(value.SessionID); err != nil {
		return err
	}
	return s.withLock(func() error {
		if _, err := os.Lstat(s.path(value.SessionID)); err == nil {
			return errors.New("session already exists")
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return s.write(value)
	})
}

func (s Store) Get(id string) (Session, error) {
	if err := ValidateID(id); err != nil {
		return Session{}, err
	}
	return s.read(s.path(id))
}

func (s Store) Delete(id string) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	return s.withLock(func() error {
		err := os.Remove(s.path(id))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	})
}

func (s Store) Update(id string, mutate func(*Session) error) (Session, error) {
	var updated Session
	err := s.withLock(func() error {
		value, err := s.Get(id)
		if err != nil {
			return err
		}
		if err := mutate(&value); err != nil {
			return err
		}
		value.UpdatedAt = time.Now().UTC()
		if err := validate(value); err != nil {
			return err
		}
		if err := s.write(value); err != nil {
			return err
		}
		updated = value
		return nil
	})
	return updated, err
}

func (s Store) List() ([]Session, error) {
	if err := platform.EnsurePrivateDir(s.Root); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return nil, err
	}
	values := make([]Session, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if ValidateID(id) != nil {
			continue
		}
		value, readErr := s.read(filepath.Join(s.Root, entry.Name()))
		if readErr != nil {
			return nil, readErr
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].StartedAt.After(values[j].StartedAt) })
	return values, nil
}

func (s Store) Active() ([]Session, error) {
	values, err := s.List()
	if err != nil {
		return nil, err
	}
	active := values[:0]
	for _, value := range values {
		if value.Active() {
			active = append(active, value)
		}
	}
	return active, nil
}

func (s Store) RuntimeDir(id string) (string, error) {
	if err := ValidateID(id); err != nil {
		return "", err
	}
	if err := platform.EnsurePrivateDir(s.Root); err != nil {
		return "", err
	}
	runtimeRoot := filepath.Join(s.Root, "runtime")
	if err := platform.EnsurePrivateDir(runtimeRoot); err != nil {
		return "", err
	}
	path := filepath.Join(runtimeRoot, id)
	if err := platform.EnsurePrivateDir(path); err != nil {
		return "", err
	}
	return path, nil
}

func (s Store) CleanupRuntime(id string) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	path := filepath.Join(s.Root, "runtime", id)
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("refusing unsafe session runtime path")
	}
	return os.RemoveAll(path)
}

func (s Store) path(id string) string { return filepath.Join(s.Root, id+".json") }

func (s Store) write(value Session) error {
	if err := validate(value); err != nil {
		return err
	}
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if len(body) > maxStateBytes {
		return errors.New("session metadata exceeds size limit")
	}
	return platform.AtomicWritePrivate(append(body, '\n'), s.path(value.SessionID))
}

func (s Store) read(path string) (Session, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return Session{}, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		unix.Close(fd)
		return Session{}, errors.New("open session state")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Session{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > maxStateBytes {
		return Session{}, errors.New("unsafe session state file")
	}
	body, err := io.ReadAll(io.LimitReader(file, maxStateBytes+1))
	if err != nil {
		return Session{}, err
	}
	if len(body) > maxStateBytes {
		return Session{}, errors.New("session metadata exceeds size limit")
	}
	var value Session
	if err := json.Unmarshal(body, &value); err != nil {
		return Session{}, err
	}
	if err := validate(value); err != nil {
		return Session{}, err
	}
	return value, nil
}

func (s Store) withLock(operation func() error) error {
	if err := platform.EnsurePrivateDir(s.Root); err != nil {
		return err
	}
	lockPath := filepath.Join(s.Root, ".lock")
	fd, err := unix.Open(lockPath, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("open session state lock: %w", err)
	}
	lock := os.NewFile(uintptr(fd), lockPath)
	if lock == nil {
		unix.Close(fd)
		return errors.New("open session state lock")
	}
	defer lock.Close()
	if err := lock.Chmod(0o600); err != nil {
		return err
	}
	info, err := lock.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("unsafe session state lock")
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		return err
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	return operation()
}

func validate(value Session) error {
	if err := ValidateID(value.SessionID); err != nil {
		return err
	}
	if value.Mode != ModeDirect && value.Mode != ModeOrchestrated && value.Mode != ModeAuto {
		return errors.New("invalid session mode")
	}
	if value.PrimaryExecutor != "codex" && value.PrimaryExecutor != "claude" {
		return errors.New("invalid primary executor")
	}
	if value.WorkingDirectory == "" || !filepath.IsAbs(value.WorkingDirectory) || strings.ContainsAny(value.WorkingDirectory, "\x00\x1b\r\n") {
		return errors.New("invalid session working directory")
	}
	if value.MaxWorkers < 1 || value.MaxWorkers > 3 || len(value.Workers) > 256 {
		return errors.New("invalid worker limit")
	}
	if !validState(value.State) || !validModel(value.PrimaryModel) {
		return errors.New("invalid session state or model metadata")
	}
	if !oneOf(value.ContextStatus, "ready", "configured", "degraded", "disabled") || !oneOf(value.MemoryStatus, "ready", "configured", "degraded", "disabled") || !oneOf(value.ServerStatus, "reachable", "configured", "unreachable", "connected", "not-connected", "degraded") {
		return errors.New("invalid service status metadata")
	}
	if (value.SwarmID != "" && !safeText(value.SwarmID, 128)) || (value.PrimaryRufloTaskID != "" && !safeText(value.PrimaryRufloTaskID, 128)) {
		return errors.New("invalid Ruflo lifecycle metadata")
	}
	if (value.Mode == ModeOrchestrated || value.Mode == ModeAuto) && value.State != StateStarting && value.State != StateBlocked && value.SwarmID == "" {
		return errors.New("orchestrated session requires a swarm ID")
	}
	if value.Mode == ModeAuto {
		if !value.Auto || !oneOf(value.InitialPlanner, "codex", "claude") || !oneOf(value.CurrentPrimary, "codex", "claude") || value.FailoverCount < 0 || value.ConsecutiveFailovers < 0 || value.FailoverCount > 100 || value.ConsecutiveFailovers > 2 {
			return errors.New("invalid automatic session metadata")
		}
		if value.LastFailoverReason != "" && !safeText(value.LastFailoverReason, 256) {
			return errors.New("invalid failover metadata")
		}
		if value.CurrentPhase != "" && !safeText(value.CurrentPhase, 64) {
			return errors.New("invalid automatic session phase")
		}
		for provider, snapshot := range value.Quota {
			if provider != quota.ProviderCodex && provider != quota.ProviderClaude || snapshot.Provider != provider || len(snapshot.Windows) > 32 {
				return errors.New("invalid quota snapshot metadata")
			}
		}
	}
	activeWorkers := 0
	workerIDs := make(map[string]struct{}, len(value.Workers))
	for _, worker := range value.Workers {
		if worker.Executor != "codex" && worker.Executor != "claude" {
			return fmt.Errorf("invalid worker executor %q", worker.Executor)
		}
		if worker.RequestedExecutor != "" && worker.RequestedExecutor != "codex" && worker.RequestedExecutor != "claude" || worker.FallbackReason != "" && !safeText(worker.FallbackReason, 256) {
			return errors.New("invalid worker routing metadata")
		}
		if len(worker.ID) != 39 || !strings.HasPrefix(worker.ID, "worker_") || !safeText(worker.Role, 64) || !validState(worker.State) || !validModel(worker.Model) {
			return errors.New("invalid worker metadata")
		}
		if worker.RufloTaskID != "" && !safeText(worker.RufloTaskID, 128) {
			return errors.New("invalid worker Ruflo lifecycle metadata")
		}
		if _, duplicate := workerIDs[worker.ID]; duplicate {
			return errors.New("duplicate worker ID")
		}
		workerIDs[worker.ID] = struct{}{}
		if worker.State == StateStarting || worker.State == StateRunning || worker.State == StateStopping {
			activeWorkers++
		}
	}
	if activeWorkers > value.MaxWorkers || activeWorkers > 3 {
		return errors.New("active worker limit exceeded")
	}
	return nil
}

func validState(value State) bool {
	switch value {
	case StateStarting, StateRunning, StateDegraded, StateStopping, StateCompleted, StateFailed, StateBlocked, StateWaiting:
		return true
	}
	return false
}

func validModel(value ModelInfo) bool {
	switch value.Source {
	case ModelRuntimeVerified, ModelArgument, ModelConfigured:
		return value.Name != "unknown" && safeText(value.Name, 128)
	case ModelUnknown:
		return value.Name == "unknown"
	}
	return false
}

func safeText(value string, limit int) bool {
	return value != "" && len(value) <= limit && !strings.ContainsAny(value, "\x00\x1b\r\n")
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
