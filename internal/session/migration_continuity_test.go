package session

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPreviousPublishedBinaryReadsContinuityState(t *testing.T) {
	previous := os.Getenv("IVOAI_PREVIOUS_BINARY")
	if previous == "" {
		t.Skip("set IVOAI_PREVIOUS_BINARY to the certified v0.10.2 asset")
	}
	root := t.TempDir()
	for _, kind := range []string{"CONFIG", "DATA", "STATE", "CACHE"} {
		t.Setenv("XDG_"+kind+"_HOME", filepath.Join(root, kind))
	}
	store, value := fixtureSession(t, filepath.Join(root, "STATE", "ivoai", "sessions"))
	value.State = StateCompleted
	value.SetFrontend("opencode", "ses_fixture")
	value.SetFrontend("codex", "thread_fixture")
	value.ExecutorSessionID = "provider_fixture"
	if err := store.Create(value); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCheckpoint(value.SessionID, Checkpoint{Objective: "Preserve fixture state", Acceptance: []string{"return fixture metadata"}}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(store.path(value.SessionID))
	for _, args := range [][]string{{"session", "list", "--json"}, {"session", "show", "--json", value.SessionID}} {
		body, err := exec.Command(previous, args...).Output()
		if err != nil {
			t.Fatal("previous published reader failed", err)
		}
		var entries []Session
		if json.Unmarshal(body, &entries) != nil || len(entries) != 1 || entries[0].SessionID != value.SessionID || entries[0].ExecutorSessionID != value.ExecutorSessionID {
			t.Fatal("previous reader lost identity")
		}
	}
	after, _ := os.ReadFile(store.path(value.SessionID))
	if string(before) != string(after) {
		t.Fatal("rollback reader mutated state")
	}
	if _, err := store.LoadCheckpoint(value.SessionID); err != nil {
		t.Fatal("reapply checkpoint failed", err)
	}
}

func TestLegacyCheckpointPromotionAndAdditiveReapply(t *testing.T) {
	store, value := fixtureSession(t, t.TempDir())
	if err := store.Create(value); err != nil {
		t.Fatal(err)
	}
	runtime := filepath.Join(store.Root, "runtime", value.SessionID)
	if err := os.MkdirAll(runtime, 0700); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"objective":"bounded fixture objective","completed":["task one"],"next_step":"verify task two","interrupted":true}`)
	if err := os.WriteFile(filepath.Join(runtime, "checkpoint.json"), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := store.LoadCheckpoint(value.SessionID)
	if err != nil || before.Objective != "bounded fixture objective" {
		t.Fatal("v0.10.2 checkpoint unreadable", err)
	}
	if err := store.CleanupRuntime(value.SessionID); err != nil {
		t.Fatal(err)
	}
	after, err := store.LoadCheckpoint(value.SessionID)
	if err != nil || after.Objective != before.Objective || len(after.Completed) != 1 {
		t.Fatal("cleanup lost legacy checkpoint", err)
	}
	value.SetFrontend("opencode", "ses_fixture")
	value.SetFrontend("codex", "thread_fixture")
	if _, err := store.Update(value.SessionID, func(v *Session) error { *v = value; return nil }); err != nil {
		t.Fatal(err)
	}
	// Old readers ignore additive keys. Simulate an old writer retaining its
	// known metadata while the new durable checkpoint remains untouched.
	body, err := os.ReadFile(store.path(value.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	var old map[string]json.RawMessage
	if json.Unmarshal(body, &old) != nil {
		t.Fatal("invalid additive session")
	}
	delete(old, "frontend_sessions")
	delete(old, "recovery_requested")
	delete(old, "lineage")
	body, _ = json.Marshal(old)
	if err := os.WriteFile(store.path(value.SessionID), body, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(value.SessionID)
	if err != nil || got.SessionID != value.SessionID || got.FrontendSessionID != "thread_fixture" {
		t.Fatal("rollback/reapply broke old identity", err)
	}
	if _, err := store.LoadCheckpoint(value.SessionID); err != nil {
		t.Fatal("reapply lost durable checkpoint", err)
	}
}
