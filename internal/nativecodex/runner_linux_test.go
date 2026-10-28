//go:build linux

package nativecodex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/evidence"
)

// A sterile protocol process, never a provider or subscription. The shell shim
// handles only version/discovery; all actual protocol behavior uses this test
// binary. No external scripting/runtime dependency is needed by CI.
func TestNativeCodexProtocolFixture(t *testing.T) {
	scenario := ""
	for index, arg := range os.Args {
		if arg == "--native-fixture" && index+1 < len(os.Args) {
			scenario = os.Args[index+1]
		}
	}
	if scenario == "" {
		t.Skip("subprocess fixture")
	}
	_ = os.WriteFile(filepath.Join(os.Getenv("HOME"), "appserver-started"), []byte("started"), 0600)
	_, _ = fmt.Fprint(os.Stderr, "private fixture diagnostic")
	if scenario == "hold raw pipe" {
		child := exec.Command(os.Args[0], "-test.run", "^TestNativeCodexHeldPipeFixture$", "--", "--held-native-pipe")
		child.Stdout = os.Stdout
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if child.Start() != nil {
			os.Exit(27)
		}
		_ = os.WriteFile(filepath.Join(os.Getenv("HOME"), "held-pipe-pid"), []byte(strconv.Itoa(child.Process.Pid)), 0600)
		os.Exit(0)
	}
	input := json.NewDecoder(os.Stdin)
	send := func(v any) { _ = json.NewEncoder(os.Stdout).Encode(v) }
	var model, effort, id string
	threadID := "thread_fixture"
	turnID := "turn_fixture"
	for {
		var request struct {
			ID     json.RawMessage            `json:"id"`
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		if input.Decode(&request) != nil {
			os.Exit(0)
		}
		switch request.Method {
		case "initialize":
			send(map[string]any{"method": "configWarning", "emittedAtMs": 1, "params": map[string]any{"message": "fixture"}})
			send(map[string]any{"id": request.ID, "result": map[string]any{"userAgent": "codex-fixture"}})
		case "initialized":
		case "thread/start", "thread/resume":
			_ = json.Unmarshal(request.Params["model"], &model)
			if request.Method == "thread/start" {
				var tools []struct {
					Schema struct {
						Properties struct {
							Evidence struct {
								Enum []string `json:"enum"`
							} `json:"evidence_id"`
						} `json:"properties"`
					} `json:"inputSchema"`
				}
				_ = json.Unmarshal(request.Params["dynamicTools"], &tools)
				if len(tools) == 0 || len(tools[0].Schema.Properties.Evidence.Enum) == 0 {
					os.Exit(23)
				}
				id = tools[0].Schema.Properties.Evidence.Enum[0]
				_ = os.WriteFile(filepath.Join(os.Getenv("CODEX_HOME"), "fixture-evidence-id"), []byte(id), 0600)
			} else {
				raw, _ := os.ReadFile(filepath.Join(os.Getenv("CODEX_HOME"), "fixture-evidence-id"))
				id = string(raw)
				_ = json.Unmarshal(request.Params["threadId"], &threadID)
				if _, ok := request.Params["dynamicTools"]; ok {
					os.Exit(24)
				}
			}
			sandbox := "readOnly"
			if scenario == "wrong sandbox" {
				sandbox = "workspaceWrite"
			}
			send(map[string]any{"id": request.ID, "result": map[string]any{"thread": map[string]any{"id": threadID}, "model": model, "reasoningEffort": "low", "approvalPolicy": "never", "sandbox": map[string]any{"type": sandbox}}})
		case "turn/start":
			_ = os.WriteFile(filepath.Join(os.Getenv("HOME"), "turn-started"), []byte("started"), 0600)
			_ = json.Unmarshal(request.Params["effort"], &effort)
			if effort != "low" {
				os.Exit(25)
			}
			send(map[string]any{"id": request.ID, "result": map[string]any{"turn": map[string]any{"id": turnID, "status": "inProgress"}}})
			send(map[string]any{"method": "turn/started", "params": map[string]any{"threadId": threadID, "turn": map[string]any{"id": turnID, "status": "inProgress"}}})
			if scenario == "child pending" {
				child := exec.Command("/bin/sh", "-c", "sleep .4; touch "+quoteShell(filepath.Join(os.Getenv("HOME"), "unexpected-child-write")))
				child.Stdout, child.Stderr = os.Stdout, os.Stderr
				if child.Start() != nil {
					os.Exit(26)
				}
			}
			if scenario == "malformed" {
				_, _ = fmt.Fprintln(os.Stdout, `{"method": invalid`)
				continue
			}
			if scenario == "oversized frame" {
				send(map[string]any{"method": "notification", "params": strings.Repeat("x", 2<<20)})
				continue
			}
			params := map[string]any{"threadId": threadID, "turnId": turnID, "callId": "call_fixture", "namespace": nil, "tool": toolName, "arguments": map[string]any{"evidence_id": id, "representation": "view"}}
			method := "item/tool/call"
			switch scenario {
			case "unknown callback field":
				params["command"] = "pwd"
			case "wrong thread":
				params["threadId"] = "foreign_thread"
			case "wrong turn":
				params["turnId"] = "foreign_turn"
			case "wrong namespace":
				params["namespace"] = "foreign"
			case "unknown tool":
				params["tool"] = "exec_command"
			case "unknown evidence":
				params["arguments"] = map[string]any{"evidence_id": "ev_" + strings.Repeat("f", 32), "representation": "view"}
			case "extra argument":
				params["arguments"] = map[string]any{"evidence_id": id, "representation": "view", "command": "pwd"}
			case "approval":
				method = "item/commandExecution/requestApproval"
			}
			send(map[string]any{"id": 901, "method": method, "params": params})
			if scenario == "parallel" {
				other := map[string]any{"threadId": threadID, "turnId": turnID, "callId": "call_parallel", "tool": toolName, "arguments": map[string]any{"evidence_id": id, "representation": "view"}}
				send(map[string]any{"id": 902, "method": "item/tool/call", "params": other})
			}
			var reply map[string]any
			if input.Decode(&reply) != nil {
				os.Exit(0)
			}
			raw, _ := json.Marshal(reply)
			_ = os.WriteFile(filepath.Join(os.Getenv("HOME"), "tool-reply.json"), raw, 0600)
			if scenario == "duplicate call" || scenario == "duplicate RPC" {
				rpc := 902
				params["callId"] = "call_fixture"
				if scenario == "duplicate RPC" {
					rpc = 901
					params["callId"] = "new_call"
				}
				send(map[string]any{"id": rpc, "method": "item/tool/call", "params": params})
				if input.Decode(&reply) != nil {
					os.Exit(0)
				}
			}
			if scenario != "no final" {
				text := "fixture accepted final"
				if scenario == "oversized final" {
					text = strings.Repeat("x", (1<<20)+1)
				}
				send(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": threadID, "turnId": turnID, "item": map[string]any{"id": "answer_fixture", "type": "agentMessage", "phase": "final_answer", "text": text}}})
			}
			status := "completed"
			if scenario == "failed turn" {
				status = "failed"
			}
			send(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": threadID, "turn": map[string]any{"id": turnID, "status": status}}})
			if scenario == "late approval" {
				send(map[string]any{"id": 999, "method": "item/commandExecution/requestApproval", "params": map[string]any{}})
				os.Exit(0)
			}
		}
	}
}

func quoteShell(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func protocolBinary(t *testing.T, scenario string) string {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "codex")
	inventory := "[]"
	if scenario == "MCP configured" {
		inventory = `[{"name":"personal"}]`
	}
	// The race runtime's default exit sleep is one second, exactly the
	// production shutdown bound. Only this synthetic subprocess omits that
	// post-test sleep; race detection and its nonzero failure exit remain on.
	body := "#!/bin/sh\nif [ \"$1\" = '--version' ]; then printf '%s\\n' 'codex-cli 0.164.0'; exit 0; fi\ncase \" $* \" in *' mcp list --json '*) printf '%s\\n' " + quoteShell(inventory) + "; exit 0;; esac\nexport GORACE=atexit_sleep_ms=0\nexec " + quoteShell(binary) + " -test.run '^TestNativeCodexProtocolFixture$' -- --native-fixture " + quoteShell(scenario) + "\n"
	if err := os.WriteFile(path, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func runnerOptions(t *testing.T, f *evidenceFixture, binary string) (Options, core.ExecutorRequest) {
	t.Helper()
	home, codehome, dir := t.TempDir(), t.TempDir(), t.TempDir()
	file, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	sum := hash.Sum(nil)
	owned := map[string]bool{}
	options := Options{Enabled: true, SyntheticProviderURL: "http://127.0.0.1:1/v1", SessionID: f.owner.SessionID, Binary: binary, SHA256: hex.EncodeToString(sum), Directory: dir, Environment: []string{"HOME=" + home, "CODEX_HOME=" + codehome, "PATH=/usr/bin:/bin", "LANG=C.UTF-8"}, Registry: f.registry, TurnTimeout: 10 * time.Second, Authorize: func(ctx context.Context, _ core.ExecutorRequest, receipt Receipt) error {
		if receipt.SessionID != f.owner.SessionID || receipt.TaskID != f.owner.TaskID || !f.allowed.Load() {
			return errDenied
		}
		if receipt.ThreadID != "" && !owned[receipt.ThreadID] {
			return errDenied
		}
		return ctx.Err()
	}, BindThread: func(ctx context.Context, id string, resume bool) error {
		if resume && !owned[id] {
			return errDenied
		}
		owned[id] = true
		return ctx.Err()
	}, Validate: func(ctx context.Context, _ core.ExecutorRequest, _ Receipt) error { return ctx.Err() }}
	request := core.ExecutorRequest{Executor: "codex", Model: "fixture-model", Effort: "low", FrontendSessionID: "frontend_fixture", Prompt: "Read the registered evidence and answer safely."}
	return options, request
}

func envValue(env []string, key string) string {
	for _, entry := range env {
		if k, v, _ := strings.Cut(entry, "="); k == key {
			return v
		}
	}
	return ""
}

func TestNativeRunnerUsesOnlyRegisteredEvidenceAndBuffersFinal(t *testing.T) {
	f := fixtureEvidence(t)
	options, request := runnerOptions(t, f, protocolBinary(t, "success"))
	validated := false
	options.Validate = func(context.Context, core.ExecutorRequest, Receipt) error { validated = true; return nil }
	emitted := ""
	result, err := New(options).Run(context.Background(), request, func(text string) error {
		if !validated {
			t.Error("output escaped before final validation")
		}
		emitted += text
		return nil
	})
	if err != nil || emitted != "fixture accepted final" || result.ExecutorSessionID != "thread_fixture" || result.Trace == nil || !result.Trace.Completion || !result.Trace.FinalResponse || result.Trace.PID == 0 || result.Trace.ExitCode != 0 || result.Trace.StderrBytes != len("private fixture diagnostic") {
		t.Fatalf("native turn: %+v %v output=%q", result, err, emitted)
	}
	reply, err := os.ReadFile(filepath.Join(envValue(options.Environment, "HOME"), "tool-reply.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytesContain := strings.Contains(string(reply), "PRIVATE_RAW_SENTINEL"); bytesContain || !strings.Contains(string(reply), "VIEW_ONLY") || !strings.Contains(string(reply), f.binding.Ref.Result.Artifact.ID) || strings.Contains(string(reply), "seal") {
		t.Fatalf("tool response leaked raw/provenance: %s", reply)
	}
	trace, _ := json.Marshal(result.Trace)
	if strings.Contains(string(trace), "VIEW_ONLY") || strings.Contains(string(trace), "PRIVATE_RAW") || strings.Contains(string(trace), f.binding.Ref.Result.Artifact.ID) || strings.Contains(string(trace), "Read the registered") {
		t.Fatal("trace retained evidence or prompt")
	}
}

func TestNativeRunnerRejectsProtocolAndScopeViolations(t *testing.T) {
	cases := map[string]string{
		"wrong sandbox": "NATIVE_CODEX_THREAD_CONFIGURATION_REJECTED",
		"wrong thread":  "NATIVE_CODEX_TOOL_CALL_REJECTED", "wrong turn": "NATIVE_CODEX_TOOL_CALL_REJECTED",
		"wrong namespace": "NATIVE_CODEX_TOOL_CALL_REJECTED", "unknown tool": "NATIVE_CODEX_TOOL_CALL_REJECTED",
		"unknown evidence": "NATIVE_CODEX_EVIDENCE_DENIED", "extra argument": "NATIVE_CODEX_TOOL_CALL_REJECTED",
		"unknown callback field": "NATIVE_CODEX_TOOL_CALL_REJECTED", "approval": "NATIVE_CODEX_SERVER_REQUEST_DENIED",
		"late approval": "NATIVE_CODEX_SERVER_REQUEST_DENIED", "duplicate call": "NATIVE_CODEX_TOOL_CALL_REJECTED",
		"duplicate RPC": "NATIVE_CODEX_TOOL_CALL_REJECTED", "malformed": "NATIVE_CODEX_STREAM_INCOMPLETE",
		"oversized frame": "NATIVE_CODEX_STREAM_INCOMPLETE", "oversized final": "NATIVE_CODEX_FINAL_TOO_LARGE",
		"no final": "FINAL_RESPONSE_MISSING", "failed turn": "NATIVE_CODEX_TURN_FAILED", "MCP configured": "NATIVE_CODEX_MCP_NOT_ADMITTED",
	}
	for scenario, expected := range cases {
		t.Run(scenario, func(t *testing.T) {
			f := fixtureEvidence(t)
			options, request := runnerOptions(t, f, protocolBinary(t, scenario))
			emitted := 0
			_, err := New(options).Run(context.Background(), request, func(string) error { emitted++; return nil })
			if core.FailureClass(err) != expected || emitted != 0 {
				t.Fatalf("unsafe native turn accepted: err=%v emitted=%d", err, emitted)
			}
			if scenario == "MCP configured" {
				if _, err := os.Stat(filepath.Join(envValue(options.Environment, "HOME"), "appserver-started")); !os.IsNotExist(err) {
					t.Fatal("configured MCP was admitted to App Server startup")
				}
			}
		})
	}
}

func TestNativeRunnerMandatoryHostAdmissionAndOffDefault(t *testing.T) {
	for _, kind := range []string{"off", "missing authorize", "missing bind", "missing validate", "denied", "zero env", "credential env", "bad hash", "resume denied"} {
		t.Run(kind, func(t *testing.T) {
			f := fixtureEvidence(t)
			options, request := runnerOptions(t, f, protocolBinary(t, "success"))
			switch kind {
			case "off":
				options.Enabled = false
			case "missing authorize":
				options.Authorize = nil
			case "missing bind":
				options.BindThread = nil
			case "missing validate":
				options.Validate = nil
			case "denied":
				options.Authorize = func(context.Context, core.ExecutorRequest, Receipt) error { return errors.New("private auth detail") }
			case "zero env":
				options.Environment = nil
			case "credential env":
				options.Environment = append(options.Environment, "OPENAI_API_KEY=must-not-forward")
			case "bad hash":
				options.SHA256 = strings.Repeat("0", 64)
			case "resume denied":
				request.ExecutorSessionID = "foreign_thread"
			}
			result, err := New(options).Run(context.Background(), request, func(string) error { t.Error("denied runner emitted"); return nil })
			if err == nil || result.Trace != nil {
				t.Fatal("denied host request acquired execution trace")
			}
			if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "must-not") {
				t.Fatal("denial exposed secret detail")
			}
		})
	}
}

func TestNativeRunnerRejectsRealProviderBeforeAnyCallbackOrProbe(t *testing.T) {
	for _, provider := range []string{"", "https://api.openai.com/v1", "http://localhost:1234/v1", "http://127.0.0.1:1234/v1#fragment", "http://127.0.0.1:1234/v1?", "http://127.0.0.1:1234/%76%31"} {
		t.Run(provider, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "must-not-execute")
			binary := filepath.Join(t.TempDir(), "codex")
			if err := os.WriteFile(binary, []byte("#!/bin/sh\n: > "+quoteShell(marker)+"\nexit 99\n"), 0700); err != nil {
				t.Fatal(err)
			}
			f := fixtureEvidence(t)
			options, request := runnerOptions(t, f, binary)
			options.SyntheticProviderURL = provider
			calls := 0
			options.Authorize = func(context.Context, core.ExecutorRequest, Receipt) error { calls++; return nil }
			options.BindThread = func(context.Context, string, bool) error { calls++; return nil }
			options.Validate = func(context.Context, core.ExecutorRequest, Receipt) error { calls++; return nil }
			result, err := New(options).Run(context.Background(), request, func(string) error { calls++; return nil })
			expected := "NATIVE_CODEX_CONFIGURATION_INVALID"
			if provider == "" {
				expected = "NATIVE_CODEX_WORKER_BOUNDARY_UNVERIFIED"
			}
			if core.FailureClass(err) != expected || calls != 0 || result.Trace != nil {
				t.Fatalf("real-provider mode entered diagnostic runtime: calls=%d result=%+v err=%v", calls, result, err)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("denied real-provider mode executed a version/MCP/process probe")
			}
		})
	}
}

func TestNativeRunnerRevocationBeforeToolResponseAndBeforeFinal(t *testing.T) {
	for _, stage := range []string{"provider revoke", "provider change", "final revoke", "final change"} {
		t.Run(stage, func(t *testing.T) {
			f := fixtureEvidence(t)
			if strings.HasPrefix(stage, "provider") {
				b := f.binding
				b.ViewProvider = viewFunc(func(context.Context, string, []byte) ([]byte, error) {
					if stage == "provider revoke" {
						f.allowed.Store(false)
					} else {
						f.generation.Store(evidence.Digest("changed"))
					}
					return []byte("view"), nil
				})
				var err error
				f.registry, err = NewRegistry(context.Background(), f.owner, []Binding{b})
				if err != nil {
					t.Fatal(err)
				}
			}
			options, request := runnerOptions(t, f, protocolBinary(t, "success"))
			if strings.HasPrefix(stage, "final") {
				options.Validate = func(context.Context, core.ExecutorRequest, Receipt) error {
					if stage == "final revoke" {
						f.allowed.Store(false)
					} else {
						f.generation.Store(evidence.Digest("changed"))
					}
					return nil
				}
			}
			emitted := 0
			_, err := New(options).Run(context.Background(), request, func(string) error { emitted++; return nil })
			if err == nil || emitted != 0 {
				t.Fatal("revoked/stale receipt emitted final")
			}
			expected := "NATIVE_CODEX_EVIDENCE_DENIED"
			if stage == "final revoke" {
				expected = "NATIVE_CODEX_ADMISSION_DENIED"
			} else if stage == "final change" {
				expected = "NATIVE_CODEX_EVIDENCE_CHANGED"
			}
			if core.FailureClass(err) != expected {
				t.Fatalf("receipt check was masked by earlier failure: want %s, got %v", expected, err)
			}
			if strings.HasPrefix(stage, "provider") {
				if _, err := os.Stat(filepath.Join(envValue(options.Environment, "HOME"), "tool-reply.json")); !os.IsNotExist(err) {
					t.Fatal("revoked/stale view reached native tool response")
				}
			}
		})
	}
}

func TestNativeRunnerParentCancellationStopsGroupAndLateView(t *testing.T) {
	f := fixtureEvidence(t)
	entered := make(chan struct{})
	b := f.binding
	b.ViewProvider = viewFunc(func(ctx context.Context, _ string, _ []byte) ([]byte, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	var err error
	f.registry, err = NewRegistry(context.Background(), f.owner, []Binding{b})
	if err != nil {
		t.Fatal(err)
	}
	options, request := runnerOptions(t, f, protocolBinary(t, "child pending"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := New(options).Run(ctx, request, func(string) error { return errors.New("canceled final escaped") })
		done <- err
	}()
	select {
	case <-entered:
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("pending callback missing")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("native cancellation failed to stop")
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(envValue(options.Environment, "HOME"), "unexpected-child-write")); !os.IsNotExist(err) {
		t.Fatal("native descendant survived cancellation")
	}
}

func TestNativeRunnerResumesOnlyOwnedThreadWithoutChangingManifest(t *testing.T) {
	f := fixtureEvidence(t)
	options, request := runnerOptions(t, f, protocolBinary(t, "success"))
	runner := New(options)
	first, err := runner.Run(context.Background(), request, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	request.ExecutorSessionID = first.ExecutorSessionID
	second, err := runner.Run(context.Background(), request, func(string) error { return nil })
	if err != nil || second.ExecutorSessionID != first.ExecutorSessionID {
		t.Fatalf("owned resume: %v", err)
	}
}

func TestNativeRunnerRejectsParallelCallbacksWithoutReplay(t *testing.T) {
	f := fixtureEvidence(t)
	started := make(chan struct{})
	b := f.binding
	b.ViewProvider = viewFunc(func(ctx context.Context, _ string, _ []byte) ([]byte, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	var err error
	f.registry, err = NewRegistry(context.Background(), f.owner, []Binding{b})
	if err != nil {
		t.Fatal(err)
	}
	options, request := runnerOptions(t, f, protocolBinary(t, "parallel"))
	_, err = New(options).Run(context.Background(), request, func(string) error { t.Error("parallel unsupported turn emitted"); return nil })
	if err == nil {
		t.Fatal("parallel callbacks were silently authorized")
	}
}

func TestNativeRunnerEmitterFailureIsReturnedWithoutRetry(t *testing.T) {
	f := fixtureEvidence(t)
	options, request := runnerOptions(t, f, protocolBinary(t, "success"))
	var calls atomic.Int32
	sentinel := errors.New("synthetic emitter failure")
	_, err := New(options).Run(context.Background(), request, func(string) error { calls.Add(1); return sentinel })
	if !errors.Is(err, sentinel) || calls.Load() != 1 {
		t.Fatal("emitter failure was retried or discarded")
	}
}

func TestProtocolJSONHasOneBoundedInterpretation(t *testing.T) {
	for _, raw := range []string{
		`{"id":901,"id":902,"method":"item/tool/call"}`,
		`{"params":{"threadId":"ours","threadId":"foreign"}}`,
		`{"params":[{"callId":"ours","callId":"foreign"}]}`,
		`{"params":{}} {}`,
		`{"unrecognized":1}`,
		strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66),
	} {
		var f frame
		if strictJSON([]byte(raw), &f) == nil {
			t.Fatalf("ambiguous/unknown protocol JSON admitted: %.100s", raw)
		}
	}
	var f frame
	if strictJSON([]byte(`{"method":"configWarning","emittedAtMs":1,"params":{"message":"fixture"}}`), &f) != nil {
		t.Fatal("official notification timestamp was rejected")
	}
}

func TestNativeCodexHeldPipeFixture(t *testing.T) {
	for _, arg := range os.Args {
		if arg == "--held-native-pipe" {
			time.Sleep(30 * time.Second)
			os.Exit(0)
		}
	}
	t.Skip("sterile subprocess holding inherited pipe")
}

func TestNativeProtocolCancellationClosesReaderWithEscapedWriter(t *testing.T) {
	home := t.TempDir()
	t.Cleanup(func() {
		raw, _ := os.ReadFile(filepath.Join(home, "held-pipe-pid"))
		if pid, err := strconv.Atoi(string(raw)); err == nil && pid > 0 {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, err := start(ctx, protocolBinary(t, "hold raw pipe"), nil, []string{"HOME=" + home, "PATH=/usr/bin:/bin"}, home)
	if err != nil {
		t.Fatal(err)
	}
	defer p.input.Close()
	defer p.output.Close()
	if err := <-p.done; err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "held-pipe-pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(raw))
	if err != nil || pid < 1 {
		t.Fatal("missing sterile child identity")
	}
	if syscall.Kill(pid, 0) != nil {
		t.Fatal("writer did not outlive leader; test is not exercising held pipe")
	}
	cancel()
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-p.events:
			if !ok {
				if syscall.Kill(pid, 0) != nil {
					t.Fatal("test accidentally relied on killing the escaped writer")
				}
				return
			}
		case <-deadline:
			t.Fatal("reader/scanner leaked while an escaped process retained stdout")
		}
	}
}
