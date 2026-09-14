package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestConversationBindingPreservesIdentityAcrossNativePicker(t *testing.T) {
	store := session.Store{Root: filepath.Join(t.TempDir(), "sessions")}
	id, _ := session.NewID()
	now := time.Now().UTC()
	initial := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true, Coordinator: "native", Frontend: "codex", WorkingDirectory: t.TempDir(), PrimaryExecutor: "codex", InitialPlanner: "codex", CurrentPrimary: "codex", PrimaryModel: session.UnknownModel(), MaxWorkers: 2, State: session.StateStarting, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected", SwarmID: "native_" + id}
	if err := store.Create(initial); err != nil {
		t.Fatal(err)
	}
	lease, err := store.Acquire(id)
	if err != nil {
		t.Fatal(err)
	}
	b := newConversationBinding(store, initial, lease)
	defer b.Close()
	if err := b.Select("thread_a"); err != nil {
		t.Fatal(err)
	}
	if b.ID() != id {
		t.Fatal("initial logical identity changed")
	}
	if err := b.Select("thread_b"); err != nil {
		t.Fatal(err)
	}
	second := b.ID()
	if second == id {
		t.Fatal("new conversation overwrote prior identity")
	}
	if err := b.Select("thread_a"); err != nil {
		t.Fatal(err)
	}
	if b.ID() != id {
		t.Fatal("native resume changed logical identity")
	}
	if !b.Available("thread_a") || !b.Available("thread_b") || b.Available("external") {
		t.Fatal("native picker scope incorrect")
	}
	other, err := store.Acquire(id)
	if err == nil {
		_ = other.Close()
		t.Fatal("duplicate primary admitted")
	}
	if _, err := store.FindFrontend("codex", "thread_a", initial.WorkingDirectory); err != nil {
		t.Fatal(err)
	}
	values, err := store.List()
	if err != nil || len(values) != 2 {
		t.Fatal("unexpected conversation duplication")
	}
}
