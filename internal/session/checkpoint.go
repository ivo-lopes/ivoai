package session

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

const maxCheckpointBytes = 32 << 10

type Checkpoint struct {
	Objective       string    `json:"objective,omitempty"`
	Decisions       []string  `json:"decisions,omitempty"`
	Completed       []string  `json:"completed,omitempty"`
	FilesChanged    []string  `json:"files_changed,omitempty"`
	ImportantChecks []string  `json:"important_checks,omitempty"`
	Outstanding     []string  `json:"outstanding,omitempty"`
	Blockers        []string  `json:"blockers,omitempty"`
	NextStep        string    `json:"next_step,omitempty"`
	Interrupted     bool      `json:"interrupted,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (s Store) SaveCheckpoint(id string, value Checkpoint) error {
	runtimeDir, err := s.RuntimeDir(id)
	if err != nil {
		return err
	}
	value.UpdatedAt = time.Now().UTC()
	if err := validateCheckpoint(value); err != nil {
		return err
	}
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil || len(body) > maxCheckpointBytes {
		return errors.New("checkpoint exceeds size limit")
	}
	return platform.AtomicWritePrivate(append(body, '\n'), filepath.Join(runtimeDir, "checkpoint.json"))
}

func (s Store) LoadCheckpoint(id string) (Checkpoint, error) {
	if err := ValidateID(id); err != nil {
		return Checkpoint{}, err
	}
	path := filepath.Join(s.Root, "runtime", id, "checkpoint.json")
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return Checkpoint{}, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return Checkpoint{}, errors.New("open checkpoint")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > maxCheckpointBytes {
		return Checkpoint{}, errors.New("unsafe checkpoint file")
	}
	body, err := io.ReadAll(io.LimitReader(file, maxCheckpointBytes+1))
	if err != nil || len(body) > maxCheckpointBytes {
		return Checkpoint{}, errors.New("invalid checkpoint")
	}
	var value Checkpoint
	if json.Unmarshal(body, &value) != nil || validateCheckpoint(value) != nil {
		return Checkpoint{}, errors.New("invalid checkpoint")
	}
	return value, nil
}

func validateCheckpoint(value Checkpoint) error {
	if len(value.Objective) > 4096 || len(value.NextStep) > 4096 || unsafeCheckpointString(value.Objective) || unsafeCheckpointString(value.NextStep) {
		return errors.New("checkpoint field exceeds limit")
	}
	for _, values := range [][]string{value.Decisions, value.Completed, value.FilesChanged, value.ImportantChecks, value.Outstanding, value.Blockers} {
		if len(values) > 64 {
			return errors.New("checkpoint list exceeds limit")
		}
		for _, item := range values {
			if len(item) > 1024 || unsafeCheckpointString(item) {
				return errors.New("unsafe checkpoint content")
			}
		}
	}
	return nil
}

func unsafeCheckpointString(value string) bool {
	return strings.ContainsAny(value, "\x00\x1b") || platform.Redact(value) != value
}
