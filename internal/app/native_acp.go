package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/codeintel"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/evidence"
	"github.com/ivo-lopes/ivoai/internal/nativeacp"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/promptgate"
	"github.com/ivo-lopes/ivoai/internal/session"
)

const antigravityFlag = "IVOAI_EXPERIMENTAL_ANTIGRAVITY"
const antigravitySHA = "b9043860a06a3bfa70b68f06c3d4527abc16207767f14ae9d1e06e3026081862"

// NativeRunOptions is an explicit, bounded headless invocation, not a TUI.
// Followups share one live host lease and authentication process epoch. Cross-
// process resume remains unavailable until official account identity is exposed.
type NativeRunOptions struct {
	Prompt, Directory, Model, Effort string
	Followups                        []string
	Approve                          func(context.Context, opencodebridge.PermissionView) (bool, error)
}

type nativeACPGuard struct {
	app                    *App
	store                  session.Store
	id, cwd, scope, binary string
	rootInfo               os.FileInfo
	binaryInfo             os.FileInfo
	receipt                core.PrimaryTurnReceipt
}

func reviewedAntigravity(ctx context.Context) (string, os.FileInfo, error) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return "", nil, errors.New("ANTIGRAVITY_PLATFORM_UNCERTIFIED")
	}
	path, err := exec.LookPath("agy")
	if err != nil {
		return "", nil, errors.New("ANTIGRAVITY_RUNTIME_UNAVAILABLE")
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, err
	}
	before, err := adaptiveFileInfo(path)
	if err != nil {
		return "", nil, err
	}
	sum, err := adaptiveBinarySHA(ctx, path)
	after, statErr := adaptiveFileInfo(path)
	if err != nil || statErr != nil || sum != antigravitySHA || !sameAdaptiveFile(before, after) {
		return "", nil, errors.New("ANTIGRAVITY_RUNTIME_UNREVIEWED")
	}
	return path, after, nil
}

func nativeACPScope(scope, root, binary string, info os.FileInfo) string {
	return evidence.Digest(adaptiveScopeDigest(scope, root, info), binary, antigravitySHA)
}

func (g *nativeACPGuard) bound(v session.Session) bool {
	if !session.PrimaryTurnActive(v) || v.Frontend != "antigravity" || v.WorkingDirectory != g.cwd || v.KnowledgeScopeID != g.scope || !session.ProcessMatches(v.FrontendPID, v.FrontendProcessStart) {
		return false
	}
	r := *v.PrimaryTurn
	r.Mode, r.PlanID = g.receipt.Mode, g.receipt.PlanID
	return r == g.receipt
}

func (g *nativeACPGuard) check(ctx context.Context) (session.Session, error) {
	deny := func() (session.Session, error) { return session.Session{}, errors.New("ANTIGRAVITY_ADMISSION_DENIED") }
	if ctx.Err() != nil {
		return session.Session{}, ctx.Err()
	}
	v, err := g.store.Get(g.id)
	if err != nil || !g.bound(v) {
		return deny()
	}
	cfg, err := g.app.Store.Load()
	if err != nil || evidence.Digest(cfg) != g.receipt.ConfigDigest {
		return deny()
	}
	state, err := g.app.Store.LoadState()
	if err != nil || evidence.Digest(state) != g.receipt.StateDigest {
		return deny()
	}
	root, err := filepath.EvalSymlinks(g.cwd)
	if err != nil || root != g.cwd {
		return deny()
	}
	info, err := os.Stat(root)
	if err != nil || !os.SameFile(info, g.rootInfo) || nativeACPScope(g.scope, g.cwd, g.binary, info) != g.receipt.ScopeDigest {
		return deny()
	}
	info, err = adaptiveFileInfo(g.binary)
	if err != nil || !sameAdaptiveFile(g.binaryInfo, info) {
		return deny()
	}
	return v, ctx.Err()
}

func (a *App) bindNativeACPServer(ctx context.Context, store session.Store, v session.Session) (*nativeACPGuard, error) {
	if v.PrimaryTurn == nil || v.PrimaryTurn.Executor != "antigravity" || !session.PrimaryTurnActive(v) {
		return nil, errors.New("ANTIGRAVITY_ADMISSION_DENIED")
	}
	binary, binaryInfo, err := reviewedAntigravity(ctx)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(v.WorkingDirectory)
	if err != nil {
		return nil, err
	}
	g := &nativeACPGuard{a, store, v.SessionID, v.WorkingDirectory, v.KnowledgeScopeID, binary, info, binaryInfo, *v.PrimaryTurn}
	_, err = g.check(ctx)
	return g, err
}

