package headroom

import (
	"context"
	"errors"

	"github.com/ivo-lopes/ivoai/internal/core"
)

type HeadroomCompressionProvider struct {
	Manager Manager
	Enabled bool
	Managed bool
}

// The pinned wrappers register MCP/code-memory configuration in client-owned
// homes. --no-mcp removes recoverability, so it is not a safe substitute.
// This policy affects only IVOAI-managed wrapping, not the installed component.
var ErrUnavailableByPolicy = errors.New("headroom unavailable_by_policy: managed wrapper configuration and exact recovery are not safely isolated")

func (p HeadroomCompressionProvider) ID() core.ComponentID { return core.ComponentCompression }

func (p HeadroomCompressionProvider) Probe(ctx context.Context) core.ComponentStatus {
	value := p.Manager.Inspect(ctx, p.Enabled)
	health := core.HealthUnavailable
	if value.Installed {
		health = core.HealthDegraded
	}
	if value.Healthy {
		health = core.HealthHealthy
	}
	compatibility := core.Compatibility{State: core.CompatibilityIncompatible, Reason: "unavailable_by_policy"}
	return core.ComponentStatus{
		ID: core.ComponentCompression, Implementation: "headroom", Active: p.Enabled,
		Installed: value.Installed, Managed: p.Managed, Available: false,
		Health: health, Lifecycle: core.LifecycleStopped,
		Provenance: core.Provenance{Source: "binary_probe", Version: value.Version, Path: p.Manager.Binary},
		Capabilities: core.CapabilitySet{
			core.CapabilityCompressionWrap:   core.SupportUnsupported,
			core.CapabilityCompressionBypass: core.SupportSupported,
		},
		Compatibility: compatibility,
		Fallback:      core.Fallback{Allowed: true, Reason: "direct official executor remains available"},
	}
}

func (p HeadroomCompressionProvider) Prepare(ctx context.Context, request core.CompressionRequest) (core.CompressionLease, error) {
	decision := core.CompressionDecision{Command: request.DirectPath, Args: append([]string(nil), request.Args...), Environment: append([]string(nil), request.Environment...)}
	if !p.Enabled || request.Fidelity == core.CompressionExactRequired || request.Fidelity == core.CompressionBypass || request.Fidelity == core.CompressionUnsupported {
		return staticLease{decision: decision}, nil
	}
	if request.Executor == core.ComponentCodex || request.Executor == core.ComponentClaude {
		// Refuse before version/help probes, personal config registration or
		// proxy startup. Existing launchers fall back to Direct exactly once.
		return nil, ErrUnavailableByPolicy
	}
	return staticLease{decision: decision}, nil
}

type staticLease struct{ decision core.CompressionDecision }

func (l staticLease) Decision() core.CompressionDecision { return l.decision }
func (staticLease) Done() <-chan error                   { return nil }
func (staticLease) Close(context.Context) error          { return nil }

var _ core.CompressionProvider = HeadroomCompressionProvider{}
