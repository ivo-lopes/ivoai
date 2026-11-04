//go:build linux

package app

// This vertical test starts the public production command. It injects no App,
// runner, facade, bridge, MCP handler, native result or admission callback.
import (
	"context"
	"debug/buildinfo"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
)

type adaptiveFullCLIReceipt struct {
	CompletedDAG         bool                       `json:"completed_DAG_before_readonly_and_resume,omitempty"`
	ArchivedWorkers      int                        `json:"archived_workers,omitempty"`
	HistoryReadable      bool                       `json:"durable_DAG_history_readable_after_resume,omitempty"`
	ChangedPaths         []string                   `json:"approved_DAG_changed_paths,omitempty"`
	BaselineFailed       bool                       `json:"fixed_baseline_tests_failed,omitempty"`
	FinalTestsPassed     bool                       `json:"fixed_final_tests_passed,omitempty"`
	LastSessionPhase     string                     `json:"last_session_phase,omitempty"`
	LastTurnState        string                     `json:"last_turn_state,omitempty"`
	LastFailureClass     string                     `json:"last_failure_class,omitempty"`
	LastReceiptMode      string                     `json:"last_receipt_mode,omitempty"`
	SchemaVersion        int                        `json:"schema_version"`
	Scope                string                     `json:"scope"`
	Outcome              string                     `json:"outcome"`
	Failure              string                     `json:"failure,omitempty"`
	Phase                string                     `json:"phase"`
	WallMS               int64                      `json:"wall_ms"`
	ProductionSHA256     string                     `json:"production_ivoai_sha256,omitempty"`
	ProductionEntrypoint bool                       `json:"verified_production_go_entrypoint"`
	PrivateCorePaths     bool                       `json:"private_xdg_core_paths"`
	AuthLocationsIntact  bool                       `json:"home_and_codex_home_preserved"`
	GuardedTurns         int                        `json:"subscription_admitted_turns"`
	ControlEnvironment   string                     `json:"post_guard_control_environment_delta"`
	ResumedAcrossProcess bool                       `json:"resumed_across_process"`
	CompletedTurns       int                        `json:"completed_real_turns"`
	ClosedReceipts       int                        `json:"closed_receipts"`
	Attempts             int                        `json:"primary_attempts"`
	Sessions             int                        `json:"core_sessions"`
	SameFrontend         bool                       `json:"same_frontend"`
	SameNativeThread     bool                       `json:"same_native_thread"`
	DAG                  bool                       `json:"dag"`
	Workers              int                        `json:"workers"`
	BackendExitZero      bool                       `json:"backend_native_exits_zero"`
	WrapperExitZero      bool                       `json:"public_wrapper_exit_zero"`
	SessionCompleted     bool                       `json:"session_finalized_normally"`
	FilesIntact          bool                       `json:"repository_data_files_intact"`
	Startup              adaptiveNativeStartup      `json:"startup"`
	OfficialUsage        []*core.ReportedTokenUsage `json:"official_client_usage,omitempty"`
	GlobalEconomicGate   string                     `json:"global_economic_gate"`
}

// Fixed setup diagnostics never include config contents, paths or credentials.
func adaptiveFullCLISaveSetup(store *config.Store, cfg config.Config, state config.State) string {
	if config.ValidateSkills(cfg.Skills) != nil {
		return "PRIVATE_CORE_SKILLS_INVALID"
	}
	if config.ValidateOpenCode(cfg.OpenCode) != nil {
		return "PRIVATE_CORE_PERMISSION_CONFIG_INVALID"
	}
	if config.ValidateCompression(cfg.Compression) != nil {
		return "PRIVATE_CORE_COMPRESSION_CONFIG_INVALID"
	}
	if config.ValidateOrchestration(cfg.Orchestration) != nil {
		return "PRIVATE_CORE_ORCHESTRATION_CONFIG_INVALID"
	}
	if state.Schema != config.StateSchemaVersion {
		return "PRIVATE_CORE_STATE_SCHEMA_INVALID"
	}
	if store.Save(cfg) != nil {
		return "PRIVATE_CORE_CONFIG_WRITE_FAILED"
	}
	if store.SaveState(state) != nil {
		return "PRIVATE_CORE_STATE_WRITE_FAILED"
	}
	return ""
}

