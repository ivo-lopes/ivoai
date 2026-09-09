package app

import (
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestFrontendGracefulClosePreservesFailedPreThreadTurn(t *testing.T) {
	store := session.Store{Root: t.TempDir()}
	id, err := session.NewID()
	if err != nil {
		t.Fatal(err)
	}
	exit := 1
	now := time.Now().UTC()
	value := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now,
		Mode: session.ModeDirect, PrimaryExecutor: "codex", Frontend: "opencode",
		WorkingDirectory: t.TempDir(), PrimaryModel: session.UnknownModel(), MaxWorkers: 2,
		ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected",
		State: session.StateRunning, TurnState: "failed", ExecutorExitCode: &exit}
	if err := store.Create(value); err != nil {
		t.Fatal(err)
	}
	(&App{}).finishSession(store, id, session.StateCompleted, 0)
	got, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.FrontendState != session.StateCompleted || got.FrontendExitCode == nil || *got.FrontendExitCode != 0 {
		t.Fatal("frontend result was not retained")
	}
	if got.TurnState != "failed" || got.ExecutorExitCode == nil || *got.ExecutorExitCode != 1 || got.ExecutorSessionID != "" {
		t.Fatal("frontend exit overwrote pre-thread failure")
	}
}
