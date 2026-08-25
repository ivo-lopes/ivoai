package app

import (
	"strings"
	"testing"
)

func TestAutomaticInstructionsEnforceFirstTurnSchedulerProtocol(t *testing.T) {
	value := automaticInstructions(true)
	for _, required := range []string{"exactly one bounded relevant lookup in ivoai-memory", "exactly one bounded relevant lookup in ivoai-context", "orchestration_bootstrap", "orchestration_plan", "orchestration_spawn_batch", "orchestration_wait", "lowest sufficient capability", "only authoritative writer", "provider_execution=false"} {
		if !strings.Contains(value, required) {
			t.Fatalf("automatic instructions missing %q", required)
		}
	}
	if strings.Contains(value, "OPENAI_API_KEY") || strings.Contains(value, "ANTHROPIC_API_KEY") {
		t.Fatal("automatic instructions must not request provider credentials")
	}
}