func (g *nativeACPGuard) consume(ctx context.Context) error {
	if _, err := g.check(ctx); err != nil {
		return err
	}
	_, err := g.store.Update(g.id, func(v *session.Session) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !g.bound(*v) {
			return errors.New("ANTIGRAVITY_ADMISSION_DENIED")
		}
		if v.PrimaryTurn.Mode == core.PrimaryDAG {
			if v.PrimaryTurn.PlanID == "" || v.PrimaryTurn.PlanID != v.PlanID {
				return errors.New("DAG_INCOMPLETE")
			}
			if err := validateAutoDAG(*v); err != nil {
				return err
			}
		} else if v.PrimaryTurn.Mode != core.PrimaryReadOnly || v.PlanID != "" || len(v.Tasks) != 0 || len(v.Workers) != 0 {
			return errors.New("ANTIGRAVITY_TURN_INCOMPLETE")
		}
		v.PrimaryTurn.Mode = core.PrimaryClosed
		return nil
	})
	return err
}

// AntigravityRun enters the existing managed turn bridge and scheduler. It adds
// no scheduler, prompt-quality classifier, credential store or conversational UI.
func (a *App) AntigravityRun(ctx context.Context, options NativeRunOptions) (runErr error) {
	if os.Getenv(antigravityFlag) != "1" {
		return errors.New("ANTIGRAVITY_EXPERIMENTAL_DISABLED: set IVOAI_EXPERIMENTAL_ANTIGRAVITY=1 for the certified headless path; native TUI interception is not available")
	}
	if len(options.Followups) > 8 {
		return errors.New("ANTIGRAVITY_INPUT_LIMIT")
	}
	prompts := append([]string{options.Prompt}, options.Followups...)
	for _, prompt := range prompts {
		if err := promptgate.ValidateInput(prompt); err != nil {
			return err
		}
	}
	binary, _, err := reviewedAntigravity(ctx)
	if err != nil {
		return err
	}
	cwd := options.Directory
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return err
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return err
	}
	if err = nativeWorkspaceBoundary(cwd); err != nil {
		return err
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return err
	}
	if !cfg.Orchestration.Auto.Enabled {
		return errors.New("automatic orchestration is disabled")
	}
	state, err := a.Store.LoadState()
	if err != nil {
		return err
	}
	scope, err := conversationScopeID(cwd, cfg, nil)
	if err != nil {
		return err
	}
	id, err := session.NewID()
	if err != nil {
		return err
	}
	frontendID, err := session.NewNativeUUID()
	if err != nil {
		return err
	}
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	lease, err := store.Acquire(id)
	if err != nil {
		return err
	}
	defer lease.Close()
	now := time.Now().UTC()
	limit := cfg.Orchestration.Auto.WorkerCap
	if limit == 0 {
		limit = session.MaxNativeWorkers
	}
	v := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true, Coordinator: "native", InitialPlanner: "antigravity", CurrentPrimary: "antigravity", PrimaryExecutor: "antigravity", Frontend: "antigravity", WorkingDirectory: cwd, KnowledgeScopeID: scope, State: session.StateStarting, FrontendState: session.StateRunning, CurrentPhase: "intake", MaxWorkers: limit, Workers: []session.Worker{}}
	v.SetFrontend("antigravity", frontendID)
	v.PrimaryModel = session.ModelInfo{Name: "unknown", Source: session.ModelUnknown}
	v.ContextStatus, v.MemoryStatus, v.ServerStatus = contextStatus(cfg), memoryStatus(cfg, state), serverStatus(cfg)
	v.FrontendPID, v.FrontendProcessStart = os.Getpid(), session.ProcessStart(os.Getpid())
	// Optional Codex workers retain the existing exact reviewed managed pair;
	// the Antigravity primary never grants an unverified worker runtime.
	if c := state.Components["codex"]; c.Managed && c.Installed {
		if sha, _, reviewed := reviewedAdaptiveDigests(c.Version, runtime.GOOS+"/"+runtime.GOARCH); reviewed {
			candidate := v
			candidate.CodexPath, candidate.CodexVersion, candidate.CodexSHA256, candidate.CodexManaged = c.Path, c.Version, sha, true
			if pair, pinErr := pinnedAdaptiveRuntime(state, candidate); pinErr == nil {
				if _, pinErr = fingerprintAdaptivePair(ctx, pair); pinErr == nil {
					v = candidate
				}
			}
		}
	}
	if err = store.Create(v); err != nil {
		return err
	}
	control := orchestration.NativeOrchestrator{Store: store, SessionID: id}
	defer func() {
		_, _ = store.Update(id, func(v *session.Session) error {
			if v.PrimaryTurn != nil {
				v.PrimaryTurn.Mode = core.PrimaryClosed
			}
			return nil
		})
		if cleanupErr := a.cleanupSession(store, id, control); runErr == nil && cleanupErr != nil {
			runErr = cleanupErr
		}
		status, code := session.StateCompleted, 0
		if runErr != nil {
			status, code = session.StateFailed, 1
		}
		a.finishSession(store, id, status, code)
	}()
	swarm, err := control.Initialize(ctx, limit)
	if err != nil {
		return err
	}
	_, err = store.Update(id, func(v *session.Session) error {
		v.SwarmID = swarm.ID
		v.SwarmState = "active"
		v.State = session.StateRunning
		return nil
	})
	if err != nil {
		return err
	}
	runtimeDir, err := store.RuntimeDir(id)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	coreEnvironment := []nativeacp.EnvironmentVariable{}
	// ACP stdio environment inheritance is not assumed. Supply only the basic
	// official-client process metadata required by Core and subscription CLIs.
	for _, name := range []string{"HOME", "PATH", "USER", "LOGNAME", "LANG", "LC_ALL", "TZ", "DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR"} {
		if value, ok := os.LookupEnv(name); ok {
			coreEnvironment = append(coreEnvironment, nativeacp.EnvironmentVariable{Name: name, Value: value})
		}
	}
	for _, pair := range []struct{ name, value string }{{"XDG_CONFIG_HOME", filepath.Dir(a.Store.Paths.ConfigDir)}, {"XDG_DATA_HOME", filepath.Dir(a.Store.Paths.DataDir)}, {"XDG_STATE_HOME", filepath.Dir(a.Store.Paths.StateDir)}, {"XDG_CACHE_HOME", filepath.Dir(a.Store.Paths.CacheDir)}} {
		coreEnvironment = append(coreEnvironment, nativeacp.EnvironmentVariable{Name: pair.name, Value: pair.value})
	}
	processEpoch := "antigravity_process_" + id
	turnState := &autoTurnState{}
	base := turnRunnerFunc(func(turnCtx context.Context, request core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
		policy := core.ExecutionPolicy{NoReplay: true, NoAutomaticFailover: true}
		fail := func(err error) (core.ExecutorResult, error) { return core.ExecutorResult{Policy: policy}, err }
		current, err := store.Get(id)
		if err != nil {
			return fail(err)
		}
		if !session.AdaptiveHistoryEligible(current) || current.PrimaryTurn != nil && (current.PrimaryTurn.Mode != core.PrimaryClosed || current.PrimaryTurn.PlanID != "") {
			return fail(errors.New("ANTIGRAVITY_CONTINUATION_INELIGIBLE"))
		}
		rootInfo, err := os.Stat(cwd)
		if err != nil {
			return fail(err)
		}
		_, binaryInfo, err := reviewedAntigravity(turnCtx)
		if err != nil {
			return fail(err)
		}
		receiptID, err := session.NewID()
		if err != nil {
			return fail(err)
		}
		receipt := core.PrimaryTurnReceipt{ID: receiptID, TurnID: current.ObservationTurnID, AttemptID: current.ObservationAttemptID, FrontendSessionID: frontendID, Executor: "antigravity", Mode: core.PrimaryReadOnly, ConfigDigest: evidence.Digest(cfg), StateDigest: evidence.Digest(state), ScopeDigest: nativeACPScope(scope, cwd, binary, rootInfo)}
		if err = receipt.Validate(); err != nil {
			return fail(err)
		}
		guard := &nativeACPGuard{a, store, id, cwd, scope, binary, rootInfo, binaryInfo, receipt}
		_, err = store.Update(id, func(v *session.Session) error {
			if !v.Active() || v.TurnState != "running" || v.ObservationTurnID != receipt.TurnID || v.ObservationAttemptID != receipt.AttemptID {
				return errors.New("ANTIGRAVITY_ADMISSION_DENIED")
			}
			if v.PrimaryTurn != nil && v.PrimaryTurn.Mode != core.PrimaryClosed {
				return errors.New("ANTIGRAVITY_ADMISSION_DENIED")
			}
			v.PrimaryTurn = &receipt
			v.CurrentPhase = string(core.PrimaryReadOnly)
			return nil
		})
		if err != nil {
			return fail(err)
		}
		defer func() {
			_, _ = store.Update(id, func(v *session.Session) error {
				if v.PrimaryTurn != nil && v.PrimaryTurn.ID == receipt.ID {
					v.PrimaryTurn.Mode = core.PrimaryClosed
				}
				return nil
			})
		}()
		k, err := a.prepareAutoPromptKnowledge(turnCtx, cfg, nil, request.Prompt, "antigravity", runtimeDir, cwd, func(event observability.Event) {
			_, _ = store.Update(id, func(v *session.Session) error { return session.AppendObservation(v, event) })
		})
		if err != nil {
			return fail(err)
		}
		defer k.close()
		turnState.set(k, nil)
		defer turnState.set(sessionKnowledge{}, nil)
		_, err = store.Update(id, func(v *session.Session) error {
			if !guard.bound(*v) {
				return errors.New("ANTIGRAVITY_ADMISSION_DENIED")
			}
			v.KnowledgeSources = k.aliases()
			v.MemoryStatus, v.ContextStatus = "disabled", "disabled"
			if k.config.MCP.Servers["ivoai-memory"].Enabled {
				v.MemoryStatus = "configured"
			}
			if k.config.MCP.Servers["ivoai-context"].Enabled {
				v.ContextStatus = "configured"
			}
			return nil
		})
		if err != nil {
			return fail(err)
		}
		if k.router != nil {
			k.router.SetToolPolicy(func(callCtx context.Context, kind, tool string) bool {
				_, err := guard.check(callCtx)
				return err == nil && readOnlyInstitutionalTool(kind, tool)
			})
		}
		if k.external != nil {
			selected := codeintel.ProjectTools(request.Prompt)
			k.external.RequirePolicyJSON()
			k.external.SetAdmission(func() bool { _, err := guard.check(turnCtx); return err == nil })
			k.external.SetToolAdmission(func(server, tool string) (bool, bool) {
				v, err := guard.check(turnCtx)
				if err != nil {
					return false, false
				}
				if v.PrimaryTurn.Mode == core.PrimaryDAG {
					return primaryMCPGrant(v, cfg, server, tool)
				}
				if server == nativeCodeMCP {
					for _, name := range selected {
						if name == tool {
							return true, false
						}
					}
				}
				return false, false
			})
		}
		servers := []nativeacp.MCPServer{{Name: "ivoai-orchestrator", Command: executable, Args: []string{"_orchestrator-serve", "--session", id}, Env: coreEnvironment}}
		names := []string{}
		for name, server := range k.config.MCP.Servers {
			if server.Enabled {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			server := k.config.MCP.Servers[name]
			token := ""
			if name == "ivoai-memory" || name == "ivoai-context" {
				token = environmentValue(k.environment, knowledgeSessionTokenEnvironment)
			} else if server.HeaderEnv["Authorization"] == externalMCPTokenEnvironment {
				token = environmentValue(k.environment, externalMCPTokenEnvironment)
			}
			if token == "" {
				return fail(errors.New("ANTIGRAVITY_MCP_PROJECTION_INVALID"))
			}
			if _, err := writeNativeMCPProjection(runtimeDir, nativeMCPProjection{Identity: receipt.Identity(), Server: name, Endpoint: server.URL, Token: token}); err != nil {
				return fail(err)
			}
			servers = append(servers, nativeacp.MCPServer{Name: name, Command: executable, Args: []string{"_native-mcp-proxy", "--session", id, "--turn", receipt.ID, "--server", name}, Env: coreEnvironment})
		}
		boundNative := ""
		if request.ExecutorSessionID != "" {
			mapping, ok := current.ExecutorSessions["antigravity:"+frontendID]
			if !ok || mapping.AuthReference != processEpoch || mapping.ExecutorSessionID != request.ExecutorSessionID {
				return fail(errors.New("ANTIGRAVITY_OWNERSHIP_DENIED"))
			}
			boundNative = request.ExecutorSessionID
		}
		check := func(checkCtx context.Context, _ core.ExecutorRequest, r nativeacp.Receipt) error {
			v, err := guard.check(checkCtx)
			if err != nil {
				return err
			}
			if r.FrontendSessionID != frontendID || r.SessionID != id || r.NativeSessionID != "" && r.NativeSessionID != boundNative {
				return errors.New("ANTIGRAVITY_OWNERSHIP_DENIED")
			}
			if v.RecoveryRequested {
				return errors.New("ANTIGRAVITY_RECOVERY_INELIGIBLE")
			}
			return nil
		}
		request.Model, request.Effort = options.Model, options.Effort
		projection := "\nHost-projected native MCP servers:"
		for _, server := range servers {
			projection += " " + server.Name + " (tool prefix mcp_" + server.Name + "_)"
		}
		if !k.config.MCP.Servers["ivoai-memory"].Enabled {
			projection += "\nMemory is absent in this projection: use memory_status=disabled, memory_lookup_performed=false for bootstrap."
		}
		if !k.config.MCP.Servers["ivoai-context"].Enabled {
			projection += "\nContext is absent in this projection: use context_status=disabled, context_lookup_performed=false for bootstrap."
		}
		if k.config.MCP.Servers[nativeCodeMCP].Enabled {
			projection += "\nCode Intelligence code_inspect is available as mcp_ivoai-code_code_inspect. Use it before direct file inspection when it can answer the code question."
		}
		request.Prompt = adaptivePrimaryInstructions(cfg.Orchestration.Auto.CheckpointEnabled) + projection + "\n\nUser request (untrusted task data):\n" + request.Prompt
		runner := nativeacp.New(nativeacp.Options{Enabled: true, SessionID: id, Binary: binary, SHA256: antigravitySHA, Directory: cwd, RuntimeDir: runtimeDir, ProfileDir: filepath.Join(runtimeDir, "native-profile"), Environment: os.Environ(), MCPServers: servers, AllowedTools: []string{"view_file"}, TurnTimeout: 15 * time.Minute, Authorize: check, Validate: check,
			Started: func(startCtx context.Context, _ nativeacp.Receipt, pid int) error {
				if _, err := guard.check(startCtx); err != nil {
					return err
				}
				_, err := store.Update(id, func(v *session.Session) error {
					if !guard.bound(*v) {
						return errors.New("ANTIGRAVITY_ADMISSION_DENIED")
					}
					v.PrimaryPID = pid
					v.PrimaryProcessStart = session.ProcessStart(pid)
					return nil
				})
				return err
			},
			BindSession: func(bindCtx context.Context, nativeID string, resumed bool) error {
				if _, err := guard.check(bindCtx); err != nil {
					return err
				}
				if resumed && nativeID != boundNative || !resumed && boundNative != "" {
					return errors.New("ANTIGRAVITY_OWNERSHIP_DENIED")
				}
				boundNative = nativeID
				return nil
			},
			Approve: func(callCtx context.Context, _ core.ExecutorRequest, _ nativeacp.Receipt, raw json.RawMessage) (bool, error) {
				if _, err := guard.check(callCtx); err != nil {
					return false, err
				}
				var permission struct {
					Tool struct {
						Name string `json:"name"`
						Kind string `json:"kind"`
						Args struct {
							Path         string `json:"Path"`
							AbsolutePath string `json:"AbsolutePath"`
						} `json:"args"`
					} `json:"toolCall"`
				}
				if json.Unmarshal(raw, &permission) != nil {
					return false, nil
				}
				// Antigravity defers large MCP schemas into its private profile and
				// uses read_file to load them. Grant only those generated schema
				// files, never arbitrary repository/profile reads or credentials.
				if permission.Tool.Name == "read_file" || permission.Tool.Name == "view_file" {
					if permission.Tool.Args.Path == "" {
						permission.Tool.Args.Path = permission.Tool.Args.AbsolutePath
					}
					if permission.Tool.Args.Path == "" {
						return false, &core.ExecutorFailure{Class: "NATIVE_SCHEMA_PATH_FIELD_MISSING", ExitCode: -1}
					}
					path := filepath.Clean(permission.Tool.Args.Path)
					root := filepath.Join(runtimeDir, "native-profile", "gemini", "antigravity-cli", "mcp")
					canonical, err := filepath.EvalSymlinks(path)
					if err != nil || canonical != path {
						return false, &core.ExecutorFailure{Class: "NATIVE_SCHEMA_PATH_NOT_CANONICAL", ExitCode: -1}
					}
					for _, server := range servers {
						if filepath.Dir(path) != filepath.Join(root, server.Name) {
							continue
						}
						name := filepath.Base(path)
						if name != "instructions.md" && (!strings.HasSuffix(name, ".json") || strings.ContainsAny(name, "/\\\x00\r\n")) {
							return false, nil
						}
						info, err := os.Lstat(path)
						return err == nil && info.Mode().IsRegular() && info.Size() <= 128<<10 && info.Mode().Perm()&0022 == 0, nil
					}
					return false, &core.ExecutorFailure{Class: "NATIVE_SCHEMA_SCOPE_DENIED", ExitCode: -1}
				}
				for _, server := range servers {
					for _, prefix := range []string{"mcp_" + server.Name + "_", "mcp__" + server.Name + "__"} {
						if strings.HasPrefix(permission.Tool.Name, prefix) {
							return true, nil
						}
					}
				}
				return false, nil // Builtin shell/write/delegation never bypass Core.
			},
		})
		var answer strings.Builder
		result, err := runner.Run(turnCtx, request, func(text string) error { answer.WriteString(text); return nil })
		if err != nil {
			return result, err
		}
		if err = guard.consume(turnCtx); err != nil {
			return result, err
		}
		return result, emit(answer.String())
	})
	bridge, err := opencodebridge.Start(opencodebridge.Options{Frontend: "antigravity", PreferredExecutor: "antigravity", RequirePromptGate: true, Runner: base, Select: func(context.Context, string) (string, error) { return "antigravity", nil }, Status: func() opencodebridge.Status {
		return opencodebridge.Status{SessionID: id, Frontend: "antigravity", Primary: "antigravity"}
	},

		NativePermissions: func() []opencodebridge.PermissionView {
			pending := []opencodebridge.PermissionView{}
			if v, err := store.Get(id); err == nil {
				for _, d := range v.Decisions {
					if d.State == "pending" {
						description := d.Summary
						if description == "" {
							description = fmt.Sprintf("Approve Core %s %s (%d planned tasks)", d.Kind, d.ID, len(v.Tasks))
						}
						pending = append(pending, opencodebridge.PermissionView{ID: d.ID, Description: description})
					}
				}
			}
			k, _ := turnState.snapshot()
			if k.external != nil {
				for _, p := range k.external.Pending() {
					pending = append(pending, opencodebridge.PermissionView{ID: p.ID, Description: p.Description})
				}
			}
			return pending
		},
		ReplyNativePermission: func(replyCtx context.Context, permissionID string, allow bool) error {
			if strings.HasPrefix(permissionID, "plan_") || strings.HasPrefix(permissionID, "routing_") {
				return store.ResolveDecision(id, permissionID, allow)
			}
			k, _ := turnState.snapshot()
			if strings.HasPrefix(permissionID, "ext_") && k.external != nil {
				return k.external.Reply(permissionID, allow)
			}
			return errors.New("CORE_PERMISSION_NO_LONGER_PENDING")
		},
		AuthReference: func(context.Context, string) (string, error) { return processEpoch, nil },
		ClaimRequest: func(frontend, message string) (bool, error) {
			if frontend != frontendID {
				return false, errors.New("ANTIGRAVITY_OWNERSHIP_DENIED")
			}
			return store.ClaimFrontendRequest(ctx, id, frontend, message)
		},
		Attempt: func(attempt opencodebridge.TurnAttempt) error {
			attempt.SessionID = id
			return persistManagedTurnAttempt(ctx, store, id, attempt)
		},
		Mapping: func(mapping opencodebridge.Mapping) error {
			return persistManagedNativeMapping(store, id, frontendID, "antigravity", processEpoch, options, mapping)
		},
	})
	if err != nil {
		return err
	}
	defer bridge.Close(context.Background())
	fmt.Fprintln(a.Err, "Antigravity experimental managed headless session:", id)
	approvalCtx, stopApprovals := context.WithCancel(ctx)
	defer stopApprovals()
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		seen := map[string]bool{}
		for {
			select {
			case <-approvalCtx.Done():
				return
			case <-ticker.C:
				pending, err := bridge.PendingDecisions(approvalCtx)
				if err != nil {
					continue
				}
				for _, p := range pending {
					if seen[p.ID] {
						continue
					}
					seen[p.ID] = true
					allow := false
					if options.Approve != nil {
						allow, err = options.Approve(approvalCtx, p)
						if err != nil {
							allow = false
						}
					}
					_ = bridge.ReplyDecision(approvalCtx, p.ID, allow)
				}
			}
		}
	}()
	for _, prompt := range prompts {
		message, err := session.NewNativeUUID()
		if err != nil {
			return err
		}
		answer, err := bridge.SubmitTurn(ctx, frontendID, message, prompt, "auto", "")
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintln(a.Out, opencodebridge.TerminalText(answer)); err != nil {
			return err
		}
	}
	return nil
}