// Offline setup verification only: these component paths are metadata and are
// never executed. This is not an official-client integration certification.
func TestOfflineAdaptiveFullCLIConfigurationStage(t *testing.T) {
	private := t.TempDir()
	for key, directory := range map[string]string{"XDG_CONFIG_HOME": "config", "XDG_DATA_HOME": "data", "XDG_STATE_HOME": "state", "XDG_CACHE_HOME": "cache"} {
		t.Setenv(key, filepath.Join(private, directory))
	}
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal("private path resolution failed")
	}
	store := config.NewStore(paths)
	cfg := config.Default()
	cfg.Memory.Enabled, cfg.Headroom.Enabled = false, false
	cfg.Compression.Provider, cfg.Compression.Source = "direct", config.CompressionSourceExplicit
	cfg.OpenCode.PermissionMode = "full"
	cfg.Orchestration.Auto.ProviderPreference = "codex"
	cfg.Orchestration.Auto.AutomaticFailover = false
	cfg.Orchestration.Auto.PlanExecution = "approve"
	state := config.State{Schema: config.StateSchemaVersion, Components: map[string]config.ComponentState{
		"codex":                {Path: filepath.Join(private, "never-executed", "codex"), Version: "0.162.1", Installed: true, Managed: true},
		"codex-code-mode-host": {Path: filepath.Join(private, "never-executed", "codex-code-mode-host"), Version: "0.162.1", Installed: true, Managed: true},
	}}
	invalid := state
	invalid.Schema = 0
	if got := adaptiveFullCLISaveSetup(store, cfg, invalid); got != "PRIVATE_CORE_STATE_SCHEMA_INVALID" {
		t.Fatal("missing state schema was not diagnosed")
	}
	if got := adaptiveFullCLISaveSetup(store, cfg, state); got != "" {
		t.Fatal(got)
	}
	savedConfig, cfgErr := store.Load()
	savedState, stateErr := store.LoadState()
	if cfgErr != nil || stateErr != nil || savedConfig.Compression.Provider != "direct" || savedConfig.OpenCode.PermissionMode != "full" || savedConfig.Memory.Enabled || savedState.Schema != config.StateSchemaVersion || !reflect.DeepEqual(savedState.Components, state.Components) {
		t.Fatal("private production setup did not round trip")
	}
	values, err := (session.Store{Root: paths.SessionsDir}).List()
	if err != nil || len(values) != 0 {
		t.Fatal("setup unexpectedly created a session")
	}
}

