// Package codexfrontend adapts the official Codex App Server protocol to the
// shared IVOAI turn boundary. It is a frontend, not another orchestrator.
package codexfrontend

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/promptgate"
)

const maxFrame = 2 << 20

type Options struct {
	Binary, Directory, RuntimeDir, SessionID string
	Bridge                                   *opencodebridge.Bridge
	Environment                              []string
}

type rpc struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

type admittedTurn struct {
	thread, nativeID, message, prompt, model, effort string
	requestID                                        json.RawMessage
	consumed                                         bool
	cancel                                           context.CancelFunc
}

type Facade struct {
	options                          Options
	ctx                              context.Context
	cancel                           context.CancelFunc
	server                           *http.Server
	listener                         net.Listener
	remoteToken, providerToken, home string
	mu                               sync.Mutex
	client                           *websocket.Conn
	active                           *admittedTurn
	sequence                         uint64
	decisions                        map[string]string
	threads                          map[string]bool
	model, effort                    string
	upstream                         io.WriteCloser
	upstreamMu                       sync.Mutex
	process                          *exec.Cmd
	done                             chan error
	closeOnce                        sync.Once
	lastTurnError                    error
}

func randomToken() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func Start(ctx context.Context, options Options) (*Facade, error) {
	if options.Bridge == nil || options.Binary == "" || options.SessionID == "" {
		return nil, errors.New("CODEX_FRONTEND_CONFIGURATION_INVALID")
	}
	ctx, cancel := context.WithCancel(ctx)
	f := &Facade{options: options, ctx: ctx, cancel: cancel, decisions: map[string]string{}, threads: map[string]bool{}}
	var err error
	f.remoteToken, err = randomToken()
	if err != nil {
		cancel()
		return nil, err
	}
	f.providerToken, err = randomToken()
	if err != nil {
		cancel()
		return nil, err
	}
	if err = os.MkdirAll(options.RuntimeDir, 0700); err != nil {
		cancel()
		return nil, err
	}
	f.home, err = os.MkdirTemp(options.RuntimeDir, "codex-native-")
	if err != nil {
		cancel()
		return nil, err
	}
	f.model, f.effort = options.Bridge.InitialSelection()
	if f.model == "" || f.model == "auto" {
		primary, ok := options.Bridge.Catalog().StrongPrimary("codex")
		if !ok {
			f.Close()
			return nil, errors.New("MODEL_UNAVAILABLE: native Codex primary requires a verified strong model")
		}
		f.model, f.effort = primary.ID, primary.DefaultEffort
	}
	f.listener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		f.Close()
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", f.connect)
	mux.HandleFunc("POST /v1/responses", f.responses)
	f.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 8192}
	go func() { _ = f.server.Serve(f.listener) }()
	args := append(f.configArgs(), "app-server", "--listen", "stdio://")
	f.process = exec.CommandContext(ctx, options.Binary, args...)
	// App Server may own background children (e.g. plugin discovery). Bound
	// them to this instance so cancellation cannot leave directory writers.
	f.process.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	f.process.Cancel = func() error {
		err := syscall.Kill(-f.process.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	f.process.WaitDelay = time.Second
	f.process.Dir, f.process.Env = options.Directory, f.Environment()
	f.process.Stderr = io.Discard // Never retain upstream diagnostic prompt/env content.
	f.upstream, err = f.process.StdinPipe()
	if err != nil {
		f.Close()
		return nil, err
	}
	output, err := f.process.StdoutPipe()
	if err != nil {
		f.Close()
		return nil, err
	}
	if err = f.process.Start(); err != nil {
		f.Close()
		return nil, errors.New("CODEX_APP_SERVER_START_FAILED")
	}
	f.done = make(chan error, 1)
	go func() { f.readUpstream(output); f.done <- f.process.Wait(); f.cancel() }()
	return f, nil
}

func (f *Facade) configArgs() []string {
	selection, _ := f.options.Bridge.Catalog().Resolve(f.model, f.effort)
	settings := []string{
		`model_provider="ivoai"`, `model=` + quote(selection.Model),
		`model_providers.ivoai.name="IVOAI orchestration"`,
		`model_providers.ivoai.base_url=` + quote("http://"+f.listener.Addr().String()+"/v1"),
		`model_providers.ivoai.env_key="IVOAI_NATIVE_PROVIDER_TOKEN"`,
		`model_providers.ivoai.wire_api="responses"`,
		`model_providers.ivoai.requires_openai_auth=false`,
		`model_providers.ivoai.supports_websockets=false`,
		`model_providers.ivoai.request_max_retries=0`,
		`model_providers.ivoai.stream_max_retries=0`,
		`model_providers.ivoai.stream_idle_timeout_ms=3600000`,
		`sandbox_mode="read-only"`, `approval_policy="never"`,
		`web_search="disabled"`, `history.persistence="none"`,
		`features.hooks=false`, `features.apps=false`,
		`features.plugins=false`, `features.remote_plugin=false`,
		`features.multi_agent=false`, `features.code_mode_prewarm=false`,
	}
	if f.effort != "" {
		settings = append(settings, `model_reasoning_effort=`+quote(f.effort))
	}
	args := []string{}
	for _, setting := range settings {
		args = append(args, "-c", setting)
	}
	return args
}

func quote(value string) string { body, _ := json.Marshal(value); return string(body) }

// Only the isolated native frontend receives these short-lived local keys.
// Official worker provider credentials and global configuration are untouched.
func (f *Facade) Environment() []string {
	allowed := map[string]bool{"HOME": true, "PATH": true, "TERM": true, "COLORTERM": true, "LANG": true, "LC_ALL": true, "TERM_PROGRAM": true, "TERM_PROGRAM_VERSION": true, "DISPLAY": true, "WAYLAND_DISPLAY": true, "XDG_RUNTIME_DIR": true}
	env := []string{}
	for _, entry := range f.options.Environment {
		name, _, _ := strings.Cut(entry, "=")
		if allowed[name] {
			env = append(env, entry)
		}
	}
	return append(env, "CODEX_HOME="+f.home, "IVOAI_NATIVE_REMOTE_TOKEN="+f.remoteToken, "IVOAI_NATIVE_PROVIDER_TOKEN="+f.providerToken)
}

func (f *Facade) Args() []string {
	return append(f.configArgs(), "--remote", "ws://"+f.listener.Addr().String(), "--remote-auth-token-env", "IVOAI_NATIVE_REMOTE_TOKEN")
}

func (f *Facade) Close() {
	f.closeOnce.Do(f.close)
}

// TurnError is independent of the native TUI process exit status.
func (f *Facade) TurnError() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastTurnError
}

