package compression

import (
	"strings"

	"github.com/ivo-lopes/ivoai/internal/core"
)

type PayloadType string

const (
	PayloadText             PayloadType = "text"
	PayloadJSON             PayloadType = "json"
	PayloadLog              PayloadType = "log"
	PayloadCode             PayloadType = "code"
	PayloadDiff             PayloadType = "diff"
	PayloadSearchResult     PayloadType = "search_result"
	PayloadWorkerOutput     PayloadType = "worker_output"
	PayloadMemoryResponse   PayloadType = "memory_response"
	PayloadContextResponse  PayloadType = "context_response"
	PayloadSkillRegistry    PayloadType = "skill_registry"
	PayloadSecurityEvidence PayloadType = "security_evidence"
	PayloadError            PayloadType = "error"
	PayloadStackTrace       PayloadType = "stack_trace"
	PayloadTestFailure      PayloadType = "test_failure"
	PayloadBuildFailure     PayloadType = "build_failure"
)

type FidelityInput struct {
	PayloadType      PayloadType
	Explicit         core.CompressionFidelity
	Authoritative    bool
	PolicyRelevant   bool
	Failed           bool
	RecoveryRequired bool
}

// Classify is deterministic and fail-safe. Unknown or authority-bearing data
// is never made eligible for lossy representation by inference.
func Classify(input FidelityInput) core.CompressionFidelity {
	if input.Explicit == core.CompressionExactRequired || input.Explicit == core.CompressionBypass {
		return input.Explicit
	}
	if input.Explicit == core.CompressionUnsupported {
		return core.CompressionUnsupported
	}
	if input.Authoritative || input.PolicyRelevant || input.Failed || input.RecoveryRequired {
		return core.CompressionExactRequired
	}
	switch input.PayloadType {
	case PayloadMemoryResponse, PayloadContextResponse, PayloadSkillRegistry, PayloadSecurityEvidence, PayloadError, PayloadStackTrace, PayloadTestFailure, PayloadBuildFailure:
		return core.CompressionExactRequired
	case PayloadText, PayloadJSON, PayloadLog, PayloadCode, PayloadDiff, PayloadSearchResult, PayloadWorkerOutput:
		return core.CompressionCompressible
	default:
		return core.CompressionExactRequired
	}
}

func NormalizePayloadType(value string) PayloadType {
	value = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(value, "-", "_")))
	switch PayloadType(value) {
	case PayloadText, PayloadJSON, PayloadLog, PayloadCode, PayloadDiff, PayloadSearchResult, PayloadWorkerOutput, PayloadMemoryResponse, PayloadContextResponse, PayloadSkillRegistry, PayloadSecurityEvidence, PayloadError, PayloadStackTrace, PayloadTestFailure, PayloadBuildFailure:
		return PayloadType(value)
	default:
		return ""
	}
}
