package app

import (
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
)

// This opt-in smoke uses the candidate/public binary, real pinned OpenCode and
// the official Codex client. Only component metadata is reused. Credentials,
// operator config, sessions and institutional profiles are never copied.
func TestLiveNativeAUTOArtifact(t *testing.T) {
	binary := os.Getenv("IVOAI_NATIVE_SMOKE_BINARY")
	if binary == "" {
		t.Skip("set IVOAI_NATIVE_SMOKE_BINARY to an installed/candidate artifact")
	}
	// Codex owns its real login. Guard its public configuration without copying
	// provider credentials or printing any configuration content.
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		codexHome = filepath.Join(home, ".codex")
	}
	configPath := filepath.Join(codexHome, "config.toml")
	before, beforeErr := os.ReadFile(configPath)
	if beforeErr != nil && !os.IsNotExist(beforeErr) {
		t.Fatal("cannot guard Codex configuration")
	}
	beforeHash := sha256.Sum256(before)
	t.Cleanup(func() {
		after, afterErr := os.ReadFile(configPath)
		if (beforeErr == nil) != (afterErr == nil) || sha256.Sum256(after) != beforeHash {
			t.Error("PERSONAL_CODEX_CONFIG_MUTATIONS detected")
		} else {
			t.Log("PERSONAL_CODEX_CONFIG_MUTATIONS=0")
		}
	})
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
	cfg.OpenCode.PermissionMode = "full"
	cfg.Headroom.Enabled = false
	cfg.Compression.Provider = "direct"
	cfg.Orchestration.Auto.WorkerCap = 2
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	state := config.State{Schema: config.StateSchemaVersion, Components: map[string]config.ComponentState{}}
	for _, name := range []string{"codex", "codex-code-mode-host", "opencode"} {
		state.Components[name] = installed.Components[name]
	}
	if path := os.Getenv("IVOAI_NATIVE_SMOKE_OPENCODE_PATH"); path != "" {
		// Public artifact tests can select the matching downloaded frontend;
		// only isolated component metadata changes, never operator installation.
		state.Components["opencode"] = config.ComponentState{Installed: true, Managed: true, Version: "1.18.25-ivoai.1", Path: path}
	}
	if err := store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	pack := exec.Command(binary, "skills", "update")
	if output, err := pack.CombinedOutput(); err != nil {
		t.Fatalf("native pack materialization: %v: %s", err, output)
	}
	t.Setenv("IVOAI_NATIVE_SMOKE_CAPABILITIES", "1")
	command := exec.Command("python3", "../../scripts/smoke-native-auto.py", binary, root)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
}
