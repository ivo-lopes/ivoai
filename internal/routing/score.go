package routing

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	taskIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	rolePattern   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
)

func Score(value Scores, weights Weights) (int, error) {
	if err := validateScores(value); err != nil {
		return 0, err
	}
	total := weights.Complexity + weights.Risk + weights.ReasoningDepth + weights.VerificationNeed + weights.ContextBreadth
	if total <= 0 {
		return 0, errors.New("routing weights must have a positive sum")
	}
	weighted := value.Complexity*weights.Complexity + value.Risk*weights.Risk + value.ReasoningDepth*weights.ReasoningDepth + value.VerificationNeed*weights.VerificationNeed + value.ContextBreadth*weights.ContextBreadth
	return (weighted + total/2) / total, nil
}

func TierForScore(score int) Tier {
	switch {
	case score <= 24:
		return TierLight
	case score <= 49:
		return TierBalanced
	case score <= 74:
		return TierStrong
	default:
		return TierMax
	}
}

func ResolvePlan(id string, inputs []TaskInput, weights Weights, resolve func(TaskInput, Tier) (ExecutionProfile, error)) (Plan, error) {
	if len(inputs) == 0 || len(inputs) > MaxTasks {
		return Plan{}, fmt.Errorf("plan must contain between 1 and %d tasks", MaxTasks)
	}
	if resolve == nil {
		return Plan{}, errors.New("profile resolver is required")
	}
	known := make(map[string]struct{}, len(inputs))
	work := make(map[string]TaskInput, len(inputs))
	for _, input := range inputs {
		if !taskIDPattern.MatchString(input.ID) || !rolePattern.MatchString(input.Role) {
			return Plan{}, errors.New("task IDs and roles must be bounded safe labels")
		}
		if _, duplicate := known[input.ID]; duplicate {
			return Plan{}, fmt.Errorf("duplicate task ID %q", input.ID)
		}
		if input.Task == "" || len(input.Task) > 32<<10 || strings.ContainsAny(input.Task, "\x00\x1b") {
			return Plan{}, fmt.Errorf("task %q has invalid bounded instructions", input.ID)
		}
		known[input.ID] = struct{}{}
		normalized := strings.Join(strings.Fields(strings.ToLower(input.Task)), " ")
		if previous, duplicate := work[normalized]; duplicate && !input.IntentionalRedundancy && !previous.IntentionalRedundancy {
			return Plan{}, fmt.Errorf("tasks %q and %q duplicate work without intentional_redundancy", previous.ID, input.ID)
		}
		work[normalized] = input
	}
	for _, input := range inputs {
		for _, dependency := range input.Dependencies {
			if dependency == input.ID {
				return Plan{}, fmt.Errorf("task %q depends on itself", input.ID)
			}
			if _, ok := known[dependency]; !ok {
				return Plan{}, fmt.Errorf("task %q references unknown dependency %q", input.ID, dependency)
			}
		}
	}
	if err := validateAcyclic(inputs); err != nil {
		return Plan{}, err
	}
	plan := Plan{ID: id, Strategy: "efficient", Tasks: make([]Task, 0, len(inputs))}
	for _, input := range inputs {
		score, err := Score(input.Scores, weights)
		if err != nil {
			return Plan{}, fmt.Errorf("task %q: %w", input.ID, err)
		}
		tier := TierForScore(score)
		profile, err := resolve(input, tier)
		if err != nil {
			return Plan{}, fmt.Errorf("task %q: %w", input.ID, err)
		}
		profile.Tier = tier
		plan.Tasks = append(plan.Tasks, Task{TaskInput: input, CapabilityScore: score, Tier: tier, Profile: profile, State: "planned"})
	}
	return plan, nil
}

func validateScores(value Scores) error {
	values := []int{value.Complexity, value.Risk, value.ReasoningDepth, value.ContextBreadth, value.VerificationNeed, value.ParallelValue, value.LatencySensitivity}
	for _, current := range values {
		if current < 0 || current > 100 {
			return errors.New("every task score must be between 0 and 100")
		}
	}
	return nil
}

func validateAcyclic(inputs []TaskInput) error {
	deps := map[string][]string{}
	for _, input := range inputs {
		deps[input.ID] = append([]string(nil), input.Dependencies...)
		sort.Strings(deps[input.ID])
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return errors.New("task dependency graph contains a cycle")
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, dependency := range deps[id] {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		return nil
	}
	for id := range deps {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}
