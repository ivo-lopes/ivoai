package app

import (
	"strings"
	"testing"
)

func TestAutomaticInstructionsEnforceFirstTurnSchedulerProtocol(t *testing.T) {
	value := automaticInstructions(true)
	for _, required := range []string{"one bounded lookup in ivoai-memory", "then one in ivoai-context", "orchestration_bootstrap", "orchestration_capabilities", "orchestration_plan", "orchestration_spawn_batch", "orchestration_wait", "lowest sufficient capability", "read-only coordinator", "native DAG scheduler", "orchestration_integrate", "ArtifactStore", "orchestration_artifact_read", "StateDelta is advisory", "Raw output must never be copied automatically"} {
		if !strings.Contains(value, required) {
			t.Fatalf("automatic instructions missing %q", required)
		}
	}
	if strings.Contains(value, "OPENAI_API_KEY") || strings.Contains(value, "ANTHROPIC_API_KEY") {
		t.Fatal("automatic instructions must not request provider credentials")
	}
}
