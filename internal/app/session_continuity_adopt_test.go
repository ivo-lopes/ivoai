package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestConversationScopeConfirmationRejectsChangedPreview(t *testing.T) {
	for _, restore := range []bool{false, true} {
		t.Run(fmt.Sprintf("restore_%t", restore), func(t *testing.T) {
			root := t.TempDir()
			a := autoTestApp(t, root, "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 1\n")
			probes := 0
			a.ProviderAccountReference = func(context.Context, string) (string, error) { probes++; return "account", nil }
			store := session.Store{Root: a.Store.Paths.SessionsDir}
			id, _ := session.NewID()
			now := time.Now().UTC()
			v := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true, Coordinator: "native", State: session.StateCompleted, Frontend: "codex", FrontendSessionID: "presentation", PrimaryExecutor: "codex", InitialPlanner: "codex", CurrentPrimary: "codex", ExecutorSessionID: "thread", WorkingDirectory: root, KnowledgeScopeID: "ks_legacy", PrimaryModel: session.UnknownModel(), MaxWorkers: 2, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected", SwarmID: "native_" + id, ExecutorSessions: map[string]session.ExecutorSessionMapping{"codex:presentation": {Executor: "codex", ExecutorSessionID: "thread", AuthReference: "account", UpdatedAt: now}}}
			if err := store.Create(v); err != nil {
				t.Fatal(err)
			}
			var preview ScopeAdoption
			var backup string
			var err error
			if restore {
				backup, err = saveScopeBackup(store.Root, v)
				if err != nil {
					t.Fatal(err)
				}
				preview, err = a.SessionRestoreScope(context.Background(), id, backup, false, nil)
			} else {
				preview, err = a.SessionAdoptScope(context.Background(), id, nil, false, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			if restore {
				_, err = store.Update(id, func(current *session.Session) error { current.KnowledgeScopeID = "ks_changed"; return nil })
			} else {
				cfg, loadErr := a.Store.Load()
				if loadErr != nil {
					t.Fatal(loadErr)
				}
				cfg.Connections.Servers = map[string]config.ServerProfile{"new": {ID: "srv_new_source", Alias: "new", URL: "https://new.example", Enabled: true, Status: "connected", Purpose: "new"}}
				err = a.Store.Save(cfg)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := store.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			backupsBefore, _ := os.ReadDir(filepath.Join(store.Root, "scope-backups"))
			if restore {
				_, err = a.SessionRestoreScope(context.Background(), id, backup, true, &preview)
			} else {
				_, err = a.SessionAdoptScope(context.Background(), id, nil, true, &preview)
			}
			if err == nil || err.Error() != "SCOPE_ADOPTION_PREVIEW_CHANGED" {
				t.Fatalf("changed preview accepted or wrong error: %v", err)
			}
			after, err := store.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			backupsAfter, _ := os.ReadDir(filepath.Join(store.Root, "scope-backups"))
			if probes != 0 || len(backupsBefore) != len(backupsAfter) || !reflect.DeepEqual(before, after) {
				t.Fatal("rejected preview probed account, created backup or changed session")
			}
			// A fresh preview can be confirmed and changes only the binding.
			if restore {
				preview, err = a.SessionRestoreScope(context.Background(), id, backup, false, nil)
			} else {
				preview, err = a.SessionAdoptScope(context.Background(), id, nil, false, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			var applied ScopeAdoption
			if restore {
				applied, err = a.SessionRestoreScope(context.Background(), id, backup, true, &preview)
			} else {
				applied, err = a.SessionAdoptScope(context.Background(), id, nil, true, &preview)
			}
			if err != nil || !applied.Applied {
				t.Fatalf("fresh preview failed: %+v %v", applied, err)
			}
			after, err = store.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			before.KnowledgeScopeID, before.UpdatedAt = preview.ProposedScope, after.UpdatedAt
			if !reflect.DeepEqual(before, after) {
				t.Fatal("fresh confirmation changed more than the binding")
			}
		})
	}
}

func TestConversationScopeAdoptionRequiresProofAndPreservesBackup(t *testing.T) {
	root := t.TempDir()
	a := autoTestApp(t, root, "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 1\n")
	account := "verified-account"
	var proofErr error
	a.ProviderAccountReference = func(context.Context, string) (string, error) { return account, proofErr }
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	id, _ := session.NewID()
	now := time.Now().UTC()
	v := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true, Coordinator: "native", State: session.StateCompleted, Frontend: "codex", FrontendSessionID: "presentation", PrimaryExecutor: "codex", InitialPlanner: "codex", CurrentPrimary: "codex", ExecutorSessionID: "provider-thread", WorkingDirectory: root, KnowledgeScopeID: "ks_legacy", PrimaryModel: session.UnknownModel(), MaxWorkers: 2, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected", ExecutorSessions: map[string]session.ExecutorSessionMapping{"codex:presentation": {Executor: "codex", ExecutorSessionID: "provider-thread", AuthReference: account, UpdatedAt: now}}}
	v.SwarmID = "native_" + id
	if err := store.Create(v); err != nil {
		t.Fatal(err)
	}
	preview, err := a.SessionAdoptScope(context.Background(), id, nil, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := store.Get(id)
	if old.KnowledgeScopeID != v.KnowledgeScopeID || preview.Applied {
		t.Fatal("preview mutated history")
	}
	if _, err := store.Update(id, func(current *session.Session) error { current.ExecutorSessions = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SessionAdoptScope(context.Background(), id, nil, true, nil); err == nil {
		t.Fatal("missing provider mapping adopted")
	}
	if _, err := store.Update(id, func(current *session.Session) error { current.ExecutorSessions = v.ExecutorSessions; return nil }); err != nil {
		t.Fatal(err)
	}
	account = "different-account"
	if _, err := a.SessionAdoptScope(context.Background(), id, nil, true, nil); err == nil {
		t.Fatal("divergent account adopted")
	}
	account = ""
	if _, err := a.SessionAdoptScope(context.Background(), id, nil, true, nil); err == nil {
		t.Fatal("empty current account proof accepted")
	}
	account = "verified-account"
	proofErr = errors.New("identity probe failed")
	if _, err := a.SessionAdoptScope(context.Background(), id, nil, true, nil); err == nil {
		t.Fatal("failed current account proof accepted")
	}
	proofErr = nil
	result, err := a.SessionAdoptScope(context.Background(), id, nil, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := store.Get(id)
	if got.KnowledgeScopeID != preview.ProposedScope || got.ExecutorSessionID != v.ExecutorSessionID || got.FrontendSessionID != v.FrontendSessionID {
		t.Fatal("adoption replaced history identity")
	}
	if !result.Applied || !filepath.IsAbs(result.BackupPath) {
		t.Fatal("missing explicit adoption backup")
	}
	data, err := os.ReadFile(result.BackupPath)
	if err != nil || len(data) == 0 {
		t.Fatal("backup missing", err)
	}
	info, _ := os.Stat(result.BackupPath)
	if info.Mode().Perm() != 0600 {
		t.Fatal("backup is not private")
	}
	unsafe := filepath.Join(filepath.Dir(result.BackupPath), "unsafe.json")
	if err := os.Symlink(result.BackupPath, unsafe); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SessionRestoreScope(context.Background(), id, unsafe, true, nil); err == nil {
		t.Fatal("symlink backup accepted")
	}
	other := v
	other.SessionID, _ = session.NewID()
	otherBody, _ := json.Marshal(other)
	controls := v
	controls.KnowledgeScopeID = "ks_\x1b[2J"
	controlBody, _ := json.Marshal(controls)
	for name, body := range map[string][]byte{"malformed": []byte("{broken"), "different_identity": otherBody, "terminal_controls": controlBody} {
		t.Run(name, func(t *testing.T) {
			sum := sha256.Sum256(body)
			path := filepath.Join(filepath.Dir(result.BackupPath), fmt.Sprintf("%s-%x.json", id, sum[:16]))
			if err := os.WriteFile(path, body, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := a.SessionRestoreScope(context.Background(), id, path, false, nil); err == nil {
				t.Fatal("invalid backup preview accepted")
			}
			if _, err := a.SessionRestoreScope(context.Background(), id, path, true, nil); err == nil {
				t.Fatal("invalid backup restoration accepted")
			}
		})
	}
	wrongDigest := filepath.Join(filepath.Dir(result.BackupPath), id+"-00000000000000000000000000000000.json")
	if err := os.WriteFile(wrongDigest, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SessionRestoreScope(context.Background(), id, wrongDigest, true, nil); err == nil {
		t.Fatal("mismatched backup digest accepted")
	}
	if _, err := a.SessionAdoptScope(context.Background(), id, nil, true, nil); err != nil {
		t.Fatal("same-scope adoption not idempotent", err)
	}
	restored, err := a.SessionRestoreScope(context.Background(), id, result.BackupPath, false, nil)
	if err != nil || restored.Applied {
		t.Fatal("restore preview failed", err)
	}
	unchanged, _ := store.Get(id)
	if unchanged.KnowledgeScopeID != preview.ProposedScope {
		t.Fatal("restore preview mutated scope")
	}
	if _, err := a.SessionRestoreScope(context.Background(), id, result.BackupPath, true, nil); err != nil {
		t.Fatal(err)
	}
	reverted, _ := store.Get(id)
	if reverted.KnowledgeScopeID != v.KnowledgeScopeID || reverted.ExecutorSessionID != v.ExecutorSessionID {
		t.Fatal("mapping-only rollback failed")
	}

}
