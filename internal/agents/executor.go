package agents

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

type CodexExecutor struct {
	Runtime Runtime
	Version string
	Managed bool
}

type ClaudeExecutor struct {
	Runtime Runtime
	Version string
	Managed bool
}

func (e CodexExecutor) ID() core.ComponentID  { return core.ComponentCodex }
func (e ClaudeExecutor) ID() core.ComponentID { return core.ComponentClaude }

func (e CodexExecutor) Probe(ctx context.Context) core.ComponentStatus {
	return probeExecutor(ctx, e.Runtime, core.ComponentCodex, "codex", e.Version, e.Managed)
}

func (e ClaudeExecutor) Probe(ctx context.Context) core.ComponentStatus {
	return probeExecutor(ctx, e.Runtime, core.ComponentClaude, "claude", e.Version, e.Managed)
}

func (e CodexExecutor) StartSession(ctx context.Context, request core.SessionRequest, observe func(core.SessionObservation)) error {
	return startSession(ctx, e.Runtime, "codex", request, observe)
}

func (e ClaudeExecutor) StartSession(ctx context.Context, request core.SessionRequest, observe func(core.SessionObservation)) error {
	return startSession(ctx, e.Runtime, "claude", request, observe)
}

func startSession(ctx context.Context, runtime Runtime, name string, request core.SessionRequest, observe func(core.SessionObservation)) error {
	return runtime.LaunchObserved(ctx, name, request.Args, request.CompressionEnabled, func(value Observation) {
		if observe != nil {
			observe(core.SessionObservation{PID: value.PID, CompressionUsed: value.HeadroomUsed})
		}
	})
}

func probeExecutor(ctx context.Context, runtime Runtime, id core.ComponentID, name, configuredVersion string, managed bool) core.ComponentStatus {
	status := core.ComponentStatus{
		ID: id, Implementation: "official-cli", Active: true, Managed: managed,
		Health: core.HealthUnavailable, Lifecycle: core.LifecycleStopped,
		Provenance: core.Provenance{Source: "unknown", Version: configuredVersion, Path: runtime.AgentPath},
		Capabilities: core.CapabilitySet{
			core.CapabilitySessionStart:    core.SupportSupported,
			core.CapabilitySessionAbort:    core.SupportSupported,
			core.CapabilityAdvisoryExecute: core.SupportNotExposed,
		},
		Compatibility: core.Compatibility{State: core.CompatibilityUnknown},
	}
	path := runtime.AgentPath
	var err error
	if path == "" && runtime.Runner != nil {
		path, err = runtime.Runner.LookPath(name)
	}
	if err != nil || path == "" {
		return status
	}
	status.Installed, status.Provenance.Path = true, path
	status.Provenance.Source = "runtime_verified"
	if runtime.Runner == nil {
		status.Health = core.HealthUnknown
		status.Compatibility = core.Compatibility{State: core.CompatibilityNotExposed}
		return status
	}
	result, runErr := runtime.Runner.Run(ctx, path, []string{"--version"}, platform.RunOptions{Timeout: 10 * time.Second})
	if runErr != nil {
		status.Health = core.HealthDegraded
		status.Compatibility = core.Compatibility{State: core.CompatibilityUnknown, Reason: "version probe failed"}
		return status
	}
	status.Available, status.Health, status.Lifecycle = true, core.HealthHealthy, core.LifecycleRunning
	status.Compatibility = core.Compatibility{State: core.CompatibilityCompatible}
	if version := strings.TrimSpace(result.Stdout); version != "" {
		status.Provenance.Version = version
	}
	return status
}

func ExecutorFor(name string, runtime Runtime, version string, managed bool) (core.Executor, error) {
	switch name {
	case "codex":
		return CodexExecutor{Runtime: runtime, Version: version, Managed: managed}, nil
	case "claude":
		return ClaudeExecutor{Runtime: runtime, Version: version, Managed: managed}, nil
	default:
		return nil, fmt.Errorf("unsupported agent %q", name)
	}
}

var _ core.Executor = CodexExecutor{}
var _ core.Executor = ClaudeExecutor{}
