package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestClientSetupRejectsRemovedFlagAndPreservesRetainedOpenCode(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("IVOAI_TEST_MODE", "1")
	for _, item := range []struct{ key, dir string }{{"XDG_CONFIG_HOME", "config"}, {"XDG_DATA_HOME", "data"}, {"XDG_STATE_HOME", "state"}, {"XDG_CACHE_HOME", "cache"}} {
		t.Setenv(item.key, filepath.Join(root, item.dir))
	}
	var output bytes.Buffer
	a, err := app.New("test", strings.NewReader(""), &output, &output)
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), a, []string{"setup", "--mode", "client"}); err != nil {
		t.Fatal(err)
	}
	state, err := a.Store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := state.Components["opencode"]; ok {
		t.Fatal("default CLI setup installed legacy component")
	}
	// Seed retained metadata/files from a previous installation. The new Core
	// must neither execute nor repair nor remove any of them.
	legacyPath := filepath.Join(root, "retained-opencode")
	if err := os.WriteFile(legacyPath, []byte("retained binary"), 0700); err != nil {
		t.Fatal(err)
	}
	legacy := config.ComponentState{Installed: true, Managed: true, Version: "retained", Path: legacyPath}
	state.Components["opencode"] = legacy
	if err := a.Store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(a.Store.Paths.State)
	for _, mode := range []string{"client", "server"} {
		if err := Run(context.Background(), a, []string{"setup", "--mode", mode, "--legacy-opencode"}); !errors.Is(err, session.ErrOpenCodeManagedDeprecated) {
			t.Fatal("removed setup flag admitted", err)
		}
		after, _ := os.ReadFile(a.Store.Paths.State)
		if !bytes.Equal(before, after) {
			t.Fatal("removed flag changed state")
		}
	}
	if err := Run(context.Background(), a, []string{"setup", "--mode", "client"}); err != nil {
		t.Fatal(err)
	}
	state, err = a.Store.LoadState()
	if err != nil || state.Components["opencode"] != legacy {
		t.Fatal("default setup changed retained component", err)
	}
	after, err := os.ReadFile(legacy.Path)
	if err != nil || string(after) != "retained binary" {
		t.Fatal("default setup modified retained binary")
	}
}