// The existing Session selection contract requires a nonempty requested model
// whenever selection_mode is set, including the literal automatic route.
func persistManagedNativeMapping(store session.Store, id, frontendID, executor, epoch string, options NativeRunOptions, mapping opencodebridge.Mapping) error {
	_, err := store.Update(id, func(v *session.Session) error {
		if mapping.FrontendSessionID != frontendID || mapping.Executor != executor || v.Frontend != executor || v.FrontendSessionID != frontendID {
			return errors.New("NATIVE_MANAGED_OWNERSHIP_DENIED")
		}
		mode, requested := mapping.SelectionMode, mapping.RequestedModel
		if mode == "" {
			mode = "auto"
		}
		if requested == "" {
			requested = "auto"
		}
		if options.Model != "" {
			mode, requested = "explicit", options.Model
			v.RequestedExecutor = executor
		}
		if v.ExecutorSessions == nil {
			v.ExecutorSessions = map[string]session.ExecutorSessionMapping{}
		}
		v.ExecutorSessionID = mapping.ExecutorSessionID
		v.EffectiveExecutor, v.EffectiveModel, v.EffectiveEffort = executor, mapping.EffectiveModel, mapping.EffectiveEffort
		v.ConfigurationSource = mapping.ConfigurationSource
		v.RequestedModel, v.RequestedEffort = requested, options.Effort
		v.SelectionMode, v.ModelCatalogRevision = mode, mapping.CatalogRevision
		if mapping.EffectiveModel != "" {
			v.PrimaryModel = session.ModelInfo{Name: mapping.EffectiveModel, Source: session.ModelRuntimeVerified}
		}
		v.ExecutorSessions[executor+":"+frontendID] = session.ExecutorSessionMapping{AuthReference: epoch, Executor: executor, ExecutorSessionID: mapping.ExecutorSessionID, SelectionMode: mode, RequestedModel: requested, EffectiveModel: mapping.EffectiveModel, EffectiveEffort: mapping.EffectiveEffort, CatalogRevision: mapping.CatalogRevision, UpdatedAt: time.Now().UTC()}
		if mapping.Trace != nil {
			v.ExecutorTrace, _ = json.Marshal(mapping.Trace)
		}
		return nil
	})
	return err
}

// Executable native customizations are not Core grants. Until their exclusion
// is independently certified, do not start a managed ACP client in a workspace
// that could shadow the private agent or execute its own hooks/MCP processes.
func nativeWorkspaceBoundary(cwd string) error {
	for directory := cwd; ; directory = filepath.Dir(directory) {
		for _, name := range []string{".agents/hooks.json", ".agents/mcp_config.json", ".agents/agents", ".agents/plugins", ".gemini/settings.json", ".gemini/hooks.json"} {
			if _, err := os.Lstat(filepath.Join(directory, name)); err == nil || !errors.Is(err, os.ErrNotExist) {
				return errors.New("ANTIGRAVITY_WORKSPACE_CUSTOMIZATION_UNCERTIFIED: executable native workspace configuration must be migrated or explicitly certified before managed ACP admission")
			}
		}
		if filepath.Dir(directory) == directory {
			return nil
		}
	}
}
