package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyCredentialMigratesWithoutReenrollment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	if err := os.WriteFile(path, []byte(`{"server":{"token":"legacy-secret","client_id":"legacy-client","scopes":["context:read"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := Store{Path: path}
	data, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if data.Schema != SchemaVersion || data.Servers[legacyServerID].Token != "legacy-secret" {
		t.Fatalf("legacy migration failed: %#v", data)
	}
	if err := store.Save(data); err != nil {
		t.Fatal(err)
	}
	roundTrip, err := store.Load()
	if err != nil || roundTrip.Server == nil || roundTrip.Server.Token != "legacy-secret" {
		t.Fatalf("v0.5 rollback mirror failed: %#v err=%v", roundTrip, err)
	}
}

func TestIndependentServerCredentials(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "secrets.json")}
	if err := store.Set("srv_a000", ClientCredential{Token: "token-a", ClientID: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("srv_b000", ClientCredential{Token: "token-b", ClientID: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove("srv_a000"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.Get("srv_a000"); ok {
		t.Fatal("removed credential remains")
	}
	if got, ok, err := store.Get("srv_b000"); err != nil || !ok || got.Token != "token-b" {
		t.Fatalf("unrelated credential changed: %#v ok=%v err=%v", got, ok, err)
	}
}

func TestMultiServerSecretStoreRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "secrets.json")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := (Store{Path: path}).Load(); err == nil {
		t.Fatal("symlink secret store accepted")
	}
}
