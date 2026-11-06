package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

type retiredComponentProbeRunner struct{ legacyCalls int }

func (r *retiredComponentProbeRunner) LookPath(name string) (string, error) {
	if name == "opencode" {
		r.legacyCalls++
	}
	return "", os.ErrNotExist
}
func (r *retiredComponentProbeRunner) Run(_ context.Context, path string, _ []string, _ platform.RunOptions) (platform.Result, error) {
	if filepath.Base(path) == "opencode" {
		r.legacyCalls++
	}
	return platform.Result{}, os.ErrNotExist
}

func TestDoctorRetainsLegacyMetadataWithoutProbingOpenCode(t *testing.T) {
	t.Setenv("IVOAI_TEST_MODE", "1")
	root := t.TempDir()
	paths := config.Paths{ConfigDir: root, StateDir: root, DataDir: root, CacheDir: root, Config: filepath.Join(root, "config.toml"), State: filepath.Join(root, "state.toml"), Secrets: filepath.Join(root, "secrets.json")}
	store := config.NewStore(paths)
	cfg := config.Default()
	cfg.Memory.Enabled, cfg.Headroom.Enabled, cfg.Orchestration.Enabled = false, false, false
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	// The retained metadata is intentionally not a usable path. Doctor must
	// report it as retained metadata rather than repair, adopt or execute it.
	legacy := config.ComponentState{Installed: true, Managed: true, Path: filepath.Join(root, "opencode"), Version: "retained"}
	if err := store.SaveState(config.State{Schema: config.SchemaVersion, Components: map[string]config.ComponentState{"opencode": legacy}}); err != nil {
		t.Fatal(err)
	}
	runner := &retiredComponentProbeRunner{}
	report := (Doctor{Store: store, Runner: runner}).Run(context.Background())
	if runner.legacyCalls != 0 || report.OpenCode.Healthy || report.OpenCode.Path != legacy.Path || report.OpenCode.Version != legacy.Version || !report.OpenCode.Installed {
		t.Fatal("retained component was probed or reinterpreted")
	}
	state, err := store.LoadState()
	if err != nil || state.Components["opencode"] != legacy {
		t.Fatal("diagnostics rewrote retained metadata", err)
	}
}
