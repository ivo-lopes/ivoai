package app

import (
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"testing"
)

func TestFrontendExplicitModelAndReasoningRemainAuthoritative(t *testing.T) {
	catalog := opencodebridge.CatalogFromRegistry(routing.Registry{Providers: map[string]routing.ProviderCapability{"codex": {Authenticated: true, Models: []routing.ModelCapability{{Name: "observed-fixture", Source: routing.SourceRuntimeVerified, SupportedEfforts: []string{"low", "high"}}}}}})
	model, effort, err := frontendSelection(catalog, "codex", []string{"--model", "observed-fixture", "-c", `model_reasoning_effort="high"`})
	if err != nil || effort != "high" {
		t.Fatal("explicit choice lost", err)
	}
	selection, ok := catalog.Resolve(model, effort)
	if !ok || selection.Model != "observed-fixture" || selection.Executor != "codex" {
		t.Fatal("runtime mapping changed")
	}
	if _, _, err := frontendSelection(catalog, "codex", []string{"--model", "invented"}); err == nil {
		t.Fatal("unverified model admitted")
	}
	if _, _, err := frontendSelection(catalog, "claude", []string{"--model", "observed-fixture"}); err == nil {
		t.Fatal("provider override crossed")
	}
	for _, args := range [][]string{{"-c", `model="observed-fixture"`}, {"--config=model=observed-fixture"}, {"-cmodel=observed-fixture"}, {"-mobserved-fixture"}} {
		id, _, err := frontendSelection(catalog, "codex", args)
		if err != nil || id != model {
			t.Fatal("official model override was silently ignored", err)
		}
	}
}
