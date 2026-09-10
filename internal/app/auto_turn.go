package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ivo-lopes/ivoai/internal/agents"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/promptgate"
	"github.com/ivo-lopes/ivoai/internal/serverpool"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workers"
)

// autoTurnState exposes only the current process-local transport to UI
// callbacks. A later turn cannot inherit the previous turn's capability.
type autoTurnState struct {
	mu        sync.RWMutex
	knowledge sessionKnowledge
	native    *agents.NativeOpenCode
}

func (s *autoTurnState) snapshot() (sessionKnowledge, *agents.NativeOpenCode) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.knowledge, s.native
}

func (s *autoTurnState) set(k sessionKnowledge, n *agents.NativeOpenCode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.knowledge, s.native = k, n
}

type autoTurnRunner struct {
	prepare  func(context.Context, opencodebridge.ExecutorRequest) (opencodebridge.ExecutorRunner, func(), error)
	resolve  func(opencodebridge.ExecutorRequest) (opencodebridge.ExecutorRequest, error)
	validate func() error
}

func (r autoTurnRunner) Run(ctx context.Context, request opencodebridge.ExecutorRequest, emit func(string) error) (opencodebridge.ExecutorResult, error) {
	if !promptgate.Assess(request.Prompt).Ready {
		return opencodebridge.ExecutorResult{}, &opencodebridge.ExecutorFailure{Class: "PROMPT_INSUFFICIENT", ExitCode: -1}
	}
	if r.resolve != nil {
		var err error
		request, err = r.resolve(request)
		if err != nil {
			return opencodebridge.ExecutorResult{}, err
		}
	}
	runner, close, err := r.prepare(ctx, request)
	if err != nil {
		return opencodebridge.ExecutorResult{}, err
	}
	defer close()
	if r.validate == nil {
		return runner.Run(ctx, request, emit)
	}
	// Operational progress comes from status/events. Buffer the final answer
	// until the native DAG has completed; partial prose is not acceptance.
	var final strings.Builder
	result, err := runner.Run(ctx, request, func(text string) error {
		if final.Len()+len(text) > 1<<20 {
			return errors.New("bounded final response exceeded")
		}
		_, err := final.WriteString(text)
		return err
	})
	if err != nil {
		return result, err
	}
	if err := r.validate(); err != nil {
		return result, err
	}
	if final.Len() == 0 {
		return result, &opencodebridge.ExecutorFailure{Class: "FINAL_RESPONSE_MISSING", ExitCode: 1}
	}
	return result, emit(final.String())
}

func (a *App) prepareAutoPromptKnowledge(ctx context.Context, cfg config.Config, selectors []string, prompt, executor, runtimeDir string, observe func(observability.Event)) (sessionKnowledge, error) {
	pool, err := serverpool.New(cfg.Connections.Servers)
	if err != nil {
		return sessionKnowledge{}, err
	}
	selection, err := pool.ResolvePurposes(serverpool.KnowledgePolicy(cfg.Orchestration.Auto.ResolvedKnowledgeRouting()), selectors, pool.MentionedPurposes(prompt))
	if err != nil {
		return sessionKnowledge{}, err
	}
	if len(selection.Groups) == 0 {
		// An empty purpose selection is not the old singleton/default route.
		// Preserve the operator's config, but remove inherited institutional
		// transports from this execution's projection.
		servers := map[string]config.MCPServer{}
		for name, entry := range cfg.MCP.Servers {
			if entry.Kind == "external" {
				servers[name] = entry
			}
		}
		cfg.MCP.Servers = servers
	}
	return a.prepareSessionKnowledgeSelection(ctx, cfg, nil, &selection, executor, runtimeDir, os.Environ(), observe, true)
}

