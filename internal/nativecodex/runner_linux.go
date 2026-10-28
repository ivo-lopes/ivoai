//go:build linux

package nativecodex

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/promptgate"
	"golang.org/x/sys/unix"
)

const clientVersion = "0.164.0"

var identity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,127}$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type frame struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
	// Official 0.164.0 notification metadata; never used as host authority.
	EmittedAtMS json.RawMessage `json:"emittedAtMs,omitempty"`
}
type readFrame struct {
	value frame
	err   error
}
type protocol struct {
	input  io.WriteCloser
	output io.ReadCloser
	events <-chan readFrame
	done   <-chan error
	cmd    *exec.Cmd
	stderr *capture
}

func (r *Runner) Run(parent context.Context, request core.ExecutorRequest, emit func(string) error) (result core.ExecutorResult, runErr error) {
	if r == nil || !r.options.Enabled {
		return result, failed("NATIVE_CODEX_DISABLED")
	}
	o := r.options
	// Official negative proof demonstrated native worker execution despite
	// disabled multi-agent flags. No real-provider capability is admitted.
	if o.SyntheticProviderURL == "" {
		return result, failed("NATIVE_CODEX_WORKER_BOUNDARY_UNVERIFIED")
	}
	if !syntheticURL(o.SyntheticProviderURL) {
		return result, failed("NATIVE_CODEX_CONFIGURATION_INVALID")
	}
	if o.Registry == nil || o.Registry.owner.SessionID != o.SessionID || o.Authorize == nil || o.BindThread == nil || o.Validate == nil || emit == nil || request.Executor != "codex" || !identity.MatchString(request.FrontendSessionID) || !identity.MatchString(request.Model) || !validEffort(request.Effort) || !promptgate.Assess(request.Prompt).Ready || !digestPattern.MatchString(o.SHA256) {
		return result, failed("NATIVE_CODEX_CONFIGURATION_INVALID")
	}
	select {
	case r.busy <- struct{}{}:
		defer func() { <-r.busy }()
	default:
		return result, failed("NATIVE_CODEX_BUSY")
	}
	timeout := o.TurnTimeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	if timeout < time.Millisecond || timeout > 15*time.Minute {
		return result, failed("NATIVE_CODEX_CONFIGURATION_INVALID")
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	receipt := Receipt{SessionID: o.SessionID, TaskID: o.Registry.owner.TaskID, FrontendSessionID: request.FrontendSessionID, ThreadID: request.ExecutorSessionID}
	authorize := func() error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if o.Authorize(ctx, request, receipt) != nil {
			return contextError(ctx, failed("NATIVE_CODEX_ADMISSION_DENIED"))
		}
		return ctx.Err()
	}
	if err := authorize(); err != nil {
		return result, err
	}
	if request.ExecutorSessionID != "" {
		if !identity.MatchString(request.ExecutorSessionID) || o.BindThread(ctx, request.ExecutorSessionID, true) != nil {
			return result, contextError(ctx, failed("NATIVE_CODEX_RESUME_DENIED"))
		}
	}
	directory, err := filepath.EvalSymlinks(o.Directory)
	if err != nil || !filepath.IsAbs(directory) {
		return result, failed("NATIVE_CODEX_CONFIGURATION_INVALID")
	}
	binary, err := filepath.EvalSymlinks(o.Binary)
	if err != nil || !filepath.IsAbs(binary) {
		return result, failed("NATIVE_CODEX_EXECUTABLE_INVALID")
	}
	if err := verifyBinary(ctx, binary, o.SHA256); err != nil {
		return result, contextError(ctx, failed("NATIVE_CODEX_EXECUTABLE_CHANGED"))
	}
	env, err := nativeEnvironment(o.Environment)
	if err != nil {
		return result, failed("NATIVE_CODEX_CONFIGURATION_INVALID")
	}
	args, err := nativeSettings(o, request)
	if err != nil {
		return result, failed("NATIVE_CODEX_CONFIGURATION_INVALID")
	}
	version, err := probe(ctx, binary, []string{"--version"}, env, directory, 256)
	if err != nil || string(version) != "codex-cli "+clientVersion+"\n" {
		return result, contextError(ctx, failed("NATIVE_CODEX_VERSION_UNSUPPORTED"))
	}
	// Discovery and execution share directory, environment and restrictions.
	// This slice admits no configured MCP server, including disabled entries.
	inventory, err := probe(ctx, binary, append(append([]string{}, args...), "mcp", "list", "--json"), env, directory, 256<<10)
	var servers []json.RawMessage
	if err != nil || strictJSON(inventory, &servers) != nil || servers == nil || len(servers) != 0 {
		return result, contextError(ctx, failed("NATIVE_CODEX_MCP_NOT_ADMITTED"))
	}
	env = append(env, "IVOAI_NATIVE_EVIDENCE_FIXTURE_KEY=local-synthetic-not-provider-credential")
	trace := &core.ExecutionTrace{Executor: "codex", Executable: binary, Version: clientVersion, Directory: directory, ExitCode: -1, RequestedModel: request.Model, RequestedEffort: request.Effort}
	result = core.ExecutorResult{Trace: trace, SelectionMode: request.SelectionMode, RequestedModel: request.Model, CatalogRevision: request.CatalogRevision}
	defer func() {
		if runErr != nil {
			trace.Failure = core.FailureClass(runErr)
		}
	}()
	processCtx, stop := context.WithCancel(ctx)
	defer stop()
	p, err := start(processCtx, binary, append(args, "app-server", "--listen", "stdio://"), env, directory)
	if err != nil {
		return result, contextError(ctx, failed("NATIVE_CODEX_START_FAILED"))
	}
	trace.PID = p.cmd.Process.Pid
	stopped := false
	defer func() {
		stop()
		_ = p.output.Close()
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		if !stopped {
			<-p.done
		}
		if p.cmd.ProcessState != nil {
			trace.ExitCode = p.cmd.ProcessState.ExitCode()
			if status, ok := p.cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				trace.Signal = status.Signal().String()
			}
		}
		trace.StderrBytes = p.stderr.count()
	}()
	if err := p.send(map[string]any{"id": "ivoai-native-1", "method": "initialize", "params": map[string]any{"clientInfo": map[string]any{"name": "ivoai_native_evidence", "version": "experimental"}, "capabilities": map[string]any{"experimentalApi": true}}}); err != nil {
		return result, failed("NATIVE_CODEX_PROTOCOL_FAILED")
	}
	if _, err := p.response(ctx, "ivoai-native-1"); err != nil {
		return result, contextError(ctx, err)
	}
	if p.send(map[string]any{"method": "initialized"}) != nil {
		return result, failed("NATIVE_CODEX_PROTOCOL_FAILED")
	}
	method := "thread/start"
	params := map[string]any{"cwd": directory, "model": request.Model, "modelProvider": "ivoai_evidence_fixture", "sandbox": "read-only", "approvalPolicy": "never"}
	if request.ExecutorSessionID != "" {
		method = "thread/resume"
		params["threadId"] = request.ExecutorSessionID
		params["excludeTurns"] = true
	} else {
		params["dynamicTools"] = []any{o.Registry.spec()}
	}
	if p.send(map[string]any{"id": "ivoai-native-2", "method": method, "params": params}) != nil {
		return result, failed("NATIVE_CODEX_PROTOCOL_FAILED")
	}
	started, err := p.response(ctx, "ivoai-native-2")
	if err != nil {
		return result, contextError(ctx, err)
	}
	var thread struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
		Model    string `json:"model"`
		Effort   string `json:"reasoningEffort"`
		Approval string `json:"approvalPolicy"`
		Sandbox  struct {
			Type string `json:"type"`
		} `json:"sandbox"`
	}
	if json.Unmarshal(started.Result, &thread) != nil || !identity.MatchString(thread.Thread.ID) || thread.Model != request.Model || thread.Approval != "never" || thread.Sandbox.Type != "readOnly" || request.ExecutorSessionID != "" && thread.Thread.ID != request.ExecutorSessionID {
		return result, failed("NATIVE_CODEX_THREAD_CONFIGURATION_REJECTED")
	}
	if thread.Effort != "" && (!validEffort(thread.Effort) || thread.Effort != request.Effort) {
		return result, failed("EXPLICIT_REASONING_MISMATCH")
	}
	if o.BindThread(ctx, thread.Thread.ID, request.ExecutorSessionID != "") != nil {
		return result, contextError(ctx, failed("NATIVE_CODEX_OWNERSHIP_DENIED"))
	}
	receipt.ThreadID = thread.Thread.ID
	result.ExecutorSessionID = thread.Thread.ID
	if err := authorize(); err != nil {
		return result, err
	}
	result.Model = thread.Model
	result.Effort = thread.Effort
	result.ConfigurationSource = "official_app_server_thread_configuration"
	trace.EffectiveModel = result.Model
	trace.EffectiveEffort = result.Effort
	trace.ConfigurationSource = result.ConfigurationSource
	if p.send(map[string]any{"id": "ivoai-native-3", "method": "turn/start", "params": map[string]any{"threadId": receipt.ThreadID, "model": request.Model, "effort": request.Effort, "input": []any{map[string]any{"type": "text", "text": request.Prompt}}, "approvalPolicy": "never", "sandboxPolicy": map[string]any{"type": "readOnly"}}}) != nil {
		return result, failed("NATIVE_CODEX_PROTOCOL_FAILED")
	}
	turn, err := p.response(ctx, "ivoai-native-3")
	if err != nil {
		return result, contextError(ctx, err)
	}
	var began struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(turn.Result, &began) != nil || !identity.MatchString(began.Turn.ID) {
		return result, failed("NATIVE_CODEX_TURN_INVALID")
	}
	receipt.TurnID = began.Turn.ID
	seenCalls, seenRPC, used := map[string]bool{}, map[string]bool{}, map[string]bool{}
	var pending <-chan preparedResult
	var pendingID json.RawMessage
	var pendingCall string
	final := ""
	finalBytes := 0
	for {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case prepared := <-pending:
			pending = nil
			if prepared.err != nil {
				return result, contextError(ctx, failed("NATIVE_CODEX_EVIDENCE_DENIED"))
			}
			receipt.CallID = pendingCall
			if err := authorize(); err != nil {
				return result, err
			}
			if prepared.value.validate(ctx) != nil {
				return result, contextError(ctx, failed("NATIVE_CODEX_EVIDENCE_CHANGED"))
			}
			if p.send(map[string]any{"id": pendingID, "result": map[string]any{"contentItems": []any{map[string]any{"type": "inputText", "text": prepared.value.text}}, "success": true}}) != nil {
				return result, contextError(ctx, failed("NATIVE_CODEX_PROTOCOL_FAILED"))
			}
			used[prepared.value.id] = true
			receipt.CallID = ""
		case incoming, ok := <-p.events:
			if !ok || incoming.err != nil {
				return result, contextError(ctx, failed("NATIVE_CODEX_STREAM_INCOMPLETE"))
			}
			v := incoming.value
			trace.Events++
			if len(v.ID) != 0 && v.Method != "" {
				if v.Method != "item/tool/call" || pending != nil || len(seenCalls) >= 32 {
					return result, failed("NATIVE_CODEX_SERVER_REQUEST_DENIED")
				}
				var call struct {
					ThreadID  string          `json:"threadId"`
					TurnID    string          `json:"turnId"`
					CallID    string          `json:"callId"`
					Namespace *string         `json:"namespace"`
					Tool      string          `json:"tool"`
					Arguments json.RawMessage `json:"arguments"`
				}
				key, idOK := requestID(v.ID)
				if len(v.Params) > 4096 || strictJSON(v.Params, &call) != nil || !idOK || seenRPC[key] || call.Tool != toolName || call.Namespace != nil && *call.Namespace != "" || call.ThreadID != receipt.ThreadID || call.TurnID != receipt.TurnID || !identity.MatchString(call.CallID) || seenCalls[call.CallID] {
					return result, failed("NATIVE_CODEX_TOOL_CALL_REJECTED")
				}
				choice, err := evidenceArguments(call.Arguments)
				if err != nil {
					return result, failed("NATIVE_CODEX_TOOL_CALL_REJECTED")
				}
				receipt.CallID = call.CallID
				if err := authorize(); err != nil {
					return result, err
				}
				pending, err = o.Registry.start(ctx, choice.id, choice.representation)
				if err != nil {
					return result, failed("NATIVE_CODEX_EVIDENCE_DENIED")
				}
				seenCalls[call.CallID], seenRPC[key] = true, true
				pendingID = append(json.RawMessage(nil), v.ID...)
				pendingCall = call.CallID
				trace.LastEvent = "item/tool/call"
				continue
			}
			if len(v.Error) != 0 {
				return result, failed("NATIVE_CODEX_PROTOCOL_FAILED")
			}
			switch v.Method {
			case "item/completed":
				var item struct {
					ThreadID string `json:"threadId"`
					TurnID   string `json:"turnId"`
					Item     struct {
						Type  string `json:"type"`
						Text  string `json:"text"`
						Phase string `json:"phase"`
					} `json:"item"`
				}
				if json.Unmarshal(v.Params, &item) != nil || item.ThreadID != receipt.ThreadID || item.TurnID != receipt.TurnID {
					return result, failed("NATIVE_CODEX_EVENT_SCOPE_REJECTED")
				}
				if item.Item.Type == "agentMessage" && (item.Item.Phase == "" || item.Item.Phase == "final_answer") {
					finalBytes += len(item.Item.Text)
					if finalBytes > 1<<20 {
						return result, failed("NATIVE_CODEX_FINAL_TOO_LARGE")
					}
					final = item.Item.Text
				}
				trace.LastEvent = "item/completed"
			case "turn/completed":
				var ended struct {
					ThreadID string `json:"threadId"`
					Turn     struct {
						ID     string `json:"id"`
						Status string `json:"status"`
					} `json:"turn"`
				}
				if json.Unmarshal(v.Params, &ended) != nil || ended.ThreadID != receipt.ThreadID || ended.Turn.ID != receipt.TurnID || ended.Turn.Status != "completed" || pending != nil {
					return result, failed("NATIVE_CODEX_TURN_FAILED")
				}
				trace.Completion = true
				trace.LastEvent = "turn/completed"
				if strings.TrimSpace(final) == "" {
					return result, failed("FINAL_RESPONSE_MISSING")
				}
				if err := p.shutdown(ctx); err != nil {
					stopped = true
					return result, contextError(ctx, err)
				}
				stopped = true
				if err := authorize(); err != nil {
					return result, err
				}
				if o.Registry.validate(ctx, used) != nil {
					return result, contextError(ctx, failed("NATIVE_CODEX_EVIDENCE_CHANGED"))
				}
				if o.Validate(ctx, request, receipt) != nil {
					return result, contextError(ctx, failed("NATIVE_CODEX_FINAL_DENIED"))
				}
				if err := authorize(); err != nil {
					return result, err
				}
				if o.Registry.validate(ctx, used) != nil {
					return result, contextError(ctx, failed("NATIVE_CODEX_EVIDENCE_CHANGED"))
				}
				if ctx.Err() != nil {
					return result, ctx.Err()
				}
				trace.FinalResponse = true
				trace.OutputBytes = len(final)
				return result, emit(final)
			}
		}
	}
}

