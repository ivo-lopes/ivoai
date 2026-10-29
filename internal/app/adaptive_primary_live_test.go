package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/codexresolver"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workers"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The private helper entry point bypasses only the Go test summary printer.
// Stdout remains exclusively MCP throughout the child's lifetime, Wait must be
// zero, and a race build explicitly retains the detector's nonzero verdict.
func TestMain(m *testing.M) {
	if os.Getenv("IVOAI_ADAPTIVE_CORE_HELPER") == "1" {
		os.Exit(adaptiveHelperExitCode(runAdaptivePrimaryCoreHelper()))
	}
	os.Exit(m.Run())
}

func runAdaptivePrimaryCoreHelper() int {
	if os.Getenv(adaptivePrimaryFlag) != "" {
		return 2
	}
	var paths config.Paths
	if json.Unmarshal([]byte(os.Getenv("IVOAI_ADAPTIVE_CORE_PATHS")), &paths) != nil || !filepath.IsAbs(paths.ConfigDir) {
		return 2
	}
	a := &App{Store: config.NewStore(paths), Runner: platform.ExecRunner{}, Out: io.Discard, Err: io.Discard}
	if err := a.OrchestratorServe(context.Background(), os.Getenv("IVOAI_ADAPTIVE_CORE_SESSION")); err != nil {
		return 1
	}
	return 0
}

