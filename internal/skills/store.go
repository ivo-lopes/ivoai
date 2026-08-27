package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

const maxRegistryBytes = 8 << 20

type Store struct{ Path string }

func RegistryPath(stateDir string) string { return filepath.Join(stateDir, "skills", "registry.json") }

func (s Store) Load() (Registry, error) {
	if err := validateRegistryPath(s.Path); err != nil {
		return Registry{}, err
	}
	data, err := platform.ReadRegularFile(s.Path, maxRegistryBytes)
	if errors.Is(err, os.ErrNotExist) {
		return EmptyRegistry(), nil
	}
	if err != nil {
		return Registry{}, err
	}
	var registry Registry
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&registry); err != nil {
		return Registry{}, fmt.Errorf("decode skill registry: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Registry{}, errors.New("skill registry has trailing data")
	}
	if err := registry.Normalize(); err != nil {
		return Registry{}, err
	}
	return registry, nil
}

func (s Store) Save(registry Registry) error {
	if err := validateRegistryPath(s.Path); err != nil {
		return err
	}
	if err := registry.Normalize(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > maxRegistryBytes {
		return errors.New("skill registry exceeds storage limit")
	}
	return platform.AtomicWritePrivate(data, s.Path)
}

func validateRegistryPath(value string) error {
	if value == "" || !filepath.IsAbs(value) || filepath.Clean(value) == string(filepath.Separator) {
		return errors.New("skill registry path must be absolute and private")
	}
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Dir(filepath.Clean(value)), string(filepath.Separator)), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("skill registry path traverses symlink: %s", current)
		}
	}
	return nil
}

func (s Store) Resolve(_ context.Context, id string) (core.SkillDescriptor, error) {
	registry, err := s.Load()
	if err != nil {
		return core.SkillDescriptor{}, err
	}
	index := sort.Search(len(registry.Entries), func(i int) bool { return registry.Entries[i].ID >= id })
	if index >= len(registry.Entries) || registry.Entries[index].ID != id {
		return core.SkillDescriptor{}, os.ErrNotExist
	}
	entry := registry.Entries[index]
	return core.SkillDescriptor{ID: entry.ID, Version: entry.Provenance.Revision.LogicalVersion, Source: core.Provenance{Source: entry.Provenance.Source.URL, Version: entry.Provenance.Revision.Commit, Path: entry.Provenance.Source.Path}}, nil
}

func (s Store) ID() core.ComponentID { return core.ComponentSkills }

func (s Store) Probe(_ context.Context) core.ComponentStatus {
	registry, err := s.Load()
	health := core.HealthHealthy
	compatibility := core.Compatibility{State: core.CompatibilityCompatible}
	available := true
	if err != nil {
		health, available = core.HealthUnavailable, false
		compatibility = core.Compatibility{State: core.CompatibilityIncompatible, Reason: "skill registry is unreadable"}
	}
	active := false
	for _, entry := range registry.Entries {
		if entry.Lifecycle == LifecycleActive {
			active = true
			break
		}
	}
	return core.ComponentStatus{
		ID: core.ComponentSkills, Implementation: "ivoai-skill-registry", Active: active,
		Installed: true, Managed: true, Available: available, Health: health, Lifecycle: core.LifecycleStopped,
		Provenance:   core.Provenance{Source: "ivoai_private_state", Version: fmt.Sprintf("schema-%d", RegistrySchemaVersion)},
		Capabilities: core.CapabilitySet{}, Compatibility: compatibility,
		Fallback: core.Fallback{Allowed: true, Reason: "empty registry leaves current sessions unchanged"},
	}
}