func validEffort(e string) bool {
	switch e {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
		return true
	}
	return false
}

func strictJSON(raw []byte, target any) error {
	// encoding/json accepts duplicate object keys. Callback identities and
	// envelopes must have one interpretation before decoding their typed shape.
	keys := json.NewDecoder(bytes.NewReader(raw))
	if uniqueJSONValue(keys, 0) != nil || keys.Decode(new(any)) != io.EOF {
		return errDenied
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errDenied
	}
	return nil
}

func uniqueJSONValue(d *json.Decoder, depth int) error {
	if depth > 64 {
		return errDenied
	}
	token, err := d.Token()
	if err != nil {
		return errDenied
	}
	delim, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return errDenied
			}
			seen[name] = true
			if uniqueJSONValue(d, depth+1) != nil {
				return errDenied
			}
		}
	case '[':
		for d.More() {
			if uniqueJSONValue(d, depth+1) != nil {
				return errDenied
			}
		}
	default:
		return errDenied
	}
	_, err = d.Token()
	return err
}

func requestID(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return "s:" + s, identity.MatchString(s) && !strings.HasPrefix(s, "ivoai-native-")
	}
	n := json.Number(string(raw))
	v, err := n.Int64()
	return "n:" + strconv.FormatInt(v, 10), err == nil && v >= 0 && v <= 1<<53-1
}

