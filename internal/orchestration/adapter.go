package orchestration

import (
	"context"

	"github.com/ivo-lopes/ivoai/internal/core"
)

// RufloOrchestratorAdapter exposes only ephemeral swarm and lifecycle
// coordination. IVOAI remains authoritative for scheduling, inference,
// routing, quota and durable memory.
type RufloOrchestratorAdapter struct {
	Control ControlPlane
	Managed bool
}

func (a RufloOrchestratorAdapter) ID() core.ComponentID { return core.ComponentOrchestration }

func (a RufloOrchestratorAdapter) Probe(ctx context.Context) core.ComponentStatus {
	value := a.Control.Inspect(ctx)
	health := core.HealthUnavailable
	available := value.Installed && value.SafeMode && !value.ProviderExecution && !value.DurableMemory
	if value.Installed {
		health = core.HealthDegraded
	}
	if available {
		health = core.HealthHealthy
	}
	compatibility := core.Compatibility{State: core.CompatibilityUnknown}
	if available {
		compatibility.State = core.CompatibilityCompatible
	} else if value.Installed {
		compatibility = core.Compatibility{State: core.CompatibilityIncompatible, Reason: "safe profile is not verified"}
	}
	return core.ComponentStatus{
		ID: core.ComponentOrchestration, Implementation: "ruflo", Active: true,
		Installed: value.Installed, Managed: a.Managed, Available: available, Health: health,
		Lifecycle:  core.LifecycleStopped,
		Provenance: core.Provenance{Source: "runtime_verified", Version: value.Version, Path: a.Control.Binary},
		Capabilities: core.CapabilitySet{
			core.CapabilityOrchestrationSwarm:     supportState(available),
			core.CapabilityOrchestrationLifecycle: supportState(available),
		},
		Compatibility: compatibility,
		Fallback:      core.Fallback{Allowed: false, Reason: "orchestrated sessions fail closed when coordination is unsafe"},
	}
}

func (a RufloOrchestratorAdapter) Initialize(ctx context.Context, maxWorkers int) (core.Swarm, error) {
	value, err := a.Control.Initialize(ctx, maxWorkers)
	return core.Swarm{ID: value.ID, Healthy: value.Healthy}, err
}
func (a RufloOrchestratorAdapter) RegisterLifecycle(ctx context.Context, role, opaqueID string) (string, error) {
	return a.Control.RegisterLifecycle(ctx, role, opaqueID)
}
func (a RufloOrchestratorAdapter) CancelLifecycle(ctx context.Context, taskID string) error {
	return a.Control.CancelLifecycle(ctx, taskID)
}
func (a RufloOrchestratorAdapter) Stop(ctx context.Context) error { return a.Control.Stop(ctx) }

func supportState(value bool) core.SupportState {
	if value {
		return core.SupportSupported
	}
	return core.SupportUnsupported
}

var _ core.Orchestrator = RufloOrchestratorAdapter{}
