package session

import (
	"encoding/json"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"strings"
	"testing"
)

func TestConsoleSnapshotBoundedMetadataOnly(t *testing.T) {
	id, _ := NewID()
	s := Session{SessionID: id, PlanID: "plan_fixture", Tasks: []TaskMetadata{{ID: "task_one", State: StateRunning, Dependencies: []string{"task_zero"}, AllowedMCPTools: map[string][]string{"plane": {"read_tool"}}}}, Workers: []Worker{{TaskID: "task_one", WorktreePath: "/private/raw-provider-transcript-canary"}}}
	for i := 0; i < 200; i++ {
		if err := AppendObservation(&s, observability.Event{Category: observability.CategoryWorker, Operation: observability.OperationWorkerLifecycle, State: observability.StateRunning, TaskID: "task_one"}); err != nil {
			t.Fatal(err)
		}
	}
	v := s.ConsoleSnapshot()
	if v.Sequence != 200 || len(v.Events) != 128 || v.Events[0].Sequence != 73 || v.Events[127].Sequence != 200 {
		t.Fatal("event cursor or ring bound incorrect")
	}
	if !v.Tasks[0].Worktree || v.Tasks[0].Integrated || v.Tasks[0].MCPTools["plane"][0] != "read_tool" {
		t.Fatal("operational projection missing")
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "raw-provider-transcript-canary") || strings.Contains(string(b), "/private") {
		t.Fatal("private content crossed console boundary")
	}
	s.Tasks[0].ID = "Bearer credential-canary"
	if s.ConsoleSnapshot().Tasks[0].ID != "redacted" {
		t.Fatal("untrusted label admitted")
	}
}
