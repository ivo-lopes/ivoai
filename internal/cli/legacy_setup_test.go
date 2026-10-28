package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/app"
)

func TestClientSetupRequiresLegacyOpenCodeOptInAndPreservesItAfterward(t *testing.T) {
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
	if err := Run(context.Background(), a, []string{"setup", "--mode", "client", "--legacy-opencode"}); err != nil {
		t.Fatal(err)
	}
	state, err = a.Store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	legacy := state.Components["opencode"]
	if !legacy.Installed || !strings.Contains(output.String(), "deprecated") {
		t.Fatal("explicit compatibility setup unavailable or silent")
	}
	before, err := os.ReadFile(legacy.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), a, []string{"setup", "--mode", "client"}); err != nil {
		t.Fatal(err)
	}
	state, err = a.Store.LoadState()
	if err != nil || state.Components["opencode"] != legacy {
		t.Fatal("default setup changed retained component", err)
	}
	after, err := os.ReadFile(legacy.Path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("default setup modified retained binary")
	}
	if err := Run(context.Background(), a, []string{"setup", "--mode", "server", "--legacy-opencode"}); err == nil || !strings.Contains(err.Error(), "client setup") {
		t.Fatal("legacy flag reached server setup", err)
	}
}