func (f *Facade) close() {
	f.cancel()
	f.mu.Lock()
	if f.active != nil && f.active.cancel != nil {
		f.active.cancel()
	}
	client := f.client
	f.mu.Unlock()
	if client != nil {
		_ = client.CloseNow()
	}
	if f.server != nil {
		_ = f.server.Close()
	}
	if f.upstream != nil {
		_ = f.upstream.Close()
	}
	if f.done != nil {
		select {
		case <-f.done:
		case <-time.After(3 * time.Second):
			if f.process.Process != nil {
				_ = f.process.Process.Kill()
			}
			<-f.done
		}
		// The parent can exit before its descendants; cancel the owned group
		// before removing the private home, not only when Wait times out.
		_ = syscall.Kill(-f.process.Process.Pid, syscall.SIGKILL)
	}
	// The directory is newly allocated by this instance, never an operator path.
	if f.home != "" && filepath.Dir(f.home) == filepath.Clean(f.options.RuntimeDir) {
		_ = os.RemoveAll(f.home)
	}
}

func authorized(r *http.Request, token string) bool {
	return r.Header.Get("Origin") == "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) == 1
}

func (f *Facade) connect(w http.ResponseWriter, r *http.Request) {
	if !authorized(r, f.remoteToken) {
		http.Error(w, "unauthorized", 401)
		return
	}
	f.mu.Lock()
	if f.client != nil {
		f.mu.Unlock()
		http.Error(w, "frontend already connected", 409)
		return
	}
	client, err := websocket.Accept(w, r, nil)
	if err != nil {
		f.mu.Unlock()
		return
	}
	f.client = client
	f.mu.Unlock()
	client.SetReadLimit(maxFrame)
	defer func() { _ = client.CloseNow(); f.cancel() }()
	for {
		kind, body, err := client.Read(f.ctx)
		if err != nil {
			return
		}
		if kind != websocket.MessageText {
			return
		}
		var message rpc
		if json.Unmarshal(body, &message) != nil {
			return
		}
		f.handle(message)
	}
}

func (f *Facade) send(message any) {
	body, err := json.Marshal(message)
	if err != nil {
		return
	}
	f.mu.Lock()
	client := f.client
	f.mu.Unlock()
	if client != nil {
		ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
		defer cancel()
		if client.Write(ctx, websocket.MessageText, body) != nil {
			f.cancel()
		}
	}
}

