package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

const maxBriefBytes = 64 << 10

type SharedContextBrief struct {
	Objective     string    `json:"objective"`
	Facts         []string  `json:"facts,omitempty"`
	Decisions     []string  `json:"decisions,omitempty"`
	References    []string  `json:"references,omitempty"`
	Constraints   []string  `json:"constraints,omitempty"`
	Gaps          []string  `json:"gaps,omitempty"`
	MemoryStatus  string    `json:"memory_status"`
	ContextStatus string    `json:"context_status"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (s Store) SaveBrief(id string, value SharedContextBrief) (BootstrapMetadata, error) {
	runtimeDir, err := s.RuntimeDir(id)
	if err != nil {
		return BootstrapMetadata{}, err
	}
	value.UpdatedAt = time.Now().UTC()
	if err := validateBrief(value); err != nil {
		return BootstrapMetadata{}, err
	}
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil || len(body) > maxBriefBytes {
		return BootstrapMetadata{}, errors.New("shared context brief exceeds size limit")
	}
	hash := sha256.Sum256(body)
	if err := platform.AtomicWritePrivate(append(body, '\n'), filepath.Join(runtimeDir, "shared-context-brief.json")); err != nil {
		return BootstrapMetadata{}, err
	}
	updated := value.UpdatedAt
	return BootstrapMetadata{Performed: true, UpdatedAt: &updated, MemoryStatus: value.MemoryStatus, ContextStatus: value.ContextStatus, ReferenceCount: len(value.References), BriefHash: hex.EncodeToString(hash[:])}, nil
}

func (s Store) LoadBrief(id string) (SharedContextBrief, error) {
	if err := ValidateID(id); err != nil {
		return SharedContextBrief{}, err
	}
	body, err := os.ReadFile(filepath.Join(s.Root, "runtime", id, "shared-context-brief.json"))
	if err != nil || len(body) > maxBriefBytes {
		return SharedContextBrief{}, errors.New("shared context brief is unavailable")
	}
	var value SharedContextBrief
	if json.Unmarshal(body, &value) != nil || validateBrief(value) != nil {
		return SharedContextBrief{}, errors.New("shared context brief is invalid")
	}
	return value, nil
}

func validateBrief(value SharedContextBrief) error {
	if value.Objective == "" || len(value.Objective) > 4096 || unsafeBrief(value.Objective) {
		return errors.New("shared context objective is invalid")
	}
	if value.MemoryStatus == "" || value.ContextStatus == "" {
		return errors.New("shared knowledge status is required")
	}
	for _, list := range [][]string{value.Facts, value.Decisions, value.References, value.Constraints, value.Gaps} {
		if len(list) > 64 {
			return errors.New("shared context list exceeds limit")
		}
		for _, item := range list {
			if len(item) > 1024 || unsafeBrief(item) {
				return errors.New("shared context item is unsafe")
			}
		}
	}
	return nil
}

func unsafeBrief(value string) bool {
	return strings.ContainsAny(value, "\x00\x1b") || platform.Redact(value) != value
}
