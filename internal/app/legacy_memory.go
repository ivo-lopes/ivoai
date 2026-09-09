package app

import (
	"os"
	"path/filepath"

	"github.com/ivo-lopes/ivoai/internal/memory"
)

// Called only at setup/update boundaries, before hook installation can create
// new ownership evidence. Launches never mutate the user's global Codex config.
func (a *App) migrateLegacyMemory() error {
	state, err := a.Store.LoadState()
	if err != nil {
		return err
	}
	ownership, err := a.Store.LoadOwnership()
	if err != nil {
		return err
	}
	component, owned := state.Components["ai-memory"], ownership.Components["ai-memory"]
	previous := !state.SetupCompletedAt.IsZero() && component.Managed && owned.Managed && component.Path != "" && component.Path == owned.Path
	if !previous {
		return nil
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		codexHome = filepath.Join(home, ".codex")
	}
	_, err = (memory.LegacyCodexMigration{ConfigPath: filepath.Join(codexHome, "config.toml"), HooksPath: filepath.Join(codexHome, "hooks.json"), OwnedHooksDir: a.Store.Paths.HooksDir, ReceiptPath: filepath.Join(a.Store.Paths.StateDir, "migrations", "ivoai-126.json"), PreviouslyManaged: previous}).Apply()
	return err
}
