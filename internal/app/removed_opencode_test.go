package app

import (
	"context"
	"errors"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/codexfrontend"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestRemovedOpenCodeRefusesBeforeAccessingDependencies(t *testing.T) {
	// A nil Store/Runner makes any accidental access fail the test. Refusals
	// must precede setup, discovery, lease creation and provider execution.
	a := &App{}
	ctx := context.Background()
	for name, action := range map[string]func() error{
		"setup":        func() error { return a.SetupWithLegacyOpenCode(ctx, true) },
		"launch":       func() error { return a.Launch(ctx, "opencode", nil) },
		"direct":       func() error { return a.SessionStart(ctx, "opencode", session.ModeDirect, nil) },
		"orchestrated": func() error { return a.SessionStart(ctx, "opencode", session.ModeOrchestrated, nil) },
		"frontend":     func() error { return a.OrchestratedWithKnowledge(ctx, "opencode", "codex", nil, nil) },
		"planner":      func() error { return a.OrchestratedWithKnowledge(ctx, "codex", "OpenCode", nil, nil) },
		"resume":       func() error { return a.SessionResumeFrontend(ctx, "unused", "opencode") },
	} {
		t.Run(name, func(t *testing.T) {
			if err := action(); !errors.Is(err, session.ErrOpenCodeManagedDeprecated) {
				t.Fatal("removed path accessed its dependencies", err)
			}
		})
	}
}

func TestAutoAliasUsesNativeCoreWithoutDiscoveringRetainedOpenCode(t *testing.T) {
	a, marker, runner, probes := nativeProviderTestApp(t)
	called := false
	a.StartCodexNative = func(_ context.Context, options codexfrontend.Options) (nativeCodexFrontend, error) {
		called = true
		for _, model := range options.Bridge.Catalog().Entries() {
			if model.Executor == "opencode" {
				t.Fatal("removed provider in native picker")
			}
		}
		return fixtureNativeFrontend{}, nil
	}
	if err := a.Auto(context.Background(), "", nil); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("auto alias did not enter the native frontend")
	}
	assertNoOpenCode(t, marker, runner, probes)
	values, err := a.SessionList()
	if err != nil || len(values) != 1 || values[0].Frontend != "codex" {
		t.Fatal("alias did not preserve Core session ownership", err)
	}
}
