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
	"github.com/ivo-lopes/ivoai/internal/codeintel"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/externalmcp"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/platform"
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

// preparedAutoTurn binds one execution to its own release and completion
// policy. Run keeps this value local; a shared runner never stores a mutable
// validation callback for another turn to replace.
type preparedAutoTurn struct {
	Policy   core.ExecutionPolicy
	Runner   core.ExecutorRunner
	Release  func()
	Validate func() error
}

type autoTurnRunner struct {
	prepare func(context.Context, core.ExecutorRequest) (preparedAutoTurn, error)
	resolve func(core.ExecutorRequest) (core.ExecutorRequest, error)
}

type turnRunnerFunc func(context.Context, core.ExecutorRequest, func(string) error) (core.ExecutorResult, error)

func (f turnRunnerFunc) Run(ctx context.Context, request core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
	return f(ctx, request, emit)
}

func (r autoTurnRunner) Run(ctx context.Context, request core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
	if !promptgate.Assess(request.Prompt).Ready {
		return core.ExecutorResult{}, &core.ExecutorFailure{Class: "PROMPT_INSUFFICIENT", ExitCode: -1}
	}
	if r.resolve != nil {
		var err error
		request, err = r.resolve(request)
		if err != nil {
			return core.ExecutorResult{}, err
		}
	}
	prepared, err := r.prepare(ctx, request)
	if prepared.Release != nil {
		defer prepared.Release()
	}
	if err != nil {
		return core.ExecutorResult{Policy: prepared.Policy}, err
	}
	if prepared.Validate == nil {
		result, runErr := prepared.Runner.Run(ctx, request, emit)
		result.Policy.NoReplay = result.Policy.NoReplay || prepared.Policy.NoReplay
		result.Policy.NoAutomaticFailover = result.Policy.NoAutomaticFailover || prepared.Policy.NoAutomaticFailover
		return result, runErr
	}
	// Operational progress comes from status/events. Buffer the final answer
	// until the native DAG has completed; partial prose is not acceptance.
	var final strings.Builder
	result, err := prepared.Runner.Run(ctx, request, func(text string) error {
		if final.Len()+len(text) > 1<<20 {
			if prepared.Policy.NoReplay {
				return &core.ExecutorFailure{Class: "FINAL_RESPONSE_TOO_LARGE", ExitCode: 1}
			}
			return errors.New("bounded final response exceeded")
		}
		_, err := final.WriteString(text)
		return err
	})
	result.Policy.NoReplay = result.Policy.NoReplay || prepared.Policy.NoReplay
	result.Policy.NoAutomaticFailover = result.Policy.NoAutomaticFailover || prepared.Policy.NoAutomaticFailover
	if err != nil {
		return result, err
	}
	if err := prepared.Validate(); err != nil {
		return result, err
	}
	if final.Len() == 0 {
		return result, &core.ExecutorFailure{Class: "FINAL_RESPONSE_MISSING", ExitCode: 1}
	}
	return result, emit(final.String())
}

func (a *App) prepareAutoPromptKnowledge(ctx context.Context, cfg config.Config, selectors []string, prompt, executor, runtimeDir, cwd string, observe func(observability.Event)) (sessionKnowledge, error) {
	scope, err := conversationScopeID(cwd, cfg, selectors)
	if err != nil {
		return sessionKnowledge{}, err
	}
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
	return a.prepareSessionKnowledgeSelection(ctx, cfg, selectors, &selection, executor, runtimeDir, cwd, scope, os.Environ(), observe, true, codeintel.ProjectTools(prompt))
}

type autoTurnOptions struct {
	// Set only by the host that created a new Codex session in this process.
	FreshLocalEvidenceSession bool
}

