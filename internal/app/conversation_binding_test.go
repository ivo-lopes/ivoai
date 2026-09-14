package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestConversationFrontendSwitchReusesVerifiedProviderThread(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "codex-args")
	a := autoTestApp(t, root, "#!/bin/sh\nprintf '%s\\n' \"$@\" > "+shellArgument(marker)+"\n", "#!/bin/sh\nexit 1\n")
	a.ProviderAccountReference = func(context.Context, string) (string, error) { return "account_fixture", nil }
	a.QuotaManager = &quota.Manager{Store: quota.Store{Root: a.Store.Paths.QuotaDir}, Probes: map[quota.Provider]quota.Probe{
		quota.ProviderCodex:  probeFunc(func(context.Context) (quota.ProviderQuota, error) { return available(quota.ProviderCodex), nil }),
		quota.ProviderClaude: probeFunc(func(context.Context) (quota.ProviderQuota, error) { return exhausted(quota.ProviderClaude), nil }),
	}}
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	id, _ := session.NewID()
	now := time.Now().UTC()
	v := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true, Coordinator: "native", Frontend: "codex", FrontendSessionID: "thread_presentation", ExecutorSessionID: "thread_fixture", PrimaryExecutor: "codex", InitialPlanner: "codex", CurrentPrimary: "codex", WorkingDirectory: root, PrimaryModel: session.UnknownModel(), MaxWorkers: 2, State: session.StateCompleted, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected", SwarmID: "native_" + id,
		ExecutorSessions: map[string]session.ExecutorSessionMapping{"codex:thread_presentation": {Executor: "codex", ExecutorSessionID: "thread_fixture", AuthReference: "account_fixture", UpdatedAt: now}},
	}
	if err := store.Create(v); err != nil {
		t.Fatal(err)
	}
	if err := a.SessionResumeFrontend(context.Background(), id, "opencode"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(id)
	if err != nil || got.Frontend != "opencode" || got.FrontendSessions["codex"] != "thread_presentation" || got.ExecutorSessionID != "thread_fixture" {
		t.Fatal("frontend switch changed provider identity", err)
	}
	args, _ := os.ReadFile(marker)
	if !strings.Contains(string(args), "resume\n") || !strings.Contains(string(args), "thread_fixture\n") {
		t.Fatal("new frontend did not use native provider resume")
	}
	values, _ := store.List()
	if len(values) != 1 {
		t.Fatal("frontend switch duplicated IVOAI session")
	}
}

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
