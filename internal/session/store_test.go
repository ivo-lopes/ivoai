package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixtureSession(t *testing.T, root string) (Store, Session) {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	return Store{Root: root}, Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: ModeDirect, PrimaryExecutor: "codex", WorkingDirectory: t.TempDir(), PrimaryModel: UnknownModel(), Workers: []Worker{}, MaxWorkers: 2, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected", State: StateStarting}
}

func TestStoreRejectsTerminalEscapeAndSecretShapedStatusTampering(t *testing.T) {
	store, value := fixtureSession(t, t.TempDir()+"/sessions")
	value.ContextStatus = "ready\x1b[2J Authorization: Bearer secret"
	if err := store.Create(value); err == nil {
		t.Fatal("untrusted status metadata accepted")
	}
}

func TestStorePersistsOnlyPrivateAtomicMetadata(t *testing.T) {
	store, value := fixtureSession(t, t.TempDir()+"/sessions")
	if err := store.Create(value); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(value.SessionID)
	if err != nil || got.SessionID != value.SessionID {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	info, _ := os.Stat(filepath.Join(store.Root, value.SessionID+".json"))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	if _, err := store.Update(value.SessionID, func(current *Session) error { current.State = StateRunning; return nil }); err != nil {
		t.Fatal(err)
	}
	active, err := store.Active()
	if err != nil || len(active) != 1 {
		t.Fatalf("active=%d err=%v", len(active), err)
	}
}

func TestStoreRejectsSymlinkAndSensitiveOversizeState(t *testing.T) {
	root := t.TempDir()
	store, value := fixtureSession(t, filepath.Join(root, "sessions"))
	if err := os.MkdirAll(store.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(store.Root, value.SessionID+".json")); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(value); err == nil {
		t.Fatal("symlink session path accepted")
	}
	value.ContextStatus = strings.Repeat("x", maxStateBytes)
	if err := store.write(value); err == nil {
		t.Fatal("oversized state accepted")
	}
}

func TestModelPrecedenceAndUnknown(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config.toml")
	if err := os.WriteFile(config, []byte("model = 'configured-model'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ResolveModel("runtime-model", "argument-model", "codex", config); got.Source != ModelRuntimeVerified {
		t.Fatalf("runtime precedence: %#v", got)
	}
	if got := ResolveModel("", "argument-model", "codex", config); got.Source != ModelArgument {
		t.Fatalf("argument precedence: %#v", got)
	}
	if got := ResolveModel("", "", "codex", config); got.Source != ModelConfigured || got.Name != "configured-model" {
		t.Fatalf("configured precedence: %#v", got)
	}
	if got := ResolveModel("", "", "codex", filepath.Join(root, "missing")); got != UnknownModel() {
		t.Fatalf("unknown model: %#v", got)
	}
}