func nativeEnvironment(input []string) ([]string, error) {
	allowed := map[string]bool{"HOME": true, "CODEX_HOME": true, "PATH": true, "LANG": true, "LC_ALL": true, "TERM": true, "XDG_CONFIG_HOME": true, "XDG_CACHE_HOME": true}
	seen := map[string]string{}
	out := []string{}
	for _, entry := range input {
		k, v, ok := strings.Cut(entry, "=")
		_, duplicate := seen[k]
		if !ok || !allowed[k] || strings.ContainsRune(v, 0) || len(v) > 16384 || duplicate {
			return nil, errDenied
		}
		seen[k] = v
		out = append(out, entry)
	}
	for _, key := range []string{"HOME", "CODEX_HOME"} {
		if !filepath.IsAbs(seen[key]) {
			return nil, errDenied
		}
		info, err := os.Stat(seen[key])
		if err != nil || !info.IsDir() {
			return nil, errDenied
		}
	}
	return out, nil
}

func nativeSettings(o Options, request core.ExecutorRequest) ([]string, error) {
	if !syntheticURL(o.SyntheticProviderURL) {
		return nil, errDenied
	}
	settings := []string{`sandbox_mode="read-only"`, `approval_policy="never"`, `web_search="disabled"`, `features.shell_tool=false`, `features.hooks=false`, `features.apps=false`, `features.plugins=false`, `features.remote_plugin=false`, `features.multi_agent=false`, `features.multi_agent_v2=false`, `features.goals=false`, `features.memories=false`, `features.code_mode_prewarm=false`, `history.persistence="none"`}
	settings = append(settings, `model_provider="ivoai_evidence_fixture"`, `model_providers.ivoai_evidence_fixture.name="Synthetic local evidence fixture"`, `model_providers.ivoai_evidence_fixture.base_url=`+strconv.Quote(o.SyntheticProviderURL), `model_providers.ivoai_evidence_fixture.env_key="IVOAI_NATIVE_EVIDENCE_FIXTURE_KEY"`, `model_providers.ivoai_evidence_fixture.wire_api="responses"`, `model_providers.ivoai_evidence_fixture.requires_openai_auth=false`, `model_providers.ivoai_evidence_fixture.supports_websockets=false`, `model_providers.ivoai_evidence_fixture.request_max_retries=0`, `model_providers.ivoai_evidence_fixture.stream_max_retries=0`)
	settings = append(settings, "model="+strconv.Quote(request.Model), "model_reasoning_effort="+strconv.Quote(request.Effort))
	args := []string{}
	for _, s := range settings {
		args = append(args, "-c", s)
	}
	return args, nil
}

func syntheticURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") || u.Path != "/v1" || u.RawPath != "" {
		return false
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}

func verifyBinary(ctx context.Context, path, expected string) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return errDenied
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 || info.Size() > 512<<20 {
		return errDenied
	}
	h := sha256.New()
	buf := make([]byte, 64<<10)
	var n int64
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		size, err := f.Read(buf)
		n += int64(size)
		if n > 512<<20 {
			return errDenied
		}
		_, _ = h.Write(buf[:size])
		if err == io.EOF {
			break
		}
		if err != nil {
			return errDenied
		}
	}
	if n != info.Size() || hex.EncodeToString(h.Sum(nil)) != expected {
		return errDenied
	}
	return nil
}

type capture struct {
	mu    sync.Mutex
	body  bytes.Buffer
	limit int
	total int
}

func (b *capture) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.total += len(p)
	if len(p) > b.limit-b.body.Len() {
		return 0, errDenied
	}
	return b.body.Write(p)
}

func (b *capture) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.total
}

func process(ctx context.Context, binary string, args, env []string, dir string) *exec.Cmd {
	c := exec.CommandContext(ctx, binary, args...)
	c.Env = env
	c.Dir = dir
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		err := syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	c.WaitDelay = time.Second
	return c
}

func probe(ctx context.Context, binary string, args, env []string, dir string, limit int) ([]byte, error) {
	out, stderr := &capture{limit: limit}, &capture{limit: 4096}
	c := process(ctx, binary, args, env, dir)
	c.Stdout, c.Stderr = out, stderr
	if err := c.Run(); err != nil {
		return nil, contextError(ctx, errDenied)
	}
	return append([]byte(nil), out.body.Bytes()...), nil
}

