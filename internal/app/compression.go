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
		spec, ok := componentSpec("caveman")
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
	spec, ok := componentSpec("caveman-mcp")
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

func compressionObservation(executor, requested string, observation core.SessionObservation) observability.Event {
	provider := observation.CompressionProvider
	reason := observability.ReasonDirect
	state := observability.StateSelected
	if provider == "headroom" && observation.CompressionUsed {
		reason = observability.ReasonHeadroomEnabled
	} else if provider == "caveman" && observation.CompressionUsed {
		reason = observability.ReasonCavemanEnabled
	} else if requested == "caveman" {
		provider, reason = "direct", observability.ReasonCavemanFallback
		state = observability.StateDegraded
	} else if requested == "headroom" {
		provider, reason = "direct", observability.ReasonHeadroomBypassed
	}
	if observation.CompressionFallback {
		state = observability.StateDegraded
	}
	return observability.Event{Category: observability.CategoryCompression, Operation: observability.OperationCompressionSelect, State: state, Executor: executor, Provider: provider, Component: core.ComponentCompression, RoutingReason: reason, DurationMilliseconds: observation.CompressionPreflightMilliseconds}
}

func componentSpec(name string) (components.Spec, bool) {
	for _, spec := range components.DefaultCatalog() {
		if spec.Name == name {
			return spec, true
		}
	}
	return components.Spec{}, false
}
