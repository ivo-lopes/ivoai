package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/memory"
)

// RepairMemoryHooks has no server, authentication, MCP or global-reinstall side
// effects. Ownership and content checks happen before the first repair write.
func (a *App) RepairMemoryHooks(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return err
	}
	if !cfg.Memory.Enabled {
		return errors.New("memory hooks are disabled")
	}
	state, err := a.Store.LoadState()
	if err != nil {
		return err
	}
	component := state.Components["ai-memory"]
	if strings.HasSuffix(component.Version, "-fixture") {
		return nil
	}
	if !component.Installed || !component.Managed {
		return errors.New("hook repair requires an IVOAI-managed ai-memory installation")
	}
	return memory.RepairHooksForBinary(a.Store.Paths.HooksDir, component.Path)
}

// prepareMemoryHooks is process-local and never rewrites provider settings.
// The caller appends env to the launched process and defers cleanup.
func (a *App) prepareMemoryHooks() ([]string, func()) {
	cfg, err := a.Store.Load()
	if err != nil || !cfg.Memory.Enabled {
		return nil, func() {}
	}
	state, err := a.Store.LoadState()
	if err != nil {
		return nil, func() {}
	}
	component := state.Components["ai-memory"]
	if !component.Installed || strings.HasSuffix(component.Version, "-fixture") {
		return nil, func() {}
	}
	env, cleanup, issues := memory.NewHookSession(a.Store.Paths.HooksDir)
	if len(issues) > 0 && a.Err != nil {
		fmt.Fprintln(a.Err, "ivoai: optional memory hooks are degraded; run ivoai doctor --repair-hooks. Unrecognized hooks require explicit review.")
	}
	return env, cleanup
}

// Preserve the session latch while turn-local capabilities are replaced.
func inheritHookSessionEnvironment(environment, previous []string) []string {
	for _, entry := range previous {
		if value, ok := strings.CutPrefix(entry, "IVOAI_HOOK_SESSION_DIR="); ok {
			return setAppEnvironment(environment, "IVOAI_HOOK_SESSION_DIR", value)
		}
	}
	return environment
}
