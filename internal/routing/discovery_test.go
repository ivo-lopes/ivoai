package routing

import "testing"

func TestCatalogTierUsesOnlyStructuredDescriptions(t *testing.T) {
	tests := map[string]Tier{
		"Fast and cost-efficient model":    TierLight,
		"Balanced model for everyday work": TierBalanced,
		"Strong model":                     TierStrong,
		"Latest frontier model":            TierMax,
		"No capability claim":              "",
	}
	for description, want := range tests {
		if got := catalogTier(description); got != want {
			t.Fatalf("%q: got %q want %q", description, got, want)
		}
	}
}

func TestClaudeEffortDiscovery(t *testing.T) {
	help := "  --effort <level>  Effort level for the current session\n                    (low, medium, high, xhigh, max)\n"
	got := parseClaudeEfforts(help)
	if len(got) != 5 || got[0] != "low" || got[4] != "max" {
		t.Fatalf("unexpected efforts: %#v", got)
	}
}

func TestModelNamesRejectTerminalAndBidirectionalControls(t *testing.T) {
	for _, value := range []string{"safe-model", "unsafe\tmodel", "unsafe\u202emodel", "unsafe\u0085model"} {
		want := value == "safe-model"
		if got := safeModelName(value); got != want {
			t.Fatalf("safeModelName(%q)=%t want=%t", value, got, want)
		}
	}
}
