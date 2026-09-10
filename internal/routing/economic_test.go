package routing

import (
	"testing"
	"time"
)

func economicRegistry() Registry {
	r := Registry{Providers: map[string]ProviderCapability{}}
	for _, p := range []string{"codex", "claude"} {
		r.Providers[p] = ProviderCapability{Provider: p, Authenticated: true, WorkerCapable: true, SupportsEffort: true, Source: SourceRuntimeVerified, Models: []ModelCapability{
			{Name: p + "-fixture-small", CapabilityTier: TierLight, Source: SourceRuntimeVerified, SupportedEfforts: []string{"low", "medium"}},
			{Name: p + "-fixture-strong", CapabilityTier: TierStrong, Source: SourceRuntimeVerified, SupportedEfforts: []string{"low", "medium", "high"}},
		}}
	}
	return r
}

func TestEconomicMinimumSufficientAndStrongPrimary(t *testing.T) {
	r := Router{Registry: economicRegistry(), Strict: true}
	for _, tt := range []struct {
		tier          Tier
		model, effort string
	}{{TierLight, "codex-fixture-small", "low"}, {TierStrong, "codex-fixture-strong", "high"}} {
		p, err := r.Resolve(TaskInput{}, tt.tier)
		if err != nil || p.Model != tt.model || p.Effort != tt.effort {
			t.Fatalf("tier=%s profile=%+v err=%v", tt.tier, p, err)
		}
	}
	provider := r.Registry.Providers["codex"]
	provider.Models[1].Source = SourceUnknown
	r.Registry.Providers["codex"] = provider
	provider = r.Registry.Providers["claude"]
	provider.Authenticated = false
	r.Registry.Providers["claude"] = provider
	if _, err := r.Resolve(TaskInput{}, TierStrong); err == nil {
		t.Fatal("unverified/weak model claimed to be strong")
	}
}

func TestEconomicExplicitSelectionNeverSilentlyFallsBack(t *testing.T) {
	r := Router{Registry: economicRegistry(), Strict: true}
	for _, input := range []TaskInput{
		{Executor: "absent"},
		{Executor: "codex", Model: "not-in-catalog"},
		{Executor: "codex", Model: "codex-fixture-small", Effort: "ultra"},
		{Model: "ambiguous"},
	} {
		if _, err := r.Resolve(input, TierLight); err == nil {
			t.Fatal("unavailable explicit selection accepted")
		}
	}
	p, err := r.Resolve(TaskInput{Executor: "claude", Model: "claude-fixture-small", Effort: "medium"}, TierLight)
	if err != nil || p.Provider != "claude" || p.Effort != "medium" || p.RequestedModel != p.Model || p.Reason != "explicit_override" {
		t.Fatalf("profile=%+v error=%v", p, err)
	}
	provider := r.Registry.Providers["claude"]
	provider.Authenticated = false
	r.Registry.Providers["claude"] = provider
	if _, err = r.Resolve(TaskInput{Executor: "claude"}, TierLight); err == nil {
		t.Fatal("explicit unavailable switched provider")
	}
	if p, err = r.Resolve(TaskInput{}, TierLight); err != nil || p.Provider != "codex" {
		t.Fatal("optional Claude blocked other executor")
	}
}

func TestEconomicObservedLatencyBreaksEquivalentQuotaTie(t *testing.T) {
	r := Router{Registry: economicRegistry(), Strict: true, Latency: map[string]time.Duration{"codex": time.Second, "claude": 100 * time.Millisecond}}
	p, err := r.Resolve(TaskInput{Scores: Scores{LatencySensitivity: 80}}, TierLight)
	if err != nil || p.Provider != "claude" {
		t.Fatalf("profile=%+v error=%v", p, err)
	}
}

func TestEconomicCapabilityRequirementsAreConstraintsNotHints(t *testing.T) {
	r := Router{Registry: economicRegistry(), Strict: true}
	c := r.Registry.Providers["claude"]
	c.Capabilities = map[string]bool{"filesystem_read": true}
	r.Registry.Providers["claude"] = c
	p, err := r.Resolve(TaskInput{RequiredCapabilities: []string{"filesystem_read"}}, TierLight)
	if err != nil || p.Provider != "claude" {
		t.Fatal("required capability ignored", err)
	}
	if _, err := r.Resolve(TaskInput{Executor: "codex", RequiredCapabilities: []string{"filesystem_read"}}, TierLight); err == nil {
		t.Fatal("explicit unsupported capability accepted")
	}
	if _, err := r.Resolve(TaskInput{RequiredCapabilities: []string{"disable_sandbox"}}, TierLight); err == nil {
		t.Fatal("unknown capability accepted")
	}
}
