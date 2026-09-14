package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestConversationTUIInspectAndHandoffCancellation(t *testing.T) {
	a, out := mcpTestApp(t)
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	id, _ := session.NewID()
	now := time.Now().UTC()
	v := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeDirect, PrimaryExecutor: "codex", ExecutorSessionID: "native_fixture", WorkingDirectory: t.TempDir(), PrimaryModel: session.UnknownModel(), MaxWorkers: 1, State: session.StateCompleted, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected"}
	if err := store.Create(v); err != nil {
		t.Fatal(err)
	}
	// Select conversation, inspect, handoff, Claude, decline confirmation, back.
	a.In = strings.NewReader("1\n1\n7\n1\nNO\n0\n0\n0\n")
	s := menuSession{ctx: context.Background(), app: a, reader: bufio.NewReader(a.In)}
	if _, err := s.conversations(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Resume native conversation", "Resume interrupted turn", "Handoff to another provider", "HANDOFF claude", "native_fixture"} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("missing TUI surface %s", expected)
		}
	}
	values, _ := store.List()
	if len(values) != 1 {
		t.Fatal("cancelled handoff created destination")
	}
	out.Reset()
	if err := writeSessions(out, values, true); err != nil {
		t.Fatal(err)
	}
	var listed []map[string]any
	if json.Unmarshal(out.Bytes(), &listed) != nil || len(listed) != 1 || listed[0]["resumable"] != true {
		t.Fatal("JSON continuity metadata missing")
	}
}
