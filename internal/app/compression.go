package app

import (
	"path/filepath"

	"github.com/ivo-lopes/ivoai/internal/caveman"
	"github.com/ivo-lopes/ivoai/internal/components"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

func (a *App) sessionCompression(cfg config.Config, state config.State, executor, runtimeDir string) (core.CompressionProvider, bool, string) {
	switch cfg.Compression.Provider {
	case "direct":
		return nil, false, "direct"
	case "caveman":
		spec, ok := a.componentSpec("caveman")
		if !ok {
			return nil, false, "direct"
		}
		expected, err := components.ResolvedSource(spec)
		if err != nil {
			return nil, false, "direct"
		}
		component := state.Components["caveman"]
		return caveman.Provider{
			Binary: component.Path, Managed: component.Managed,
			SupplyRoot: filepath.Join(a.Store.Paths.DataDir, "supply-chain"), Expected: expected, Runner: a.Runner,
		}, true, "caveman"
	default:
		if executor == "opencode" {
			return nil, false, "direct"
		}
		return nil, primaryHeadroomEnabled(cfg), "headroom"
	}
}

func (a *App) workingContextCompressor(cfg config.Config, state config.State, runtimeDir string) workingcontext.RepresentationCompressor {
	if cfg.Compression.Provider != "caveman" {
		return nil
	}
	spec, ok := a.componentSpec("caveman-mcp")
	if !ok {
		return nil
	}
	expected, err := components.ResolvedSource(spec)
	if err != nil {
		return nil
	}
	component := state.Components["caveman-mcp"]
	return caveman.MCPCompressor{Binary: component.Path, RuntimeDir: runtimeDir, SupplyRoot: filepath.Join(a.Store.Paths.DataDir, "supply-chain"), Expected: expected, Managed: component.Managed}
}

func compressionObservation(executor string, observation core.SessionObservation, policy sharedKnowledgeCompressionPolicy) observability.Event {
	requested := policy.RequestedProvider
	provider := observation.CompressionProvider
	reason := observability.ReasonDirect
	state := observability.StateSelected
	fallbackReason := observability.Reason("")
	if policy.Bypassed {
		provider, reason = "direct", observability.ReasonAuthoritativeSharedKnowledge
	} else if provider == "headroom" && observation.CompressionUsed {
		reason = observability.ReasonHeadroomEnabled
	} else if provider == "caveman" && observation.CompressionUsed {
		reason = observability.ReasonCavemanEnabled
	} else if requested == "caveman" {
		provider, reason = "direct", observability.ReasonCavemanFallback
		state = observability.StateDegraded
	} else if requested == "headroom" {
		provider, reason = "direct", observability.ReasonHeadroomBypassed
		state, fallbackReason = observability.StateDegraded, observability.ReasonPolicyDenied
	}
	if observation.CompressionFallback {
		state = observability.StateDegraded
	}
	return observability.Event{Category: observability.CategoryCompression, Operation: observability.OperationCompressionSelect, State: state, Executor: executor, Provider: provider, RequestedProvider: requested, ProviderSource: policy.ProviderSource, Component: core.ComponentCompression, RoutingReason: reason, FallbackReason: fallbackReason, DurationMilliseconds: observation.CompressionPreflightMilliseconds, CompressionBypassed: policy.Bypassed, AuthoritativeKnowledge: policy.AuthoritativeActive, SelectedSourceCount: policy.SelectedSourceCount}
}

func managedTurnCompressionObservation(executor string, observation core.SessionObservation, policy sharedKnowledgeCompressionPolicy, fidelity core.CompressionFidelity) observability.Event {
	event := compressionObservation(executor, observation, policy)
	if fidelity != core.CompressionExactRequired || observation.CompressionUsed || observation.CompressionProvider != "direct" {
		return event
	}
	event.FidelityClass = string(fidelity)
	if policy.RequestedProvider == "direct" {
		return event
	}
	event.Provider, event.State, event.RoutingReason = "direct", observability.StateSelected, observability.ReasonExactRequired
	event.CompressionBypassed, event.AuthoritativeKnowledge = true, true
	event.CompressionResult, event.FallbackReason = "bypassed", ""
	return event
}

func (a *App) componentSpec(name string) (components.Spec, bool) {
	catalog := components.DefaultCatalog()
	if a.componentCatalog != nil {
		catalog = a.componentCatalog()
	}
	for _, spec := range catalog {
		if spec.Name == name {
			return spec, true
		}
	}
	return components.Spec{}, false
}

// Fixtures can supply a reviewed catalog without mutating package-wide build
// metadata. Production keeps the existing client setup/compatibility policy.
func (a *App) setupComponentCatalog(includeLegacy bool) []components.Spec {
	if a.componentCatalog == nil {
		return components.ClientSetupCatalog(includeLegacy)
	}
	catalog := a.componentCatalog()
	if includeLegacy {
		return catalog
	}
	result := make([]components.Spec, 0, len(catalog))
	for _, spec := range catalog {
		if spec.Name != "opencode" {
			result = append(result, spec)
		}
	}
	return result
}