func (a *App) scopeAutoRunner(base opencodebridge.ExecutorRunner, original config.Config, state config.State, store session.Store, id, cwd, runtimeDir, instructionsPath string, agentArgs, selectors []string, current *autoTurnState, catalog opencodebridge.ModelCatalog) opencodebridge.ExecutorRunner {
	wrapped := autoTurnRunner{prepare: func(ctx context.Context, request opencodebridge.ExecutorRequest) (opencodebridge.ExecutorRunner, func(), error) {
		k, err := a.prepareAutoPromptKnowledge(ctx, original, selectors, request.Prompt, request.Executor, runtimeDir, func(event observability.Event) {
			_, _ = store.Update(id, func(value *session.Session) error { return session.AppendObservation(value, event) })
		})
		if err != nil {
			return nil, nil, err
		}
		_, err = store.Update(id, func(value *session.Session) error {
			value.KnowledgeSources = k.aliases()
			value.KnowledgeScopeID = knowledgeScopeID(cwd, k)
			value.CurrentPhase = "planning"
			value.Tasks = nil
			value.KnowledgeBootstrap = session.BootstrapMetadata{}
			value.MemoryStatus, value.ContextStatus = "disabled", "disabled"
			if k.config.MCP.Servers["ivoai-memory"].Enabled {
				value.MemoryStatus = "configured"
			}
			if k.config.MCP.Servers["ivoai-context"].Enabled {
				value.ContextStatus = "configured"
			}
			return nil
		})
		if err != nil {
			k.close()
			return nil, nil, err
		}
		if k.external != nil {
			k.external.SetAdmission(func() bool {
				v, err := store.Get(id)
				if err != nil || !v.Active() || len(v.Tasks) == 0 || v.PlanID == "" {
					return false
				}
				if original.Orchestration.Auto.ResolvedPlanExecution() == "immediate" {
					return true
				}
				for _, d := range v.Decisions {
					if d.Kind == "plan" && d.ID == v.PlanID && d.State == "approved" {
						return true
					}
				}
				return false
			})
		}
		runner := base
		_, managedCLI := base.(opencodebridge.CLIRunner)
		if routed, ok := runner.(agents.RoutedRunner); ok {
			managedCLI = true
			runner = routed.Official
		}
		if cli, ok := runner.(opencodebridge.CLIRunner); ok {
			for _, executor := range []string{"codex", "claude"} {
				args, err := a.autoBridgeArgs(executor, agentArgs, id, runtimeDir, instructionsPath, k.config)
				if err != nil {
					k.close()
					return nil, nil, err
				}
				spec := &cli.Codex
				if executor == "claude" {
					spec = &cli.Claude
				}
				if executor == "codex" && spec.Path != "" {
					args, err = (workers.Adapter{Runner: a.Runner}).IsolateCodexMCPs(ctx, spec.Path, args)
					if err != nil {
						k.close()
						return nil, nil, err
					}
				}
				spec.Args, spec.Env = args, executorBridgeEnvironment(k.environment)
				spec.ObserveProgress = func(trace opencodebridge.ExecutionTrace) {
					encoded, err := json.Marshal(trace)
					if err == nil {
						_, _ = store.Update(id, func(v *session.Session) error { v.ExecutorTrace = encoded; return nil })
					}
				}
				if executor == "claude" {
					spec.Env = setAppEnvironment(spec.Env, "DISABLE_AUTOUPDATER", "1")
				}
			}
			runner = cli
		}
		var native *agents.NativeOpenCode
		if request.Executor == "opencode" && managedCLI {
			native = a.nativeOpenCode(k.config, state, cwd, filepath.Join(runtimeDir, "native-turn"), k.environment, false)
			if native == nil {
				k.close()
				return nil, nil, errors.New("native OpenCode scoped transport unavailable")
			}
			instructions, err := os.ReadFile(instructionsPath)
			if err != nil {
				k.close()
				return nil, nil, err
			}
			native.Options.Instructions = string(instructions) + "\n\n" + native.Options.Instructions
			// Native primary follows the same read-only coordinator contract;
			// the approved scheduler, not frontend tools, performs integration.
			native.Options.NativePermissions = opencodebridge.NativePermissionPolicy("full", true)
			native.Options.NativePermissions["ivoai-orchestrator_*"] = "allow"
			for _, name := range externalMCPNames(k.config) {
				native.Options.NativePermissions[name+"_*"] = "allow"
			}
			executable, err := os.Executable()
			if err != nil {
				k.close()
				return nil, nil, err
			}
			environment, err := opencodebridge.NativeControlPlaneEnvironment(executorBridgeEnvironment(k.environment))
			if err != nil {
				k.close()
				return nil, nil, err
			}
			native.Options.NativeMCP["ivoai-orchestrator"] = map[string]any{"type": "local", "command": []string{executable, "_orchestrator-serve", "--session", id}, "environment": environment}
			runner = agents.RoutedRunner{Official: runner, Native: native}
		}
		current.set(k, native)
		return runner, k.close, nil
	}}
	if a.OpenCodeBridgeRunner == nil {
		wrapped.resolve = func(request opencodebridge.ExecutorRequest) (opencodebridge.ExecutorRequest, error) {
			if request.SelectionMode == "auto" {
				model, ok := catalog.StrongPrimary(request.Executor)
				if !ok {
					return request, &opencodebridge.ExecutorFailure{Class: "MODEL_UNAVAILABLE", ExitCode: 1}
				}
				request.Model, request.Effort = model.UpstreamModel, model.DefaultEffort
			}
			return request, nil
		}
		wrapped.validate = func() error {
			v, err := store.Get(id)
			if err != nil {
				return err
			}
			if v.CurrentPhase != "synthesizing" || len(v.Tasks) == 0 {
				return &opencodebridge.ExecutorFailure{Class: "DAG_INCOMPLETE", ExitCode: 1}
			}
			for _, task := range v.Tasks {
				if task.State != session.StateCompleted {
					return &opencodebridge.ExecutorFailure{Class: "ACCEPTANCE_INCOMPLETE", ExitCode: 1}
				}
			}
			return nil
		}
	}
	return wrapped
}
