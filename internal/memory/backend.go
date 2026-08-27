package memory

import (
	"context"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/core"
)

// AIMemoryBackend adapts the supported ai-memory client integration to the
// provider-neutral MemoryBackend contract. It does not expose memory contents;
// Web/runtime tool calls remain behind their existing authenticated boundary.
type AIMemoryBackend struct {
	Manager Manager
	Version string
	Managed bool
	Hooks   bool
}

func (b AIMemoryBackend) ID() core.ComponentID { return core.ComponentMemory }

func (b AIMemoryBackend) Probe(ctx context.Context) core.ComponentStatus {
	value, err := b.Manager.Status(ctx)
	installed := !strings.Contains(value, "not-installed")
	available := err == nil && value == "ready"
	health := core.HealthUnavailable
	if installed {
		health = core.HealthDegraded
	}
	if available {
		health = core.HealthHealthy
	}
	return core.ComponentStatus{
		ID: core.ComponentMemory, Implementation: "ai-memory", Active: true,
		Installed: installed, Managed: b.Managed, Available: available, Health: health,
		Lifecycle:  core.LifecycleRunning,
		Provenance: core.Provenance{Source: "runtime_verified", Version: b.Version, Path: b.Manager.Binary},
		Capabilities: core.CapabilitySet{
			core.CapabilityMemoryConfigure: core.SupportSupported,
			core.CapabilityMemoryHooks:     core.SupportSupported,
			core.CapabilityMemoryStatus:    core.SupportSupported,
		},
		Compatibility: core.Compatibility{State: core.CompatibilityCompatible},
	}
}

func (b AIMemoryBackend) Configure(ctx context.Context, value core.MemoryConfiguration) error {
	return b.Manager.ConfigureWith(ctx, Configuration{
		MCPEndpoint: value.MCPEndpoint, HooksBaseURL: value.HooksBaseURL, Token: value.Token,
		InstallMCP: value.InstallMCP, InstallHooks: value.InstallHooks,
	})
}

func (b AIMemoryBackend) Disable(ctx context.Context) error { return b.Manager.Disable(ctx) }
func (b AIMemoryBackend) Status(ctx context.Context) (string, error) {
	return b.Manager.Status(ctx)
}

var _ core.MemoryBackend = AIMemoryBackend{}
