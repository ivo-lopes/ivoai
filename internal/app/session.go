package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ivo-lopes/ivoai/internal/agents"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/orchestrator"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/terminalui"
	"github.com/ivo-lopes/ivoai/internal/workers"
)

func (a *App) SessionStart(ctx context.Context, executor string, mode session.Mode, args []string) error {
	if executor != "codex" && executor != "claude" {
		return errors.New("session executor must be codex or claude")
	}
	if mode != session.ModeDirect && mode != session.ModeOrchestrated {
		return errors.New("session mode must be direct or orchestrated")
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return err
	}
	state, err := a.Store.LoadState()
	if err != nil {
		return err
	}
	if err := validateManagedAgentRuntime(executor, state); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	id, err := session.NewID()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	value := session.Session{
		SessionID: id, StartedAt: now, UpdatedAt: now, Mode: mode, PrimaryExecutor: executor,
		WorkingDirectory: cwd, PrimaryModel: session.ResolveModel("", session.ParseModelArgument(args), executor, agentModelConfig(executor)),
		HeadroomRequested: cfg.Headroom.Enabled, RufloEnabled: mode == session.ModeOrchestrated,
		ProviderExecution: false, Workers: []session.Worker{}, MaxWorkers: cfg.Orchestration.MaxWorkers,
		ContextStatus: contextStatus(cfg), MemoryStatus: memoryStatus(cfg, state), ServerStatus: serverStatus(cfg), State: session.StateStarting,
	}
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	if err := store.Create(value); err != nil {
		return err
	}
	skillResult, err := a.evaluateSessionSkills(ctx, executor, cwd, args)
	if err != nil {
		_ = store.Delete(id)
		return err
	}
	value, err = store.Update(id, func(current *session.Session) error {
		return appendSkillObservations(skillResult.Events, id, func(event observability.Event) error {
			return session.AppendObservation(current, event)
		})
	})
	if err != nil {
		_ = store.Delete(id)
		return err
	}
	args = managedAgentArgs(executor, args, cfg, skillResult.Instructions)
	var control core.Orchestrator
	if mode == session.ModeOrchestrated {
		if !cfg.Orchestration.Enabled {
			_ = store.Delete(id)
			return errors.New("orchestration is disabled; enable it and run ivoai setup")
		}
		runtimeDir, runtimeErr := store.RuntimeDir(id)
		if runtimeErr != nil {
			_ = store.Delete(id)
			return runtimeErr
		}
		control = orchestration.RufloOrchestratorAdapter{Control: orchestration.ControlPlane{Manager: a.orchestrationManager(state), RuntimeDir: runtimeDir}, Managed: state.Components["ruflo"].Managed}
		swarm, initErr := control.Initialize(ctx, cfg.Orchestration.MaxWorkers)
		if initErr != nil {
			_ = store.CleanupRuntime(id)
			_ = store.Delete(id)
			return fmt.Errorf("orchestrated session refused: %w", initErr)
		}
		value, err = store.Update(id, func(current *session.Session) error {
			current.SwarmID, current.SwarmState = swarm.ID, "active"
			current.RufloHealthy, current.RufloSafeMode = true, true
			return nil
		})
		if err != nil {
			return err
		}
		taskID, registerErr := control.RegisterLifecycle(ctx, "primary", id)
		if registerErr != nil {
			a.finishSession(store, id, session.StateFailed, 1)
			_ = store.CleanupRuntime(id)
			return fmt.Errorf("register primary in Ruflo: %w", registerErr)
		}
		value, _ = store.Update(id, func(current *session.Session) error { current.PrimaryRufloTaskID = taskID; return nil })
		args, err = a.orchestratedAgentArgs(executor, args, id, runtimeDir)
		if err != nil {
			a.finishSession(store, id, session.StateFailed, 1)
			return err
		}
	}
	a.printSessionSummary(value, mode == session.ModeOrchestrated)
	environment, err := a.serverCredentialEnvironment()
	if err != nil {
		return err
	}
	component := executor
	if executor == "claude" {
		component = "claude-code"
	}
	runtime := agents.Runtime{Runner: a.Runner, In: a.In, Out: a.Out, Err: a.Err, AgentPath: state.Components[component].Path, HeadroomPath: state.Components["headroom"].Path, Environment: environment}
	implementation, err := agents.ExecutorFor(executor, runtime, state.Components[component].Version, state.Components[component].Managed)
	if err != nil {
		return err
	}
	useHeadroom := primaryHeadroomEnabled(cfg)
	if headroomBypassedForSharedKnowledge(cfg) {
		a.warn(sharedKnowledgeHeadroomBypass, nil)
	}
	launchErr := implementation.StartSession(ctx, core.SessionRequest{Args: args, CompressionEnabled: useHeadroom}, func(observation core.SessionObservation) {
		_, _ = store.Update(id, func(current *session.Session) error {
			current.PrimaryPID = observation.PID
			current.PrimaryProcessStart = session.ProcessStart(observation.PID)
			current.HeadroomUsed = observation.CompressionUsed
			current.State = session.StateRunning
			return nil
		})
	})
	exitCode, finalState := 0, session.StateCompleted
	if launchErr != nil {
		finalState, exitCode = session.StateFailed, 1
		var exitErr *agents.ExitError
		if errors.As(launchErr, &exitErr) {
			exitCode = exitErr.Code
		}
	}
	cleanupErr := a.cleanupSession(store, id, control)
	if cleanupErr != nil && launchErr == nil {
		launchErr, finalState, exitCode = fmt.Errorf("session cleanup failed: %w", cleanupErr), session.StateFailed, 1
	}
	a.finishSession(store, id, finalState, exitCode)
	return launchErr
}

