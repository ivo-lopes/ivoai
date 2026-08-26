package migration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func transactionFixture(t *testing.T) (Manager, string, string) {
	t.Helper()
	root := t.TempDir()
	managed := filepath.Join(root, "managed")
	if err := os.MkdirAll(managed, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(managed, "config.toml")
	optionalPath := filepath.Join(managed, "created-later.toml")
	if err := os.WriteFile(configPath, []byte("schema=1\nvalue='before'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := Manager{Root: filepath.Join(root, "updates"), Files: []FileSpec{
		{Name: "config", Artifact: ArtifactConfig, Path: configPath, Root: managed},
		{Name: "optional", Artifact: ArtifactState, Path: optionalPath, Root: managed, Optional: true},
	}}
	return manager, configPath, optionalPath
}

func TestTransactionSnapshotsAndRollbackRestoresExactState(t *testing.T) {
	manager, configPath, optionalPath := transactionFixture(t)
	tx, err := manager.Begin(context.Background(), "v0.5.0", "dev", Schemas{ArtifactConfig: 1, ArtifactState: 1}, Schemas{ArtifactConfig: 1, ArtifactState: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("schema=1\nvalue='after'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(optionalPath, []byte("created=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := tx.MarkPromoted(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(configPath)
	if string(data) != "schema=1\nvalue='before'\n" {
		t.Fatalf("config not restored: %s", data)
	}
	if _, err := os.Stat(optionalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file absent before update was not removed: %v", err)
	}
	rolled, err := manager.RollbackLast(context.Background())
	if err != nil || !rolled {
		t.Fatalf("repeated rollback is not safe: rolled=%t err=%v", rolled, err)
	}
	if info, err := os.Stat(manager.Root); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("update root permissions: info=%v err=%v", info, err)
	}
	journalInfo, _ := os.Stat(filepath.Join(manager.Root, "current.json"))
	if journalInfo.Mode().Perm() != 0o600 {
		t.Fatalf("journal mode=%o", journalInfo.Mode().Perm())
	}
}

func TestInterruptedTransactionIsRecovered(t *testing.T) {
	manager, configPath, _ := transactionFixture(t)
	tx, err := manager.Begin(context.Background(), "v0.5.0", "dev", Schemas{ArtifactConfig: 1, ArtifactState: 1}, Schemas{ArtifactConfig: 1, ArtifactState: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("interrupted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := tx.MarkPromoted(); err != nil {
		t.Fatal(err)
	}
	releaseLock(tx.lock)
	tx.lock = nil
	recovered, err := manager.Recover(context.Background())
	if err != nil || !recovered {
		t.Fatalf("recovery=%t err=%v", recovered, err)
	}
	data, _ := os.ReadFile(configPath)
	if !strings.Contains(string(data), "before") {
		t.Fatalf("interrupted state not restored: %s", data)
	}
}

func TestNeedsRecoveryIsReadOnly(t *testing.T) {
	manager, _, _ := transactionFixture(t)
	tx, err := manager.Begin(context.Background(), "v0.5.0", "dev", Schemas{ArtifactConfig: 1, ArtifactState: 1}, Schemas{ArtifactConfig: 1, ArtifactState: 1})
	if err != nil {
		t.Fatal(err)
	}
	if needed, err := manager.NeedsRecovery(); err != nil || !needed {
		t.Fatalf("prepared recovery state: needed=%t err=%v", needed, err)
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	if needed, err := manager.NeedsRecovery(); err != nil || needed {
		t.Fatalf("terminal recovery state: needed=%t err=%v", needed, err)
	}
}

func TestTransactionDetectsCorruptJournalAndConcurrentUpdate(t *testing.T) {
	manager, _, _ := transactionFixture(t)
	tx, err := manager.Begin(context.Background(), "v0.5.0", "dev", Schemas{ArtifactConfig: 1, ArtifactState: 1}, Schemas{ArtifactConfig: 1, ArtifactState: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Begin(context.Background(), "dev", "next", Schemas{ArtifactConfig: 1, ArtifactState: 1}, Schemas{ArtifactConfig: 1, ArtifactState: 1}); err == nil || !strings.Contains(err.Error(), "another ivoai update") {
		t.Fatalf("concurrent transaction accepted: %v", err)
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(manager.Root, "current.json")
	valid, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, append(valid, []byte("{}")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Recover(context.Background()); err == nil || !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("trailing journal data accepted: %v", err)
	}
	if err := os.WriteFile(journalPath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Recover(context.Background()); err == nil || !strings.Contains(err.Error(), "corrupted update journal") {
		t.Fatalf("corrupt journal accepted: %v", err)
	}
}

func TestTransactionRejectsSymlinkAndEscapingPath(t *testing.T) {
	root := t.TempDir()
	managed := filepath.Join(root, "managed")
	if err := os.MkdirAll(managed, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(managed, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for name, spec := range map[string]FileSpec{
		"symlink": {Name: "link", Artifact: ArtifactConfig, Path: link, Root: managed},
		"escape":  {Name: "escape", Artifact: ArtifactConfig, Path: target, Root: managed},
	} {
		t.Run(name, func(t *testing.T) {
			manager := Manager{Root: filepath.Join(root, "updates-"+name), Files: []FileSpec{spec}}
			if _, err := manager.Begin(context.Background(), "old", "new", Schemas{ArtifactConfig: 1}, Schemas{ArtifactConfig: 1}); err == nil {
				t.Fatal("unsafe snapshot path accepted")
			}
		})
	}
}

func TestTransactionRejectsOversizedOrInsufficientSnapshotSpace(t *testing.T) {
	for name, configure := range map[string]func(*Manager){
		"bounded storage": func(manager *Manager) { manager.MaxSnapshotBytes = 4 },
		"disk preflight": func(manager *Manager) {
			manager.AvailableDiskBytes = func(string) (uint64, error) { return 1, nil }
		},
		"permission preflight": func(manager *Manager) {
			manager.CheckWritableRoot = func(string) error { return os.ErrPermission }
		},
	} {
		t.Run(name, func(t *testing.T) {
			manager, _, _ := transactionFixture(t)
			configure(&manager)
			if _, err := manager.Begin(context.Background(), "v0.5.0", "dev", Schemas{ArtifactConfig: 1, ArtifactState: 1}, Schemas{ArtifactConfig: 1, ArtifactState: 1}); err == nil {
				t.Fatal("unsafe snapshot preflight was accepted")
			}
			if _, err := os.Stat(filepath.Join(manager.Root, "snapshots")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("snapshot was created before preflight completed: %v", err)
			}
		})
	}
}

func TestRollbackUsesJournaledManagedPathAfterManifestChanges(t *testing.T) {
	manager, configPath, _ := transactionFixture(t)
	managedRoot := filepath.Dir(configPath)
	tx, err := manager.Begin(context.Background(), "v0.5.0", "dev", Schemas{ArtifactConfig: 1, ArtifactState: 1}, Schemas{ArtifactConfig: 1, ArtifactState: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := tx.MarkPromoted(); err != nil {
		t.Fatal(err)
	}
	releaseLock(tx.lock)
	tx.lock = nil

	recovery := manager
	recovery.Files = recovery.Files[1:] // Simulate metadata no longer listing config.
	recovery.AllowedRoots = []string{managedRoot}
	recovered, err := recovery.Recover(context.Background())
	if err != nil || !recovered {
		t.Fatalf("recovery=%t err=%v", recovered, err)
	}
	data, _ := os.ReadFile(configPath)
	if !strings.Contains(string(data), "before") {
		t.Fatalf("journaled managed path was not restored: %s", data)
	}
}

func TestCommittedRollbackRefusesDriftUnlessForced(t *testing.T) {
	manager, configPath, _ := transactionFixture(t)
	tx, err := manager.Begin(context.Background(), "v0.5.0", "dev", Schemas{ArtifactConfig: 1, ArtifactState: 1}, Schemas{ArtifactConfig: 1, ArtifactState: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("operator change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RollbackLast(context.Background()); err == nil || !strings.Contains(err.Error(), "changed after update") {
		t.Fatalf("drift was overwritten without force: %v", err)
	}
	data, _ := os.ReadFile(configPath)
	if string(data) != "operator change\n" {
		t.Fatal("refused rollback changed the operator file")
	}
	if rolled, err := manager.RollbackLastForce(context.Background()); err != nil || !rolled {
		t.Fatalf("forced rollback=%t err=%v", rolled, err)
	}
}
