package codeintel

import (
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/observability"
)

// Observation deliberately excludes queries, filenames, source hashes, source
// text, error strings and exported artifacts. Nil metrics stay unavailable.
func Observation(r core.CodeResult, err error) observability.Event {
	provider := "graft"
	reason := observability.ReasonCapabilityMatch
	if r.ProviderVersion == "direct" {
		provider = "direct"
		reason = observability.ReasonDirect
	}
	state := observability.StateCompleted
	if err != nil {
		state = observability.StateDegraded
		reason = observability.ReasonProviderUnavailable
	}
	e := observability.Event{Category: observability.CategoryCapability, Operation: observability.OperationCodeInspect, State: state, Component: core.ComponentID("code_intelligence"), Provider: provider, RoutingReason: reason, DurationMilliseconds: r.DurationMS, BytesAfter: int64(len(r.Text)), Truncated: r.Truncated, FidelityClass: "exact_required", TokenBasis: "unavailable", PeakMemoryBytes: r.PeakMemoryBytes, CPUMilliseconds: r.CPUMS}
	if err == nil {
		e.CacheHit = &r.CacheHit
		e.IndexFresh = &r.Fresh
		e.SourceFiles = &r.Files
		if provider == "graft" {
			e.IndexFiles = &r.IndexedFiles
		}
	}
	return e
}
