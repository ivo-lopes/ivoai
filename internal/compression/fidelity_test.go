package compression

import (
	"testing"

	"github.com/ivo-lopes/ivoai/internal/core"
)

func TestExactRequiredPolicyFailsSafe(t *testing.T) {
	for _, payload := range []PayloadType{PayloadMemoryResponse, PayloadContextResponse, PayloadSkillRegistry, PayloadSecurityEvidence, PayloadError, PayloadStackTrace, PayloadTestFailure, PayloadBuildFailure, "unknown"} {
		if got := Classify(FidelityInput{PayloadType: payload}); got != core.CompressionExactRequired {
			t.Errorf("payload=%s fidelity=%s", payload, got)
		}
	}
	for _, payload := range []PayloadType{PayloadText, PayloadJSON, PayloadLog, PayloadCode, PayloadDiff, PayloadSearchResult, PayloadWorkerOutput} {
		if got := Classify(FidelityInput{PayloadType: payload}); got != core.CompressionCompressible {
			t.Errorf("payload=%s fidelity=%s", payload, got)
		}
	}
	if got := Classify(FidelityInput{PayloadType: PayloadText, Failed: true}); got != core.CompressionExactRequired {
		t.Fatalf("failed payload fidelity=%s", got)
	}
	if got := Classify(FidelityInput{PayloadType: PayloadText, PolicyRelevant: true}); got != core.CompressionExactRequired {
		t.Fatalf("policy payload fidelity=%s", got)
	}
}

func TestExplicitBypassCannotBeOverridden(t *testing.T) {
	if got := Classify(FidelityInput{PayloadType: PayloadText, Explicit: core.CompressionBypass}); got != core.CompressionBypass {
		t.Fatalf("fidelity=%s", got)
	}
}
