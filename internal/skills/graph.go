package skills

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type ResolutionRequest struct {
	IDs                   []string
	Executor              string
	AvailableCapabilities map[string]bool
	MaximumRisk           RiskTier
}

type Resolution struct {
	Ordered []Entry            `json:"ordered"`
	Phases  map[Phase][]string `json:"phases"`
}

type ResolutionError struct {
	Kind   string
	Skills []string
	Detail string
}

func (e *ResolutionError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("skill resolution %s: %s", e.Kind, strings.Join(e.Skills, ","))
	}
	return fmt.Sprintf("skill resolution %s: %s: %s", e.Kind, strings.Join(e.Skills, ","), e.Detail)
}

type Resolver struct{ Registry Registry }

func (r Resolver) Resolve(request ResolutionRequest) (Resolution, error) {
	registry := r.Registry
	if err := registry.Normalize(); err != nil {
		return Resolution{}, err
	}
	byID := make(map[string]Entry, len(registry.Entries))
	for _, entry := range registry.Entries {
		if entry.Lifecycle != LifecycleStaged && entry.Lifecycle != LifecycleActive {
			continue
		}
		byID[entry.ID] = entry
	}
	selected := map[string]bool{}
	var include func(string) error
	include = func(id string) error {
		id = strings.ToLower(strings.TrimSpace(id))
		if selected[id] {
			return nil
		}
		entry, ok := byID[id]
		if !ok {
			return &ResolutionError{Kind: "missing_dependency", Skills: []string{id}}
		}
		selected[id] = true
		for _, dependency := range entry.RequiredDependencies {
			if err := include(dependency); err != nil {
				var resolutionErr *ResolutionError
				if errors.As(err, &resolutionErr) {
					resolutionErr.Skills = append([]string{id}, resolutionErr.Skills...)
				}
				return err
			}
		}
		return nil
	}
	for _, id := range normalizedList(request.IDs) {
		if err := include(id); err != nil {
			return Resolution{}, err
		}
	}
	for id := range selected {
		entry := byID[id]
		if entry.Role == "control_plane" || contains(entry.Capabilities, "orchestration.authority") {
			return Resolution{}, &ResolutionError{Kind: "orchestration_authority_reserved", Skills: []string{id}}
		}
		if request.Executor != "" && len(entry.Compatibility.Executors) > 0 && !contains(entry.Compatibility.Executors, strings.ToLower(request.Executor)) {
			return Resolution{}, &ResolutionError{Kind: "incompatible_executor", Skills: []string{id}, Detail: request.Executor}
		}
		if request.MaximumRisk != "" && riskWeight(entry.Risk) > riskWeight(request.MaximumRisk) {
			return Resolution{}, &ResolutionError{Kind: "risk_exceeds_policy", Skills: []string{id}, Detail: string(entry.Risk)}
		}
		for _, capability := range entry.Capabilities {
			if !request.AvailableCapabilities[capability] {
				return Resolution{}, &ResolutionError{Kind: "capability_unavailable", Skills: []string{id}, Detail: capability}
			}
		}
	}
	if err := detectConflicts(selected, byID); err != nil {
		return Resolution{}, err
	}
	dependencies := map[string]map[string]bool{}
	for id := range selected {
		dependencies[id] = map[string]bool{}
		for _, dependency := range byID[id].RequiredDependencies {
			dependencies[id][dependency] = true
		}
		for _, optional := range byID[id].OptionalDependencies {
			if selected[optional] {
				dependencies[id][optional] = true
			}
		}
	}
	orderedIDs, err := topologicalSkills(dependencies, byID)
	if err != nil {
		return Resolution{}, err
	}
	result := Resolution{Ordered: make([]Entry, 0, len(orderedIDs)), Phases: map[Phase][]string{}}
	for _, id := range orderedIDs {
		entry := byID[id]
		result.Ordered = append(result.Ordered, entry)
		result.Phases[entry.Phase] = append(result.Phases[entry.Phase], id)
	}
	return result, nil
}

func detectConflicts(selected map[string]bool, byID map[string]Entry) error {
	var ids []string
	for id := range selected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for leftIndex, leftID := range ids {
		left := byID[leftID]
		for _, rightID := range ids[leftIndex+1:] {
			right := byID[rightID]
			explicit := contains(left.Conflicts, rightID) || contains(right.Conflicts, leftID)
			exclusiveRole := left.Role != "" && left.Role == right.Role && left.Phase == right.Phase && (left.RoleMode == RoleExclusive || right.RoleMode == RoleExclusive)
			orchestrationAuthority := left.Phase == PhaseOrchestration && right.Phase == PhaseOrchestration && (left.Role == "control_plane" || right.Role == "control_plane")
			if explicit || exclusiveRole || orchestrationAuthority {
				return &ResolutionError{Kind: "conflict", Skills: []string{leftID, rightID}, Detail: conflictReason(explicit, exclusiveRole, orchestrationAuthority)}
			}
		}
	}
	return nil
}

func conflictReason(explicit, role, authority bool) string {
	if explicit {
		return "declared_conflict"
	}
	if authority {
		return "orchestration_authority_conflict"
	}
	if role {
		return "mutually_exclusive_role"
	}
	return "unresolved_conflict"
}

func topologicalSkills(dependencies map[string]map[string]bool, byID map[string]Entry) ([]string, error) {
	done := map[string]bool{}
	ordered := make([]string, 0, len(dependencies))
	for len(ordered) < len(dependencies) {
		var ready []string
		for id, required := range dependencies {
			if done[id] {
				continue
			}
			eligible := true
			for dependency := range required {
				if !done[dependency] {
					eligible = false
					break
				}
			}
			if eligible {
				ready = append(ready, id)
			}
		}
		if len(ready) == 0 {
			var cycle []string
			for id := range dependencies {
				if !done[id] {
					cycle = append(cycle, id)
				}
			}
			sort.Strings(cycle)
			return nil, &ResolutionError{Kind: "dependency_cycle", Skills: cycle}
		}
		sort.Slice(ready, func(i, j int) bool {
			left, right := phaseWeight(byID[ready[i]].Phase), phaseWeight(byID[ready[j]].Phase)
			if left == right {
				return ready[i] < ready[j]
			}
			return left < right
		})
		for _, id := range ready {
			done[id] = true
			ordered = append(ordered, id)
		}
	}
	return ordered, nil
}

func phaseWeight(phase Phase) int {
	switch phase {
	case PhasePlanning:
		return 1
	case PhaseResearch:
		return 2
	case PhaseArtDirection:
		return 3
	case PhaseImplementation:
		return 4
	case PhaseAudit:
		return 5
	case PhaseSecurity:
		return 6
	case PhaseOrchestration:
		return 7
	case PhaseInteractionProfile:
		return 8
	default:
		return 99
	}
}
