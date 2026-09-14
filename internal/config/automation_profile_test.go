package config

import (
	"reflect"
	"testing"
)

func TestAutomationProfilesPreserveAuthority(t *testing.T) {
	for _, name := range []string{"economic", "balanced", "quality"} {
		c := AutoConfig{ProviderPreference: "claude", KnowledgeRouting: "explicit-only", Profiles: AutoProfilesConfig{Codex: AutoTierProfiles{Strong: AutoProfileConfig{Model: "explicit-model", Effort: "high"}}}}
		original := c.Profiles
		if err := c.ApplyAutomationProfile(name); err != nil {
			t.Fatal(err)
		}
		if c.ProviderPreference != "claude" || c.KnowledgeRouting != "explicit-only" || c.Profiles != original || c.PlanExecution != "approve" || c.LowQuotaThreshold != 10 {
			t.Fatal("preset changed explicit authority or security invariant")
		}
		before := c
		if err := c.ApplyAutomationProfile("custom"); err != nil {
			t.Fatal(err)
		}
		before.AutomationProfile = "custom"
		if !reflect.DeepEqual(before, c) {
			t.Fatal("custom reset existing settings")
		}
		if err := c.ApplyAutomationProfile("unknown"); err == nil {
			t.Fatal("unknown preset accepted")
		}
	}
}
