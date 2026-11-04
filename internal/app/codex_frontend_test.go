package app

import (
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
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

func TestResumeRetainsEffectiveModelAndEffortWithoutSilentSubstitution(t *testing.T) {
	catalog := opencodebridge.CatalogFromRegistry(routing.Registry{Providers: map[string]routing.ProviderCapability{"codex": {Authenticated: true, Models: []routing.ModelCapability{{Name: "retained-model", Source: routing.SourceRuntimeVerified, SupportedEfforts: []string{"low", "high"}}}}}})
	v := session.Session{PrimaryExecutor: "codex", EffectiveModel: "retained-model", EffectiveEffort: "low"}
	model, effort, err := resumedFrontendSelection(catalog, v, "auto", "high")
	resolved, ok := catalog.Resolve(model, effort)
	if err != nil || !ok || resolved.Model != v.EffectiveModel || effort != "low" {
		t.Fatal("resume silently replaced the retained selection", err)
	}
	for _, change := range []string{"model", "effort", "provider"} {
		t.Run(change, func(t *testing.T) {
			changed := v
			switch change {
			case "model":
				changed.EffectiveModel = "unavailable-model"
			case "effort":
				changed.EffectiveEffort = "unavailable-effort"
			case "provider":
				changed.PrimaryExecutor = "claude"
			}
			if _, _, err := resumedFrontendSelection(catalog, changed, "auto", ""); err == nil {
				t.Fatal("unavailable retained selection was substituted")
			}
		})
	}
}
