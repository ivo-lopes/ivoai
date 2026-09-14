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
	"github.com/ivo-lopes/ivoai/internal/routing"
	"golang.org/x/sys/unix"
)

const maxCheckpointBytes = 32 << 10

type Checkpoint struct {
	// Recovery is written by the host scheduler, never by the checkpoint tool.
	Recovery        *RecoveryPlan `json:"recovery,omitempty"`
	Objective       string        `json:"objective,omitempty"`
	Constraints     []string      `json:"constraints,omitempty"`
	Acceptance      []string      `json:"acceptance,omitempty"`
	Decisions       []string      `json:"decisions,omitempty"`
	Completed       []string      `json:"completed,omitempty"`
	FilesChanged    []string      `json:"files_changed,omitempty"`
	ImportantChecks []string      `json:"important_checks,omitempty"`
	Outstanding     []string      `json:"outstanding,omitempty"`
	Blockers        []string      `json:"blockers,omitempty"`
	NextStep        string        `json:"next_step,omitempty"`
	Interrupted     bool          `json:"interrupted,omitempty"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

type RecoveryPlan struct {
	Approved           bool         `json:"approved"`
	RepositoryIdentity string       `json:"repository_identity"`
	RepositoryHead     string       `json:"repository_head,omitempty"`
	Plan               routing.Plan `json:"plan"`
	Directory          string       `json:"directory"`
	Integrated         bool         `json:"integrated"`
}

func (s Store) UpdateCheckpoint(id string, mutate func(*Checkpoint) error) error {
	return s.withLock(func() error {
		value, err := s.LoadCheckpoint(id)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := mutate(&value); err != nil {
			return err
		}
		return s.SaveCheckpoint(id, value)
	})
}

func (s Store) SaveCheckpoint(id string, value Checkpoint) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	if err := platform.EnsurePrivateDir(s.Root); err != nil {
		return err
	}
	checkpointDir := filepath.Join(s.Root, "checkpoints")
	if err := platform.EnsurePrivateDir(checkpointDir); err != nil {
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
	return platform.AtomicWritePrivate(append(body, '\n'), filepath.Join(checkpointDir, id+".json"))
}

func (s Store) LoadCheckpoint(id string) (Checkpoint, error) {
	if err := ValidateID(id); err != nil {
		return Checkpoint{}, err
	}
	path := filepath.Join(s.Root, "checkpoints", id+".json")
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		// Read-only compatibility with v0.10.2 runtime checkpoints.
		path = filepath.Join(s.Root, "runtime", id, "checkpoint.json")
		fd, err = unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	}
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
	if value.Recovery != nil {
		r := value.Recovery
		if !filepath.IsAbs(r.Directory) || unsafeCheckpointString(r.Directory) || len(r.Plan.Tasks) == 0 || len(r.Plan.Tasks) > routing.MaxTasks {
			return errors.New("invalid recovery plan")
		}
		body, err := json.Marshal(r)
		if err != nil || len(body) > maxCheckpointBytes || platform.Redact(string(body)) != string(body) {
			return errors.New("unsafe recovery plan")
		}
		var tree any
		if json.Unmarshal(body, &tree) != nil || !checkpointStringsSafe(tree) {
			return errors.New("unsafe recovery fields")
		}
		inputs := make([]routing.TaskInput, 0, len(r.Plan.Tasks))
		for _, task := range r.Plan.Tasks {
			if len(task.Task) > 1024 || unsafeCheckpointString(task.Task) || len(task.Acceptance) == 0 {
				return errors.New("unsafe recovery objective")
			}
			inputs = append(inputs, task.TaskInput)
		}
		if _, err := routing.ResolvePlan(r.Plan.ID, inputs, routing.DefaultWeights(), func(input routing.TaskInput, tier routing.Tier) (routing.ExecutionProfile, error) {
			return routing.ExecutionProfile{Provider: "codex", Tier: tier}, nil
		}); err != nil {
			return errors.New("invalid recovery DAG")
		}
	}
	if len(value.Objective) > 4096 || len(value.NextStep) > 4096 || unsafeCheckpointString(value.Objective) || unsafeCheckpointString(value.NextStep) {
		return errors.New("checkpoint field exceeds limit")
	}
	for _, values := range [][]string{value.Decisions, value.Completed, value.FilesChanged, value.ImportantChecks, value.Outstanding, value.Blockers, value.Constraints, value.Acceptance} {
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

func checkpointStringsSafe(value any) bool {
	switch value := value.(type) {
	case string:
		return len(value) <= 4096 && !unsafeCheckpointString(value)
	case []any:
		for _, item := range value {
			if !checkpointStringsSafe(item) {
				return false
			}
		}
	case map[string]any:
		for key, item := range value {
			if !checkpointStringsSafe(key) || !checkpointStringsSafe(item) {
				return false
			}
		}
	}
	return true
}

func unsafeCheckpointString(value string) bool {
	return strings.ContainsAny(value, "\x00\x1b") || platform.Redact(value) != value
}
