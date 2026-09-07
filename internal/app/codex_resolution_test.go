package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/codexresolver"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestPinnedCodexRejectsMidSessionReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(path, []byte("version1"), 0700); err != nil {
		t.Fatal(err)
	}
	hash, _ := codexresolver.Fingerprint(path)
	value := session.Session{CodexPath: path, CodexVersion: "0.153.4", CodexSHA256: hash}
	state := config.State{Components: map[string]config.ComponentState{"codex": {Path: "/managed/old/codex", Version: "0.148.0", Managed: true}}}
	pinned, err := pinnedCodexState(state, value)
	if err != nil || pinned.Components["codex"].Path != path {
		t.Fatalf("worker did not inherit session selection: %v", err)
	}
	if err := os.WriteFile(path, []byte("version2"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := pinnedCodexState(state, value); err == nil || !strings.Contains(err.Error(), "CODEX_SESSION_EXECUTABLE_CHANGED") {
		t.Fatalf("silent session upgrade: %v", err)
	}
}

func TestCodexUpdateRefusesActiveFrontend(t *testing.T) {
	root := t.TempDir()
	a := sessionTestApp(t, root, "/fixture/codex", "/fixture/claude", "/fixture/ruflo")
	now := time.Now().UTC()
	id, _ := session.NewID()
	value := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, State: session.StateRunning, Mode: session.ModeDirect, PrimaryExecutor: "codex", WorkingDirectory: root, PrimaryModel: session.UnknownModel(), MaxWorkers: 3, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected", FrontendPID: os.Getpid(), FrontendProcessStart: session.ProcessStart(os.Getpid())}
	if err := (session.Store{Root: a.Store.Paths.SessionsDir}).Create(value); err != nil {
		t.Fatal(err)
	}
	for _, rollback := range []bool{false, true} {
		if err := a.UpdateCodex(context.Background(), rollback); err == nil || !strings.Contains(err.Error(), "CODEX_SESSION_ACTIVE") {
			t.Fatalf("updated active session: %v", err)
		}
	}
}