func (f *Facade) reject(id json.RawMessage, reason string) {
	if len(id) == 0 {
		return
	}
	f.send(map[string]any{"id": id, "error": map[string]any{"code": -32600, "message": reason}})
}

func (f *Facade) forward(message rpc) {
	f.upstreamMu.Lock()
	defer f.upstreamMu.Unlock()
	if json.NewEncoder(f.upstream).Encode(message) != nil {
		f.cancel()
	}
}

func (f *Facade) readUpstream(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxFrame)
	for scanner.Scan() {
		var message rpc
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			f.cancel()
			return
		}
		if len(message.Error) > 0 {
			f.mu.Lock()
			if f.active != nil && string(f.active.requestID) == string(message.ID) {
				f.lastTurnError = errors.New("CODEX_NATIVE_TURN_REJECTED")
				f.active = nil
			}
			f.mu.Unlock()
		}
		if message.Method == "thread/started" {
			var p struct {
				Thread struct {
					ID string `json:"id"`
				} `json:"thread"`
			}
			_ = json.Unmarshal(message.Params, &p)
			f.mu.Lock()
			if len(f.threads) < 128 {
				f.threads[p.Thread.ID] = true
			}
			f.mu.Unlock()
		}
		if message.Method == "turn/started" || message.Method == "turn/completed" {
			var p struct {
				ThreadID string `json:"threadId"`
				Turn     struct {
					ID     string `json:"id"`
					Status string `json:"status"`
				} `json:"turn"`
			}
			_ = json.Unmarshal(message.Params, &p)
			f.mu.Lock()
			if f.active != nil && f.active.thread == p.ThreadID {
				if message.Method == "turn/started" {
					f.active.nativeID = p.Turn.ID
				} else {
					if p.Turn.Status == "failed" && f.lastTurnError == nil {
						f.lastTurnError = errors.New("CODEX_NATIVE_TURN_FAILED")
					}
					if p.Turn.Status == "interrupted" && f.lastTurnError == nil {
						f.lastTurnError = errors.New("TURN_CANCELLED")
					}
					if f.active.cancel != nil {
						f.active.cancel()
					}
					f.active = nil
					f.decisions = map[string]string{}
				}
			}
			f.mu.Unlock()
		}
		f.send(message)
	}
}

func (f *Facade) handle(message rpc) {
	if message.Method == "" {
		var key string
		_ = json.Unmarshal(message.ID, &key)
		f.mu.Lock()
		decision, ok := f.decisions[key]
		if ok {
			f.decisions[key] = ""
		}
		f.mu.Unlock()
		if ok {
			if decision == "" {
				f.reject(message.ID, "DECISION_ALREADY_CONSUMED")
				return
			}
			var answer struct {
				Answers map[string]struct {
					Answers []string `json:"answers"`
				} `json:"answers"`
			}
			_ = json.Unmarshal(message.Result, &answer)
			values := answer.Answers["decision"].Answers
			allow := len(values) == 1 && values[0] == "Approve"
			_ = f.options.Bridge.ReplyDecision(f.ctx, decision, allow)
			return
		}
		// Only known IVOAI decisions may authorize execution. Other upstream
		// prompts are not an alternate authority over the orchestrated session.
		f.reject(message.ID, "UNSUPPORTED_NATIVE_DECISION")
		return
	}
	switch message.Method {
	case "initialize", "initialized", "thread/read", "thread/list", "thread/loaded/list", "thread/turns/list", "thread/items/list", "thread/name/set", "thread/unsubscribe", "account/read", "account/rateLimits/read", "config/read", "configRequirements/read", "modelProvider/capabilities/read", "collaborationMode/list", "experimentalFeature/list", "permissionProfile/list", "skills/list", "hooks/list", "mcpServerStatus/list", "app/list", "plugin/list", "fuzzyFileSearch", "fuzzyFileSearch/sessionStart", "fuzzyFileSearch/sessionUpdate", "fuzzyFileSearch/sessionStop":
		f.forward(message)
	case "model/list":
		f.send(map[string]any{"id": message.ID, "result": f.models()})
	case "thread/start":
		var p map[string]any
		if json.Unmarshal(message.Params, &p) != nil {
			f.reject(message.ID, "INVALID_THREAD")
			return
		}
		// Never accept provider, tools, hooks or policy configuration from the UI.
		selection, _ := f.options.Bridge.Catalog().Resolve(f.model, f.effort)
		clean := map[string]any{"cwd": f.options.Directory, "model": selection.Model, "modelProvider": "ivoai", "sandbox": "read-only", "approvalPolicy": "never", "ephemeral": true}
		message.Params, _ = json.Marshal(clean)
		f.forward(message)
	case "turn/start":
		f.admit(message)
	case "turn/interrupt":
		var p struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
		}
		if json.Unmarshal(message.Params, &p) != nil {
			f.reject(message.ID, "INVALID_INTERRUPT")
			return
		}
		f.mu.Lock()
		if f.active == nil || f.active.thread != p.ThreadID || f.active.nativeID != p.TurnID {
			f.mu.Unlock()
			f.reject(message.ID, "TURN_NOT_AVAILABLE")
			return
		}
		f.lastTurnError = errors.New("TURN_CANCELLED")
		f.decisions = map[string]string{}
		if f.active != nil && f.active.cancel != nil {
			f.active.cancel()
		}
		f.mu.Unlock()
		f.forward(message)
	default:
		// Includes shellCommand, command/exec, process/spawn, review/start,
		// turn/steer, inject_items, config writes and future unknown methods.
		f.reject(message.ID, "ORCHESTRATED_METHOD_DENIED: use a structured turn or an explicit direct session")
	}
}

