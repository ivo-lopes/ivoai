package servercmd

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/secrets"
)

func TestRemoteRevalidatesStoredServerOrigin(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))

	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	store := config.NewStore(paths)
	cfg := config.Default()
	cfg.Connections.Server = config.Connection{Status: "connected", URL: "http://169.254.169.254"}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if err := (secrets.Store{Path: paths.Secrets}).Save(secrets.Data{Server: &secrets.ClientCredential{Token: "scoped-test-token"}}); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	r := &runner{out: &output, errOut: &output}
	err = r.remote(context.Background(), []string{"status"})
	if err == nil || !strings.Contains(err.Error(), "stored server URL is unsafe") {
		t.Fatalf("unsafe persisted remote origin was accepted: %v", err)
	}
}