func realAdaptiveFixture(t *testing.T) (*localMetadataFixture, []string) {
	t.Helper()
	if os.Getenv("IVOAI_ADAPTIVE_LIVE") != "1" {
		t.Skip("explicit pinned no-auth protocol proof required")
	}
	source, hostSource := os.Getenv("IVOAI_ADAPTIVE_REAL_CODEX"), os.Getenv("IVOAI_ADAPTIVE_REAL_COMPANION")
	if !filepath.IsAbs(source) || !filepath.IsAbs(hostSource) {
		t.Fatal("explicit reviewed pair paths required")
	}
	f := newAdaptiveFixture(t)
	f.a.adaptiveRuntimeCheck = nil // Production identity check, never the hermetic seam.
	f.a.Runner = platform.ExecRunner{}
	f.a.Store.Paths.QuotaDir = filepath.Join(f.a.Store.Paths.StateDir, "quota")
	f.a.Store.Paths.Secrets = filepath.Join(f.a.Store.Paths.ConfigDir, "secrets.json")
	bin := t.TempDir()
	for _, copy := range []struct{ source, name string }{{source, "codex"}, {hostSource, "codex-code-mode-host"}} {
		input, err := os.Open(copy.source)
		if err != nil {
			t.Fatal("reviewed runtime unavailable")
		}
		out, err := os.OpenFile(filepath.Join(bin, copy.name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
		if err != nil {
			input.Close()
			t.Fatal(err)
		}
		_, err = io.Copy(out, io.LimitReader(input, 384<<20+1))
		input.Close()
		closeErr := out.Close()
		if err != nil || closeErr != nil {
			t.Fatal("private runtime copy failed")
		}
	}
	path, host := filepath.Join(bin, "codex"), filepath.Join(bin, "codex-code-mode-host")
	f.state.Components["codex"] = config.ComponentState{Path: path, Version: "0.162.1", Installed: true, Managed: true}
	f.state.Components["codex-code-mode-host"] = config.ComponentState{Path: host, Version: "0.162.1", Installed: true, Managed: true}
	if err := f.a.Store.SaveState(f.state); err != nil {
		t.Fatal(err)
	}
	f.state, _ = f.a.Store.LoadState()
	f.update(t, func(v *session.Session) {
		v.CodexPath = path
		v.CodexSHA256 = adaptiveCodexSHA
		v.CodexVersion = "0.162.1"
		v.CodexManaged = true
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := fingerprintAdaptivePair(ctx, adaptiveRuntime{path, adaptiveCodexSHA, host, adaptiveCompanionSHA}); err != nil {
		t.Fatal("official pair digest mismatch")
	}
	home, codehome := filepath.Join(t.TempDir(), "home"), filepath.Join(t.TempDir(), "codehome")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(codehome, 0700); err != nil {
		t.Fatal(err)
	}
	// The discovery adapter and its subprocess use only these disposable homes.
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", codehome)
	env := []string{"HOME=" + home, "CODEX_HOME=" + codehome, "PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	return f, env
}

func adaptiveCoreCommand(f *localMetadataFixture, environment []string, ctx context.Context) (*exec.Cmd, error) {
	binary, err := os.Executable()
	if err != nil {
		return nil, err
	}
	paths, _ := json.Marshal(f.a.Store.Paths)
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = append(append([]string(nil), environment...), "IVOAI_ADAPTIVE_CORE_HELPER=1", "IVOAI_ADAPTIVE_CORE_PATHS="+string(paths), "IVOAI_ADAPTIVE_CORE_SESSION="+f.id)
	if raceOptions, ok := os.LookupEnv("GORACE"); ok {
		cmd.Env = append(cmd.Env, "GORACE="+raceOptions)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	return cmd, nil
}

func connectAdaptiveCore(ctx context.Context, cmd *exec.Cmd) (*mcp.ClientSession, func() error, error) {
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	privateLog, err := os.CreateTemp("", "ivoai-adaptive-core-stderr-*.log")
	if err != nil {
		return nil, nil, err
	}
	cmd.Stderr = privateLog
	if err := cmd.Start(); err != nil {
		privateLog.Close()
		return nil, nil, err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "ivoai-adaptive-isolated-proof", Version: "1"}, nil)
	conn, err := client.Connect(ctx, &mcp.IOTransport{Reader: out, Writer: in, MaxLineLength: 1 << 20}, nil)
	cleanup := func() error {
		defer privateLog.Close()
		if conn != nil {
			conn.Close()
		}
		in.Close()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err == nil {
				return nil
			}
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				status, _ := exit.ProcessState.Sys().(syscall.WaitStatus)
				return fmt.Errorf("Core child nonzero exit: code=%d signal=%d", exit.ExitCode(), status.Signal())
			}
			return errors.New("Core child wait unavailable")
		case <-time.After(3 * time.Second):
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			<-done
			return errors.New("Core child did not exit cleanly")
		}
	}
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return conn, cleanup, nil
}

func adaptiveCoreCall(ctx context.Context, c *mcp.ClientSession, name string, args any) (*mcp.CallToolResult, error) {
	r, err := c.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return nil, err
	}
	if r.IsError {
		return r, errors.New("Core tool refused")
	}
	return r, nil
}

func TestLiveAdaptivePrimaryCoreEscalationWithoutChildFlag(t *testing.T) {
	runLiveAdaptiveCore(t, true)
}
func TestLiveAdaptivePrimaryCoreReadBootstrapWithoutChildFlag(t *testing.T) {
	runLiveAdaptiveCore(t, false)
}

func runLiveAdaptiveCore(t *testing.T, withPlan bool) {
	f, environment := realAdaptiveFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
	defer cancel()
	var conn *mcp.ClientSession
	var cleanup func() error
	defer func() {
		if cleanup != nil {
			if err := cleanup(); err != nil {
				t.Error(err)
			}
		}
	}()
	base := turnRunnerFunc(func(runCtx context.Context, _ core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
		cmd, err := adaptiveCoreCommand(f, environment, ctx)
		if err != nil {
			return core.ExecutorResult{}, err
		}
		conn, cleanup, err = connectAdaptiveCore(ctx, cmd)
		if err != nil {
			return core.ExecutorResult{}, err
		}
		boot, err := adaptiveCoreCall(ctx, conn, "orchestration_bootstrap", map[string]any{"objective": "Read a disposable fixture", "memory_status": "disabled", "context_status": "disabled", "memory_lookup_performed": false, "context_lookup_performed": false})
		if err != nil {
			return core.ExecutorResult{}, err
		}
		raw, _ := json.Marshal(boot.StructuredContent)
		if !strings.Contains(string(raw), "host_dag_protocol") {
			return core.ExecutorResult{}, errors.New("Core protocol missing")
		}
		if !withPlan {
			return core.ExecutorResult{}, emit("CORE_READ_BOOTSTRAP_ACCEPTED")
		}
		task := map[string]any{"id": "inspect", "role": "research", "task": "Inspect the disposable fixture", "acceptance": []string{"fixture remains readable"}, "delegate": false, "scores": map[string]int{"complexity": 1, "risk": 1, "reasoning_depth": 1, "context_breadth": 1, "verification_need": 1, "parallel_value": 0, "latency_sensitivity": 1}}
		finished := make(chan error, 1)
		go func() {
			_, err := adaptiveCoreCall(ctx, conn, "orchestration_plan", map[string]any{"tasks": []any{task}})
			finished <- err
		}()
		planID := ""
		for planID == "" && ctx.Err() == nil {
			v, _ := f.store.Get(f.id)
			for _, decision := range v.Decisions {
				if decision.Kind == "plan" && decision.State == "pending" {
					planID = decision.ID
					break
				}
			}
			if planID == "" {
				select {
				case err := <-finished:
					if err != nil {
						return core.ExecutorResult{}, err
					}
					return core.ExecutorResult{}, errors.New("plan bypassed approval")
				case <-time.After(10 * time.Millisecond):
				}
			}
		}
		if planID == "" {
			return core.ExecutorResult{}, errors.New("plan approval missing")
		}
		if _, err := adaptiveCoreCall(ctx, conn, "orchestration_primary_complete", map[string]any{"plan_id": planID, "task_id": "inspect"}); err == nil {
			return core.ExecutorResult{}, errors.New("unapproved work completed")
		}
		if err := f.store.ResolveDecision(f.id, planID, true); err != nil {
			return core.ExecutorResult{}, err
		}
		if err := <-finished; err != nil {
			return core.ExecutorResult{}, err
		}
		if _, err := adaptiveCoreCall(ctx, conn, "orchestration_primary_complete", map[string]any{"plan_id": planID, "task_id": "inspect"}); err != nil {
			return core.ExecutorResult{}, err
		}
		if _, err := adaptiveCoreCall(ctx, conn, "orchestration_integrate", map[string]any{"plan_id": planID}); err != nil {
			return core.ExecutorResult{}, err
		}
		return core.ExecutorResult{}, emit("CORE_DAG_ACCEPTED")
	})
	b, results := adaptiveBridge(t, f, base)
	status, body := metadataHTTP(t, b, []map[string]string{{"role": "user", "content": "Inspect a disposable fixture"}})
	result := <-results
	expectedFinal := "CORE_DAG_ACCEPTED"
	if !withPlan {
		expectedFinal = "CORE_READ_BOOTSTRAP_ACCEPTED"
	}
	if status != 200 || !strings.Contains(body, expectedFinal) || !result.Policy.NoReplay || conn == nil {
		t.Fatalf("real Core MCP escalation failed: status=%d", status)
	}
	v, _ := f.store.Get(f.id)
	acceptedTasks := !withPlan && len(v.Tasks) == 0 && v.PlanID == "" || withPlan && len(v.Tasks) == 1 && v.Tasks[0].State == session.StateCompleted
	if v.PrimaryTurn.Mode != core.PrimaryClosed || len(v.Workers) != 0 || !acceptedTasks {
		t.Fatal("Core acceptance or receipt closure failed")
	}
	if _, err := adaptiveCoreCall(ctx, conn, "orchestration_plan", map[string]any{}); err == nil {
		t.Fatal("closed receipt accepted late plan")
	}
	oldPlan := v.PlanID
	_, err := f.store.Update(f.id, func(v *session.Session) error {
		copy := *v.PrimaryTurn
		copy.ID = "new_receipt"
		copy.TurnID = "new_turn"
		copy.AttemptID = "new_attempt"
		copy.PlanID = ""
		copy.Mode = core.PrimaryReadOnly
		v.PrimaryTurn = &copy
		v.ObservationTurnID = copy.TurnID
		v.ObservationAttemptID = copy.AttemptID
		v.TurnState = "running"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adaptiveCoreCall(ctx, conn, "orchestration_plan", map[string]any{}); err == nil {
		t.Fatal("stale child adopted later receipt")
	}
	v, _ = f.store.Get(f.id)
	if v.PlanID != oldPlan || len(v.Workers) != 0 {
		t.Fatal("stale child caused plan effects")
	}
	t.Logf(`ADAPTIVE_CORE_PROTOCOL={"scope":"real_Core_MCP_child_and_reviewed_pair_identity","admission_flag_in_child":false,"same_turn_DAG":%v,"plan_approval_exercised":%v,"read_bootstrap_sufficient_without_plan":%v,"closed_and_stale_child_denied":true,"workers":0,"model_inference":false}`, withPlan, withPlan, !withPlan)
}

func TestLiveAdaptivePrimaryOfficialCLIWithoutDAG(t *testing.T) {
	for _, withUsage := range []bool{false, true} {
		t.Run(map[bool]string{false: "usage_absent", true: "usage_complete"}[withUsage], func(t *testing.T) { runLiveAdaptivePrimaryCLI(t, withUsage) })
	}
}

func runLiveAdaptivePrimaryCLI(t *testing.T, withUsage bool) {
	f, environment := realAdaptiveFixture(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
		if err != nil {
			w.WriteHeader(413)
			return
		}
		if !json.Valid(body) {
			w.WriteHeader(400)
			return
		}
		n := requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(value any) { encoded, _ := json.Marshal(value); fmt.Fprintf(w, "data: %s\n\n", encoded) }
		id := fmt.Sprintf("adaptive_response_%d", n)
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": id}})
		emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": map[string]any{"type": "message", "role": "assistant", "status": "completed", "id": "adaptive_answer", "content": []any{map[string]any{"type": "output_text", "text": "SYNTHETIC_FOUR"}}}})
		response := map[string]any{"id": id, "status": "completed"}
		if withUsage {
			response["usage"] = map[string]any{"input_tokens": 100, "output_tokens": 2, "total_tokens": 102, "input_tokens_details": map[string]any{"cached_tokens": 60}, "output_tokens_details": map[string]any{"reasoning_tokens": 0}}
		}
		emit(map[string]any{"type": "response.completed", "response": response})
	}))
	defer server.Close()
	codehome := strings.TrimPrefix(environment[1], "CODEX_HOME=")
	configuration := `model_provider="adaptive_fixture"
[model_providers.adaptive_fixture]
name="Synthetic loopback fixture"
base_url=` + strconv.Quote(server.URL+"/v1") + `
wire_api="responses"
requires_openai_auth=false
supports_websockets=false
request_max_retries=0
stream_max_retries=0
`
	if err := os.WriteFile(filepath.Join(codehome, "config.toml"), []byte(configuration), 0600); err != nil {
		t.Fatal(err)
	}
	base := turnRunnerFunc(func(ctx context.Context, request core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
		runtime, err := f.store.RuntimeDir(f.id)
		if err != nil {
			return core.ExecutorResult{}, err
		}
		instructions := filepath.Join(runtime, "instructions.md")
		if err := platform.AtomicWritePrivate([]byte(adaptivePrimaryInstructions(true)), instructions); err != nil {
			return core.ExecutorResult{}, err
		}
		args, err := f.a.autoBridgeArgs("codex", nil, f.id, runtime, instructions, f.cfg)
		if err != nil {
			return core.ExecutorResult{}, err
		}
		paths, _ := json.Marshal(f.a.Store.Paths)
		// Only the test executable's entry-point arguments differ: its helper
		// runs the same production OrchestratorServe against disposable roots.
		for key, value := range map[string]string{"IVOAI_ADAPTIVE_CORE_HELPER": "1", "IVOAI_ADAPTIVE_CORE_PATHS": string(paths), "IVOAI_ADAPTIVE_CORE_SESSION": f.id} {
			args = append(args, "-c", "mcp_servers.ivoai-orchestrator.env."+key+"="+strconv.Quote(value))
		}
		args = append(args, "-c", `mcp_servers.ivoai-orchestrator.args=[]`, "--skip-git-repo-check", "-c", `web_search="disabled"`)
		scope, err := codexresolver.NewConfigContext(platform.RunOptions{Dir: f.root, Env: environment, CleanEnv: true})
		if err != nil {
			return core.ExecutorResult{}, err
		}
		args, err = (workers.Adapter{Runner: f.a.Runner}).IsolateCodexMCPsInContext(ctx, f.state.Components["codex"].Path, args, scope)
		if err != nil {
			return core.ExecutorResult{}, err
		}
		runner := opencodebridge.CLIRunner{Codex: opencodebridge.ExecutorSpec{Path: f.state.Components["codex"].Path, SHA256: adaptiveCodexSHA, Version: "0.162.1", Args: args, Env: scope.Environment(), Dir: scope.Directory(), CodexConfigContext: &scope}}
		return runner.Run(ctx, request, emit)
	})
	b, results := adaptiveBridge(t, f, base)
	status, body := metadataHTTP(t, b, []map[string]string{{"role": "user", "content": "What is two plus two?"}})
	result := <-results
	if status != 200 || !strings.Contains(body, "SYNTHETIC_FOUR") || requests.Load() != 1 || !result.Policy.NoReplay || result.Trace == nil || !result.Trace.FinalResponse {
		failure, last, events := "", "", 0
		if result.Trace != nil {
			failure = result.Trace.Failure
			last = result.Trace.LastEvent
			events = result.Trace.Events
		}
		saved, _ := f.store.Get(f.id)
		admitted := saved.PrimaryTurn != nil
		t.Fatalf("official CLI primary proof failed: status=%d requests=%d failure=%s last=%s events=%d admitted=%v phase=%s", status, requests.Load(), failure, last, events, admitted, saved.CurrentPhase)
	}
	v, _ := f.store.Get(f.id)
	if v.PlanID != "" || len(v.Tasks) != 0 || len(v.Workers) != 0 || v.PrimaryTurn.Mode != core.PrimaryClosed || result.ConfigurationSource == core.LocalMetadataSource {
		t.Fatal("official primary fabricated DAG or local-only execution")
	}
	if status, _ := metadataHTTP(t, b, []map[string]string{{"role": "user", "content": "What is two plus two?"}}); status != 409 || requests.Load() != 1 {
		t.Fatal("official admitted primary replayed")
	}
	t.Logf(`ADAPTIVE_OFFICIAL_CLI={"version":"0.162.1","provider":"synthetic_loopback","requests":1,"DAG":false,"workers":0,"duplicate":409,"fake_provider_usage_present":%v,"real_inference":false,"subscription_authentication":false}`, withUsage)
	t.Logf(`ADAPTIVE_INSTRUCTION_BYTES={"initial_utf8_bytes":%d,"legacy_utf8_bytes":%d,"DAG_protocol_utf8_bytes":%d,"not_token_or_cost_measurement":true}`, len(adaptivePrimaryInstructions(true)), len(automaticInstructions(true)), len(automaticInstructions(true)))
}
