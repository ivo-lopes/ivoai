// Package compression owns provider-neutral compression selection policy.
// Providers are alternatives, never a pipeline: one request selects Caveman,
// legacy Headroom, or direct execution.
package compression

import (
	"fmt"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/core"
)

type Implementation string

const (
	Caveman  Implementation = "caveman"
	Headroom Implementation = "headroom"
	Direct   Implementation = "direct"
)

type Candidate struct {
	Implementation Implementation
	Status         core.ComponentStatus
}

type Selection struct {
	Implementation Implementation
	Fidelity       core.CompressionFidelity
	Fallback       bool
	Reason         string
}

// Select chooses exactly one implementation. Exact-required and bypass data
// always select direct execution. A provider must be available, healthy and
// compatible; otherwise selection falls through in the caller-supplied order
// and ultimately to direct execution.
func Select(fidelity core.CompressionFidelity, candidates ...Candidate) (Selection, error) {
	switch fidelity {
	case core.CompressionExactRequired, core.CompressionBypass:
		return Selection{Implementation: Direct, Fidelity: fidelity, Reason: "compression bypass required by fidelity policy"}, nil
	case core.CompressionUnsupported:
		return Selection{Implementation: Direct, Fidelity: fidelity, Fallback: true, Reason: "content is unsupported by compression providers"}, nil
	case core.CompressionCompressible:
	default:
		return Selection{}, fmt.Errorf("unknown compression fidelity %q", fidelity)
	}

	var reasons []string
	for _, candidate := range candidates {
		if candidate.Implementation != Caveman && candidate.Implementation != Headroom {
			return Selection{}, fmt.Errorf("unknown compression implementation %q", candidate.Implementation)
		}
		status := candidate.Status
		if status.Available && status.Health == core.HealthHealthy && status.Compatibility.State == core.CompatibilityCompatible && status.Capabilities.Supports(core.CapabilityCompressionWrap) {
			return Selection{Implementation: candidate.Implementation, Fidelity: fidelity, Fallback: len(reasons) > 0, Reason: strings.Join(reasons, "; ")}, nil
		}
		reasons = append(reasons, string(candidate.Implementation)+" unavailable or incompatible")
	}
	return Selection{Implementation: Direct, Fidelity: fidelity, Fallback: len(candidates) > 0, Reason: strings.Join(reasons, "; ")}, nil
}
