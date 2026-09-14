package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/promptgate"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestConversationHandoffExplicitBothProvidersAndNativeResume(t *testing.T) {
	for _, sourceProvider := range []string{"codex", "claude"} {
		t.Run(sourceProvider, func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(root, "provider-arguments")
			script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellArgument(marker) + "\n"
			a := sessionTestApp(t, root, appExecutable(t, root, "codex", script), appExecutable(t, root, "claude", script), appExecutable(t, root, "ruflo", "#!/bin/sh\nexit 0\n"))
			store := session.Store{Root: a.Store.Paths.SessionsDir}
			id, _ := session.NewID()
			native, _ := session.NewNativeUUID()
			now := time.Now().UTC()
			source := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeDirect, State: session.StateCompleted, PrimaryExecutor: sourceProvider, ExecutorSessionID: native, WorkingDirectory: root, PrimaryModel: session.UnknownModel(), MaxWorkers: 2, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected"}
			if err := store.Create(source); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveCheckpoint(id, session.Checkpoint{Objective: "Finish fixture documentation", Completed: []string{"fixture inspected"}, Acceptance: []string{"documentation describes fixture"}}); err != nil {
				t.Fatal(err)
			}
			to := "claude"
			if sourceProvider == "claude" {
				to = "codex"
			}
			if err := a.SessionHandoff(context.Background(), id, to, false); err == nil {
				t.Fatal("silent handoff")
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("provider ran before confirmation")
			}
			if err := a.SessionHandoff(context.Background(), id, to, true); err != nil {
				t.Fatal(err)
			}
			values, err := store.List()
			if err != nil || len(values) != 2 {
				t.Fatal("handoff family missing", err)
			}
			var destination session.Session
			for _, v := range values {
				if v.SessionID != id {
					destination = v
				}
			}
			if destination.Mode != session.ModeDirect || destination.PrimaryExecutor != to || destination.Lineage == nil || destination.Lineage.SourceSession != id || destination.ExecutorSessionID == native {
				t.Fatal("handoff confused with resume")
			}
			body, _ := json.Marshal(destination)
			if strings.Contains(string(body), "Finish fixture documentation") || strings.Contains(string(body), "Bounded HandoffBrief") {
				t.Fatal("prompt entered session metadata")
			}
			if err := a.SessionResume(context.Background(), id); err != nil {
				t.Fatal(err)
			}
			args, _ := os.ReadFile(marker)
			flag := "resume\n"
			if sourceProvider == "claude" {
				flag = "--resume\n"
			}
			if !strings.Contains(string(args), flag+native) {
				t.Fatal("same-provider resume not native")
			}
			values, _ = store.List()
			if len(values) != 2 {
				t.Fatal("resume duplicated logical session")
			}
		})
	}
}

func TestConversationRecoveryPromptRemainsStructured(t *testing.T) {
	for _, prompt := range []string{recoveryPrompt, handoffInput(session.HandoffBrief{Objective: "Finish fixture report", Acceptance: []string{"return the verified result"}})} {
		if result := promptgate.Assess(prompt); !result.Ready {
			t.Fatalf("control input failed normal admission: %v", result.Missing)
		}
	}
}