func (a *App) scopeAutoRunner(base core.ExecutorRunner, original config.Config, state config.State, store session.Store, id, cwd, runtimeDir, instructionsPath string, agentArgs, selectors []string, current *autoTurnState, catalog opencodebridge.ModelCatalog, options ...autoTurnOptions) core.ExecutorRunner {
	enforceDAG := a.OpenCodeBridgeRunner == nil
	wrapped := autoTurnRunner{prepare: func(ctx context.Context, request core.ExecutorRequest) (preparedAutoTurn, error) {
		value, err := store.Get(id)
		if err != nil {
			return preparedAutoTurn{}, err
		}
		if session.NativeProvidersOnly(value) && request.Executor == "opencode" {
			return preparedAutoTurn{}, session.ErrOpenCodeManagedDeprecated
		}
		if err := store.CheckNativeProviders(value); err != nil {
			return preparedAutoTurn{}, err
		}
		if err := store.RetirePrimaryTurn(id); err != nil {
			return preparedAutoTurn{Policy: core.ExecutionPolicy{NoReplay: true, NoAutomaticFailover: true}}, err
		}
		if len(options) == 1 && options[0].FreshLocalEvidenceSession {
			if local, accepted, err := a.prepareLocalMetadataTurn(ctx, original, state, store, id, cwd, selectors, request); accepted {
				return local, err
			}
		}
		var adaptive *adaptivePrimaryTurn
		var policy core.ExecutionPolicy
		release := func() {}
		fail := func(err error) (preparedAutoTurn, error) {
			return preparedAutoTurn{Release: release, Policy: policy}, err
		}
		if len(options) == 1 && options[0].FreshLocalEvidenceSession {
			var accepted bool
			var err error
			adaptive, accepted, err = a.admitAdaptivePrimary(ctx, original, state, store, id, cwd, selectors, request)
			if accepted {
				policy = core.ExecutionPolicy{NoReplay: true, NoAutomaticFailover: true}
				release = adaptive.close
			}
			if err != nil {
				return fail(err)
			}
		}
		if adaptive != nil {
			if _, err := adaptive.check(ctx); err != nil {
				return fail(err)
			}
		}
		k, err := a.prepareAutoPromptKnowledge(ctx, original, selectors, request.Prompt, request.Executor, runtimeDir, cwd, func(event observability.Event) {
			_, _ = store.Update(id, func(value *session.Session) error {
				if adaptive != nil && !adaptive.boundTo(*value) {
					return errors.New("ADAPTIVE_PRIMARY_DENIED")
				}
				return session.AppendObservation(value, event)
			})
		})
		if err != nil {
			return fail(err)
		}
		release = func() {
			k.close()
			if adaptive != nil {
				adaptive.close()
			}
		}
		if adaptive != nil {
			if err := platform.AtomicWritePrivate([]byte(adaptivePrimaryInstructions(original.Orchestration.Auto.CheckpointEnabled)), instructionsPath); err != nil {
				return fail(err)
			}
			if k.router != nil {
				k.router.SetToolPolicy(func(callCtx context.Context, kind, tool string) bool {
					if !readOnlyInstitutionalTool(kind, tool) {
						return false
					}
					_, err := adaptive.check(callCtx)
					return err == nil
				})
			}
		}
		_, err = store.Update(id, func(value *session.Session) error {
			if adaptive != nil && !adaptive.boundTo(*value) {
				return errors.New("ADAPTIVE_PRIMARY_DENIED")
			}
			value.KnowledgeSources = k.aliases()
			// Prompt-local purpose routing changes active sources, never the
			// configured organization boundary used to reopen native history.
			if adaptive == nil {
				value.CurrentPhase = "planning"
			}
			if !value.RecoveryRequested {
				value.Tasks = nil
			}
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
			return fail(err)
		}
		if k.external != nil {
			selectedNativeTools := codeintel.ProjectTools(request.Prompt)
			k.external.SetInventoryObserver(func(_ string, metrics externalmcp.InventoryMetrics) {
				_, _ = store.Update(id, func(v *session.Session) error {
					if adaptive != nil && !adaptive.boundTo(*v) {
						return errors.New("ADAPTIVE_PRIMARY_DENIED")
					}
					return session.AppendObservation(v, observability.Event{Category: observability.CategoryCapability, Operation: observability.OperationCapabilityResolve, Component: "tools", Capability: "tool.projection", State: observability.StateCompleted, ToolsAvailable: &metrics.Available, ToolsPresented: &metrics.Presented, BytesBefore: int64(metrics.SchemaBytesBefore), BytesAfter: int64(metrics.SchemaBytesAfter), TokenBasis: "unavailable", RoutingReason: observability.ReasonContextBudgetApplied})
				})
			})
			k.external.SetToolAdmission(func(server, tool string) (bool, bool) {
				v, err := store.Get(id)
				if err != nil {
					return false, false
				}
				if adaptive != nil {
					admitted, err := adaptive.check(ctx)
					if err != nil {
						return false, false
					}
					v = admitted // Resolve DAG grants from the same revalidated snapshot.
					if admitted.PrimaryTurn.Mode == core.PrimaryReadOnly {
						if server == nativeCodeMCP && nativeCodeTool(tool) {
							for _, name := range selectedNativeTools {
								if name == tool {
									return true, false
								}
							}
						}
						return false, false
					}
				}
				return primaryMCPGrant(v, original, server, tool)
			})
			if adaptive != nil {
				k.external.RequirePolicyJSON()
				// Native code projection was already narrowed to this query.
				selected := selectedNativeTools
				k.external.SetToolProjection(func(server, tool string) bool {
					admitted, err := adaptive.check(ctx)
					if err != nil {
						return false
					}
					if admitted.PrimaryTurn.Mode == core.PrimaryDAG {
						return true
					}
					if server != nativeCodeMCP || !nativeCodeTool(tool) {
						return false
					}
					for _, name := range selected {
						if name == tool {
							return true
						}
					}
					return false
				})
			}
			k.external.SetAdmission(func() bool {
				if adaptive != nil {
					admitted, err := adaptive.check(ctx)
					if err != nil {
						return false
					}
					if admitted.PrimaryTurn.Mode == core.PrimaryReadOnly {
						return true
					}
				}
				v, err := store.Get(id)
				if err != nil || !v.Active() || v.RecoveryRequested || len(v.Tasks) == 0 || v.PlanID == "" {
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
					return fail(err)
				}
				spec := &cli.Codex
				if executor == "claude" {
					spec = &cli.Claude
				}
				if executor == "codex" && spec.Path != "" {
					args, err = (workers.Adapter{Runner: a.Runner}).IsolateCodexMCPs(ctx, spec.Path, args)
					if err != nil {
						return fail(err)
					}
				}
				spec.Args, spec.Env = args, inheritHookSessionEnvironment(executorBridgeEnvironment(k.environment), spec.Env)
				spec.ObserveProgress = func(trace core.ExecutionTrace) {
					encoded, err := json.Marshal(trace)
					if err == nil {
						_, _ = store.Update(id, func(v *session.Session) error {
							if adaptive != nil && !adaptive.boundTo(*v) {
								return errors.New("ADAPTIVE_PRIMARY_DENIED")
							}
							v.ExecutorTrace = encoded
							return nil
						})
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
				return fail(errors.New("native OpenCode scoped transport unavailable"))
			}
			instructions, err := os.ReadFile(instructionsPath)
			if err != nil {
				return fail(err)
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
				return fail(err)
			}
			environment, err := opencodebridge.NativeControlPlaneEnvironment(executorBridgeEnvironment(k.environment))
			if err != nil {
				return fail(err)
			}
			native.Options.NativeMCP["ivoai-orchestrator"] = map[string]any{"type": "local", "command": []string{executable, "_orchestrator-serve", "--session", id}, "environment": environment}
			runner = agents.RoutedRunner{Official: runner, Native: native}
		}
		current.set(k, native)
		prepared := preparedAutoTurn{Runner: runner, Release: release, Policy: policy}
		if adaptive != nil {
			prepared.Validate = func() error { return adaptive.validate(ctx) }
		} else if enforceDAG {
			prepared.Validate = func() error {
				v, err := store.Get(id)
				if err != nil {
					return err
				}
				return validateAutoDAG(v)
			}
		}
		return prepared, nil
	}}
	if enforceDAG {
		wrapped.resolve = func(request core.ExecutorRequest) (core.ExecutorRequest, error) {
			if request.SelectionMode == "auto" {
				model, ok := catalog.StrongPrimary(request.Executor)
				if !ok {
					return request, &core.ExecutorFailure{Class: "MODEL_UNAVAILABLE", ExitCode: 1}
				}
				request.Model, request.Effort = model.UpstreamModel, model.DefaultEffort
			}
			return request, nil
		}
	}
	return wrapped
}
