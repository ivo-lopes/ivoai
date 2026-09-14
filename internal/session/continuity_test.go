package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContinuityLeaseAndReopen(t *testing.T) {
	store, value := fixtureSession(t, filepath.Join(t.TempDir(), "sessions"))
	value.State = StateCompleted
	value.Frontend, value.FrontendSessionID = "codex", "native-fixture"
	if err := store.Create(value); err != nil {
		t.Fatal(err)
	}
	lease, err := store.Acquire(value.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := store.Acquire(value.SessionID); err == nil {
		_ = other.Close()
		t.Fatal("duplicate resume admitted")
	}
	if _, err := store.Reopen(value.SessionID, "/wrong", ModeDirect); err == nil {
		t.Fatal("cwd mismatch admitted")
	}
	if _, err := store.Reopen(value.SessionID, value.WorkingDirectory, ModeAuto); err == nil {
		t.Fatal("mode converted")
	}
	got, err := store.Reopen(value.SessionID, value.WorkingDirectory, ModeDirect)
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != value.SessionID || got.FrontendSessionID != value.FrontendSessionID || got.CurrentPhase != "waiting_for_input" {
		t.Fatal("identity lost or last turn replayed")
	}
	if _, err := store.FindFrontend("codex", value.FrontendSessionID, value.WorkingDirectory); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindFrontend("codex", "external-native-id", value.WorkingDirectory); err == nil {
		t.Fatal("unmanaged thread imported")
	}
	_ = lease.Close()
	next, err := store.Acquire(value.SessionID)
	if err != nil {
		t.Fatal("lease not released")
	}
	_ = next.Close()
}

func TestContinuityStalePIDAndUnsafeLease(t *testing.T) {
	store, value := fixtureSession(t, filepath.Join(t.TempDir(), "sessions"))
	value.PrimaryPID, value.PrimaryProcessStart = os.Getpid(), "wrong-process-start"
	if err := store.Create(value); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reopen(value.SessionID, value.WorkingDirectory, ModeDirect); err != nil {
		t.Fatal("reused PID blocked resume", err)
	}
	_, err := store.Update(value.SessionID, func(v *Session) error {
		v.PrimaryPID = os.Getpid()
		v.PrimaryProcessStart = ProcessStart(os.Getpid())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reopen(value.SessionID, value.WorkingDirectory, ModeDirect); err == nil {
		t.Fatal("live primary duplicated")
	}
	if err := os.MkdirAll(filepath.Join(store.Root, "leases"), 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "not-a-lease")
	if err := os.WriteFile(target, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(store.Root, "leases", value.SessionID+".lock")); err != nil {
		t.Fatal(err)
	}
	if lease, err := store.Acquire(value.SessionID); err == nil {
		_ = lease.Close()
		t.Fatal("symlink lease accepted")
	}
}
