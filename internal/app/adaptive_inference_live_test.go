package app

// Explicit subscription integration. The real official CLI owns its normal
// authentication in place. The only substituted entry point is the disposable
// Core MCP child's TestMain; its handler is production OrchestratorServe.
import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
)

type adaptiveInferenceObservation struct {
	Scenario         string                      `json:"scenario"`
	Outcome          string                      `json:"outcome"`
	Failure          string                      `json:"failure,omitempty"`
	WallMS           int64                       `json:"wall_ms"`
	Attempts         int                         `json:"attempts"`
	DAG              bool                        `json:"dag"`
	Workers          int                         `json:"workers"`
	SameThread       bool                        `json:"same_thread"`
	Closed           bool                        `json:"receipt_closed"`
	ExitZero         bool                        `json:"native_exit_zero"`
	CodeCalls        int                         `json:"code_calls"`
	CodeStates       map[observability.State]int `json:"code_states,omitempty"`
	ToolInventories  int                         `json:"tool_inventory_observations"`
	CodeCapabilities map[core.Capability]int     `json:"code_capabilities,omitempty"`
	Usage            *core.ReportedTokenUsage    `json:"official_client_usage,omitempty"`
}

func adaptiveInferenceFixture(t *testing.T, binary string) *localMetadataFixture {
	t.Helper()
	version, pair := adaptiveLiveRuntime(t, binary)
	f := newAdaptiveFixture(t)
	f.a.adaptiveRuntimeCheck = nil
	f.a.Runner = platform.ExecRunner{}
	f.a.Out, f.a.Err = io.Discard, io.Discard
	paths := f.a.Store.Paths
	paths.DataDir = filepath.Join(paths.StateDir, "data")
	paths.QuotaDir = filepath.Join(paths.StateDir, "quota")
	paths.Secrets = filepath.Join(paths.ConfigDir, "secrets.json")
	f.a.Store = config.NewStore(paths)
	f.state.Components["codex"] = config.ComponentState{Path: binary, Version: version, Installed: true, Managed: true}
	f.state.Components["codex-code-mode-host"] = config.ComponentState{Path: filepath.Join(filepath.Dir(binary), "codex-code-mode-host"), Version: version, Installed: true, Managed: true}
	if err := f.a.Store.SaveState(f.state); err != nil {
		t.Fatal("private state unavailable")
	}
	f.state, _ = f.a.Store.LoadState()
	f.cfg.Connections.Servers = map[string]config.ServerProfile{}
	f.cfg.MCP.Servers = map[string]config.MCPServer{}
	f.cfg.Orchestration.Auto.KnowledgeRouting = "explicit-only"
	if err := f.a.Store.Save(f.cfg); err != nil {
		t.Fatal("private config unavailable")
	}
	f.cfg, _ = f.a.Store.Load()
	scope, err := conversationScopeID(f.root, f.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.update(t, func(v *session.Session) {
		v.CodexPath = binary
		v.CodexSHA256 = pair.sha
		v.CodexVersion = version
		v.CodexManaged = true
		v.KnowledgeScopeID = scope
	})
	files := map[string]string{"go.mod": "module adaptivefixture\n\ngo 1.27.0\n", "auth.go": "package adaptivefixture\n\n// Authenticate checks the local fixture token.\nfunc Authenticate(token string) bool { return token == \"fixture-allowed\" }\n", "math.go": "package adaptivefixture\nfunc Add(a,b int) int { return a-b }\n", "math_test.go": "package adaptivefixture\nimport \"testing\"\nfunc TestAdd(t *testing.T) { if Add(2,3)!=5 { t.Fatal(\"addition\") } }\n"}
	for n, b := range files {
		if os.WriteFile(filepath.Join(f.root, n), []byte(b), 0600) != nil {
			t.Fatal("fixture write failed")
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=IVOAI fixture", "-c", "user.email=fixture@example.invalid", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "fixture"}} {
		cmd := exec.Command("/usr/bin/git", args...)
		cmd.Dir = f.root
		cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + paths.StateDir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}
		if cmd.Run() != nil {
			t.Fatal("fixture Git setup failed")
		}
	}
	return f
}

func adaptiveInferenceBridge(t *testing.T, f *localMetadataFixture, environment []string, catalog opencodebridge.ModelCatalog) (*opencodebridge.Bridge, <-chan core.ExecutorResult) {
	t.Helper()
	runtimeDir, err := f.store.RuntimeDir(f.id)
	if err != nil {
		t.Fatal(err)
	}
	instructions := filepath.Join(runtimeDir, "instructions.md")
	base := opencodebridge.CLIRunner{Codex: opencodebridge.ExecutorSpec{Path: f.state.Components["codex"].Path, SHA256: mustAdaptiveSHA(t, f), Version: f.state.Components["codex"].Version, Env: environment, Dir: f.root}}
	turnState := &autoTurnState{}
	scoped := f.a.scopeAutoRunner(base, f.cfg, f.state, f.store, f.id, f.root, runtimeDir, instructions, nil, nil, turnState, catalog, autoTurnOptions{ManagedAdaptiveSession: true}).(autoTurnRunner)
	prepare := scoped.prepare
	scoped.prepare = func(ctx context.Context, request core.ExecutorRequest) (preparedAutoTurn, error) {
		p, err := prepare(ctx, request)
		if err != nil {
			return p, err
		}
		cli, ok := p.Runner.(opencodebridge.CLIRunner)
		if !ok {
			p.Release()
			return preparedAutoTurn{}, errors.New("actual managed CLI unavailable")
		}
		paths, _ := json.Marshal(f.a.Store.Paths)
		for key, value := range map[string]string{"IVOAI_ADAPTIVE_CORE_HELPER": "1", "IVOAI_ADAPTIVE_CORE_PATHS": string(paths), "IVOAI_ADAPTIVE_CORE_SESSION": f.id, adaptivePrimaryFlag: ""} {
			cli.Codex.Args = append(cli.Codex.Args, "-c", "mcp_servers.ivoai-orchestrator.env."+key+"="+strconv.Quote(value))
		}
		cli.Codex.Args = append(cli.Codex.Args, "-c", `mcp_servers.ivoai-orchestrator.args=[]`, "-c", `mcp_servers.ivoai-code.required=true`, "--skip-git-repo-check", "-c", `web_search="disabled"`, "-c", `model_provider="openai"`, "-c", `openai_base_url="`+prefetchSubscriptionInference+`"`, "-c", `chatgpt_base_url="`+prefetchSubscriptionBackend+`"`)
		p.Runner = cli
		return p, nil
	}
	results := make(chan core.ExecutorResult, 8)
	opts := opencodebridge.Options{NativePermissions: func() []opencodebridge.PermissionView {
		k, _ := turnState.snapshot()
		var views []opencodebridge.PermissionView
		if k.external != nil {
			for _, pending := range k.external.Pending() {
				views = append(views, opencodebridge.PermissionView{ID: pending.ID, Description: pending.Description})
			}
		}
		return views
	}, ReplyNativePermission: func(ctx context.Context, id string, allow bool) error {
		k, _ := turnState.snapshot()
		if k.external == nil {
			return errors.New("no scoped approval")
		}
		return k.external.Reply(id, allow)
	}, Frontend: "codex", RequirePromptGate: true, Catalog: catalog, PreferredExecutor: "codex", Runner: turnRunnerFunc(func(ctx context.Context, r core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
		v, e := scoped.Run(ctx, r, emit)
		results <- v
		return v, e
	}), Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() opencodebridge.Status { return opencodebridge.Status{Frontend: "codex"} }, Mapping: adaptivePersistMapping(f), AuthReference: func(ctx context.Context, _ string) (string, error) {
		return quota.AccountReference(ctx, f.state.Components["codex"].Path, "codex")
	}, ClaimRequest: func(frontend, message string) (bool, error) {
		return f.store.ClaimFrontendRequest(context.Background(), f.id, frontend, message)
	}, Attempt: func(attempt opencodebridge.TurnAttempt) error {
		attempt.SessionID = f.id
		body, err := json.Marshal(attempt)
		if err != nil {
			return err
		}
		if err := f.store.AppendTurnAttempt(context.Background(), f.id, body); err != nil {
			return err
		}
		_, e := f.store.Update(f.id, func(v *session.Session) error {
			raw, _ := json.Marshal(attempt)
			found := false
			for i, b := range v.TurnAttempts {
				var old opencodebridge.TurnAttempt
				if json.Unmarshal(b, &old) == nil && old.ID == attempt.ID {
					v.TurnAttempts[i] = raw
					found = true
				}
			}
			if !found {
				v.TurnAttempts = append(v.TurnAttempts, raw)
			}
			v.TurnState = attempt.State
			return appendTurnObservation(v, attempt)
		})
		return e
	}}
	b, err := opencodebridge.Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := b.Close(ctx); err != nil {
			t.Error("managed bridge cleanup failed")
		}
	})
	return b, results
}

