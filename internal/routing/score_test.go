package routing

import (
	"testing"

	"github.com/ivo-lopes/ivoai/internal/quota"
)

func TestScoreAndTiers(t *testing.T) {
	tests := []struct {
		name string
		s    Scores
		want Tier
	}{
		{"simple", Scores{Complexity: 10, Risk: 5, ReasoningDepth: 15, VerificationNeed: 15, ContextBreadth: 10}, TierLight},
		{"medium", Scores{Complexity: 40, Risk: 30, ReasoningDepth: 45, VerificationNeed: 50, ContextBreadth: 35}, TierBalanced},
		{"architecture", Scores{Complexity: 70, Risk: 55, ReasoningDepth: 75, VerificationNeed: 65, ContextBreadth: 75}, TierStrong},
		{"security", Scores{Complexity: 85, Risk: 100, ReasoningDepth: 90, VerificationNeed: 100, ContextBreadth: 75}, TierMax},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			score, err := Score(test.s, DefaultWeights())
			if err != nil || TierForScore(score) != test.want {
				t.Fatalf("score=%d tier=%s err=%v", score, TierForScore(score), err)
			}
		})
	}
}

func TestResolvePlanRejectsCycles(t *testing.T) {
	_, err := ResolvePlan("plan", []TaskInput{
		{ID: "a", Role: "analysis", Task: "A", Dependencies: []string{"b"}},
		{ID: "b", Role: "review", Task: "B", Dependencies: []string{"a"}},
	}, DefaultWeights(), func(_ TaskInput, tier Tier) (ExecutionProfile, error) {
		return ExecutionProfile{Provider: "codex", Tier: tier}, nil
	})
	if err == nil {
		t.Fatal("expected cycle rejection")
	}
}

func TestRouterChoosesLowestSufficientRuntimeModelAndHonorsModelQuota(t *testing.T) {
	models := []ModelCapability{
		{Name: "frontier", Provider: "codex", CapabilityTier: TierMax, SupportedEfforts: []string{"low", "medium", "high", "max"}, IsDefault: true, Source: SourceRuntimeVerified},
		{Name: "lightweight", Provider: "codex", CapabilityTier: TierLight, SupportedEfforts: []string{"low", "medium"}, Source: SourceRuntimeVerified},
		{Name: "balanced", Provider: "codex", CapabilityTier: TierBalanced, SupportedEfforts: []string{"low", "medium", "high"}, Source: SourceRuntimeVerified},
	}
	router := Router{Registry: Registry{Providers: map[string]ProviderCapability{"codex": {Provider: "codex", Authenticated: true, WorkerCapable: true, SupportsEffort: true, Models: models}}}}
	profile, err := router.Resolve(TaskInput{PreferredExecutor: "codex"}, TierLight)
	if err != nil || profile.Model != "lightweight" || profile.Effort != "low" {
		t.Fatalf("profile=%+v err=%v", profile, err)
	}
}

func TestRouterFallsBackWithinProviderForExhaustedModelQuota(t *testing.T) {
	models := []ModelCapability{
		{Name: "balanced-a", Provider: "codex", CapabilityTier: TierBalanced, SupportedEfforts: []string{"medium"}, Source: SourceRuntimeVerified},
		{Name: "balanced-b", Provider: "codex", CapabilityTier: TierBalanced, SupportedEfforts: []string{"medium"}, Source: SourceRuntimeVerified},
	}
	current := quota.ProviderQuota{Provider: quota.ProviderCodex, Authenticated: true, Eligible: true, Windows: []quota.Window{{Kind: quota.KindModelWeekly, Model: "balanced-a", Available: true, Authoritative: true, RemainingPercent: 0}}}
	router := Router{Registry: Registry{Providers: map[string]ProviderCapability{"codex": {Provider: "codex", Authenticated: true, WorkerCapable: true, SupportsEffort: true, Models: models}}}, Quota: map[quota.Provider]quota.ProviderQuota{quota.ProviderCodex: current}}
	profile, err := router.Resolve(TaskInput{PreferredExecutor: "codex"}, TierBalanced)
	if err != nil || profile.Model != "balanced-b" {
		t.Fatalf("profile=%+v err=%v", profile, err)
	}
}
