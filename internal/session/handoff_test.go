package session

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContinuityBriefBoundedAndPrivate(t *testing.T) {
	store, value := fixtureSession(t, t.TempDir())
	if err := store.Create(value); err != nil {
		t.Fatal(err)
	}
	for _, c := range []Checkpoint{{Objective: "safe", Constraints: []string{"Authorization: Bearer canary-secret"}}, {Objective: "safe", Acceptance: []string{"secret_key=canary-secret"}}} {
		if err := store.SaveCheckpoint(value.SessionID, c); err == nil {
			t.Fatal("credential accepted in continuity")
		}
	}
	if err := store.SaveCheckpoint(value.SessionID, Checkpoint{Objective: "Fixture continuation", Completed: []string{"bounded finding"}, Acceptance: []string{"return fixture version"}}); err != nil {
		t.Fatal(err)
	}
	brief, err := store.HandoffBrief(value.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(brief)
	if len(body) > 8192 || strings.Contains(string(body), "recovery") || strings.Contains(string(body), "canary-secret") {
		t.Fatal("unsafe handoff")
	}
}

func TestContinuityFrontendSwitchKeepsMappings(t *testing.T) {
	store, v := fixtureSession(t, t.TempDir())
	v.SetFrontend("opencode", "ses_fixture")
	v.SetFrontend("codex", "thread_fixture")
	if err := store.Create(v); err != nil {
		t.Fatal(err)
	}
	for frontend, id := range map[string]string{"codex": "thread_fixture", "opencode": "ses_fixture"} {
		found, err := store.FindFrontend(frontend, id, v.WorkingDirectory)
		if err != nil || found.SessionID != v.SessionID {
			t.Fatal("presentation changed identity", err)
		}
	}
}