func start(ctx context.Context, binary string, args, env []string, dir string) (*protocol, error) {
	c := process(ctx, binary, args, env, dir)
	stderr := &capture{limit: 4096}
	c.Stderr = stderr
	input, err := c.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, writer, err := os.Pipe()
	if err != nil {
		input.Close()
		return nil, err
	}
	c.Stdout = writer
	if err := c.Start(); err != nil {
		input.Close()
		output.Close()
		writer.Close()
		return nil, err
	}
	// Wait must not close a StdoutPipe before the reader consumes the final
	// frame. Own the pipe explicitly; only the scanner closes its read end.
	writer.Close()
	events, done := make(chan readFrame, 8), make(chan error, 1)
	readerDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			// A child may retain stdout after its process-group leader exits.
			// Closing our read end also interrupts a blocked scanner in that case.
			_ = output.Close()
		case <-readerDone:
		}
	}()
	go func() {
		defer close(readerDone)
		defer close(events)
		defer output.Close()
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 64<<10), 2<<20)
		total, count := 0, 0
		for scanner.Scan() {
			raw := scanner.Bytes()
			total += len(raw)
			count++
			var value frame
			var err error
			if total > 32<<20 || count > 4096 {
				err = errDenied
			} else {
				err = strictJSON(raw, &value)
			}
			select {
			case events <- readFrame{value, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case events <- readFrame{err: errDenied}:
			case <-ctx.Done():
			}
		}
	}()
	go func() { done <- c.Wait() }()
	return &protocol{input: input, output: output, events: events, done: done, cmd: c, stderr: stderr}, nil
}

