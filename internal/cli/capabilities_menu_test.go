package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/app"
)

func TestNativeCapabilityTUIUsesPersistentClientDomain(t *testing.T) {
	root := t.TempDir()
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, filepath.Join(root, key))
	}
	t.Setenv("NO_COLOR", "1")
	var output bytes.Buffer
	// Public launcher -> Skills & Capabilities -> install reviewed baseline ->
	// Ponytail -> Off -> return. No provider-specific setup or credential input.
	a, err := app.New("fixture", strings.NewReader("12\n1\n2\n2\n0\n0\n"), &output, &output)
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), a, nil); err != nil {
		t.Fatal(err)
	}
	cfg, err := a.Store.Load()
	if err != nil || cfg.Skills.ResolvedPonytail() != "off" {
		t.Fatal("TUI did not persist Ponytail mode", err)
	}
	rows, err := a.NativeCapabilities(context.Background())
	if err != nil || len(rows) != 13 {
		t.Fatal("native TUI pack unavailable", err)
	}
	for _, row := range rows {
		if row.Status != "ready" {
			t.Fatalf("%s status=%s", row.ID, row.Status)
		}
	}
	for _, label := range []string{"Skills & Capabilities", "implementation workers only", "Ponytail"} {
		if !strings.Contains(output.String(), label) {
			t.Fatalf("missing landmark %s", label)
		}
	}
	if strings.Contains(output.String(), "# Ponytail") {
		t.Fatal("TUI printed skill body")
	}
}
