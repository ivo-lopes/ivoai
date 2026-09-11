package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestOrchestratedFrontendSessionRollbackCompatibility(t *testing.T) {
	previous := os.Getenv("IVOAI_UPGRADE_PREVIOUS_BINARY")
	if previous == "" {
		t.Skip("provide verified v0.9.9 binary")
	}
	root := t.TempDir()
	for _, kind := range []string{"CONFIG", "DATA", "STATE", "CACHE"} {
		t.Setenv("XDG_"+kind+"_HOME", filepath.Join(root, kind))
	}
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	store := session.Store{Root: paths.SessionsDir}
	id, _ := session.NewID()
	now := time.Now().UTC()
	v := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true, InitialPlanner: "codex", CurrentPrimary: "codex", PrimaryExecutor: "codex", Frontend: "codex", Coordinator: "native", WorkingDirectory: root, PrimaryModel: session.UnknownModel(), State: session.StateCompleted, MaxWorkers: 2, MemoryStatus: "disabled", ContextStatus: "disabled", ServerStatus: "not-connected"}
	v.SwarmID = "native_" + id
	if err := store.Create(v); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(previous, "session", "list", "--json").Run(); err != nil {
		t.Fatal("previous binary cannot list sessions after rollback", err)
	}
	restored, err := store.Get(id)
	if err != nil || restored.Frontend != "codex" || restored.PrimaryProvider != "codex" {
		t.Fatal("reapply lost frontend metadata", err)
	}
	t.Log("V099_SESSION_ROLLBACK=PASS FRONTEND_METADATA_REAPPLY=PASS")
}
