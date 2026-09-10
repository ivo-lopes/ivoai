package routing

import (
	"context"
	"os"
	"testing"
)

func TestLiveOfficialCatalogHasVerifiedPrimaryCapability(t *testing.T) {
	path := os.Getenv("IVOAI_LIVE_CATALOG_CODEX")
	if path == "" {
		t.Skip("official catalog live probe not requested")
	}
	r := (Discoverer{CodexPath: path}).Discover(context.Background())
	capability, ok := r.Providers["codex"]
	if !ok || !capability.Authenticated {
		t.Fatal("official Codex catalog/auth unavailable")
	}
	counts := map[Tier]int{}
	for _, model := range capability.Models {
		counts[model.CapabilityTier]++
	}
	t.Logf("observed model classes: %v", counts)
	if _, err := (Router{Strict: true, Registry: r}).Resolve(TaskInput{Executor: "codex"}, TierStrong); err != nil {
		t.Fatal("no verified strong primary class", err)
	}
}
