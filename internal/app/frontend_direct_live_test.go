package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/session"
)

// Public/candidate binary smoke. Only installed component metadata is reused;
// provider auth remains owned by the official clients and is never copied.
func TestFrontendArtifactDirectModes(t *testing.T) {
	binary := os.Getenv("IVOAI_FRONTEND_ARTIFACT")
	if binary == "" {
		t.Skip("provide verified artifact")
	}
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	installed, err := config.NewStore(paths).LoadState()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, kind := range []string{"CONFIG", "DATA", "STATE", "CACHE"} {
		t.Setenv("XDG_"+kind+"_HOME", filepath.Join(root, kind))
	}
	paths, err = config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	store := config.NewStore(paths)
	cfg := config.Default()
	cfg.Headroom.Enabled = false
	cfg.Compression.Provider = "direct"
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	state := config.State{Schema: config.StateSchemaVersion, Components: map[string]config.ComponentState{}}
	for _, name := range []string{"codex", "codex-code-mode-host", "opencode"} {
		state.Components[name] = installed.Components[name]
	}
	if err := store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"codex", "--direct", "--", "--version"}, {"opencode", "--direct", "--", "--version"}, {"session", "start", "--executor", "codex", "--mode", "direct", "--", "--version"}} {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = root
		output, err := cmd.CombinedOutput()
		cancel()
		if err != nil || len(output) == 0 {
			t.Fatalf("official direct surface failed: %s: %v", args[0], err)
		}
	}
	values, err := (session.Store{Root: paths.SessionsDir}).List()
	if err != nil || len(values) != 3 {
		t.Fatal("direct session metadata missing", err)
	}
	for _, v := range values {
		if v.Mode != session.ModeDirect || len(v.Tasks) != 0 || len(v.Workers) != 0 || len(v.TurnAttempts) != 0 || v.ExitCode == nil || *v.ExitCode != 0 {
			t.Fatal("direct session entered orchestration")
		}
	}
	t.Log("CODEX_DIRECT=PASS OPENCODE_DIRECT=PASS PROVIDER_NEUTRAL_DIRECT=PASS NO_GATE_NO_DAG=true")
}