func (p *protocol) send(v any) error { return json.NewEncoder(p.input).Encode(v) }
func (p *protocol) response(ctx context.Context, id string) (frame, error) {
	for {
		select {
		case <-ctx.Done():
			return frame{}, ctx.Err()
		case incoming, ok := <-p.events:
			if !ok || incoming.err != nil {
				return frame{}, failed("NATIVE_CODEX_STREAM_INCOMPLETE")
			}
			v := incoming.value
			if len(v.ID) != 0 && v.Method != "" {
				return frame{}, failed("NATIVE_CODEX_SERVER_REQUEST_DENIED")
			}
			var actual string
			if json.Unmarshal(v.ID, &actual) == nil && actual == id {
				if len(v.Error) != 0 || len(v.Result) == 0 {
					return frame{}, failed("NATIVE_CODEX_RPC_FAILED")
				}
				return v, nil
			}
		}
	}
}

func (p *protocol) shutdown(ctx context.Context) error {
	defer p.output.Close()
	_ = p.input.Close()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	events := p.events
	done := p.done
	killAndWait := func() {
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		if done != nil {
			<-done
			done = nil
		}
	}
	for {
		if done == nil && events == nil {
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
			return nil
		}
		select {
		case err := <-done:
			done = nil
			if err != nil {
				killAndWait()
				return failed("NATIVE_CODEX_SHUTDOWN_FAILED")
			}
			// Drain the owned stdout pipe after Wait. A queued server request
			// must still fail closed even if process exit won this select.
		case incoming, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if incoming.err != nil || len(incoming.value.ID) != 0 && incoming.value.Method != "" {
				killAndWait()
				return failed("NATIVE_CODEX_SERVER_REQUEST_DENIED")
			}
		case <-ctx.Done():
			killAndWait()
			return ctx.Err()
		case <-timer.C:
			killAndWait()
			return failed("NATIVE_CODEX_SHUTDOWN_FAILED")
		}
	}
}
