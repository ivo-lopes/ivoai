package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/routing"
	"golang.org/x/sys/unix"
)

func TestNativeProviderMigrationGatePreservesCheckpointAndMetadata(t *testing.T) {
	store := Store{Root: t.TempDir()}
	id, _ := NewID()
	value := Session{SessionID: id, Frontend: "codex", PrimaryExecutor: "codex"}
	if err := store.CheckNativeProviders(value); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"primary", "current", "task", "worker"} {
		v := value
		switch kind {
		case "primary":
			v.PrimaryExecutor = "opencode"
		case "current":
			v.CurrentPrimary = "opencode"
		case "task":
			v.Tasks = []TaskMetadata{{Executor: "opencode"}}
		case "worker":
			v.Workers = []Worker{{Executor: "opencode", State: StateCompleted}}
		}
		if !errors.Is(store.CheckNativeProviders(v), ErrOpenCodeMigrationRequired) {
			t.Fatal("retained legacy executor admitted", kind)
		}
		v.Frontend = "opencode"
		if err := store.CheckNativeProviders(v); err != nil {
			t.Fatal("explicit legacy path rejected", err)
		}
	}
	plan := routing.Plan{ID: "plan_fixture", Strategy: "efficient", CreatedAt: time.Now(), Tasks: []routing.Task{{TaskInput: routing.TaskInput{ID: "t1", Role: "research", Task: "Inspect the fixture", Acceptance: []string{"report the finding"}}, Profile: routing.ExecutionProfile{Provider: "opencode", Tier: routing.TierLight}, Tier: routing.TierLight, State: "planned", ExecutionMode: "worker"}}}
	if err := store.SaveCheckpoint(id, Checkpoint{Recovery: &RecoveryPlan{Plan: plan, Directory: store.Root, Approved: true}}); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(store.Root, "checkpoints", id+".json")
	body, err := os.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(store.CheckNativeProviders(value), ErrOpenCodeMigrationRequired) {
		t.Fatal("retained checkpoint executor admitted")
	}
	after, _ := os.ReadFile(current)
	if string(after) != string(body) {
		t.Fatal("migration gate rewrote checkpoint")
	}
	legacy := filepath.Join(store.Root, "runtime", id, "checkpoint.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(current, legacy); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(store.CheckNativeProviders(value), ErrOpenCodeMigrationRequired) {
		t.Fatal("legacy checkpoint location bypassed migration gate")
	}
	after, _ = os.ReadFile(legacy)
	if string(after) != string(body) {
		t.Fatal("legacy checkpoint rewritten")
	}
}

func TestCanonicalCheckpointLoadRejectsFIFOWithoutBlocking(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		root := t.TempDir()
		store := Store{Root: root}
		id, _ := NewID()
		path := filepath.Join(root, "checkpoints", id+".json")
		if legacy {
			path = filepath.Join(root, "runtime", id, "checkpoint.json")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mkfifo(path, 0600); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := store.LoadCheckpoint(id); done <- err }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("FIFO admitted")
			}
		case <-time.After(time.Second):
			// Unblock the old implementation if this regression ever returns.
			fd, _ := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK, 0)
			if fd >= 0 {
				_ = unix.Close(fd)
			}
			t.Fatal("checkpoint loader blocked on a FIFO")
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, "target")
		if err := os.WriteFile(target, []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		if _, err := store.LoadCheckpoint(id); err == nil {
			t.Fatal("checkpoint symlink followed")
		}
	}
}
