package app

import (
	"path/filepath"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/caveman"
	"github.com/ivo-lopes/ivoai/internal/config"
)

func TestCompressionSelectionIsMutuallyExclusiveWithCavemanDefault(t *testing.T) {
	root := t.TempDir()
	a := &App{Store: config.NewStore(config.Paths{DataDir: root})}
	state := config.State{Components: map[string]config.ComponentState{
		"caveman": {Installed: true, Managed: true, Path: filepath.Join(root, "caveman")},
	}}

	cfg := config.Default()
	provider, enabled, name := a.sessionCompression(cfg, state, "codex", filepath.Join(root, "runtime"))
	if _, ok := provider.(caveman.Provider); !ok || !enabled || name != "caveman" {
		t.Fatalf("default selection provider=%T enabled=%t name=%q", provider, enabled, name)
	}

	cfg.Compression = config.CompressionConfig{Provider: "headroom", Source: config.CompressionSourceExplicit}
	provider, enabled, name = a.sessionCompression(cfg, state, "codex", filepath.Join(root, "runtime"))
	if provider != nil || !enabled || name != "headroom" {
		t.Fatalf("Headroom selection provider=%T enabled=%t name=%q", provider, enabled, name)
	}

	cfg.Compression = config.CompressionConfig{Provider: "direct", Source: config.CompressionSourceExplicit}
	provider, enabled, name = a.sessionCompression(cfg, state, "codex", filepath.Join(root, "runtime"))
	if provider != nil || enabled || name != "direct" {
		t.Fatalf("Direct selection provider=%T enabled=%t name=%q", provider, enabled, name)
	}
}
