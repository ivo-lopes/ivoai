package config

import (
	"os"
	"path/filepath"
	"testing"
)

func testPaths(root string) Paths {
	return Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), BinDir: filepath.Join(root, "bin"), Config: filepath.Join(root, "config", "config.toml"), State: filepath.Join(root, "state", "state.toml"), Secrets: filepath.Join(root, "config", "secrets.json"), Ownership: filepath.Join(root, "state", "ownership.toml"), HooksDir: filepath.Join(root, "data", "hooks")}
}

func TestStoreRoundTripAndPermissions(t *testing.T) {
	store := NewStore(testPaths(t.TempDir()))
	c := Default()
	c.Client.Profile = "work"
	if err := store.Save(c); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Client.Profile != "work" || got.Connections.ChatGPT.Status != "not-connected" {
		t.Fatalf("bad config: %#v", got)
	}
	info, _ := os.Stat(store.Paths.Config)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
}

func TestResolvePathsHonorsAbsoluteXDGOnly(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xc"))
	t.Setenv("XDG_DATA_HOME", "relative")
	p, err := ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	if p.ConfigDir != filepath.Join(root, "xc", "ivoai") {
		t.Fatalf("config %s", p.ConfigDir)
	}
	if p.DataDir != filepath.Join(root, ".local", "share", "ivoai") {
		t.Fatalf("data %s", p.DataDir)
	}
}
