package orchestration

import (
	"context"
	"errors"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/session"
)

// NativeOrchestrator owns opaque lifecycle identities. Scheduling, task state,
// process ownership and cancellation remain in the existing session scheduler.
// No provider, subprocess or durable knowledge backend is started here. Ruflo
// remains supported by the separate explicit orchestrated-session adapter.
type NativeOrchestrator struct {
	Store     session.Store
	SessionID string
}

func (n NativeOrchestrator) ID() core.ComponentID { return core.ComponentOrchestration }

func (n NativeOrchestrator) Probe(context.Context) core.ComponentStatus {
	return core.ComponentStatus{
		ID: n.ID(), Implementation: "native", Installed: true, Managed: true, Available: true,
		Health: core.HealthHealthy, Lifecycle: core.LifecycleStopped,
		Compatibility: core.Compatibility{State: core.CompatibilityCompatible},
		Capabilities:  core.CapabilitySet{core.CapabilityOrchestrationSwarm: core.SupportSupported, core.CapabilityOrchestrationLifecycle: core.SupportSupported},
	}
}

func (n NativeOrchestrator) Initialize(ctx context.Context, limit int) (core.Swarm, error) {
	if err := ctx.Err(); err != nil {
		return core.Swarm{}, err
	}
	if limit < 1 || limit > session.MaxNativeWorkers {
		return core.Swarm{}, errors.New("invalid native worker limit")
	}
	value, err := n.Store.Get(n.SessionID)
	if err != nil || !value.Active() || value.Mode != session.ModeAuto || value.Coordinator != "native" || value.ProviderExecution {
		return core.Swarm{}, errors.New("native orchestration requires an active AUTO session")
	}
	return core.Swarm{ID: "native_" + n.SessionID, Healthy: true}, nil
}

func (n NativeOrchestrator) RegisterLifecycle(ctx context.Context, role, opaqueID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value, err := n.Store.Get(n.SessionID)
	if err != nil || !value.Active() || value.Coordinator != "native" {
		return "", errors.New("native session is not active")
	}
	owned := role == "primary" && opaqueID == n.SessionID
	if role == "worker" {
		for _, worker := range value.Workers {
			if worker.ID == opaqueID && worker.State == session.StateStarting {
				owned = true
			}
		}
	}
	if !owned {
		return "", errors.New("native lifecycle does not belong to session")
	}
	return "native_" + n.SessionID + "_" + opaqueID, nil
}

func (n NativeOrchestrator) CancelLifecycle(_ context.Context, id string) error {
	if !strings.HasPrefix(id, "native_"+n.SessionID+"_") {
		return errors.New("native lifecycle does not belong to session")
	}
	// The scheduler cancels the actual process using its owned context/process
	// group and records the result. This identity never authorizes killing a PID.
	return nil
}

func (n NativeOrchestrator) Stop(context.Context) error { return nil }

var _ core.Orchestrator = NativeOrchestrator{}