func (a *App) SessionList() ([]session.Session, error) {
	return (session.Store{Root: a.Store.Paths.SessionsDir}).List()
}

func (a *App) SessionShow(id string) (session.Session, error) {
	return (session.Store{Root: a.Store.Paths.SessionsDir}).Get(id)
}

func (a *App) SessionStop(id string) error {
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	value, err := store.Get(id)
	if err != nil {
		return err
	}
	if !value.Active() {
		return errors.New("session is not active")
	}
	_, _ = store.Update(id, func(current *session.Session) error { current.State = session.StateStopping; return nil })
	var failures []error
	signalled := false
	for _, worker := range value.Workers {
		if session.ProcessMatches(worker.PID, worker.ProcessStart) {
			signalled = true
			if err := syscall.Kill(-worker.PID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
				failures = append(failures, err)
			}
		}
	}
	if session.ProcessMatches(value.PrimaryPID, value.PrimaryProcessStart) {
		signalled = true
		// Interactive primaries share ivoai's foreground process group so the
		// terminal can deliver job-control signals correctly. Signal the recorded
		// process itself; workers retain dedicated process groups.
		if err := syscall.Kill(value.PrimaryPID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			failures = append(failures, err)
		}
	}
	if !signalled && len(failures) == 0 {
		a.finishSession(store, id, session.StateFailed, 130)
	}
	return errors.Join(failures...)
}

func (a *App) OrchestratorServe(ctx context.Context, id string) error {
	cfg, err := a.Store.Load()
	if err != nil {
		return err
	}
	state, err := a.Store.LoadState()
	if err != nil {
		return err
	}
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	value, err := store.Get(id)
	if err != nil {
		return err
	}
	runtimeDir, err := store.RuntimeDir(id)
	if err != nil {
		return err
	}
	server := orchestrator.Server{
		Store: store, SessionID: id, Directory: value.WorkingDirectory, RuntimeDir: runtimeDir,
		ReviewExecutor:        cfg.Orchestration.ReviewExecutor,
		Adapter:               workers.Adapter{Runner: a.Runner, CodexPath: state.Components["codex"].Path, ClaudePath: state.Components["claude-code"].Path, HeadroomPath: state.Components["headroom"].Path, HeadroomEnabled: primaryHeadroomEnabled(cfg), KnowledgeServers: cfg.MCP.Servers},
		Control:               orchestration.RufloOrchestratorAdapter{Control: orchestration.ControlPlane{Manager: a.orchestrationManager(state), RuntimeDir: runtimeDir}, Managed: state.Components["ruflo"].Managed},
		Quota:                 a.automaticQuotaManager(cfg, state),
		CheckpointEnabled:     cfg.Orchestration.Auto.CheckpointEnabled,
		BootstrapRequired:     value.Mode == session.ModeAuto && cfg.Orchestration.Auto.Optimization.SharedContextBootstrap,
		ProgressiveEscalation: cfg.Orchestration.Auto.Optimization.ProgressiveEscalation,
		Parallelism:           cfg.Orchestration.Auto.Optimization.Parallelism,
		Weights:               routing.Weights{Complexity: cfg.Orchestration.Auto.Optimization.Weights.Complexity, Risk: cfg.Orchestration.Auto.Optimization.Weights.Risk, ReasoningDepth: cfg.Orchestration.Auto.Optimization.Weights.ReasoningDepth, VerificationNeed: cfg.Orchestration.Auto.Optimization.Weights.VerificationNeed, ContextBreadth: cfg.Orchestration.Auto.Optimization.Weights.ContextBreadth},
		Registry:              routing.Discoverer{CodexPath: state.Components["codex"].Path, ClaudePath: state.Components["claude-code"].Path, CachePath: filepath.Join(a.Store.Paths.CacheDir, "capabilities.json")}.Discover(ctx),
		Overrides:             routingOverrides(cfg.Orchestration.Auto.Profiles),
	}
	return server.Run(ctx)
}

func routingOverrides(value config.AutoProfilesConfig) map[string]map[routing.Tier]routing.ProfileOverride {
	convert := func(input config.AutoTierProfiles) map[routing.Tier]routing.ProfileOverride {
		return map[routing.Tier]routing.ProfileOverride{
			routing.TierLight:    {Model: input.Light.Model, Effort: input.Light.Effort},
			routing.TierBalanced: {Model: input.Balanced.Model, Effort: input.Balanced.Effort},
			routing.TierStrong:   {Model: input.Strong.Model, Effort: input.Strong.Effort},
			routing.TierMax:      {Model: input.Max.Model, Effort: input.Max.Effort},
		}
	}
	return map[string]map[routing.Tier]routing.ProfileOverride{"codex": convert(value.Codex), "claude": convert(value.Claude)}
}

