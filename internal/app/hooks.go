package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ivo-lopes/ivoai/internal/memory"
)

// HookHealth never probes a lifecycle event: validation must not submit data or
// execute commands extracted from provider configuration.
func (a *App) HookHealth(repair bool) ([]memory.HookHealth, error) {
	return a.hookHealth(repair, nil)
}

func (a *App) hookHealth(repair bool, previous []string) ([]memory.HookHealth, error) {
	if a.Store == nil {
		return nil, errors.New("hook state unavailable")
	}
	state, err := a.Store.LoadState()
	if err != nil {
		return nil, err
	}
	ownership, err := a.Store.LoadOwnership()
	if err != nil {
		return nil, err
	}
	component, owned := state.Components["ai-memory"], ownership.Components["ai-memory"]
	managed := component.Managed && owned.Managed && component.Path != "" && component.Path == owned.Path
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	codexHome, claudeHome := os.Getenv("CODEX_HOME"), os.Getenv("CLAUDE_CONFIG_DIR")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	if claudeHome == "" {
		claudeHome = filepath.Join(home, ".claude")
	}
	result := []memory.HookHealth{}
	for _, provider := range []struct{ agent, path string }{{"codex", filepath.Join(codexHome, "hooks.json")}, {"claude-code", filepath.Join(claudeHome, "settings.json")}} {
		m := memory.HookMaintenance{Agent: provider.agent, ConfigPath: provider.path, Binary: component.Path, HooksDir: a.Store.Paths.HooksDir, DataDir: filepath.Join(filepath.Dir(a.Store.Paths.DataDir), "ai-memory"), Managed: managed, PreviousBinaries: previous}
		health, err := m.Inspect(repair)
		if err != nil {
			return nil, err
		}
		result = append(result, health...)
	}
	return result, nil
}

func (a *App) MemoryHooks(repair, jsonOutput bool, previous ...string) error {
	health, err := a.hookHealth(repair, previous)
	if err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(a.Out).Encode(health)
	}
	if len(health) == 0 {
		fmt.Fprintln(a.Out, "Hooks: no verified IVOAI-owned registrations")
		return nil
	}
	for _, h := range health {
		fmt.Fprintf(a.Out, "%s %s: %s owner=%s", h.Agent, h.Event, h.State, h.Owner)
		if h.Reason != "" {
			fmt.Fprintf(a.Out, " reason=%s", h.Reason)
		}
		fmt.Fprintln(a.Out)
	}
	return nil
}

func (a *App) preflightMemoryHooks() {
	if os.Getenv("IVOAI_TEST_MODE") == "1" {
		return
	}
	health, err := a.HookHealth(true)
	if err != nil {
		a.warn("IVOAI memory hooks degraded; run ivoai memory hooks validate", nil)
		return
	}
	for _, h := range health {
		if h.State != "healthy" {
			a.warn("IVOAI memory hooks degraded; run ivoai memory hooks repair", nil)
			return
		}
	}
}