func (f *Facade) admit(message rpc) {
	var p struct {
		ThreadID string `json:"threadId"`
		Model    string `json:"model"`
		Effort   string `json:"effort"`
		Input    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"input"`
	}
	if json.Unmarshal(message.Params, &p) != nil || len(p.Input) == 0 {
		f.reject(message.ID, "PROMPT_INPUT_UNSUPPORTED")
		return
	}
	parts := []string{}
	for _, input := range p.Input {
		if input.Type != "text" {
			f.reject(message.ID, "PROMPT_INPUT_UNSUPPORTED: structured text is required")
			return
		}
		parts = append(parts, input.Text)
	}
	prompt := strings.Join(parts, "\n")
	readiness := promptgate.Assess(prompt)
	if !readiness.Ready {
		f.reject(message.ID, "PROMPT_INSUFFICIENT: "+readiness.Message())
		return
	}
	f.mu.Lock()
	if f.active != nil || !f.threads[p.ThreadID] {
		f.mu.Unlock()
		f.reject(message.ID, "TURN_NOT_AVAILABLE")
		return
	}
	model, effort := f.model, f.effort
	if p.Model != "" {
		model = ""
		for _, entry := range f.options.Bridge.Catalog().Entries() {
			if entry.Executor == "codex" && (entry.UpstreamModel == p.Model || entry.ID == p.Model) {
				model = entry.ID
				break
			}
		}
	}
	if p.Effort != "" {
		effort = p.Effort
	}
	selection, ok := f.options.Bridge.Catalog().Resolve(model, effort)
	if !ok || selection.Executor != "codex" {
		f.mu.Unlock()
		f.reject(message.ID, "MODEL_UNAVAILABLE: explicit Codex primary selection cannot fall back silently")
		return
	}
	f.sequence++
	f.active = &admittedTurn{thread: p.ThreadID, requestID: append(json.RawMessage(nil), message.ID...), message: fmt.Sprintf("native_%d", f.sequence), prompt: prompt, model: model, effort: effort}
	f.lastTurnError = nil
	f.model, f.effort = model, effort
	f.mu.Unlock()
	clean := map[string]any{"threadId": p.ThreadID, "input": []map[string]any{{"type": "text", "text": prompt, "text_elements": []any{}}}, "model": selection.Model, "effort": effort, "approvalPolicy": "never", "sandboxPolicy": map[string]any{"type": "readOnly"}}
	message.Params, _ = json.Marshal(clean)
	f.forward(message)
}

func (f *Facade) models() any {
	f.mu.Lock()
	selected := f.model
	f.mu.Unlock()
	data := []any{}
	for _, entry := range f.options.Bridge.Catalog().Entries() {
		if entry.Executor != "codex" {
			continue
		}
		efforts := []any{}
		for _, effort := range entry.SupportedEfforts {
			efforts = append(efforts, map[string]any{"reasoningEffort": effort, "description": effort})
		}
		data = append(data, map[string]any{"id": entry.UpstreamModel, "model": entry.UpstreamModel, "displayName": entry.Name, "description": "IVOAI primary; workers are routed independently", "hidden": false, "isDefault": entry.ID == selected, "defaultReasoningEffort": entry.DefaultEffort, "supportedReasoningEfforts": efforts, "inputModalities": []string{"text"}})
	}
	return map[string]any{"data": data, "nextCursor": nil}
}
