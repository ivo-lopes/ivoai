package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/agents"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/doctor"
	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/session"
)

const maxAutomaticFailovers = 2

func (a *App) Auto(ctx context.Context, planner string, agentArgs []string) error {
	cfg, err := a.Store.Load()
	if err != nil {
		return err
	}
	if !cfg.Orchestration.Auto.Enabled {
		return errors.New("automatic orchestration is disabled")
	}
	if !cfg.Orchestration.Auto.Quota.Enabled {
		return errors.New("automatic quota routing is disabled")
	}
	if planner == "" {
		planner, err = a.selectPlanner(cfg.Orchestration.Auto.DefaultPlanner)
		if err != nil {
			return err
		}
	}
	planner = strings.ToLower(strings.TrimSpace(planner))
	if planner != "codex" && planner != "claude" {
		return errors.New("planner must be codex or claude")
	}
	state, err := a.Store.LoadState()
	if err != nil {
		return err
	}
	if err := validateManagedAgentRuntime("codex", state); err != nil {
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
	contextState, memoryState, serverState := a.autoServiceStatuses(ctx, cfg, state)
	value := session.Session{
		SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true,
		InitialPlanner: planner, CurrentPrimary: planner, PrimaryExecutor: planner,
		WorkingDirectory: cwd, PrimaryModel: session.ResolveModel("", session.ParseModelArgument(agentArgs), planner, agentModelConfig(planner)),
		HeadroomRequested: cfg.Headroom.Enabled, RufloEnabled: true, ProviderExecution: false,
		Workers: []session.Worker{}, MaxWorkers: cfg.Orchestration.Auto.MaxWorkers,
		ContextStatus: contextState, MemoryStatus: memoryState, ServerStatus: serverState,
		State: session.StateStarting, CurrentPhase: "quota_preflight", Quota: map[quota.Provider]quota.ProviderQuota{},
		OptimizationStrategy: cfg.Orchestration.Auto.Optimization.Strategy,
	}
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	if err := store.Create(value); err != nil {
		return err
	}
	manager := a.automaticQuotaManager(cfg, state)
	for _, provider := range []quota.Provider{quota.ProviderCodex, quota.ProviderClaude} {
		current, _ := manager.Probe(ctx, provider, true)
		value.Quota[provider] = current
	}
	_, _ = store.Update(id, func(current *session.Session) error { current.Quota = value.Quota; return nil })
	decision, err := manager.Resolve(ctx, quota.Provider(planner), "", false)
	if err != nil {
		_, _ = store.Update(id, func(current *session.Session) error {
			current.State, current.CurrentPhase = session.StateBlocked, "waiting_for_quota"
			return nil
		})
		a.printNoProvider(value.Quota)
		return err
	}
	current := string(decision.Resolved)
	value.CurrentPrimary, value.PrimaryExecutor = current, current
	if decision.Fallback {
		a.printStartupFallback(planner, current, decision.Reason)
	}
	value, err = store.Update(id, func(currentSession *session.Session) error {
		currentSession.CurrentPrimary, currentSession.PrimaryExecutor = current, current
		if decision.Fallback {
			now := time.Now().UTC()
			currentSession.FailoverCount = 1
			currentSession.LastFailoverAt = &now
			currentSession.LastFailoverReason = decision.Reason
		}
		return nil
	})
	if err != nil {
		return err
	}
	a.printAutoPreflight(value.Quota, current, value, cfg)
	runtimeDir, err := store.RuntimeDir(id)
	if err != nil {
		return err
	}
	control := orchestration.ControlPlane{Manager: a.orchestrationManager(state), RuntimeDir: runtimeDir}
	swarm, err := control.Initialize(ctx, cfg.Orchestration.Auto.MaxWorkers)
	if err != nil {
		_ = store.CleanupRuntime(id)
		_, _ = store.Update(id, func(current *session.Session) error { current.State = session.StateFailed; return nil })
		return fmt.Errorf("automatic session refused: %w", err)
	}
	value, err = store.Update(id, func(sessionValue *session.Session) error {
		sessionValue.SwarmID, sessionValue.SwarmState = swarm.ID, "active"
		sessionValue.RufloHealthy, sessionValue.RufloSafeMode = true, true
		sessionValue.CurrentPrimary, sessionValue.PrimaryExecutor = current, current
		sessionValue.CurrentPhase = "starting_primary"
		return nil
	})
	if err != nil {
		_ = control.Stop(context.Background())
		_ = store.CleanupRuntime(id)
		return err
	}
	taskID, err := control.RegisterLifecycle(ctx, "primary", id)
	if err != nil {
		_ = control.Stop(context.Background())
		_ = store.CleanupRuntime(id)
		return err
	}
	_, _ = store.Update(id, func(current *session.Session) error { current.PrimaryRufloTaskID = taskID; return nil })
	defer func() {
		_ = control.CancelLifecycle(context.Background(), taskID)
		_ = a.cleanupSession(store, id, control)
	}()
	instructionsPath := filepath.Join(runtimeDir, "automatic-instructions.md")
	if err := platform.AtomicWritePrivate([]byte(automaticInstructions(cfg.Orchestration.Auto.CheckpointEnabled)), instructionsPath); err != nil {
		return err
	}
	environment, err := a.serverCredentialEnvironment()
	if err != nil {
		return err
	}

	var handoff string
	for {
		launchArgs, argsErr := a.autoAgentArgs(current, agentArgs, id, runtimeDir, instructionsPath, handoff, cfg)
		if argsErr != nil {
			a.finishSession(store, id, session.StateFailed, 1)
			return argsErr
		}
		fmt.Fprintf(a.Out, "Starting %s...\n", displayProvider(current))
		launchCtx, cancelLaunch := context.WithCancel(ctx)
		limitReason := make(chan string, 1)
		monitorDone := make(chan struct{})
		go a.monitorPrimaryQuota(launchCtx, manager, quota.Provider(current), limitReason, cancelLaunch, cfg.Orchestration.Auto.QuotaRefreshSeconds, monitorDone)
		launchErr := a.launchAutomaticPrimary(launchCtx, store, id, current, launchArgs, state, cfg, environment)
		cancelLaunch()
		<-monitorDone
		reason := ""
		select {
		case reason = <-limitReason:
		default:
		}
		if reason == "" && launchErr != nil && ctx.Err() == nil {
			latest, _ := manager.Probe(context.Background(), quota.Provider(current), true)
			if latest.HardLimitReached {
				reason = latest.Reason
			}
		}
		if reason == "" {
			if launchErr != nil {
				a.finishSession(store, id, session.StateFailed, exitCode(launchErr))
				return launchErr
			}
			a.finishSession(store, id, session.StateCompleted, 0)
			return nil
		}
		if !cfg.Orchestration.Auto.AutomaticFailover {
			a.finishSession(store, id, session.StateBlocked, 1)
			return fmt.Errorf("%s quota exhausted and automatic failover is disabled", current)
		}
		updated, _ := store.Get(id)
		if updated.ConsecutiveFailovers >= maxAutomaticFailovers {
			a.finishSession(store, id, session.StateBlocked, 1)
			return errors.New("automatic failover limit reached; refusing a provider loop")
		}
		_ = manager.MarkExhausted(quota.Provider(current), reason)
		alternateDecision, routeErr := manager.Resolve(ctx, quota.Other(quota.Provider(current)), "", true)
		if routeErr != nil || alternateDecision.Resolved == quota.Provider(current) {
			a.finishSession(store, id, session.StateBlocked, 1)
			a.printNoProvider(value.Quota)
			return errors.New("no subscription-backed LLM is currently available")
		}
		previous := current
		current = string(alternateDecision.Resolved)
		handoff = a.failoverBootstrap(store, id, previous, current, reason, cwd)
		failoverAt := time.Now().UTC()
		_, err = store.Update(id, func(currentSession *session.Session) error {
			currentSession.FailoverCount++
			currentSession.ConsecutiveFailovers++
			currentSession.LastFailoverAt = &failoverAt
			currentSession.LastFailoverReason = reason
			currentSession.CurrentPrimary, currentSession.PrimaryExecutor = current, current
			currentSession.PrimaryModel = session.UnknownModel()
			currentSession.CurrentPhase = "automatic_failover"
			currentSession.State = session.StateStarting
			return nil
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(a.Out, "\nAutomatic Failover\nFrom       %s\nTo         %s\nReason     %s\nCheckpoint %s\nWorking tree preserved\n\n", displayProvider(previous), displayProvider(current), reason, checkpointLabel(store, id))
		agentArgs = nil
	}
}

func (a *App) automaticQuotaManager(cfg config.Config, state config.State) *quota.Manager {
	if a.QuotaManager != nil {
		return a.QuotaManager
	}
	ttl := time.Duration(cfg.Orchestration.Auto.QuotaRefreshSeconds) * time.Second
	manager := &quota.Manager{Store: quota.Store{Root: a.Store.Paths.QuotaDir}, TTL: ttl, Probes: map[quota.Provider]quota.Probe{
		quota.ProviderCodex:  quota.CodexAdapter{Binary: state.Components["codex"].Path},
		quota.ProviderClaude: quota.ClaudeAdapter{Binary: state.Components["claude-code"].Path, Runner: a.Runner, Store: quota.Store{Root: a.Store.Paths.QuotaDir}, TTL: ttl},
	}}
	return manager
}

func (a *App) selectPlanner(defaultPlanner string) (string, error) {
	if defaultPlanner == "" {
		defaultPlanner = "codex"
	}
	fmt.Fprintln(a.Out, "Automatic Orchestration\n\nSubscription quota (cached)")
	snapshot := quota.Snapshot{Providers: map[quota.Provider]quota.ProviderQuota{}}
	if a.Store != nil {
		snapshot, _ = (quota.Store{Root: a.Store.Paths.QuotaDir}).Load()
	}
	printQuotaSummary(a.Out, snapshot.Providers)
	codexDefault, claudeDefault := "", ""
	if defaultPlanner == "claude" {
		claudeDefault = " [default]"
	} else {
		codexDefault = " [default]"
	}
	fmt.Fprintf(a.Out, "\nPlanner / Primary\n  1. Codex%s\n  2. Claude Code%s\n", codexDefault, claudeDefault)
	answer, err := a.Prompt("\nSelect ["+defaultPlanner+"] > ", false)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return defaultPlanner, nil
		}
		return "", err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "":
		return defaultPlanner, nil
	case "1", "codex":
		return "codex", nil
	case "2", "claude", "claude code":
		return "claude", nil
	default:
		return "", errors.New("select codex or claude")
	}
}

func (a *App) launchAutomaticPrimary(ctx context.Context, store session.Store, id, executor string, args []string, state config.State, cfg config.Config, environment []string) error {
	component := executor
	if executor == "claude" {
		component = "claude-code"
	}
	runtime := agents.Runtime{Runner: a.Runner, In: a.In, Out: a.Out, Err: a.Err, AgentPath: state.Components[component].Path, HeadroomPath: state.Components["headroom"].Path, Environment: environment}
	return runtime.LaunchObserved(ctx, executor, args, primaryHeadroomEnabled(cfg), func(observation agents.Observation) {
		_, _ = store.Update(id, func(current *session.Session) error {
			current.PrimaryPID = observation.PID
			current.PrimaryProcessStart = session.ProcessStart(observation.PID)
			current.HeadroomUsed = observation.HeadroomUsed
			current.State, current.CurrentPhase = session.StateRunning, "conversation"
			return nil
		})
	})
}

func (a *App) autoAgentArgs(executor string, existing []string, id, runtimeDir, instructionsPath, handoff string, cfg config.Config) ([]string, error) {
	args, err := a.orchestratedAgentArgs(executor, existing, id, runtimeDir)
	if err != nil {
		return nil, err
	}
	if executor == "codex" {
		body, err := os.ReadFile(instructionsPath)
		if err != nil {
			return nil, err
		}
		args = append([]string{"-c", "developer_instructions=" + strconv.Quote(string(body))}, args...)
		args = codexSharedKnowledgeReadApprovalArgs(args, cfg)
	} else {
		settingsPath := filepath.Join(runtimeDir, "claude-auto-settings.json")
		executable, err := os.Executable()
		if err != nil {
			return nil, err
		}
		executable, err = filepath.EvalSymlinks(executable)
		if err != nil {
			return nil, err
		}
		statuslineCommand, err := a.claudeStatuslineCommand(id, runtimeDir, executable)
		if err != nil {
			return nil, err
		}
		settings := map[string]any{"statusLine": map[string]any{"type": "command", "command": statuslineCommand, "refreshInterval": 5}}
		body, err := json.Marshal(settings)
		if err != nil {
			return nil, err
		}
		if err := platform.AtomicWritePrivate(body, settingsPath); err != nil {
			return nil, err
		}
		args = append([]string{"--append-system-prompt-file", instructionsPath, "--settings", settingsPath}, args...)
	}
	if handoff != "" {
		args = append(args, handoff)
	}
	return args, nil
}

func (a *App) claudeStatuslineCommand(id, runtimeDir, executable string) (string, error) {
	capture := shellArgument(executable) + " _quota-statusline --session " + shellArgument(id)
	original := existingClaudeStatusline()
	if original == "" {
		return capture, nil
	}
	commandPath := filepath.Join(runtimeDir, "claude-original-statusline")
	if err := platform.AtomicWritePrivate([]byte(original), commandPath); err != nil {
		return "", err
	}
	wrapperPath := filepath.Join(runtimeDir, "claude-statusline-wrapper.sh")
	wrapper := `#!/bin/sh
set -u
umask 077
payload=$(mktemp ` + shellArgument(filepath.Join(runtimeDir, "statusline-payload.XXXXXX")) + `) || exit 1
trap 'rm -f -- "$payload"' EXIT HUP INT TERM
cat > "$payload" || exit 1
` + capture + ` < "$payload" || true
/bin/sh -c "$(cat ` + shellArgument(commandPath) + `)" < "$payload"
`
	if err := platform.AtomicWritePrivate([]byte(wrapper), wrapperPath); err != nil {
		return "", err
	}
	return "/bin/sh " + shellArgument(wrapperPath), nil
}

func existingClaudeStatusline() string {
	paths := []string{}
	if configDir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); filepath.IsAbs(configDir) {
		paths = append(paths, filepath.Join(configDir, "settings.json"))
	} else if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".claude", "settings.json"))
	}
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(cwd, ".claude", "settings.json"), filepath.Join(cwd, ".claude", "settings.local.json"))
	}
	command := ""
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var settings struct {
			StatusLine struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"statusLine"`
		}
		if json.Unmarshal(body, &settings) != nil || settings.StatusLine.Type != "command" {
			continue
		}
		candidate := strings.TrimSpace(settings.StatusLine.Command)
		if candidate != "" && len(candidate) <= 16<<10 && !strings.ContainsAny(candidate, "\x00\r\n") {
			command = candidate
		}
	}
	return command
}

func (a *App) monitorPrimaryQuota(ctx context.Context, manager *quota.Manager, provider quota.Provider, reason chan<- string, cancel context.CancelFunc, refreshSeconds int, done chan<- struct{}) {
	defer close(done)
	interval := time.Duration(refreshSeconds) * time.Second
	if a.AutoPollInterval > 0 {
		interval = a.AutoPollInterval
	}
	if interval < 30*time.Second && a.AutoPollInterval == 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			value, _ := manager.Probe(ctx, provider, true)
			if value.HardLimitReached {
				select {
				case reason <- value.Reason:
				default:
				}
				cancel()
				return
			}
		}
	}
}

func (a *App) failoverBootstrap(store session.Store, id, from, to, reason, cwd string) string {
	checkpoint, checkpointErr := store.LoadCheckpoint(id)
	if checkpointErr != nil {
		checkpoint = session.Checkpoint{Interrupted: true, NextStep: "Inspect the current repository state before continuing."}
		_ = store.SaveCheckpoint(id, checkpoint)
	}
	return fmt.Sprintf("IvoAI automatic failover.\n\nPrevious primary: %s\nNew primary: %s\nReason: %s\nThe last turn may have been interrupted.\n\nLast confirmed checkpoint:\n%s\n\nCurrent working tree summary:\n%s\n\nDo not repeat completed work. Inspect current state before continuing. Remain the conversation owner and use ivoai-orchestrator for bounded delegation.", displayProvider(from), displayProvider(to), reason, formatCheckpoint(checkpoint), a.worktreeSummary(cwd))
}

func (a *App) worktreeSummary(cwd string) string {
	parts := []string{}
	for _, args := range [][]string{{"status", "--short"}, {"diff", "--stat"}, {"diff", "--cached", "--stat"}} {
		path, err := a.Runner.LookPath("git")
		if err != nil {
			return "Git metadata unavailable; inspect the working directory directly."
		}
		result, err := a.Runner.Run(context.Background(), path, args, platform.RunOptions{Dir: cwd, Timeout: 5 * time.Second})
		if err == nil && strings.TrimSpace(result.Stdout) != "" {
			parts = append(parts, cleanBootstrap(result.Stdout))
		}
	}
	if len(parts) == 0 {
		return "Working tree clean or not a Git repository."
	}
	return strings.Join(parts, "\n")
}

func (a *App) QuotaStatusline(id string, body []byte) (string, error) {
	sessionStore := session.Store{Root: a.Store.Paths.SessionsDir}
	active, err := sessionStore.Get(id)
	if err != nil || active.Mode != session.ModeAuto || active.CurrentPrimary != "claude" || !active.Active() {
		return "", errors.New("statusline is not authorized for this session")
	}
	value, err := quota.ParseClaudeStatusline(body, time.Now().UTC())
	if err != nil {
		return "", err
	}
	store := quota.Store{Root: a.Store.Paths.QuotaDir}
	if err := store.Put(value); err != nil {
		return "", err
	}
	_, err = sessionStore.Update(id, func(current *session.Session) error {
		if current.Mode != session.ModeAuto || current.CurrentPrimary != "claude" || !current.Active() {
			return errors.New("statusline is not authorized for this session")
		}
		if current.Quota == nil {
			current.Quota = map[quota.Provider]quota.ProviderQuota{}
		}
		current.Quota[quota.ProviderClaude] = value
		if value.Model != "" {
			current.PrimaryModel = session.ModelInfo{Name: value.Model, Source: session.ModelRuntimeVerified}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	fiveHour := quotaValueFor(quota.ProviderClaude, value, quota.KindSession)
	weekly := quotaValueFor(quota.ProviderClaude, value, quota.KindWeekly)
	return fmt.Sprintf("ivoai auto · Claude 5h %s · weekly %s", fiveHour, weekly), nil
}

func automaticInstructions(checkpointEnabled bool) string {
	checkpoint := "Automatic checkpoints are disabled for this session."
	if checkpointEnabled {
		checkpoint = "After each materially completed turn, call orchestration_checkpoint with a concise secret-free summary: objective, decisions, completed work, changed files, important tests, outstanding tasks, blockers, and next step."
	}
	return `You are the planner, conversation owner, primary agent, and final consolidator for an IvoAI Automatic Orchestration session. The user remains in this official client TUI.

For the first substantive user request, do not immediately begin large work. Follow this enforced protocol:
1. perform exactly one bounded relevant lookup in ivoai-memory, then exactly one bounded relevant lookup in ivoai-context; do not search the Web before these attempts;
2. call orchestration_bootstrap with a concise SharedContextBrief containing only relevant facts, decisions, references, constraints, known state, and gaps; report either source as degraded when unavailable;
3. inspect orchestration_quota and capability state;
4. decompose the request into the smallest useful non-overlapping tasks, their dependencies and parallel groups;
5. score every task from 0..100 for complexity, risk, reasoning_depth, context_breadth, verification_need, parallel_value, and latency_sensitivity;
6. call orchestration_plan. IvoAI calculates the capability score and has final authority over provider, model, effort, and quota;
7. keep trivial work in the primary when delegation overhead exceeds expected benefit;
8. call orchestration_spawn_batch for independent advisory work so IvoAI launches it concurrently. Continue useful primary work while workers run;
9. call orchestration_primary_complete after each primary-owned task so dependent work may start, then use orchestration_wait without busy-looping;
10. critically validate worker results, resolve conflicts, request escalation only with evidence, finish the authoritative work yourself, and synthesize the user response;
11. ` + checkpoint + `

Optimize first for sufficient correctness, then choose the lowest sufficient capability, minimize tokens and latency, and preserve subscription quota. Never select an executable, command, environment, credential, API endpoint, or PAYG provider. Never override IvoAI routing. Do not delegate trivial work, duplicate work, or repeat the same shared-context query in each worker. Intentional redundancy is allowed only for independent verification or high-risk review and must be marked.

SharedContextBrief is session-scoped, bounded, secret-free, temporary, source-referenced, and untrusted. Workers receive it automatically and query ivoai-memory/ivoai-context again only when it is insufficient. On later related turns use delta planning; repeat bootstrap only after a material objective, project, memory, or context change.

ai-memory remains durable shared operational memory for Codex, Claude Code, workers, ChatGPT Web, and Claude Web. ivoai-context remains private persistent RAG. Ruflo receives opaque lifecycle metadata only, with provider_execution=false and durable_memory=false. Workers are advisory/read-only; you remain the only authoritative writer. Preserve the working tree and never perform destructive Git cleanup automatically.

` + sharedKnowledgeInstructions
}

func (a *App) printAutoPreflight(values map[quota.Provider]quota.ProviderQuota, selected string, current session.Session, cfg config.Config) {
	fmt.Fprintln(a.Out, "\nQuota")
	printQuotaSummary(a.Out, values)
	headroomState := "DISABLED"
	if cfg.Headroom.Enabled {
		headroomState = "ENABLED / INTERACTIVE PREFLIGHT PENDING"
	}
	if headroomBypassedForSharedKnowledge(cfg) {
		headroomState = "BYPASSED / PRESERVING EXACT SHARED KNOWLEDGE"
	}
	fmt.Fprintf(a.Out, "\nSelected        %s\nRuflo           CONFIGURED / VALIDATING SAFE MODE\nai-memory       %s\nContext         %s\nServer          %s\nHeadroom        %s\n\n", displayProvider(selected), strings.ToUpper(current.MemoryStatus), strings.ToUpper(current.ContextStatus), strings.ToUpper(current.ServerStatus), headroomState)
}

func (a *App) autoServiceStatuses(ctx context.Context, cfg config.Config, state config.State) (string, string, string) {
	contextState, memoryState, serverState := contextStatus(cfg), memoryStatus(cfg, state), serverStatus(cfg)
	health := doctor.ProbeServer(ctx, cfg.Connections.Server, a.statusHTTPClient())
	if !health.Configured {
		return contextState, memoryState, serverState
	}
	if !health.Reachable || !health.ProtocolCompatible || (!health.TLS && !loopbackURL(health.URL)) {
		if contextState != "disabled" {
			contextState = "degraded"
		}
		if memoryState != "disabled" {
			memoryState = "degraded"
		}
		return contextState, memoryState, "unreachable"
	}
	if contextState != "disabled" {
		contextState = "ready"
	}
	if memoryState != "disabled" {
		memoryState = "ready"
	}
	return contextState, memoryState, "reachable"
}

func (a *App) printStartupFallback(from, to, reason string) {
	fmt.Fprintf(a.Out, "\nRequested primary    %s\nAutomatic fallback  %s\nReason              %s\n", displayProvider(from), displayProvider(to), reason)
}

func (a *App) printNoProvider(values map[quota.Provider]quota.ProviderQuota) {
	fmt.Fprintln(a.Out, "No subscription-backed LLM is currently available.")
	printQuotaSummary(a.Out, values)
}

func printQuotaSummary(out io.Writer, values map[quota.Provider]quota.ProviderQuota) {
	codex := values[quota.ProviderCodex]
	claude := values[quota.ProviderClaude]
	fmt.Fprintf(out, "  Codex       weekly  %s\n", quotaValueFor(quota.ProviderCodex, codex, quota.KindWeekly))
	fmt.Fprintf(out, "  Claude Code 5h      %s\n", quotaValueFor(quota.ProviderClaude, claude, quota.KindSession))
	fmt.Fprintf(out, "              weekly  %s\n", quotaValueFor(quota.ProviderClaude, claude, quota.KindWeekly))
}

func quotaValueFor(provider quota.Provider, value quota.ProviderQuota, kind quota.Kind) string {
	window, ok := value.Window(kind)
	if !ok {
		if provider == quota.ProviderClaude && (kind == quota.KindSession || kind == quota.KindWeekly) {
			return "awaiting first response"
		}
		return "N/A / not exposed"
	}
	switch window.TelemetryState() {
	case quota.TelemetryPending:
		return "awaiting first response"
	case quota.TelemetryNotExposed:
		return "N/A / not exposed"
	case quota.TelemetryStale:
		if window.Available {
			return formatPercent(window.RemainingPercent) + "% remaining / stale"
		}
		return "stale telemetry"
	default:
		if !window.Available || !window.Authoritative {
			return "N/A / not exposed"
		}
		return formatPercent(window.RemainingPercent) + "% remaining"
	}
}

func formatPercent(value float64) string {
	if value == float64(int64(value)) {
		return fmt.Sprintf("%.0f", value)
	}
	return fmt.Sprintf("%.1f", value)
}

func displayProvider(value string) string {
	if value == "claude" {
		return "Claude Code"
	}
	return "Codex"
}

func shellArgument(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func cleanBootstrap(value string) string {
	value = platform.Redact(value)
	value = strings.ReplaceAll(value, "\x1b", "")
	if len(value) > 8192 {
		value = value[:8192] + "..."
	}
	return strings.TrimSpace(value)
}

func formatCheckpoint(value session.Checkpoint) string {
	body, err := json.Marshal(value)
	if err != nil {
		return "Checkpoint unavailable."
	}
	return cleanBootstrap(string(body))
}

func checkpointLabel(store session.Store, id string) string {
	if _, err := store.LoadCheckpoint(id); err == nil {
		return "restored"
	}
	return "unavailable"
}

func exitCode(err error) int {
	var value *agents.ExitError
	if errors.As(err, &value) {
		return value.Code
	}
	return 1
}
