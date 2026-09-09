package connections

import (
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/secrets"
)

func TestProfileMutationsSerializeAndPreservePersonalBindings(t *testing.T) {
	store := connStore(t.TempDir())
	cfg := config.Default()
	p := config.ServerProfile{ID: config.LegacyServerID, Alias: "default", Purpose: "default", Enabled: true, Status: "connected", URL: "https://legacy.example.invalid", ContextMCPURL: "https://legacy.example.invalid/context"}
	cfg.Connections.Servers["default"] = p
	cfg.Connections.Server = config.Connection{Status: "connected", URL: p.URL}
	personal := config.MCPServer{URL: "https://personal.example.invalid/context", Kind: "external", Enabled: true}
	cfg.MCP.Servers["ivoai-context"] = personal
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	private := secrets.Store{Path: store.Paths.Secrets}
	if err := private.Set(p.ID, secrets.ClientCredential{Token: "fixture-legacy"}); err != nil {
		t.Fatal(err)
	}
	c := ServerConnector{Store: store, Secrets: private}
	unlock, err := c.lockProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetEnabled("default", false); err == nil {
		t.Fatal("concurrent mutation was not rejected")
	}
	if _, err := c.ConnectProfile(context.Background(), ConnectOptions{Mode: EnrollmentCreate, Alias: "other", BaseURL: "http://127.0.0.1:1", Code: "fixture"}); err == nil {
		t.Fatal("enrollment ignored mutation lock")
	}
	unlock()
	if err := c.EditMetadata("default", ProfileMetadata{Purpose: "administrative", Priority: 10}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetEnabled("default", false); err != nil {
		t.Fatal(err)
	}
	got, _ := store.Load()
	if got.Connections.Servers["default"].ID != p.ID || got.Connections.Servers["default"].Enabled {
		t.Fatal("metadata/enable operation changed identity")
	}
	if err := c.DisconnectProfile("default"); err != nil {
		t.Fatal(err)
	}
	got, _ = store.Load()
	if len(got.Connections.Servers) != 0 || !reflect.DeepEqual(got.MCP.Servers["ivoai-context"], personal) {
		t.Fatal("removal damaged personal binding or resurrected default")
	}
	if data, _ := private.Load(); len(data.Servers) != 0 || data.Server != nil {
		t.Fatal("selected credential retained")
	}
}

func TestProfileMetadataFailureDoesNotTouchSecretsOrConfig(t *testing.T) {
	store := connStore(t.TempDir())
	if err := store.Save(config.Default()); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(store.Paths.Config)
	c := ServerConnector{Store: store, Secrets: secrets.Store{Path: store.Paths.Secrets}}
	for _, err := range []error{c.EditMetadata("missing", ProfileMetadata{Purpose: "valid"}), c.EditMetadata("missing", ProfileMetadata{Purpose: "bad\x1bname"}), c.SetEnabled("missing", true)} {
		if err == nil {
			t.Fatal("invalid mutation succeeded")
		}
	}
	after, _ := os.ReadFile(store.Paths.Config)
	if string(before) != string(after) {
		t.Fatal("invalid mutation changed config")
	}
	if _, err := os.Stat(store.Paths.Secrets); !os.IsNotExist(err) {
		t.Fatal("metadata mutation created/read-write secret store")
	}
}