func (a *App) orchestratedAgentArgs(executor string, existing []string, id, runtimeDir string) ([]string, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return nil, err
	}
	bridgeArgs := []string{"_orchestrator-serve", "--session", id}
	if executor == "codex" {
		command := "mcp_servers.ivoai-orchestrator.command=" + strconv.Quote(executable)
		encoded := make([]string, len(bridgeArgs))
		for index, value := range bridgeArgs {
			encoded[index] = strconv.Quote(value)
		}
		arguments := "mcp_servers.ivoai-orchestrator.args=[" + strings.Join(encoded, ",") + "]"
		return append([]string{"-c", command, "-c", arguments}, existing...), nil
	}
	configPath := filepath.Join(runtimeDir, "claude-mcp.json")
	body, err := json.Marshal(map[string]any{"mcpServers": map[string]any{"ivoai-orchestrator": map[string]any{"type": "stdio", "command": executable, "args": bridgeArgs}}})
	if err != nil {
		return nil, err
	}
	if err := platform.AtomicWritePrivate(body, configPath); err != nil {
		return nil, err
	}
	return append([]string{"--mcp-config", configPath}, existing...), nil
}

func (a *App) cleanupSession(store session.Store, id string, control core.Orchestrator) error {
	var cleanupErr error
	value, err := store.Get(id)
	if err == nil {
		for _, worker := range value.Workers {
			if session.ProcessMatches(worker.PID, worker.ProcessStart) {
				_ = syscall.Kill(-worker.PID, syscall.SIGTERM)
			}
			if worker.RufloTaskID != "" && control != nil {
				_ = control.CancelLifecycle(context.Background(), worker.RufloTaskID)
			}
		}
		if value.PrimaryRufloTaskID != "" && control != nil {
			_ = control.CancelLifecycle(context.Background(), value.PrimaryRufloTaskID)
		}
		if (value.Mode == session.ModeOrchestrated || value.Mode == session.ModeAuto) && control != nil {
			cleanupErr = control.Stop(context.Background())
		}
	}
	if err := store.CleanupRuntime(id); err != nil {
		cleanupErr = errors.Join(cleanupErr, err)
	}
	return cleanupErr
}

func (a *App) finishSession(store session.Store, id string, final session.State, exitCode int) {
	now := time.Now().UTC()
	_, _ = store.Update(id, func(value *session.Session) error {
		value.State, value.ExitCode = final, &exitCode
		if final == session.StateWaiting {
			value.EndedAt = nil
		} else {
			value.EndedAt = &now
		}
		if value.Mode == session.ModeOrchestrated || value.Mode == session.ModeAuto {
			value.SwarmState = "stopped"
		}
		return nil
	})
}

func (a *App) printSessionSummary(value session.Session, orchestrated bool) {
	color := terminalui.ColorEnabled(a.Out)
	fmt.Fprintf(a.Out, "IvoAI Session\n  Session       %s\n  Executor      %s\n  Mode          %s\n  Model         %s (%s)\n  Headroom      requested=%t\n", value.SessionID, value.PrimaryExecutor, strings.ToUpper(string(value.Mode)), value.PrimaryModel.Name, strings.ReplaceAll(string(value.PrimaryModel.Source), "_", " "), value.HeadroomRequested)
	if orchestrated {
		fmt.Fprintf(a.Out, "  Ruflo         %s\n  Swarm         %s\n", terminalui.Success("ACTIVE / provider disabled", color), value.SwarmID)
	}
	fmt.Fprintf(a.Out, "  Context       %s\n  ai-memory     %s\n\nStarting %s...\n", value.ContextStatus, value.MemoryStatus, value.PrimaryExecutor)
}

func agentModelConfig(executor string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if executor == "codex" {
		if configured := strings.TrimSpace(os.Getenv("CODEX_HOME")); filepath.IsAbs(configured) {
			return filepath.Join(configured, "config.toml")
		}
		return filepath.Join(home, ".codex", "config.toml")
	}
	if configured := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); filepath.IsAbs(configured) {
		return filepath.Join(configured, "settings.json")
	}
	return filepath.Join(home, ".claude", "settings.json")
}

func contextStatus(cfg config.Config) string {
	if server, ok := cfg.MCP.Servers["ivoai-context"]; ok && server.Enabled {
		return "configured"
	}
	if cfg.Connections.Server.Status == "connected" {
		return "degraded"
	}
	return "disabled"
}

func memoryStatus(cfg config.Config, state config.State) string {
	if !cfg.Memory.Enabled {
		return "disabled"
	}
	if componentPresent(state.Components["ai-memory"]) {
		return "configured"
	}
	return "degraded"
}

func serverStatus(cfg config.Config) string {
	if cfg.Connections.Server.Status == "connected" {
		return "configured"
	}
	return "not-connected"
}
