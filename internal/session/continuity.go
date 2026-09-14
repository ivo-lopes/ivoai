package session

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"golang.org/x/sys/unix"
)

func NewNativeUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func ValidNativeUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(value[:8] + value[9:13] + value[14:18] + value[19:23] + value[24:])
	return err == nil
}

// Lease covers the whole frontend lifetime, not just one atomic store update.
// Kernel ownership disappears on crash; PID reuse cannot steal or retain it.
type Lease struct{ file *os.File }

func (l *Lease) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Close()
}

func (s Store) Acquire(id string) (*Lease, error) {
	if err := ValidateID(id); err != nil {
		return nil, err
	}
	if err := platform.EnsurePrivateDir(s.Root); err != nil {
		return nil, err
	}
	root := filepath.Join(s.Root, "leases")
	if err := platform.EnsurePrivateDir(root); err != nil {
		return nil, err
	}
	fd, err := unix.Open(filepath.Join(root, id+".lock"), unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, errors.New("UNSAFE_SESSION_LEASE")
	}
	file := os.NewFile(uintptr(fd), id)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		_ = file.Close()
		return nil, errors.New("UNSAFE_SESSION_LEASE")
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, errors.New("SESSION_ALREADY_ACTIVE")
	}
	return &Lease{file}, nil
}

// FindFrontend resolves an existing mapping, never imports an arbitrary native
// conversation. A duplicate mapping is corruption, not a latest-wins decision.
func (s Store) FindFrontend(frontend, nativeID, cwd string) (Session, error) {
	if !safeText(nativeID, 128) || nativeID == "" {
		return Session{}, errors.New("INVALID_NATIVE_SESSION")
	}
	values, err := s.List()
	if err != nil {
		return Session{}, err
	}
	var found Session
	for _, value := range values {
		mapped := value.FrontendSessions[frontend]
		if value.Frontend == frontend {
			mapped = value.FrontendSessionID
		}
		if mapped != nativeID || value.WorkingDirectory != cwd {
			continue
		}
		if found.SessionID != "" {
			return Session{}, errors.New("AMBIGUOUS_NATIVE_MAPPING")
		}
		found = value
	}
	if found.SessionID == "" {
		return Session{}, errors.New("NATIVE_SESSION_NOT_MANAGED")
	}
	return found, nil
}

// Reopen preserves identity, accepted metadata and completed work. It does not
// replay a turn or restore ephemeral grants. Callers must hold Acquire's lease.
func (s Store) Reopen(id, cwd string, mode Mode) (Session, error) {
	return s.Update(id, func(value *Session) error {
		if value.WorkingDirectory != cwd || value.Mode != mode {
			return errors.New("SESSION_SCOPE_MISMATCH")
		}
		if ProcessMatches(value.PrimaryPID, value.PrimaryProcessStart) || ProcessMatches(value.FrontendPID, value.FrontendProcessStart) {
			return errors.New("SESSION_ALREADY_ACTIVE")
		}
		for _, worker := range value.Workers {
			if ProcessMatches(worker.PID, worker.ProcessStart) {
				return errors.New("SESSION_WORKER_STILL_ACTIVE")
			}
		}
		value.State, value.FrontendState = StateStarting, StateStarting
		value.PrimaryPID, value.FrontendPID = 0, 0
		value.PrimaryProcessStart, value.FrontendProcessStart = "", ""
		value.EndedAt, value.ExitCode, value.FrontendExitCode, value.ExecutorExitCode = nil, nil, nil, nil
		value.CurrentPhase = "waiting_for_input"
		value.QuotaMode = ""
		for i := range value.Decisions {
			if value.Decisions[i].State == "pending" {
				now := time.Now().UTC()
				value.Decisions[i].State, value.Decisions[i].ResolvedAt = "cancelled", &now
			}
		}
		value.UpdatedAt = time.Now().UTC()
		return nil
	})
}

func (s Session) Resumable() bool {
	if ProcessMatches(s.PrimaryPID, s.PrimaryProcessStart) || ProcessMatches(s.FrontendPID, s.FrontendProcessStart) {
		return false
	}
	for _, w := range s.Workers {
		if ProcessMatches(w.PID, w.ProcessStart) {
			return false
		}
	}
	if s.Mode == ModeAuto {
		return s.FrontendSessionID != ""
	}
	return s.Mode == ModeDirect && (s.PrimaryExecutor == "codex" || s.PrimaryExecutor == "claude" || s.PrimaryExecutor == "opencode") && s.ExecutorSessionID != ""
}

func (s *Session) SetFrontend(frontend, id string) {
	if s.FrontendSessions == nil {
		s.FrontendSessions = map[string]string{}
	}
	if s.Frontend != "" && s.FrontendSessionID != "" {
		s.FrontendSessions[s.Frontend] = s.FrontendSessionID
	}
	s.Frontend, s.FrontendSessionID = frontend, id
	if id != "" {
		s.FrontendSessions[frontend] = id
	}
}
