package app

import (
	"context"
	"errors"
	"os"

	"github.com/ivo-lopes/ivoai/internal/orchestrator"
	"github.com/ivo-lopes/ivoai/internal/session"
)

type recoverySessionKey struct{}
type resumeFrontendKey struct{}

func (a *App) SessionResumeFrontend(ctx context.Context, id, frontend string) error {
	if frontend != "codex" && frontend != "opencode" {
		return errors.New("INVALID_RESUME_FRONTEND")
	}
	return a.SessionResume(context.WithValue(ctx, resumeFrontendKey{}, frontend), id)
}

// This public control message contains no original prompt or checkpoint body.
const recoveryPrompt = "Objective: Analyze and resume the interrupted IVOAI task graph using orchestration_recover, not orchestration_plan. Deliverable: finish only remaining approved tasks, integrate their changes and synthesize the result. Constraints: do not repeat completed tasks or uncertain mutations; retain the saved scope and acceptance criteria. Acceptance: the result must preserve completed tasks, checkpoint reconciliation must succeed, new plan approval must be obtained, all remaining local acceptance criteria must pass, and unresolved blockers must be reported honestly."

func (a *App) SessionRecover(ctx context.Context, id string) error {
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	value, err := store.Get(id)
	if err != nil {
		return err
	}
	if value.Mode != session.ModeAuto || value.Coordinator != "native" {
		return errors.New("INTERRUPTED_RECOVERY_NOT_SUPPORTED")
	}
	if _, err := orchestrator.ReconcileRecovery(ctx, store, id); err != nil {
		return err
	}
	return a.SessionResume(context.WithValue(ctx, recoverySessionKey{}, true), id)
}

// SessionResume reopens a conversation, not its last turn. Provider-native
// input remains in the official frontend and every new turn is admitted anew.
func (a *App) SessionResume(ctx context.Context, id string) error {
	value, err := a.SessionShow(id)
	if err != nil {
		return err
	}
	info, err := os.Stat(value.WorkingDirectory)
	if err != nil || !info.IsDir() {
		return errors.New("SESSION_DIRECTORY_UNAVAILABLE")
	}
	if value.Mode == session.ModeAuto {
		frontend := value.Frontend
		if override, _ := ctx.Value(resumeFrontendKey{}).(string); override != "" {
			frontend = override
		}
		if frontend == "codex" && value.PrimaryExecutor != "codex" {
			return errors.New("CODEX_FRONTEND_REQUIRES_CODEX_PRIMARY: use explicit provider handoff")
		}
		if value.FrontendSessionID == "" && frontend == value.Frontend {
			return errors.New("NATIVE_SESSION_MAPPING_UNAVAILABLE")
		}
		return a.OrchestratedWithKnowledge(context.WithValue(ctx, resumeSessionKey{}, id), frontend, value.PrimaryExecutor, nil, nil)
	}
	if override, _ := ctx.Value(resumeFrontendKey{}).(string); override != "" {
		return errors.New("DIRECT_SESSION_FRONTEND_SWITCH_NOT_SUPPORTED")
	}
	if value.Mode == session.ModeDirect && (value.PrimaryExecutor == "codex" || value.PrimaryExecutor == "claude" || value.PrimaryExecutor == "opencode") {
		return a.SessionStartWithKnowledge(context.WithValue(ctx, resumeSessionKey{}, id), value.PrimaryExecutor, value.Mode, nil, nil)
	}
	return errors.New("NATIVE_RESUME_NOT_SUPPORTED_FOR_SESSION")
}
