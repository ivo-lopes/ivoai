package cli

import (
	"bufio"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestConversationScopeCommandsPreviewAndRequireConfirmation(t *testing.T) {
	a, out := mcpTestApp(t)
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	id, _ := session.NewID()
	now := time.Now().UTC()
	v := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true, Coordinator: "native", SwarmID: "native_" + id, Frontend: "codex", FrontendSessionID: "native_fixture", PrimaryExecutor: "codex", InitialPlanner: "codex", CurrentPrimary: "codex", ExecutorSessionID: "thread_fixture", KnowledgeScopeID: "ks_old", WorkingDirectory: t.TempDir(), PrimaryModel: session.UnknownModel(), MaxWorkers: 1, State: session.StateCompleted, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected"}
	if err := store.Create(v); err != nil {
		t.Fatal(err)
	}
	if err := runSession(context.Background(), a, []string{"adopt-scope", id}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Existing native history is preserved") || !strings.Contains(out.String(), "--confirm") {
		t.Fatal("preview omitted transfer warning or confirmation")
	}
	got, _ := store.Get(id)
	if got.KnowledgeScopeID != "ks_old" {
		t.Fatal("preview adopted scope")
	}
	a.ProviderAccountReference = func(context.Context, string) (string, error) { t.Fatal("preview probed provider"); return "", nil }
	a.In = strings.NewReader("1\n7\n\nNO\n0\n0\n")
	menu := menuSession{ctx: context.Background(), app: a, reader: bufio.NewReader(a.In)}
	if _, err := menu.conversations(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ADOPT SCOPE") {
		t.Fatal("TUI omitted explicit scope confirmation")
	}
	got, _ = store.Get(id)
	if got.KnowledgeScopeID != "ks_old" {
		t.Fatal("TUI cancellation changed scope")
	}
	if err := runSession(context.Background(), a, []string{"adopt-scope", id, "--json"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"resume", id, "--knowledge-source"}, {"recover", id, "--knowledge-source"}, {"restore-scope", id}} {
		if err := runSession(context.Background(), a, args); err == nil {
			t.Fatalf("missing argument accepted: %v", args)
		}
	}
}