func TestLiveAdaptivePrimaryFullCLI(t *testing.T) {
	if os.Getenv("IVOAI_ADAPTIVE_FULL_CLI_LIVE") != "1" {
		t.Skip("NOT_RUN: explicit public production CLI integration required")
	}
	production, codex, report := os.Getenv("IVOAI_ADAPTIVE_FULL_CLI_BINARY"), os.Getenv("IVOAI_LIVE_CODEX_PATH"), os.Getenv("IVOAI_ADAPTIVE_FULL_CLI_REPORT")
	if !filepath.IsAbs(production) || !filepath.IsAbs(codex) || primaryDirectActivationOutput(report) != nil {
		t.Fatal("reviewed production and official executables and new private report required")
	}
	row := adaptiveFullCLIReceipt{SchemaVersion: 1, Scope: "public_production_ivoai_codex_official_native_TUI", Outcome: "FAIL", GlobalEconomicGate: "INCOMPLETE", ControlEnvironment: "IVOAI_EXPERIMENTAL_ADAPTIVE_PRIMARY=1 only; Core creates its own scoped capabilities"}
	start := time.Now()
	defer func() {
		row.WallMS = time.Since(start).Milliseconds()
		file, err := os.OpenFile(report, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Error("bounded public CLI receipt unavailable")
			return
		}
		err = json.NewEncoder(file).Encode(row)
		if file.Close() != nil || err != nil {
			t.Error("bounded public CLI receipt write failed")
		}
	}()
	fail := func(code string) { t.Helper(); row.Failure = code; t.Fatal(code) }
	withDAG := os.Getenv("IVOAI_ADAPTIVE_FULL_CLI_DAG_LIVE") == "1"
	caseTimeout := 6 * time.Minute
	if withDAG {
		caseTimeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), caseTimeout)
	defer cancel()
	row.Phase = "production_identity"
	info, err := buildinfo.ReadFile(production)
	if err != nil || info.Path != "github.com/ivo-lopes/ivoai/cmd/ivoai" || info.Main.Path != "github.com/ivo-lopes/ivoai" {
		fail("PRODUCTION_GO_ENTRYPOINT_NOT_VERIFIED")
	}
	row.ProductionEntrypoint = true
	row.ProductionSHA256, err = adaptiveBinarySHA(ctx, production)
	if err != nil {
		fail("PRODUCTION_BINARY_NOT_STABLE")
	}
	if _, err := fingerprintAdaptivePair(ctx, func() adaptiveRuntime { _, p := adaptiveLiveRuntime(t, codex); return p }()); err != nil {
		fail("OFFICIAL_CODEX_PAIR_NOT_MATCHED")
	}
	// The reusable fixture creates only repository data here. Its App, session,
	// fake-runtime seam and test helper are never passed to the production CLI.
	f := adaptiveInferenceFixture(t, codex)
	goBinary := os.Getenv("IVOAI_ADAPTIVE_ORACLE_GO")
	if withDAG && !filepath.IsAbs(goBinary) {
		fail("DAG_FIXED_ORACLE_BINARY_REQUIRED")
	}
	oraclePrivate := t.TempDir()
	if withDAG {
		row.BaselineFailed = !adaptiveChangeGoTest(ctx, goBinary, f.root, oraclePrivate)
		if !row.BaselineFailed {
			fail("PUBLIC_DAG_BASELINE_DID_NOT_FAIL")
		}
	}
	before, err := adaptiveChangeSnapshot(f.root)
	if err != nil {
		fail("INITIAL_REPOSITORY_SNAPSHOT_FAILED")
	}
	home, homePresent := os.LookupEnv("HOME")
	codeHome, codeHomePresent := os.LookupEnv("CODEX_HOME")
	private := t.TempDir()
	for key, directory := range map[string]string{"XDG_CONFIG_HOME": "config", "XDG_DATA_HOME": "data", "XDG_STATE_HOME": "state", "XDG_CACHE_HOME": "cache"} {
		t.Setenv(key, filepath.Join(private, directory))
	}
	paths, err := config.ResolvePaths()
	if err != nil || paths.ConfigDir != filepath.Join(private, "config", "ivoai") || paths.StateDir != filepath.Join(private, "state", "ivoai") || paths.SessionsDir != filepath.Join(paths.StateDir, "sessions") || paths.DataDir != filepath.Join(private, "data", "ivoai") || paths.CacheDir != filepath.Join(private, "cache", "ivoai") {
		fail("PRIVATE_CORE_PATHS_UNAVAILABLE")
	}
	row.PrivateCorePaths = true
	store := config.NewStore(paths)
	if store.Ensure() != nil {
		fail("PRIVATE_CORE_DIRECTORIES_UNAVAILABLE")
	}
	cfg := f.cfg
	cfg.Memory.Enabled = false
	cfg.Headroom.Enabled = false
	cfg.Compression.Provider, cfg.Compression.Source = "direct", config.CompressionSourceExplicit
	cfg.OpenCode.PermissionMode = "full" // Core still admits the Primary as read-only.
	cfg.Orchestration.Auto.ProviderPreference = "codex"
	cfg.Orchestration.Auto.AutomaticFailover = false
	cfg.Orchestration.Auto.PlanExecution = "approve"
	if withDAG {
		// Explicit authorization is limited to this private disposable fixture.
		cfg.Orchestration.Auto.PlanExecution = "immediate"
	}
	state := config.State{Schema: config.StateSchemaVersion, Components: map[string]config.ComponentState{"codex": f.state.Components["codex"], "codex-code-mode-host": f.state.Components["codex-code-mode-host"]}}
	if failure := adaptiveFullCLISaveSetup(store, cfg, state); failure != "" {
		fail(failure)
	}
	sessions := session.Store{Root: paths.SessionsDir}
	if values, err := sessions.List(); err != nil || len(values) != 0 {
		fail("CORE_SESSION_NAMESPACE_NOT_EMPTY")
	}
	// Both discovery and the child receive these effective XDG locations. HOME
	// and CODEX_HOME keep the official client's existing authentication in place.
	environment := prefetchSubscriptionEnvironment()
	newHome, newHomePresent := os.LookupEnv("HOME")
	newCodeHome, newCodeHomePresent := os.LookupEnv("CODEX_HOME")
	row.AuthLocationsIntact = home == newHome && homePresent == newHomePresent && codeHome == newCodeHome && codeHomePresent == newCodeHomePresent
	if !row.AuthLocationsIntact {
		fail("OFFICIAL_AUTH_LOCATION_CHANGED")
	}
	row.Phase = "official_model_discovery"
	catalog := opencodebridge.CatalogFromRegistry((routing.Discoverer{CodexPath: codex}).Discover(ctx)).RestrictedTo("codex")
	model, available := catalog.StrongPrimary("codex")
	for _, entry := range catalog.Entries() {
		if entry.Executor == "codex" && entry.UpstreamModel == "gpt-6.1-sol" && entry.ModelSource == string(routing.SourceRuntimeVerified) {
			if selected, valid := catalog.Resolve(entry.ID, "low"); valid && selected.Effort == "low" {
				model, available = entry, true
				model.DefaultEffort = "low"
			}
		}
	}
	if !available || model.ModelSource != string(routing.SourceRuntimeVerified) {
		row.Outcome, row.Failure = "NOT_RUN", "OBSERVED_OFFICIAL_MODEL_UNAVAILABLE"
		t.Skip("NOT_RUN: observed official model unavailable")
	}
	guard := func() {
		t.Helper()
		scope, err := prefetchNewDirectScope(codex, f.root, model.UpstreamModel, "read-only", environment)
		if err != nil {
			fail("OFFICIAL_SUBSCRIPTION_SCOPE_UNAVAILABLE")
		}
		_, admission := prefetchCommonLiveAdmission(ctx, scope, prefetchSubscriptionHostPolicy{PermitFilteredEnvironment: true})
		if admission.State != "ADMITTED" {
			row.Failure = admission.Reason
			if row.CompletedTurns == 0 {
				row.Outcome = "NOT_RUN"
				t.Skip("NOT_RUN: existing official admission refused")
			}
			fail("SUBSEQUENT_TURN_ADMISSION_REFUSED")
		}
		row.GuardedTurns++
	}
	guard()
	// The closed-list PR68 guard intentionally receives only the frozen auth
	// environment above. The sole extra host control is IVOAI's opt-in flag;
	// no auth token, route, proxy, API key or test helper is added.
	execEnvironment := setAppEnvironment(environment, adaptivePrimaryFlag, "1")
	args := []string{"codex", "--planner", "codex", "--", "--model", model.UpstreamModel, "-c", "model_reasoning_effort=" + strconv.Quote(model.DefaultEffort)}
	row.Phase = "public_cli_startup"
	pty, err := adaptiveNativePTY(ctx, production, f.root, args, execEnvironment)
	if err != nil {
		fail("PUBLIC_CLI_PTY_START_FAILED")
	}
	defer pty.close()
	selectedID := ""
	current := func() (session.Session, bool) {
		values, err := sessions.List()
		if err != nil || len(values) != 1 {
			return session.Session{}, false
		}
		v := values[0]
		if v.WorkingDirectory != f.root || v.Frontend != "codex" || v.Coordinator != "native" || selectedID != "" && v.SessionID != selectedID {
			return session.Session{}, false
		}
		if selectedID == "" {
			selectedID = v.SessionID
		}
		return v, true
	}
	defer func() {
		if row.Outcome != "FAIL" {
			return
		}
		v, valid := current()
		if !valid {
			return
		}
		row.LastSessionPhase, row.LastTurnState = v.CurrentPhase, v.TurnState
		row.Attempts, row.DAG, row.Workers = len(v.TurnAttempts), v.PlanID != "", len(v.Workers)
		if v.PrimaryTurn != nil {
			row.LastReceiptMode = string(v.PrimaryTurn.Mode)
		}
		if len(v.TurnAttempts) > 0 {
			var attempt opencodebridge.TurnAttempt
			if json.Unmarshal(v.TurnAttempts[len(v.TurnAttempts)-1], &attempt) == nil && attempt.Trace != nil {
				row.LastFailureClass = attempt.Trace.Failure
			}
		}
	}()
	wait := func(bound time.Duration, ready func() bool) {
		t.Helper()
		deadline := time.NewTimer(bound)
		defer deadline.Stop()
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			if ready() {
				return
			}
			select {
			case <-pty.done:
				fail("PUBLIC_CLI_EXITED_EARLY")
			case <-ctx.Done():
				fail("PUBLIC_CLI_CASE_DEADLINE")
			case <-deadline.C:
				fail("PUBLIC_CLI_STATE_DEADLINE")
			case <-tick.C:
			}
		}
	}
	wait(45*time.Second, func() bool {
		v, valid := current()
		row.Startup = pty.startup(model, valid && v.FrontendSessionID != "")
		return valid && row.Startup.ready()
	})
	submit := func(text string) {
		t.Helper()
		if pty.send(text) != nil {
			fail("PUBLIC_NATIVE_INPUT_FAILED")
		}
		select {
		case <-time.After(250 * time.Millisecond):
		case <-ctx.Done():
			fail("PUBLIC_CLI_CASE_DEADLINE")
		}
		if pty.send("\r") != nil {
			fail("PUBLIC_NATIVE_ENTER_FAILED")
		}
	}
	firstFrontend, firstNative, firstReceipt := "", "", ""
	prompts := []string{"Explique como funciona a autenticação deste projeto.", "Qual função faz essa verificação? Informe o nome e o arquivo."}
	if withDAG {
		prompts = []string{"Corrija o bug de soma em math.go usando um worker aprovado do IVOAI e integre o resultado. Add(2, 3) deveria retornar 5. Preserve os outros arquivos e os testes existentes.", "Qual função foi corrigida e qual operação ela executa agora? Responda usando a alteração anterior, sem criar outro plano ou worker."}
	}
	for index, prompt := range prompts {
		if index > 0 {
			// Keep the admission's closed environment unchanged by host flags.
			environment = prefetchSubscriptionEnvironment()
			guard()
		}
		row.Phase = "public_turn_" + strconv.Itoa(index+1)
		submit(prompt)
		turnBound := 110 * time.Second
		if withDAG && index == 0 {
			turnBound = 300 * time.Second
		}
		wait(turnBound, func() bool {
			v, valid := current()
			if !valid || len(v.TurnAttempts) != index+1 || v.PrimaryTurn == nil || v.PrimaryTurn.Mode != core.PrimaryClosed {
				return false
			}
			var attempt opencodebridge.TurnAttempt
			return json.Unmarshal(v.TurnAttempts[index], &attempt) == nil && attempt.State == "completed" && attempt.EndedAt != nil
		})
		v, valid := current()
		if !valid {
			fail("PUBLIC_CORE_SESSION_UNAVAILABLE")
		}
		var attempt opencodebridge.TurnAttempt
		if json.Unmarshal(v.TurnAttempts[index], &attempt) != nil || attempt.Trace == nil {
			fail("PUBLIC_OFFICIAL_ATTEMPT_UNAVAILABLE")
		}
		trace := attempt.Trace
		row.BackendExitZero = trace.Executor == "codex" && trace.Executable == codex && trace.PID > 0 && trace.ExitCode == 0 && trace.Signal == "" && trace.Completion && trace.FinalResponse && trace.Failure == ""
		row.Attempts, row.DAG, row.Workers = len(v.TurnAttempts), v.PlanID != "", len(v.Workers)
		if index == 0 {
			firstFrontend, firstNative, firstReceipt = v.FrontendSessionID, attempt.NativeSessionID, v.PrimaryTurn.ID
		}
		row.SameFrontend = firstFrontend != "" && v.FrontendSessionID == firstFrontend && v.PrimaryTurn.FrontendSessionID == firstFrontend
		row.SameNativeThread = firstNative != "" && attempt.NativeSessionID == firstNative && v.ExecutorSessionID == firstNative
		if !row.BackendExitZero || !row.SameFrontend || !row.SameNativeThread || (index > 0 && v.PrimaryTurn.ID == firstReceipt) || v.PrimaryTurn.Executor != "codex" || attempt.EffectiveExecutor != "codex" {
			fail("PUBLIC_CORE_LIFECYCLE_OR_ISOLATION_FAILED")
		}
		if withDAG && index == 0 {
			if !row.DAG || len(v.Workers) != 1 || len(v.Tasks) == 0 || v.PrimaryTurn.PlanID != v.PlanID {
				fail("PUBLIC_DAG_REQUIRED_WORKER_NOT_OBSERVED")
			}
			for _, w := range v.Workers {
				if w.State != session.StateCompleted || w.EndedAt == nil || w.ExitCode == nil || *w.ExitCode != 0 || w.Executor != "codex" {
					fail("PUBLIC_DAG_WORKER_NOT_SUCCESSFUL")
				}
			}
			if !adaptiveChangePureFile(filepath.Join(f.root, "math.go"), "Add", "int", "int", 2) {
				fail("PUBLIC_DAG_CODE_OUTSIDE_PURE_ORACLE")
			}
			after, err := adaptiveChangeSnapshot(f.root)
			if err != nil {
				fail("PUBLIC_DAG_SNAPSHOT_FAILED")
			}
			var allowed bool
			row.ChangedPaths, allowed = adaptiveChangePaths(before, after, map[string]bool{"math.go": true})
			row.FinalTestsPassed = adaptiveChangeGoTest(ctx, goBinary, f.root, oraclePrivate)
			if !allowed || !row.FinalTestsPassed {
				fail("PUBLIC_DAG_FIXED_ORACLE_OR_ISOLATION_FAILED")
			}
			row.CompletedDAG = true
			before = after
		} else if row.DAG || row.Workers != 0 || len(v.Tasks) != 0 || len(v.Decisions) != 0 {
			fail("PUBLIC_READONLY_TURN_INHERITED_DAG")
		}
		if withDAG && index == 1 {
			if v.CompletedPrimaryPlan == nil || v.PrimaryTurn.HistoryDigest == "" {
				fail("PUBLIC_DAG_HISTORY_MISSING")
			}
			history, err := sessions.ReadCompletedPrimaryPlan(ctx, v.SessionID, *v.CompletedPrimaryPlan)
			if err != nil || len(history.Session.Workers) != 1 || history.Session.PrimaryTurn.ID != firstReceipt {
				fail("PUBLIC_DAG_HISTORY_NOT_OWNED")
			}
			row.ArchivedWorkers = len(history.Session.Workers)
		}
		row.CompletedTurns++
		row.ClosedReceipts++
		if trace.Usage != nil {
			row.OfficialUsage = append(row.OfficialUsage, trace.Usage)
		}
		wait(15*time.Second, func() bool {
			if withDAG {
				return pty.contains("math.go")
			}
			if index == 0 {
				return pty.contains("Authenticate") && pty.contains("fixture-allowed")
			}
			return pty.contains("auth.go")
		})
		select {
		case <-time.After(350 * time.Millisecond):
		case <-ctx.Done():
			fail("PUBLIC_CLI_CASE_DEADLINE")
		}
	}
	row.Phase = "public_normal_exit"
	submit("/exit")
	select {
	case <-pty.done:
		row.WrapperExitZero = pty.err == nil
	case <-time.After(20 * time.Second):
		fail("PUBLIC_NORMAL_EXIT_NOT_OBSERVED")
	case <-ctx.Done():
		fail("PUBLIC_CLI_CASE_DEADLINE")
	}
	v, valid := current()
	values, listErr := sessions.List()
	row.Sessions = len(values)
	row.SessionCompleted = valid && v.State == session.StateCompleted && v.ExitCode != nil && *v.ExitCode == 0 && v.FrontendState == session.StateCompleted && v.FrontendExitCode != nil && *v.FrontendExitCode == 0 && v.EndedAt != nil && v.SwarmState == "stopped"
	after, snapshotErr := adaptiveChangeSnapshot(f.root)
	row.FilesIntact = snapshotErr == nil && reflect.DeepEqual(before, after)
	finalSHA, hashErr := adaptiveBinarySHA(ctx, production)
	if !row.WrapperExitZero || !row.SessionCompleted || !row.FilesIntact || listErr != nil || row.Sessions != 1 || len(v.TurnAttempts) != 2 || len(v.FrontendRequests) != 2 || v.PrimaryTurn == nil || v.PrimaryTurn.Mode != core.PrimaryClosed || len(v.Workers) != 0 || v.PlanID != "" || hashErr != nil || finalSHA != row.ProductionSHA256 {
		fail("PUBLIC_FINALIZATION_OR_REPLAY_INVARIANT_FAILED")
	}
	if os.Getenv("IVOAI_ADAPTIVE_RESUME_LIVE") == "1" {
		row.Phase = "public_cross_process_resume"
		environment = prefetchSubscriptionEnvironment()
		guard()
		pty, err = adaptiveNativePTY(ctx, production, f.root, []string{"session", "resume", selectedID}, execEnvironment)
		if err != nil {
			fail("PUBLIC_RESUME_PTY_START_FAILED")
		}
		defer pty.close()
		wait(45*time.Second, func() bool {
			v, valid := current()
			return valid && v.State == session.StateRunning && pty.startup(model, v.FrontendSessionID == firstFrontend).ready()
		})
		resumePrompt := "Qual função verificamos antes? Informe somente seu nome e arquivo, usando a conversa anterior."
		if withDAG {
			resumePrompt = "Qual função e arquivo corrigimos antes? Informe a operação atual usando a conversa anterior. Não crie plano ou worker."
		}
		submit(resumePrompt)
		wait(110*time.Second, func() bool {
			v, valid := current()
			if !valid || len(v.TurnAttempts) != 3 || v.PrimaryTurn == nil || v.PrimaryTurn.Mode != core.PrimaryClosed {
				return false
			}
			var attempt opencodebridge.TurnAttempt
			return json.Unmarshal(v.TurnAttempts[2], &attempt) == nil && attempt.State == "completed" && attempt.EndedAt != nil
		})
		v, valid = current()
		var attempt opencodebridge.TurnAttempt
		if !valid || json.Unmarshal(v.TurnAttempts[2], &attempt) != nil || attempt.NativeSessionID != firstNative || v.FrontendSessionID != firstFrontend || attempt.Trace == nil || attempt.Trace.ExitCode != 0 || attempt.Trace.Signal != "" || !attempt.Trace.Completion || !attempt.Trace.FinalResponse || v.PrimaryTurn.ID == firstReceipt || v.PlanID != "" || len(v.Tasks) != 0 || len(v.Workers) != 0 {
			fail("PUBLIC_RESUME_IDENTITY_OR_AUTHORITY_FAILED")
		}
		wait(15*time.Second, func() bool {
			if withDAG {
				return pty.contains("Add") && pty.contains("math.go")
			}
			return pty.contains("Authenticate") && pty.contains("auth.go")
		})
		if withDAG {
			if v.CompletedPrimaryPlan == nil || v.PrimaryTurn.HistoryDigest == "" {
				fail("PUBLIC_RESUME_DAG_HISTORY_MISSING")
			}
			history, err := sessions.ReadCompletedPrimaryPlan(ctx, v.SessionID, *v.CompletedPrimaryPlan)
			row.HistoryReadable = err == nil && len(history.Session.Workers) == 1 && history.Session.PrimaryTurn.ID == firstReceipt
			if !row.HistoryReadable {
				fail("PUBLIC_RESUME_DAG_HISTORY_UNREADABLE")
			}
		}
		submit("/exit")
		select {
		case <-pty.done:
			if pty.err != nil {
				fail("PUBLIC_RESUME_NORMAL_EXIT_FAILED")
			}
		case <-time.After(20 * time.Second):
			fail("PUBLIC_RESUME_NORMAL_EXIT_TIMEOUT")
		}
		v, valid = current()
		if !valid || v.State != session.StateCompleted || v.ExitCode == nil || *v.ExitCode != 0 || len(v.TurnAttempts) != 3 || len(v.FrontendRequests) != 3 {
			fail("PUBLIC_RESUME_FINALIZATION_FAILED")
		}
		row.ResumedAcrossProcess = true
		row.Attempts, row.CompletedTurns, row.ClosedReceipts = 3, 3, 3
		if attempt.Trace.Usage != nil {
			row.OfficialUsage = append(row.OfficialUsage, attempt.Trace.Usage)
		}
		after, snapshotErr = adaptiveChangeSnapshot(f.root)
		if snapshotErr != nil || !reflect.DeepEqual(before, after) {
			fail("PUBLIC_RESUME_REPOSITORY_CHANGED")
		}
	}
	row.Phase, row.Outcome = "completed", "PASS"
}
