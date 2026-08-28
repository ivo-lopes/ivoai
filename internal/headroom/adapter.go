package headroom

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/core"
)

type HeadroomCompressionProvider struct {
	Manager Manager
	Enabled bool
	Managed bool
}

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
	compatibility := core.Compatibility{State: core.CompatibilityUnknown}
	if value.Healthy && (value.CodexCompatible || value.ClaudeCompatible) {
		compatibility = core.Compatibility{State: core.CompatibilityCompatible}
	}
	if value.Installed && !value.CodexCompatible && !value.ClaudeCompatible {
		compatibility = core.Compatibility{State: core.CompatibilityIncompatible, Reason: "no supported executor wrapper was verified"}
	}
	return core.ComponentStatus{
		ID: core.ComponentCompression, Implementation: "headroom", Active: p.Enabled,
		Installed: value.Installed, Managed: p.Managed, Available: p.Enabled && value.Healthy,
		Health: health, Lifecycle: core.LifecycleStopped,
		Provenance: core.Provenance{Source: "runtime_verified", Version: value.Version, Path: p.Manager.Binary},
		Capabilities: core.CapabilitySet{
			core.CapabilityCompressionWrap:   support(value.Healthy),
			core.CapabilityCompressionBypass: core.SupportSupported,
		},
		Compatibility: compatibility,
		Fallback:      core.Fallback{Allowed: true, Reason: "direct official executor remains available"},
	}
}

func (p HeadroomCompressionProvider) Prepare(ctx context.Context, request core.CompressionRequest) (core.CompressionLease, error) {
	decision := core.CompressionDecision{Command: request.DirectPath, Args: append([]string(nil), request.Args...), Environment: append([]string(nil), request.Environment...)}
	if !p.Enabled {
		return staticLease{decision: decision}, nil
	}
	value := p.Manager.Inspect(ctx, true)
	compatible := request.Executor == core.ComponentCodex && value.CodexCompatible || request.Executor == core.ComponentClaude && value.ClaudeCompatible
	if !value.Healthy || !compatible {
		return staticLease{decision: decision}, nil
	}
	path := p.Manager.Binary
	if path == "" {
		var err error
		path, err = p.Manager.Runner.LookPath("headroom")
		if err != nil || path == "" {
			return staticLease{decision: decision}, nil
		}
	}
	name := string(request.Executor)
	decision.Command = path
	decision.Args = append([]string{"wrap", name, "--"}, request.Args...)
	decision.Environment = prependPath(request.Environment, filepath.Dir(request.DirectPath))
	decision.Used = true
	decision.Provider = "headroom"
	return staticLease{decision: decision}, nil
}

type staticLease struct{ decision core.CompressionDecision }

func (l staticLease) Decision() core.CompressionDecision { return l.decision }
func (staticLease) Done() <-chan error                   { return nil }
func (staticLease) Close(context.Context) error          { return nil }

func support(value bool) core.SupportState {
	if value {
		return core.SupportSupported
	}
	return core.SupportUnsupported
}

func prependPath(environment []string, directory string) []string {
	if environment == nil {
		environment = os.Environ()
	}
	result := append([]string(nil), environment...)
	for index, entry := range result {
		if strings.HasPrefix(entry, "PATH=") {
			result[index] = "PATH=" + directory + string(os.PathListSeparator) + strings.TrimPrefix(entry, "PATH=")
			return result
		}
	}
	return append(result, "PATH="+directory)
}

var _ core.CompressionProvider = HeadroomCompressionProvider{}
