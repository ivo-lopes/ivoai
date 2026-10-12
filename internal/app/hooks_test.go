package app

import (
	"context"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
)

func TestRepairMemoryHooksRefusesUnownedInstallation(t *testing.T) {
	a := sessionTestApp(t, t.TempDir(), "/fixture/codex", "/fixture/claude", "/fixture/ruflo")
	state, err := a.Store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	state.Components["ai-memory"] = config.ComponentState{Installed: true, Path: "/foreign/ai-memory", Version: "2.6.3", Managed: false}
	if err := a.Store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	err = a.RepairMemoryHooks(context.Background())
	if err == nil || !strings.Contains(err.Error(), "managed") {
		t.Fatalf("unowned repair: %v", err)
	}
}

func TestHookPreflightDoesNotExecuteUnreceiptedHooks(t *testing.T) {
	a := sessionTestApp(t, t.TempDir(), "/fixture/codex", "/fixture/claude", "/fixture/ruflo")
	state, err := a.Store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	state.Components["ai-memory"] = config.ComponentState{Installed: true, Path: "/foreign/ai-memory", Version: "2.6.3", Managed: false}
	if err := a.Store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	a.Runner = missingRunner{}
	env, cleanup := a.prepareMemoryHooks()
	defer cleanup()
	if len(env) != 0 {
		t.Fatal("unknown hooks were adopted")
	}
}

func TestHookSessionEnvironmentSurvivesTurnEnvironmentRefresh(t *testing.T) {
	base := []string{"IVOAI_HOOK_SESSION_DIR=/private/session", "OLD_CAPABILITY=never-copy"}
	refreshed := []string{"NEW_CAPABILITY=current", "IVOAI_HOOK_SESSION_DIR=/wrong"}
	got := inheritHookSessionEnvironment(refreshed, base)
	joined := strings.Join(got, "\n")
	if strings.Count(joined, "IVOAI_HOOK_SESSION_DIR=") != 1 || !strings.Contains(joined, "IVOAI_HOOK_SESSION_DIR=/private/session") || strings.Contains(joined, "OLD_CAPABILITY") {
		t.Fatalf("turn hook environment: %v", got)
	}
}
