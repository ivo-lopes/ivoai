package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestV050CanonicalFixturesLoadWithoutMigration(t *testing.T) {
	fixtureRoot := filepath.Join("..", "..", "tests", "fixtures", "v0.5.0", "client")
	for _, configName := range []string{"config.toml", "config-connected.toml"} {
		t.Run(configName, func(t *testing.T) {
			root := t.TempDir()
			paths := Paths{
				ConfigDir: filepath.Join(root, "config"),
				StateDir:  filepath.Join(root, "state"),
				Config:    filepath.Join(root, "config", "config.toml"),
				State:     filepath.Join(root, "state", "state.toml"),
				Ownership: filepath.Join(root, "state", "ownership.toml"),
			}
			for _, dir := range []string{paths.ConfigDir, paths.StateDir} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			copyFixture(t, filepath.Join(fixtureRoot, configName), paths.Config)
			copyFixture(t, filepath.Join(fixtureRoot, "state.toml"), paths.State)
			copyFixture(t, filepath.Join(fixtureRoot, "ownership.toml"), paths.Ownership)

			store := NewStore(paths)
			cfg, err := store.Load()
			if err != nil || cfg.IVOAI.Version != ConfigSchemaVersion {
				t.Fatalf("config fixture: version=%d err=%v", cfg.IVOAI.Version, err)
			}
			state, err := store.LoadState()
			if err != nil || state.Schema != StateSchemaVersion || !state.Components["headroom"].Installed || !state.Components["ruflo"].Installed {
				t.Fatalf("state fixture: schema=%d err=%v", state.Schema, err)
			}
			ownership, err := store.LoadOwnership()
			if err != nil || ownership.Schema != OwnershipSchemaVersion || !ownership.Components["ivoai"].Managed {
				t.Fatalf("ownership fixture: schema=%d err=%v", ownership.Schema, err)
			}
		})
	}
}

func TestV050MixedOwnershipKeepsExternalProvidersUnmanaged(t *testing.T) {
	path := filepath.Join("..", "..", "tests", "fixtures", "v0.5.0", "client", "ownership-mixed.toml")
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ownershipPath := filepath.Join(stateDir, "ownership.toml")
	copyFixture(t, path, ownershipPath)
	store := NewStore(Paths{StateDir: stateDir, Ownership: ownershipPath})
	ownership, err := store.LoadOwnership()
	if err != nil {
		t.Fatal(err)
	}
	if ownership.Components["codex"].Managed || ownership.Components["claude-code"].Managed {
		t.Fatal("externally managed provider clients became IVOAI-owned")
	}
	if !ownership.Components["ai-memory"].Managed {
		t.Fatal("managed component ownership was lost")
	}
}

func copyFixture(t *testing.T, source, target string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
