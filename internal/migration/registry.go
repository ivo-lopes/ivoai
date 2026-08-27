// Package migration provides the small, explicit migration graph and durable
// update transaction used by ivoai. It deliberately knows nothing about
// provider authentication stores.
package migration

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

type Artifact string

const (
	ArtifactExecutable Artifact = "executable"
	ArtifactConfig     Artifact = "config"
	ArtifactState      Artifact = "state"
	ArtifactOwnership  Artifact = "ownership"
	ArtifactComponents Artifact = "components"
	// ArtifactSkillRegistry is a snapshot participant with an independent
	// schema stored inside registry.json. It deliberately is not added to the
	// global config/state schema graph.
	ArtifactSkillRegistry Artifact = "skill_registry"
	ArtifactServer        Artifact = "server"
)

type Schemas map[Artifact]int

type Workspace struct {
	Files      map[Artifact]string
	NamedFiles map[string]string
}

type Step struct {
	ID           string
	Artifact     Artifact
	From         int
	To           int
	DependsOn    []string
	Precondition func(context.Context, Workspace) error
	Apply        func(context.Context, Workspace) error
	Validate     func(context.Context, Workspace) error
	Rollback     func(context.Context, Workspace) error
}

type Plan struct {
	Source Schemas
	Target Schemas
	Steps  []Step
}

type Registry struct {
	Steps []Step
}

func (r Registry) SupportedSources(target Schemas) (map[Artifact][]int, error) {
	result := make(map[Artifact][]int, len(target))
	for artifact, goal := range target {
		candidates := map[int]bool{goal: true}
		for _, step := range r.Steps {
			if step.Artifact == artifact && step.From >= 0 && step.From <= goal {
				candidates[step.From] = true
			}
		}
		for source := range candidates {
			if _, err := r.resolveArtifact(artifact, source, goal); err == nil {
				result[artifact] = append(result[artifact], source)
			}
		}
		sort.Ints(result[artifact])
		if len(result[artifact]) == 0 {
			return nil, fmt.Errorf("no supported source schema for %s target %d", artifact, goal)
		}
	}
	return result, nil
}

func (r Registry) Resolve(source, target Schemas) (Plan, error) {
	plan := Plan{Source: cloneSchemas(source), Target: cloneSchemas(target)}
	artifacts := make([]string, 0, len(target))
	for artifact := range target {
		artifacts = append(artifacts, string(artifact))
	}
	sort.Strings(artifacts)
	for _, name := range artifacts {
		artifact := Artifact(name)
		current, ok := source[artifact]
		if !ok {
			return Plan{}, fmt.Errorf("missing source schema for %s", artifact)
		}
		steps, err := r.resolveArtifact(artifact, current, target[artifact])
		if err != nil {
			return Plan{}, err
		}
		plan.Steps = append(plan.Steps, steps...)
	}
	ordered, err := orderSteps(plan.Steps)
	if err != nil {
		return Plan{}, err
	}
	plan.Steps = ordered
	return plan, nil
}

// resolveArtifact verifies the sequential path for one artifact without
// attempting to satisfy cross-artifact dependencies. SupportedSources uses
// this projection because those dependencies are meaningful only once the
// complete multi-artifact plan is assembled.
func (r Registry) resolveArtifact(artifact Artifact, current, goal int) ([]Step, error) {
	if current < 0 || goal < 0 || current > goal {
		return nil, fmt.Errorf("unsupported schema transition for %s: %d -> %d", artifact, current, goal)
	}
	var result []Step
	visited := map[int]bool{}
	for current < goal {
		if visited[current] {
			return nil, fmt.Errorf("migration cycle for %s at schema %d", artifact, current)
		}
		visited[current] = true
		var matches []Step
		for _, step := range r.Steps {
			if step.Artifact == artifact && step.From == current && step.To > current && step.To <= goal {
				matches = append(matches, step)
			}
		}
		if len(matches) != 1 {
			return nil, fmt.Errorf("migration path for %s schema %d has %d candidates", artifact, current, len(matches))
		}
		step := matches[0]
		if step.ID == "" || step.Apply == nil || step.Validate == nil || step.Rollback == nil {
			return nil, fmt.Errorf("migration %q does not provide apply, validation, and rollback", step.ID)
		}
		result = append(result, step)
		current = step.To
	}
	return result, nil
}

func orderSteps(steps []Step) ([]Step, error) {
	byID := make(map[string]Step, len(steps))
	dependencies := make(map[string]map[string]bool, len(steps))
	lastByArtifact := map[Artifact]string{}
	for _, step := range steps {
		if _, duplicate := byID[step.ID]; duplicate {
			return nil, fmt.Errorf("duplicate migration ID %q", step.ID)
		}
		byID[step.ID] = step
		dependencies[step.ID] = map[string]bool{}
		for _, dependency := range step.DependsOn {
			dependencies[step.ID][dependency] = true
		}
		if previous := lastByArtifact[step.Artifact]; previous != "" {
			dependencies[step.ID][previous] = true
		}
		lastByArtifact[step.Artifact] = step.ID
	}
	for id, values := range dependencies {
		for dependency := range values {
			if _, ok := byID[dependency]; !ok {
				return nil, fmt.Errorf("migration %s depends on unknown migration %s", id, dependency)
			}
		}
	}
	var ordered []Step
	done := map[string]bool{}
	for len(ordered) < len(steps) {
		var ready []string
		for id, values := range dependencies {
			if done[id] {
				continue
			}
			eligible := true
			for dependency := range values {
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
			return nil, errors.New("migration dependency cycle")
		}
		sort.Strings(ready)
		for _, id := range ready {
			done[id] = true
			ordered = append(ordered, byID[id])
		}
	}
	return ordered, nil
}

func (p Plan) Apply(ctx context.Context, workspace Workspace) error {
	var applied []Step
	for _, step := range p.Steps {
		if step.Precondition != nil {
			if err := step.Precondition(ctx, workspace); err != nil {
				return errors.Join(fmt.Errorf("migration %s precondition: %w", step.ID, err), rollbackSteps(ctx, workspace, applied))
			}
		}
		if err := step.Apply(ctx, workspace); err != nil {
			return errors.Join(fmt.Errorf("migration %s apply: %w", step.ID, err), rollbackSteps(ctx, workspace, append(applied, step)))
		}
		applied = append(applied, step)
		if err := step.Validate(ctx, workspace); err != nil {
			rollbackErr := rollbackSteps(ctx, workspace, applied)
			return errors.Join(fmt.Errorf("migration %s validation: %w", step.ID, err), rollbackErr)
		}
	}
	return nil
}

func rollbackSteps(ctx context.Context, workspace Workspace, steps []Step) error {
	var result error
	for index := len(steps) - 1; index >= 0; index-- {
		if err := steps[index].Rollback(ctx, workspace); err != nil {
			result = errors.Join(result, fmt.Errorf("migration %s rollback: %w", steps[index].ID, err))
		}
	}
	return result
}

func cloneSchemas(value Schemas) Schemas {
	result := make(Schemas, len(value))
	for artifact, schema := range value {
		result[artifact] = schema
	}
	return result
}
