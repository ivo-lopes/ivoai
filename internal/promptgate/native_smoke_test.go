package promptgate

import (
	"os"
	"regexp"
	"testing"
)

func TestNativeRecoverySmokePromptPassesIntake(t *testing.T) {
	source, err := os.ReadFile("../../scripts/smoke-native-auto.py")
	if err != nil {
		t.Fatal(err)
	}
	// Validate the actual fixture submitted by the live harness, not a copy
	// that could drift while the live scenario stops before approved execution.
	match := regexp.MustCompile(`send\("(Objective: [^"\n]+two sequential primary-owned research tasks[^"\n]+)"\)`).FindSubmatch(source)
	if len(match) != 2 {
		t.Fatal("readonly recovery fixture prompt unavailable")
	}
	if result := Assess(string(match[1])); !result.Ready {
		t.Fatalf("recovery fixture was rejected before execution: %v", result.Missing)
	}
}
