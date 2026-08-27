package app

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/policy"
	"github.com/ivo-lopes/ivoai/internal/skillgate"
	"github.com/ivo-lopes/ivoai/internal/skills"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

func (a *App) evaluateSessionSkills(ctx context.Context, executor, cwd string, args []string) (skillgate.Result, error) {
	gate := skillgate.Gate{
		Registry: skills.Store{Path: skills.RegistryPath(a.Store.Paths.StateDir)},
		Supply:   supplychain.Manager{Root: filepath.Join(a.Store.Paths.DataDir, "supply-chain")},
		Policy:   policy.DefaultEngine(),
	}
	result, err := gate.Evaluate(ctx, skillgate.Input{Intent: sessionSkillIntent(cwd, args), Executor: executor})
	if result.Degraded {
		a.warn("Skill Gate degraded; continuing without unavailable external skills", nil)
	}
	return result, err
}

func sessionSkillIntent(cwd string, args []string) string {
	parts := []string{filepath.Base(filepath.Clean(cwd))}
	for _, value := range args {
		value = strings.TrimSpace(value)
		if value == "" || strings.HasPrefix(value, "-") {
			continue
		}
		parts = append(parts, value)
	}
	value := strings.Join(parts, " ")
	if len(value) > 4096 {
		return value[:4096]
	}
	return value
}

func appendSkillObservations(events []observability.Event, sessionID string, appendEvent func(observability.Event) error) error {
	for _, event := range events {
		if event.Operation == "" {
			continue
		}
		event.SessionID = sessionID
		if err := appendEvent(event); err != nil {
			return err
		}
	}
	return nil
}
