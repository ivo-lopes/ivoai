package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestConversationBindingRejectsDifferentKnowledgeScope(t *testing.T) {
	for _, targetScope := range []string{"ks_other", ""} {
		t.Run("target_"+targetScope, func(t *testing.T) {
			store := session.Store{Root: filepath.Join(t.TempDir(), "sessions")}
			id, _ := session.NewID()
			now := time.Now().UTC()
			current := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true, Coordinator: "native", Frontend: "codex", FrontendSessionID: "thread_current", WorkingDirectory: t.TempDir(), KnowledgeScopeID: "ks_current", PrimaryExecutor: "codex", InitialPlanner: "codex", CurrentPrimary: "codex", PrimaryModel: session.UnknownModel(), MaxWorkers: 2, State: session.StateCompleted, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected", SwarmID: "native_" + id}
			if err := store.Create(current); err != nil {
				t.Fatal(err)
			}
			target := current
			target.SessionID, _ = session.NewID()
			target.FrontendSessionID, target.KnowledgeScopeID = "thread_target", targetScope
			if err := store.Create(target); err != nil {
				t.Fatal(err)
			}
			lease, err := store.Acquire(id)
			if err != nil {
				t.Fatal(err)
			}
			binding := newConversationBinding(store, current, lease)
			defer binding.Close()
			if binding.Available("thread_target") {
				t.Error("different or unknown scope exposed in picker")
			}
			if err := binding.Select("thread_target"); err == nil {
				t.Error("different or unknown scope selected")
			}
			if binding.ID() != id {
				t.Error("rejected selection changed binding")
			}
		})
	}
}

func TestConversationNewThreadInheritsKnowledgeScope(t *testing.T) {
	store := session.Store{Root: filepath.Join(t.TempDir(), "sessions")}
	id, _ := session.NewID()
	now := time.Now().UTC()
	current := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true, Coordinator: "native", Frontend: "codex", FrontendSessionID: "thread_current", WorkingDirectory: t.TempDir(), KnowledgeScopeID: "ks_current", PrimaryExecutor: "codex", InitialPlanner: "codex", CurrentPrimary: "codex", PrimaryModel: session.UnknownModel(), MaxWorkers: 2, State: session.StateCompleted, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected", SwarmID: "native_" + id}
	if err := store.Create(current); err != nil {
		t.Fatal(err)
	}
	lease, err := store.Acquire(id)
	if err != nil {
		t.Fatal(err)
	}
	binding := newConversationBinding(store, current, lease)
	defer binding.Close()
	if err := binding.Select("thread_new"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(binding.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.KnowledgeScopeID != current.KnowledgeScopeID {
		t.Fatal("new conversation lost organization scope")
	}
}
