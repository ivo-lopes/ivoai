package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/app"
)

func TestOrchestratedEntrypointsAndDeprecatedAlias(t *testing.T) {
	root := t.TempDir()
	for _, kind := range []string{"CONFIG", "DATA", "STATE", "CACHE"} {
		t.Setenv("XDG_"+kind+"_HOME", filepath.Join(root, kind))
	}
	var out, diagnostic bytes.Buffer
	a, err := app.New("fixture", strings.NewReader(""), &out, &diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := a.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Orchestration.Auto.Enabled = false
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"codex", "opencode", "auto"} {
		out.Reset()
		diagnostic.Reset()
		err := runCommand(context.Background(), a, []string{command})
		if err == nil || err.Error() != "automatic orchestration is disabled" {
			t.Fatalf("%s bypasses common core: %v", command, err)
		}
		if out.Len() != 0 {
			t.Fatal("alias or mode polluted stdout")
		}
		if (strings.Count(diagnostic.String(), "deprecated") == 1) != (command == "auto") {
			t.Fatalf("incorrect deprecation: %s", command)
		}
	}
	for _, command := range []string{"codex", "opencode"} {
		err := runCommand(context.Background(), a, []string{command, "--direct", "--", "--version"})
		if err != nil && strings.Contains(err.Error(), "orchestration is disabled") {
			t.Fatal("direct entered orchestration")
		}
	}
}

func TestLaunchMenuExposesOrchestratedAndDirectModes(t *testing.T) {
	root := t.TempDir()
	for _, kind := range []string{"CONFIG", "DATA", "STATE", "CACHE"} {
		t.Setenv("XDG_"+kind+"_HOME", filepath.Join(root, kind))
	}
	var output bytes.Buffer
	a, err := app.New("fixture", strings.NewReader("1\n0\n0\n"), &output, &output)
	if err != nil {
		t.Fatal(err)
	}
	if err := menu(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"Codex Orchestrated", "OpenCode Orchestrated", "Codex Direct", "Claude Direct", "OpenCode Direct", "deprecated alias"} {
		if !strings.Contains(output.String(), label) {
			t.Fatalf("missing mode %s", label)
		}
	}
}
