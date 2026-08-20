package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecretStorePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "secrets.json")
	store := Store{Path: path}
	if err := store.Save(Data{Server: &ClientCredential{Token: "never-print", ClientID: "client"}}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Server.Token != "never-print" {
		t.Fatal("token did not round trip")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("expected insecure mode rejection")
	}
}

func TestSecretStoreRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.json")
	if err := os.WriteFile(target, []byte(`{"server":{"token":"secret"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "secrets.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := (Store{Path: link}).Load(); err == nil {
		t.Fatal("secret store followed a symlink")
	}
}

func TestSecretStoreRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate((1 << 20) + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := (Store{Path: path}).Load(); err == nil {
		t.Fatal("secret store accepted oversized input")
	}
}
