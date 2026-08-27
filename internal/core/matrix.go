package core

import (
	"fmt"
	"sort"
)

type Matrix struct {
	Components []ComponentStatus `json:"components"`
}

type Selection struct {
	Component      ComponentStatus `json:"component"`
	FallbackUsed   bool            `json:"fallback_used"`
	FallbackReason string          `json:"fallback_reason,omitempty"`
}

func NewMatrix(values ...ComponentStatus) (Matrix, error) {
	matrix := Matrix{Components: append([]ComponentStatus(nil), values...)}
	seen := map[string]bool{}
	for _, value := range matrix.Components {
		if err := value.Validate(); err != nil {
			return Matrix{}, err
		}
		key := string(value.ID) + "\x00" + value.Implementation
		if seen[key] {
			return Matrix{}, fmt.Errorf("duplicate component implementation %s/%s", value.ID, value.Implementation)
		}
		seen[key] = true
	}
	sort.SliceStable(matrix.Components, func(i, j int) bool {
		if matrix.Components[i].ID != matrix.Components[j].ID {
			return matrix.Components[i].ID < matrix.Components[j].ID
		}
		if matrix.Components[i].Active != matrix.Components[j].Active {
			return matrix.Components[i].Active
		}
		return matrix.Components[i].Implementation < matrix.Components[j].Implementation
	})
	return matrix, nil
}

func (m Matrix) Entries(id ComponentID) []ComponentStatus {
	var result []ComponentStatus
	for _, value := range m.Components {
		if value.ID == id {
			copy := value
			copy.Capabilities = value.Capabilities.Clone()
			result = append(result, copy)
		}
	}
	return result
}

func (m Matrix) Resolve(id ComponentID, required ...Capability) (Selection, error) {
	entries := m.Entries(id)
	if len(entries) == 0 {
		return Selection{}, &UnavailableError{Component: id, Reason: "no implementation is registered"}
	}
	var active *ComponentStatus
	for index := range entries {
		if entries[index].Active {
			active = &entries[index]
			break
		}
	}
	if active != nil {
		if err := selectable(*active, required); err == nil {
			return Selection{Component: *active}, nil
		}
	}
	for _, candidate := range entries {
		if candidate.Active || !candidate.Fallback.Allowed {
			continue
		}
		if err := selectable(candidate, required); err == nil {
			reason := candidate.Fallback.Reason
			if reason == "" && active != nil {
				reason = active.Implementation + " is unavailable or incompatible"
			}
			return Selection{Component: candidate, FallbackUsed: true, FallbackReason: reason}, nil
		}
	}
	if active == nil {
		return Selection{}, &UnavailableError{Component: id, Reason: "no active implementation"}
	}
	return Selection{}, selectable(*active, required)
}

func selectable(value ComponentStatus, required []Capability) error {
	if !value.Installed || !value.Available || value.Health == HealthUnavailable {
		return &UnavailableError{Component: value.ID, Reason: value.Implementation}
	}
	if value.Compatibility.State == CompatibilityIncompatible {
		return &IncompatibleError{Component: value.ID, Reason: value.Compatibility.Reason}
	}
	for _, capability := range required {
		if !value.Capabilities.Supports(capability) {
			return &UnsupportedError{Component: value.ID, Capability: capability}
		}
	}
	return nil
}
