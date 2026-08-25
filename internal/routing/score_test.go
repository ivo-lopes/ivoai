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

func TestDelegationDecisionRejectsTrivialOverheadAndAcceptsUsefulParallelWork(t *testing.T) {
	if delegated, benefit, overhead := DelegationDecision(Scores{Complexity: 5, Risk: 5, VerificationNeed: 5, ContextBreadth: 5, ParallelValue: 5, LatencySensitivity: 50}); delegated || benefit >= overhead {
		t.Fatalf("trivial task delegated: benefit=%d overhead=%d", benefit, overhead)
	}
	if delegated, benefit, overhead := DelegationDecision(Scores{Complexity: 70, Risk: 60, VerificationNeed: 70, ContextBreadth: 60, ParallelValue: 90, LatencySensitivity: 80}); !delegated || benefit <= overhead {
		t.Fatalf("valuable parallel work stayed primary: benefit=%d overhead=%d", benefit, overhead)
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

func TestRouterUsesQuotaPressureWithoutLoweringCapability(t *testing.T) {
	registry := Registry{Providers: map[string]ProviderCapability{
		"codex":  {Provider: "codex", Authenticated: true, WorkerCapable: true, Models: []ModelCapability{{Name: "codex-strong", Provider: "codex", CapabilityTier: TierStrong, Source: SourceRuntimeVerified}}},
		"claude": {Provider: "claude", Authenticated: true, WorkerCapable: true, Models: []ModelCapability{{Name: "claude-strong", Provider: "claude", CapabilityTier: TierStrong, Source: SourceRuntimeVerified}}},
	}}
	quotas := map[quota.Provider]quota.ProviderQuota{
		quota.ProviderCodex:  {Provider: quota.ProviderCodex, Authenticated: true, Eligible: true, Windows: []quota.Window{{Kind: quota.KindWeekly, Available: true, Authoritative: true, RemainingPercent: 10}}},
		quota.ProviderClaude: {Provider: quota.ProviderClaude, Authenticated: true, Eligible: true, Windows: []quota.Window{{Kind: quota.KindWeekly, Available: true, Authoritative: true, RemainingPercent: 80}}},
	}
	profile, err := (Router{Registry: registry, Quota: quotas}).Resolve(TaskInput{PreferredExecutor: "codex"}, TierStrong)
	if err != nil || profile.Provider != "claude" || profile.Tier != TierStrong {
		t.Fatalf("profile=%+v err=%v", profile, err)
	}
}

func TestRouterDegradesUnsupportedEffortToClientDefault(t *testing.T) {
	registry := Registry{Providers: map[string]ProviderCapability{
		"claude": {Provider: "claude", Authenticated: true, WorkerCapable: true, SupportsEffort: false, Models: []ModelCapability{{Provider: "claude", CapabilityTier: TierMax, IsDefault: true, Source: SourceDefault}}},
	}}
	profile, err := (Router{Registry: registry}).Resolve(TaskInput{PreferredExecutor: "claude"}, TierStrong)
	if err != nil || profile.Effort != "" || profile.EffortSource != SourceUnsupported {
		t.Fatalf("profile=%+v err=%v", profile, err)
	}
}

func TestRouterRejectsUnavailableProviders(t *testing.T) {
	registry := Registry{Providers: map[string]ProviderCapability{
		"codex":  {Provider: "codex", Authenticated: false, WorkerCapable: true},
		"claude": {Provider: "claude", Authenticated: true, WorkerCapable: false},
	}}
	if _, err := (Router{Registry: registry}).Resolve(TaskInput{}, TierLight); err == nil {
		t.Fatal("unavailable providers were accepted")
	}
}

func TestRepresentativeAutomaticPlanPreservesQualityFloorAndDependencies(t *testing.T) {
	inputs := []TaskInput{
		{ID: "inventory", Role: "analyst", Task: "Inventory the relevant packages", Scores: Scores{Complexity: 15, Risk: 10, ReasoningDepth: 15, ContextBreadth: 25, VerificationNeed: 20, ParallelValue: 80}},
		{ID: "architecture", Role: "architect", Task: "Assess architecture and constraints", Scores: Scores{Complexity: 75, Risk: 60, ReasoningDepth: 80, ContextBreadth: 75, VerificationNeed: 70, ParallelValue: 85}},
		{ID: "implementation", Role: "primary", Task: "Integrate the authoritative changes", Dependencies: []string{"inventory", "architecture"}, Scores: Scores{Complexity: 65, Risk: 60, ReasoningDepth: 65, ContextBreadth: 55, VerificationNeed: 70}},
		{ID: "tests", Role: "tester", Task: "Run focused verification", Dependencies: []string{"implementation"}, Scores: Scores{Complexity: 40, Risk: 35, ReasoningDepth: 35, ContextBreadth: 30, VerificationNeed: 65, ParallelValue: 55}},
		{ID: "security", Role: "security", Task: "Perform independent security review", Dependencies: []string{"implementation"}, Scores: Scores{Complexity: 90, Risk: 100, ReasoningDepth: 90, ContextBreadth: 70, VerificationNeed: 100, ParallelValue: 75}, IntentionalRedundancy: true},
	}
	plan, err := ResolvePlan("plan_fixture", inputs, DefaultWeights(), func(_ TaskInput, tier Tier) (ExecutionProfile, error) {
		return ExecutionProfile{Provider: "fixture", Tier: tier, ModelSource: SourceRuntimeVerified, EffortSource: SourceUnsupported}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Tier{"inventory": TierLight, "architecture": TierStrong, "implementation": TierStrong, "tests": TierBalanced, "security": TierMax}
	maxCount := 0
	for _, task := range plan.Tasks {
		if task.Tier != want[task.ID] {
			t.Fatalf("task %s score=%d tier=%s want=%s", task.ID, task.CapabilityScore, task.Tier, want[task.ID])
		}
		if task.Tier == TierMax {
			maxCount++
		}
	}
	if maxCount != 1 {
		t.Fatalf("optimized plan used %d MAX profiles; legacy all-MAX would use %d", maxCount, len(inputs))
	}
}
