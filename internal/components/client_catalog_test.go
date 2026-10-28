package components

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
)

func TestClientSetupCatalogSkipsLegacyAndPreservesRetainedFilesAndOwnership(t *testing.T) {
	for _, retained := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "retained"}[retained], func(t *testing.T) {
			t.Setenv("IVOAI_TEST_MODE", "1")
			root := t.TempDir()
			paths := config.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), Config: filepath.Join(root, "config", "config.toml"), State: filepath.Join(root, "state", "state.toml"), Ownership: filepath.Join(root, "state", "ownership.toml")}
			store := config.NewStore(paths)
			if err := store.Save(config.Default()); err != nil {
				t.Fatal(err)
			}
			legacy := config.ComponentState{Installed: true, Managed: false, Version: "retained-user-version", Path: filepath.Join(root, "user-opencode")}
			owned := config.OwnedItem{Managed: false, Path: legacy.Path}
			state := config.State{Schema: config.SchemaVersion, Components: map[string]config.ComponentState{}}
			ownership := config.Ownership{Schema: config.SchemaVersion, Components: map[string]config.OwnedItem{}}
			if retained {
				state.Components["opencode"] = legacy
				ownership.Components["opencode"] = owned
			}
			if err := store.SaveState(state); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveOwnership(ownership); err != nil {
				t.Fatal(err)
			}
			files := []string{legacy.Path, filepath.Join(root, "personal-opencode-config"), filepath.Join(root, "personal-opencode-history")}
			for _, path := range files {
				if err := os.WriteFile(path, []byte("retained user data"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			installer := Installer{Store: store, Catalog: ClientSetupCatalog(false)}
			if err := installer.Setup(context.Background()); err != nil {
				t.Fatal(err)
			}
			after, err := store.LoadState()
			if err != nil {
				t.Fatal(err)
			}
			afterOwned, err := store.LoadOwnership()
			if err != nil {
				t.Fatal(err)
			}
			if retained {
				if after.Components["opencode"] != legacy || !reflect.DeepEqual(afterOwned.Components["opencode"], owned) {
					t.Fatal("retained legacy state/ownership changed")
				}
			} else {
				if _, ok := after.Components["opencode"]; ok {
					t.Fatal("default setup installed legacy component")
				}
			}
			for _, path := range files {
				body, err := os.ReadFile(path)
				if err != nil || string(body) != "retained user data" {
					t.Fatal("personal file changed")
				}
			}
			for _, spec := range ClientSetupCatalog(false) {
				if spec.Name == "opencode" {
					t.Fatal("legacy component in native setup catalog")
				}
			}
			legacyFound := false
			for _, spec := range ClientSetupCatalog(true) {
				legacyFound = legacyFound || spec.Name == "opencode"
			}
			if !legacyFound {
				t.Fatal("explicit compatibility catalog removed")
			}
		})
	}
}
