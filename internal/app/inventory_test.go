package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
)

func TestSupportInventoryIsSanitizedAndSchemaAware(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("IVOAI_TEST_MODE", "1")
	t.Setenv("IVOAI_SERVER_ROOT", filepath.Join(root, "server-root"))
	executable := filepath.Join(root, "bin", "ivoai")
	if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	a, err := New("0.5.0", strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	a.ExecutablePath = executable
	if err := a.Store.Save(config.Default()); err != nil {
		t.Fatal(err)
	}
	state := config.State{Schema: config.StateSchemaVersion, Components: map[string]config.ComponentState{"codex": {Installed: true, Managed: false, Version: "fixture", Path: "/usr/bin/codex"}}}
	if err := a.Store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	ownership := config.Ownership{Schema: config.OwnershipSchemaVersion, Components: map[string]config.OwnedItem{"ivoai": {Managed: true, Path: executable}, "codex": {Managed: false, Path: "/usr/bin/codex"}}}
	if err := a.Store.SaveOwnership(ownership); err != nil {
		t.Fatal(err)
	}
	secret := "do-not-leak-inventory-secret"
	if err := os.WriteFile(a.Store.Paths.Secrets, []byte(`{"server_token":"`+secret+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	serverConfig := filepath.Join(os.Getenv("IVOAI_SERVER_ROOT"), "etc", "ivoai", "server.toml")
	if err := os.MkdirAll(filepath.Dir(serverConfig), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(serverConfig, []byte("protocol_version=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	value := a.SupportInventory(context.Background())
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(secret)) || bytes.Contains(data, []byte("server_token")) {
		t.Fatalf("inventory leaked secret material: %s", data)
	}
	if value.Schemas.Config != 1 || value.Schemas.State != 1 || value.Schemas.Ownership != 1 || value.ServerProtocol != 1 || value.InstallProvenance != "ivoai-managed" || len(value.Modes) != 2 {
		t.Fatalf("unexpected inventory: %+v", value)
	}
}

func TestV050ServerInventoryFixtureMatchesSupportSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "v0.5.0", "server", "inventory.json"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value Inventory
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("canonical server inventory fixture no longer matches the support schema: %v", err)
	}
	if len(value.Modes) != 1 || value.Modes[0] != "server" || value.ServerProtocol != 1 || value.Services["ivoai-gateway.service"] != "active" {
		t.Fatalf("incomplete server fixture: %+v", value)
	}
}
