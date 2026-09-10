package opencodebridge

import (
	"testing"

	"github.com/ivo-lopes/ivoai/internal/routing"
)

func TestAutomaticPrimaryRequiresObservedStrongClassWithoutModelHardcodes(t *testing.T) {
	registry := routing.Registry{Providers: map[string]routing.ProviderCapability{"codex": {Authenticated: true, Models: []routing.ModelCapability{
		{Name: "fixture-fast", CapabilityTier: routing.TierLight, Source: routing.SourceRuntimeVerified, IsDefault: true},
		{Name: "fixture-strong", CapabilityTier: routing.TierStrong, Source: routing.SourceRuntimeVerified},
		{Name: "unverified-max", CapabilityTier: routing.TierMax, Source: routing.SourceUnknown},
	}}}}
	catalog := CatalogFromRegistry(registry)
	model, ok := catalog.StrongPrimary("codex")
	if !ok || model.UpstreamModel != "fixture-strong" {
		t.Fatal("primary silently selected weak/default model")
	}
	capability := registry.Providers["codex"]
	capability.Models = append(capability.Models[:1], capability.Models[2:]...)
	registry.Providers["codex"] = capability
	if _, ok := CatalogFromRegistry(registry).StrongPrimary("codex"); ok {
		t.Fatal("unverified model claimed strong capability")
	}
}

func TestCatalogPublishesRuntimeModelsAndOnlySupportedEfforts(t *testing.T) {
	catalog := CatalogFromRegistry(routing.Registry{Providers: map[string]routing.ProviderCapability{
		"codex":  {Provider: "codex", Authenticated: true, Models: []routing.ModelCapability{{Name: "gpt-fixture", SupportedEfforts: []string{"low", "high"}, DefaultEffort: "high", Source: routing.SourceRuntimeVerified}}},
		"claude": {Provider: "claude", Authenticated: true, Models: []routing.ModelCapability{{SupportedEfforts: []string{"medium", "max"}, Source: routing.SourceDefault}}},
	}})
	entries := catalog.Entries()
	if len(entries) != 3 || entries[0].ID != "auto" || entries[1].Executor != "claude" || entries[2].Executor != "codex" {
		t.Fatalf("unexpected deterministic catalog: %+v", entries)
	}
	models := catalog.OpenCodeModels()
	for _, entry := range entries[1:] {
		value := models[entry.ID].(map[string]any)
		if _, invented := value["limit"]; invented {
			t.Fatalf("catalog invented an unverified context/output limit for %+v", entry)
		}
		if value["reasoning"] != true {
			t.Fatalf("reasoning variants absent for %+v", entry)
		}
		variants := value["variants"].(map[string]any)
		for _, effort := range allEfforts {
			variant := variants[effort].(map[string]any)
			if contains(entry.SupportedEfforts, effort) && variant["reasoningEffort"] != effort {
				t.Fatalf("supported effort %s is not active: %#v", effort, variant)
			}
			if !contains(entry.SupportedEfforts, effort) && variant["disabled"] != true {
				t.Fatalf("unsupported effort %s was exposed: %#v", effort, variant)
			}
		}
	}
}

func TestCatalogRejectsUnknownModelAndUnsupportedReasoning(t *testing.T) {
	catalog := newCatalog([]ModelSpec{{ID: "codex-fixture", Name: "Codex fixture", Mode: "explicit", Executor: "codex", UpstreamModel: "gpt-fixture", SupportedEfforts: []string{"low", "high"}}})
	if _, ok := catalog.Resolve("unknown", ""); ok {
		t.Fatal("unknown model was accepted")
	}
	if _, ok := catalog.Resolve("codex-fixture", "max"); ok {
		t.Fatal("unsupported reasoning effort was accepted")
	}
	selection, ok := catalog.Resolve("codex-fixture", "high")
	if !ok || selection.Executor != "codex" || selection.Model != "gpt-fixture" || selection.Effort != "high" || selection.Mode != "explicit" {
		t.Fatalf("selection=%+v ok=%v", selection, ok)
	}
}

func TestCatalogRejectsTerminalControlModelNames(t *testing.T) {
	catalog := newCatalog([]ModelSpec{{ID: "unsafe", Name: "Codex\u202e spoof", Mode: "explicit", Executor: "codex", UpstreamModel: "safe"}, {ID: "safe", Name: "Codex safe", Mode: "explicit", Executor: "codex", UpstreamModel: "safe"}})
	if len(catalog.Entries()) != 2 || catalog.Entries()[1].ID != "safe" {
		t.Fatalf("unsafe model display reached the catalog: %+v", catalog.Entries())
	}
}

func TestClaudeIsOptionalWithoutRemovingAuthenticatedCapability(t *testing.T) {
	for _, state := range []string{"absent", "unauthenticated", "authenticated"} {
		t.Run(state, func(t *testing.T) {
			providers := map[string]routing.ProviderCapability{
				"codex": {Provider: "codex", Authenticated: true, Models: []routing.ModelCapability{{Name: "codex-fixture", Source: routing.SourceRuntimeVerified}}},
			}
			if state != "absent" {
				providers["claude"] = routing.ProviderCapability{Provider: "claude", Authenticated: state == "authenticated", Models: []routing.ModelCapability{{Name: "claude-fixture", SupportedEfforts: []string{"high"}, Source: routing.SourceRuntimeVerified}}}
			}
			catalog := CatalogFromRegistry(routing.Registry{Providers: providers})
			found := map[string]bool{}
			for _, entry := range catalog.Entries() {
				found[entry.Executor] = true
			}
			if _, ok := catalog.Resolve("auto", ""); !ok || !found["codex"] || found["claude"] != (state == "authenticated") {
				t.Fatalf("optional Claude affected availability: %+v", catalog.Entries())
			}
		})
	}
}