func TestLiveAdaptivePrimaryFunctionalCodex(t *testing.T) {
	if os.Getenv("IVOAI_ADAPTIVE_INFERENCE_LIVE") != "1" {
		t.Skip("NOT_RUN: explicit official subscription inference required")
	}
	binary := os.Getenv("IVOAI_LIVE_CODEX_PATH")
	if !filepath.IsAbs(binary) {
		t.Fatal("reviewed official executable required")
	}
	report := os.Getenv("IVOAI_ADAPTIVE_INFERENCE_REPORT")
	if primaryDirectActivationOutput(report) != nil {
		t.Fatal("new private report path required")
	}
	observations := []adaptiveInferenceObservation{}
	defer func() {
		f, e := os.OpenFile(report, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			t.Error("receipt unavailable")
			return
		}
		e = json.NewEncoder(f).Encode(map[string]any{"schema_version": 1, "scope": "real_official_Codex_managed_adaptive_primary", "version": func() string { v, _ := adaptiveLiveRuntime(t, binary); return v }(), "global_economic_gate": "INCOMPLETE", "observations": observations})
		_ = f.Close()
		if e != nil {
			t.Error("receipt failed")
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	environment := prefetchSubscriptionEnvironment()
	f := adaptiveInferenceFixture(t, binary)
	if _, err := fingerprintAdaptivePair(ctx, func() adaptiveRuntime { _, p := adaptiveLiveRuntime(t, binary); return p }()); err != nil {
		t.Fatal("official pair not matched")
	}
	registry := (routing.Discoverer{CodexPath: binary}).Discover(ctx)
	catalog := opencodebridge.CatalogFromRegistry(registry).RestrictedTo("codex")
	var strong opencodebridge.ModelSpec
	ok := false
	for _, entry := range catalog.Entries() {
		if entry.Executor == "codex" && entry.UpstreamModel == "gpt-6.1-sol" && entry.ModelSource == string(routing.SourceRuntimeVerified) {
			if selected, valid := catalog.Resolve(entry.ID, "low"); !valid || selected.Effort != "low" {
				continue
			}
			strong = entry
			strong.DefaultEffort = "low"
			ok = true
			break
		}
	}
	if !ok {
		observations = append(observations, adaptiveInferenceObservation{Scenario: "catalog", Outcome: "NOT_RUN", Failure: "STRONG_MODEL_UNAVAILABLE"})
		t.Skip("NOT_RUN: official strong catalog unavailable")
	}
	b, results := adaptiveInferenceBridge(t, f, environment, catalog)
	firstThread := ""
	for _, test := range []struct {
		id, prompt string
		expected   []string
	}{{"A", "Explique como funciona a autenticação deste projeto.", []string{"Authenticate", "fixture-allowed"}}, {"E", "Qual função faz essa verificação? Informe o nome e o arquivo.", []string{"Authenticate", "auth.go"}}, {"B", "Onde está implementada Authenticate? Consulte a ferramenta code_inspect do IVOAI e informe o arquivo e a assinatura.", []string{"Authenticate", "auth.go"}}} {
		if selected := os.Getenv("IVOAI_ADAPTIVE_INFERENCE_SCENARIOS"); selected != "" && !strings.Contains(","+selected+",", ","+test.id+",") {
			continue
		}
		t.Run(test.id, func(t *testing.T) {
			start := time.Now()
			before, _ := f.store.Get(f.id)
			row := adaptiveInferenceObservation{Scenario: test.id, Outcome: "FAIL", CodeStates: map[observability.State]int{}, CodeCapabilities: map[core.Capability]int{}}
			defer func() { row.WallMS = time.Since(start).Milliseconds(); observations = append(observations, row) }()
			scope, scopeErr := prefetchNewDirectScope(binary, f.root, strong.UpstreamModel, "read-only", environment)
			if scopeErr != nil {
				row.Failure = "SCOPE_UNAVAILABLE"
				t.Fatal("official scope unavailable")
			}
			_, admission := prefetchCommonLiveAdmission(ctx, scope, prefetchSubscriptionHostPolicy{PermitFilteredEnvironment: true})
			if admission.State != "ADMITTED" {
				row.Outcome, row.Failure = "NOT_RUN", admission.Reason
				t.Skip("NOT_RUN: fresh official admission refused")
			}
			answer, e := adaptiveSubmitReadApproved(ctx, b, test.id, test.prompt, strong.ID, strong.DefaultEffort)
			var result core.ExecutorResult
			if e != nil {
				select {
				case result = <-results:
				default:
					row.Failure = "REFUSED_BEFORE_EXECUTOR"
					t.Fatal("managed admission refused before executor")
				}
			} else {
				select {
				case result = <-results:
				case <-ctx.Done():
					row.Failure = "RESULT_UNAVAILABLE"
					t.Fatal("managed result missing")
				}
			}
			v, _ := f.store.Get(f.id)
			row.Attempts = len(v.TurnAttempts)
			row.DAG = v.PlanID != ""
			row.Workers = len(v.Workers)
			row.Closed = v.PrimaryTurn != nil && v.PrimaryTurn.Mode == core.PrimaryClosed
			for _, event := range v.Observability {
				if event.Sequence <= before.ObservationSequence {
					continue
				}
				if event.Operation == observability.OperationCodeInspect {
					row.CodeStates[event.State]++
					row.CodeCapabilities[event.Capability]++
				}
				if event.Capability == "tool.projection" {
					row.ToolInventories++
				}
				if event.Operation == observability.OperationCodeInspect && event.State == observability.StateCompleted {
					row.CodeCalls++
				}
			}
			if result.Trace != nil {
				row.ExitZero = result.Trace.ExitCode == 0 && result.Trace.Signal == ""
				row.Usage = result.Trace.Usage
			}
			if e != nil {
				row.Failure = core.FailureClass(e)
				t.Fatal("managed inference failed: " + row.Failure)
			}
			for _, want := range test.expected {
				if !strings.Contains(answer, want) {
					row.Failure = "ANSWER_ORACLE_FAILED"
					t.Fatal("answer did not meet fixture oracle")
				}
			}
			if firstThread == "" {
				firstThread = result.ExecutorSessionID
			}
			row.SameThread = firstThread != "" && result.ExecutorSessionID == firstThread
			if row.DAG || row.Workers != 0 || !row.Closed || !row.ExitZero || !row.SameThread || !result.Policy.NoReplay {
				row.Failure = "LIFECYCLE_OR_ISOLATION_FAILED"
				t.Fatal("lifecycle/continuity contract failed")
			}
			if test.id == "B" && row.CodeCalls == 0 {
				row.Failure = "CODE_TOOL_NOT_OBSERVED"
				t.Fatal("Code Intelligence call not observed")
			}
			row.Outcome = "PASS"
		})
		if t.Failed() {
			break
		}
	}
}

// Exercise the same pending-decision/reply surface as the managed frontend.
// The host authorizes this one fixture read, never a wildcard tool or plan.
func adaptiveSubmitReadApproved(ctx context.Context, b *opencodebridge.Bridge, id, prompt, model, effort string) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type response struct {
		answer string
		err    error
	}
	done := make(chan response, 1)
	go func() { a, e := b.SubmitTurn(ctx, "fresh_frontend", id, prompt, model, effort); done <- response{a, e} }()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	approved := map[string]bool{}
	for {
		select {
		case out := <-done:
			return out.answer, out.err
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			pending, err := b.PendingDecisions(ctx)
			if err != nil {
				return "", errors.New("approval listing failed")
			}
			for _, p := range pending {
				if approved[p.ID] {
					continue
				}
				if !strings.HasPrefix(p.ID, "ext_") || p.Description != "ivoai-code · code_inspect" {
					return "", errors.New("unexpected approval scope")
				}
				if b.ReplyDecision(ctx, p.ID, true) != nil {
					return "", errors.New("approval reply failed")
				}
				approved[p.ID] = true
			}
		}
	}
}
