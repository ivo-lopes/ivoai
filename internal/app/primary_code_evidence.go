package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"

	"github.com/ivo-lopes/ivoai/internal/codeintel"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/policy"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/skillgate"
	"github.com/ivo-lopes/ivoai/internal/skills"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

type codeProviderFactory func(context.Context, string, string) (*codeintel.Provider, error)

func (a *App) preparePrimaryCodeEvidence(ctx context.Context, store session.Store, id string, task routing.Task, budget int, factory codeProviderFactory) (json.RawMessage, error) {
	query, classifies := codeintel.ForTask(task.Task)
	if !classifies {
		return nil, nil
	}
	if budget < 1024 || budget > 4096 {
		return nil, errors.New("invalid primary code evidence budget")
	}
	// An explicit tool restriction is not permission to bypass the tool via
	// automatic context. Specific native aliases remain callable explicitly.
	if tools, present := task.AllowedMCPTools[nativeCodeMCP]; present && !slices.Contains(tools, "code_inspect") {
		return nil, nil
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return nil, err
	}
	value, err := store.Get(id)
	if err != nil {
		return nil, err
	}
	if !primaryCodeTaskAdmitted(value, cfg, task) {
		return nil, errors.New("primary code task not admitted")
	}
	gate := skillgate.Gate{Registry: skills.Store{Path: skills.RegistryPath(a.Store.Paths.StateDir)}, Supply: supplychain.Manager{Root: filepath.Join(a.Store.Paths.DataDir, "supply-chain")}, Policy: policy.DefaultEngine()}
	excluded := []string{}
	for name, preference := range cfg.Skills.Sources {
		if preference.Disabled {
			excluded = append(excluded, name)
		}
	}
	if cfg.Skills.ResolvedPonytail() == "off" {
		excluded = append(excluded, "ponytail")
	}
	// Validate explicit required skills using only this read capability. Never
	// activate offensive/write capabilities merely to obtain source evidence.
	if _, err := gate.Evaluate(ctx, skillgate.Input{ExplicitOnly: true, Required: task.Skills, Executor: value.CurrentPrimary, ExcludedArtifacts: excluded, AvailableCapabilities: map[string]bool{"filesystem.read": true}}); err != nil {
		return nil, err
	}
	provider, err := factory(ctx, value.WorkingDirectory, value.KnowledgeScopeID)
	if err != nil {
		return nil, err
	}
	provider.Observe = func(result core.CodeResult, inspectErr error) {
		event := codeintel.Observation(result, inspectErr)
		event.SessionID = id
		event.TaskID = task.ID
		_, _ = store.Update(id, func(v *session.Session) error { return session.AppendObservation(v, event) })
	}
	// Reserve envelope space. Retrieval and its exact artifact share this budget;
	// unexpected JSON expansion degrades instead of silently truncating evidence.
	query.BudgetBytes = budget - 768
	body, err := a.prefetchCodeEvidence(ctx, provider, id, task.ID, "", query)
	if err != nil {
		return nil, err
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) != nil {
		return nil, errors.New("invalid primary evidence")
	}
	envelope["task_id"], _ = json.Marshal(task.ID)
	body, err = json.Marshal(envelope)
	if err != nil || len(body) > budget {
		return nil, errors.New("primary evidence exceeds context budget")
	}
	fresh, err := store.Get(id)
	if err != nil || fresh.PlanID != value.PlanID || fresh.KnowledgeScopeID != value.KnowledgeScopeID || fresh.WorkingDirectory != value.WorkingDirectory || !primaryCodeTaskAdmitted(fresh, cfg, task) {
		return nil, errors.New("primary admission changed during inspection")
	}
	return body, nil
}

func primaryCodeTaskAdmitted(value session.Session, cfg config.Config, task routing.Task) bool {
	if !value.Active() || value.State == session.StateStopping || value.State == session.StateWaiting || value.PlanID == "" {
		return false
	}
	if allowed, _ := primaryMCPGrant(value, cfg, nativeCodeMCP, "code_inspect"); !allowed {
		return false
	}
	completed := map[string]bool{}
	for _, row := range value.Tasks {
		completed[row.ID] = row.State == session.StateCompleted
	}
	for _, row := range value.Tasks {
		if row.ID != task.ID || row.ExecutionMode != "primary" || row.State != session.StatePrimary {
			continue
		}
		if !slices.Equal(row.Skills, task.Skills) {
			return false
		}
		if tools, present := row.AllowedMCPTools[nativeCodeMCP]; present && !slices.Contains(tools, "code_inspect") {
			return false
		}
		for _, dependency := range row.Dependencies {
			if !completed[dependency] {
				return false
			}
		}
		return true
	}
	return false
}
