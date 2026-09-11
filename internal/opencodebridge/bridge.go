// Package opencodebridge connects the managed OpenCode frontend to IVOAI's
// subscription-backed executor contracts. It never reads or copies executor
// credentials: Codex and Claude Code remain responsible for their own auth.
package opencodebridge

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ivo-lopes/ivoai/internal/promptgate"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxRequestBytes = 1 << 20
	maxPromptBytes  = 512 << 10
)

type ExecutorRequest struct {
	Executor          string
	Model             string
	Effort            string
	SelectionMode     string
	CatalogRevision   string
	Prompt            string
	FrontendSessionID string
	ExecutorSessionID string
}

type ExecutorResult struct {
	AuthReference       string
	Trace               *ExecutionTrace
	ExecutorSessionID   string
	CompressionUsed     bool
	CompressionProvider string
	SelectionMode       string
	RequestedModel      string
	Model               string
	Effort              string
	CatalogRevision     string
	ConfigurationSource string
}

type ExecutorRunner interface {
	Run(context.Context, ExecutorRequest, func(string) error) (ExecutorResult, error)
}

type SelectExecutor func(context.Context, string) (string, error)
type MonitorExecutor func(context.Context, string) string
type BuildFailoverHandoff func(string, string, string) string
type LookupMapping func(string) []Mapping
type PersistMapping func(Mapping) error
type ClaimRequest func(frontendID, messageID string) (bool, error)

type Mapping struct {
	AuthReference       string          `json:"auth_reference,omitempty"`
	Trace               *ExecutionTrace `json:"trace,omitempty"`
	FrontendSessionID   string          `json:"frontend_session_id"`
	Executor            string          `json:"executor"`
	ExecutorSessionID   string          `json:"executor_session_id"`
	CompressionUsed     bool            `json:"compression_used"`
	CompressionProvider string          `json:"compression_provider,omitempty"`
	SelectionMode       string          `json:"selection_mode,omitempty"`
	RequestedModel      string          `json:"requested_model,omitempty"`
	EffectiveModel      string          `json:"effective_model,omitempty"`
	EffectiveEffort     string          `json:"effective_effort,omitempty"`
	CatalogRevision     string          `json:"catalog_revision,omitempty"`
	ConfigurationSource string          `json:"configuration_source,omitempty"`
}

type ServerView struct {
	AuthState string `json:"auth_state"`
	ID        string `json:"id"`
	Alias     string `json:"alias"`
	Purpose   string `json:"purpose,omitempty"`
	Selected  bool   `json:"selected"`
	Enabled   bool   `json:"enabled"`
	Health    string `json:"health"`
}

type Status struct {
	KnowledgePolicy       string       `json:"knowledge_policy,omitempty"`
	ConcurrencyPolicy     string       `json:"concurrency_policy,omitempty"`
	ConcurrencyLimit      int          `json:"concurrency_limit,omitempty"`
	WorkerCap             int          `json:"worker_cap,omitempty"`
	Workers               []WorkerView `json:"workers,omitempty"`
	QuotaMode             string       `json:"quota_mode,omitempty"`
	ParallelWriteDegraded bool         `json:"parallel_write_degraded,omitempty"`
	PlanState             string       `json:"plan_state,omitempty"`
	TaskCount             int          `json:"task_count,omitempty"`
	WorkersActive         int          `json:"workers_active,omitempty"`
	WorkersQueued         int          `json:"workers_queued,omitempty"`
	WorkersDone           int          `json:"workers_done,omitempty"`
	PromptReadiness       string       `json:"prompt_readiness,omitempty"`
	PromptMissing         []string     `json:"prompt_missing,omitempty"`
	ResumePolicy          string       `json:"resume_policy,omitempty"`
	PermissionMode        string       `json:"permission_mode"`
	Version               string       `json:"version"`
	SessionID             string       `json:"session_id"`
	Frontend              string       `json:"frontend"`
	Primary               string       `json:"primary"`
	SelectionMode         string       `json:"selection_mode,omitempty"`
	RequestedExecutor     string       `json:"requested_executor,omitempty"`
	RequestedEffort       string       `json:"requested_effort,omitempty"`
	RequestedModel        string       `json:"requested_model,omitempty"`
	EffectiveModel        string       `json:"effective_model,omitempty"`
	EffectiveEffort       string       `json:"effective_effort,omitempty"`
	ConfigurationSource   string       `json:"configuration_source,omitempty"`
	Mode                  string       `json:"mode"`
	SessionState          string       `json:"session_state"`
	KnowledgeMode         string       `json:"knowledge_mode"`
	ConfiguredCount       int          `json:"configured_count"`
	EnabledCount          int          `json:"enabled_count"`
	ConnectedCount        int          `json:"connected_count"`
	SelectedCount         int          `json:"selected_count"`
	Servers               []ServerView `json:"servers"`
	CodexAuth             string       `json:"codex_auth"`
	ClaudeAuth            string       `json:"claude_auth"`
	CodexQuota            string       `json:"codex_quota"`
	ClaudeQuota           string       `json:"claude_quota"`
	OpenCodeAuth          string       `json:"opencode_auth"`
	OpenCodeQuota         string       `json:"opencode_quota"`
	Compression           string       `json:"compression"`
	Memory                string       `json:"memory"`
	Context               string       `json:"context"`
	Skills                string       `json:"skills"`
	UpdatedAt             time.Time    `json:"updated_at"`
}

// WorkerView intentionally excludes task prompts, transcripts, result bodies,
// filesystem paths and credentials. Only operational metadata reaches the TUI.
type WorkerView struct {
	Skills   []string `json:"skills,omitempty"`
	ID       string   `json:"id"`
	Role     string   `json:"role"`
	Executor string   `json:"executor"`
	Tier     string   `json:"tier"`
	Model    string   `json:"model"`
	Effort   string   `json:"effort"`
	State    string   `json:"state"`
	Purposes []string `json:"purposes"`
	MCPs     []string `json:"mcps"`
}

type Options struct {
	// RequirePromptGate is always enabled by AUTO. Direct executor sessions keep
	// their own intake contract; it is not a user-configurable bypass for AUTO.
	RequirePromptGate     bool
	NativePermissions     func() []PermissionView
	ReplyNativePermission func(context.Context, string, bool) error
	// AuthReference returns only an official non-sensitive identity/epoch.
	// Empty means continuity cannot be proved: never reuse a native resume ID.
	AuthReference      func(context.Context, string) (string, error)
	Token              string
	PreferredExecutor  string
	Runner             ExecutorRunner
	Select             SelectExecutor
	SelectAlternate    func(context.Context, string, []string) (string, error)
	Monitor            MonitorExecutor
	FailoverHandoff    BuildFailoverHandoff
	MaxFailovers       int
	Status             func() Status
	Mapping            PersistMapping
	Attempt            func(TurnAttempt) error
	LookupMapping      LookupMapping
	ClaimRequest       ClaimRequest
	Catalog            ModelCatalog
	AuthorizeSelection func(context.Context, Selection) error
	OnSelection        func(Selection)
}

type Bridge struct {
	requirePromptGate     bool
	promptReadiness       promptgate.Result
	nativePermissions     func() []PermissionView
	replyNativePermission func(context.Context, string, bool) error
	authReference         func(context.Context, string) (string, error)
	server                *http.Server
	listener              net.Listener
	url                   string
	token                 string
	runner                ExecutorRunner
	selectFn              SelectExecutor
	selectAlternate       func(context.Context, string, []string) (string, error)
	monitor               MonitorExecutor
	handoff               BuildFailoverHandoff
	maxFailovers          int
	statusFn              func() Status
	mapping               PersistMapping
	attempt               func(TurnAttempt) error
	lookup                LookupMapping
	claim                 ClaimRequest
	catalog               ModelCatalog
	authorizeSelection    func(context.Context, Selection) error
	onSelection           func(Selection)
	mu                    sync.Mutex
	writer                sync.Mutex
	sessions              map[string]map[string]Mapping
	lastExecutor          map[string]string
	active                map[string]activeExecution
	completed             map[string]cachedCompletion
	completedOrder        []string
	nextRun               uint64
	closed                chan struct{}
}

type activeExecution struct {
	id     uint64
	cancel context.CancelFunc
}

type cachedCompletion struct {
	content    string
	failed     bool
	replayable bool
	selection  string
}

func Start(options Options) (*Bridge, error) {
	if options.Runner == nil || options.Select == nil || options.Status == nil {
		return nil, errors.New("OpenCode bridge requires runner, selector, and status source")
	}
	token := strings.TrimSpace(options.Token)
	if token == "" {
		var raw [32]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return nil, fmt.Errorf("generate OpenCode bridge capability: %w", err)
		}
		token = hex.EncodeToString(raw[:])
	}
	if len(options.Catalog.entries) == 0 {
		options.Catalog = DefaultCatalog()
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for OpenCode bridge: %w", err)
	}
	bridge := &Bridge{
		requirePromptGate: options.RequirePromptGate,
		nativePermissions: options.NativePermissions, replyNativePermission: options.ReplyNativePermission,
		selectAlternate: options.SelectAlternate,
		authReference:   options.AuthReference,
		listener:        listener, url: "http://" + listener.Addr().String(), token: token,
		runner: options.Runner, selectFn: options.Select, monitor: options.Monitor, handoff: options.FailoverHandoff, maxFailovers: options.MaxFailovers, statusFn: options.Status,
		mapping: options.Mapping, attempt: options.Attempt, lookup: options.LookupMapping, claim: options.ClaimRequest, catalog: options.Catalog, authorizeSelection: options.AuthorizeSelection, onSelection: options.OnSelection,
		sessions: map[string]map[string]Mapping{}, lastExecutor: map[string]string{}, active: map[string]activeExecution{}, completed: map[string]cachedCompletion{}, closed: make(chan struct{}),
	}
	if bridge.maxFailovers <= 0 || bridge.maxFailovers > 2 {
		bridge.maxFailovers = 2
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", bridge.authorize(bridge.health))
	mux.HandleFunc("GET /status", bridge.authorize(bridge.status))
	mux.HandleFunc("GET /native-permissions", bridge.authorize(bridge.nativePermissionList))
	mux.HandleFunc("POST /native-permissions/reply", bridge.authorize(bridge.nativePermissionReply))
	mux.HandleFunc("GET /v1/models", bridge.authorize(bridge.models))
	mux.HandleFunc("POST /v1/chat/completions", bridge.authorize(bridge.chat))
	bridge.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	go func() {
		_ = bridge.server.Serve(listener)
		close(bridge.closed)
	}()
	return bridge, nil
}

func (b *Bridge) URL() string   { return b.url }
func (b *Bridge) Token() string { return b.token }

func (b *Bridge) Close(ctx context.Context) error {
	b.mu.Lock()
	for _, execution := range b.active {
		execution.cancel()
	}
	b.mu.Unlock()
	err := b.server.Shutdown(ctx)
	select {
	case <-b.closed:
	case <-ctx.Done():
		if err == nil {
			err = ctx.Err()
		}
	}
	return err
}

func (b *Bridge) authorize(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			http.Error(w, "loopback clients only", http.StatusForbidden)
			return
		}
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if len(provided) != len(b.token) || subtle.ConstantTimeCompare([]byte(provided), []byte(b.token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (b *Bridge) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"healthy": true})
}

func (b *Bridge) status(w http.ResponseWriter, _ *http.Request) {
	value := b.statusFn()
	if b.requirePromptGate {
		b.mu.Lock()
		value.PromptReadiness = b.promptReadiness.State
		value.PromptMissing = append([]string(nil), b.promptReadiness.Missing...)
		b.mu.Unlock()
		if value.PromptReadiness == "" {
			value.PromptReadiness = "waiting_for_prompt"
		}
	}
	value.UpdatedAt = time.Now().UTC()
	writeJSON(w, http.StatusOK, value)
}

func (b *Bridge) models(w http.ResponseWriter, _ *http.Request) {
	data := make([]map[string]any, 0, len(b.catalog.entries))
	for _, model := range b.catalog.entries {
		data = append(data, map[string]any{"id": model.ID, "object": "model", "owned_by": "ivoai"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (b *Bridge) Catalog() ModelCatalog { return b.catalog }

type chatRequest struct {
	Model           string        `json:"model"`
	ReasoningEffort string        `json:"reasoning_effort"`
	Stream          bool          `json:"stream"`
	Messages        []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func (b *Bridge) chat(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes+1))
	if err != nil || len(body) > maxRequestBytes {
		writeOpenAIError(w, http.StatusRequestEntityTooLarge, "request exceeds the IVOAI bridge limit")
		return
	}
	var request chatRequest
	if json.Unmarshal(body, &request) != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid IVOAI bridge request")
		return
	}
	selection, ok := b.catalog.Resolve(request.Model, request.ReasoningEffort)
	if !ok {
		writeOpenAIErrorCode(w, http.StatusBadRequest, "unknown model or unsupported reasoning effort", "EXPLICIT_MODEL_UNAVAILABLE")
		return
	}
	selectionKey := selection.CatalogRevision + ":" + selection.RequestedID + ":" + selection.Effort
	prompt, err := lastUserText(request.Messages)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error())
		return
	}
	frontendID := strings.TrimSpace(r.Header.Get("X-IVOAI-OpenCode-Session"))
	if !safeID(frontendID) {
		writeOpenAIError(w, http.StatusBadRequest, "missing or invalid OpenCode session identity")
		return
	}
	messageID := strings.TrimSpace(r.Header.Get("X-IVOAI-OpenCode-Message"))
	if !safeID(messageID) {
		writeOpenAIError(w, http.StatusBadRequest, "missing or invalid OpenCode message identity")
		return
	}
	requestKey := frontendID + "\x00" + messageID
	b.writer.Lock()
	defer b.writer.Unlock()
	b.mu.Lock()
	previousCompletion, alreadyCompleted := b.completed[requestKey]
	b.mu.Unlock()
	if alreadyCompleted {
		if b.authReference != nil {
			writeOpenAIError(w, http.StatusConflict, "duplicate request not replayed across an authentication reprobe")
			return
		}
		if previousCompletion.failed || !previousCompletion.replayable || previousCompletion.selection != selectionKey {
			writeOpenAIError(w, http.StatusConflict, "duplicate OpenCode request was not re-executed")
			return
		}
		writeCompletion(w, request.Model, request.Stream, previousCompletion.content)
		return
	}
	if b.requirePromptGate {
		readiness := promptgate.Assess(prompt)
		b.mu.Lock()
		b.promptReadiness = readiness
		b.mu.Unlock()
		if !readiness.Ready {
			// A refusal is a completed intake response, not an executor turn.
			// No selector, auth reprobe, mapping, claim, or runner is invoked.
			writeCompletion(w, request.Model, request.Stream, readiness.Message())
			return
		}
	}
	b.mu.Lock()
	mappings := b.sessions[frontendID]
	previousExecutor := b.lastExecutor[frontendID]
	b.mu.Unlock()
	if len(mappings) == 0 && b.lookup != nil {
		mappings = map[string]Mapping{}
		for _, stored := range b.lookup(frontendID) {
			if stored.FrontendSessionID != frontendID || !safeID(stored.ExecutorSessionID) || !quota.Supported(quota.Provider(stored.Executor)) {
				continue
			}
			if previousExecutor == "" {
				previousExecutor = stored.Executor
			}
			mappings[stored.Executor] = stored
		}
		b.mu.Lock()
		b.sessions[frontendID] = mappings
		b.lastExecutor[frontendID] = previousExecutor
		b.mu.Unlock()
	}
	executor := selection.Executor
	if selection.Mode == "auto" {
		executor, err = b.selectFn(r.Context(), previousExecutor)
		if err != nil {
			writeOpenAIError(w, http.StatusServiceUnavailable, "no eligible subscription executor")
			return
		}
	}
	if !quota.Supported(quota.Provider(executor)) {
		writeOpenAIError(w, http.StatusServiceUnavailable, "invalid executor selection")
		return
	}
	selection.Executor = executor
	if selection.Mode == "explicit" && b.authorizeSelection != nil {
		if err := b.authorizeSelection(r.Context(), selection); err != nil {
			writeOpenAIErrorCode(w, http.StatusServiceUnavailable, "selected executor or model is not currently eligible", "EXPLICIT_MODEL_UNAVAILABLE")
			return
		}
	}
	if b.claim != nil {
		claimed, claimErr := b.claim(frontendID, messageID)
		if claimErr != nil {
			writeOpenAIError(w, http.StatusInternalServerError, "IVOAI could not persist the request claim")
			return
		}
		if !claimed {
			writeOpenAIError(w, http.StatusConflict, "duplicate OpenCode request was not re-executed")
			return
		}
	}
	if b.onSelection != nil {
		b.onSelection(selection)
	}
	resumeID := ""
	authReference := ""
	if b.authReference != nil {
		authReference, err = b.authReference(r.Context(), executor)
		if err != nil || authReference != "" && !safeID(authReference) {
			writeOpenAIErrorCode(w, http.StatusServiceUnavailable, "executor authentication could not be verified", "EXECUTOR_AUTH_UNAVAILABLE")
			return
		}
	}
	if previous, ok := mappings[executor]; ok {
		identityMatches := b.authReference == nil || authReference != "" && previous.AuthReference == authReference
		if identityMatches && (previous.RequestedModel == selection.RequestedModel() || previous.RequestedModel == selection.RequestedID || previous.RequestedModel == "" && selection.Mode == "auto") {
			resumeID = previous.ExecutorSessionID
		}
	}
	executionCtx, cancel := context.WithCancel(r.Context())
	b.mu.Lock()
	if old, ok := b.active[frontendID]; ok {
		old.cancel()
	}
	b.nextRun++
	runID := b.nextRun
	b.active[frontendID] = activeExecution{id: runID, cancel: cancel}
	b.mu.Unlock()
	defer func() {
		cancel()
		b.mu.Lock()
		if current, ok := b.active[frontendID]; ok && current.id == runID {
			delete(b.active, frontendID)
		}
		b.mu.Unlock()
	}()

	stream := request.Stream
	var chunks []string
	var emitted atomic.Bool
	emit := func(text string) error {
		if text == "" {
			return nil
		}
		emitted.Store(true)
		chunks = append(chunks, text)
		if stream {
			return writeChunk(w, request.Model, text)
		}
		return nil
	}
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_ = writeChunk(w, request.Model, "")
	}
	var result ExecutorResult
	attempted := []string{executor}
	chooseAlternate := func() (string, error) {
		var next string
		var selectErr error
		if b.selectAlternate != nil {
			next, selectErr = b.selectAlternate(r.Context(), executor, append([]string(nil), attempted...))
		} else {
			next, selectErr = b.selectFn(r.Context(), executor)
		}
		for _, used := range attempted {
			if used == next {
				return "", errors.New("executor already attempted")
			}
		}
		if !quota.Supported(quota.Provider(next)) {
			return "", errors.New("executor unavailable")
		}
		return next, selectErr
	}
	advance := func(next, reason string) {
		cancel()
		if b.handoff != nil {
			if handoff := strings.TrimSpace(b.handoff(executor, next, reason)); handoff != "" {
				prompt += "\n\n" + handoff
			}
		}
		if stream {
			_ = writeChunk(w, request.Model, "\n\n[IVOAI switched executor: "+executor+" → "+next+".]\n\n")
		}
		executor, resumeID = next, ""
		attempted = append(attempted, next)
		selection.Executor = next
		if b.onSelection != nil {
			b.onSelection(selection)
		}
		executionCtx, cancel = context.WithCancel(r.Context())
		b.mu.Lock()
		b.active[frontendID] = activeExecution{id: runID, cancel: cancel}
		b.mu.Unlock()
	}
	for attempt := 0; ; attempt++ {
		if attempt > 0 && b.authReference != nil {
			authReference, err = b.authReference(executionCtx, executor)
			if err != nil || authReference != "" && !safeID(authReference) {
				if stream {
					_ = writeStreamError(w, "executor authentication could not be verified", "EXECUTOR_AUTH_UNAVAILABLE")
				} else {
					writeOpenAIError(w, http.StatusServiceUnavailable, "executor authentication could not be verified")
				}
				return
			}
			resumeID = ""
		}
		type executionResult struct {
			value ExecutorResult
			err   error
		}
		finished := make(chan executionResult, 1)
		turn := TurnAttempt{ID: fmt.Sprintf("%d_%d_%d", time.Now().UnixNano(), runID, attempt), Frontend: "opencode", FrontendSessionID: frontendID, RequestedExecutor: selection.Executor, EffectiveExecutor: executor, RequestedModel: selection.Model, RequestedEffort: selection.Effort, StartedAt: time.Now().UTC(), State: "running"}
		if b.attempt != nil {
			if err := b.attempt(turn); err != nil {
				if stream {
					_ = writeStreamError(w, "IVOAI could not persist the turn attempt", "turn_attempt_persistence_failure")
				} else {
					writeOpenAIError(w, http.StatusInternalServerError, "IVOAI could not persist the turn attempt")
				}
				return
			}
		}
		go func(currentExecutor, currentPrompt, currentResume string, attemptCtx context.Context) {
			value, runErr := b.runner.Run(attemptCtx, ExecutorRequest{
				Executor: currentExecutor, Model: selection.Model, Effort: selection.Effort,
				SelectionMode: selection.Mode, CatalogRevision: selection.CatalogRevision,
				Prompt: currentPrompt, FrontendSessionID: frontendID, ExecutorSessionID: currentResume,
			}, emit)
			value.SelectionMode, value.RequestedModel, value.CatalogRevision = selection.Mode, selection.RequestedModel(), selection.CatalogRevision
			value.AuthReference = authReference
			ended := time.Now().UTC()
			turn.EndedAt, turn.EffectiveModel, turn.EffectiveEffort = &ended, value.Model, value.Effort
			turn.NativeSessionID, turn.Trace, turn.State = value.ExecutorSessionID, value.Trace, "completed"
			if value.Trace != nil {
				exit := value.Trace.ExitCode
				turn.ExitCode, turn.FinalResponse = &exit, value.Trace.FinalResponse
			}
			if runErr != nil {
				turn.State, turn.Failure = "failed", FailureClass(runErr)
			}
			if b.attempt != nil {
				if err := b.attempt(turn); err != nil {
					runErr = failure("turn_attempt_persistence_failure")
				}
			}
			finished <- executionResult{value: value, err: runErr}
		}(executor, prompt, resumeID, executionCtx)
		limit := make(chan string, 1)
		if b.monitor != nil && selection.Mode == "auto" {
			go func(currentExecutor string, attemptCtx context.Context) {
				if reason := strings.TrimSpace(b.monitor(attemptCtx, currentExecutor)); reason != "" {
					limit <- reason
				}
			}(executor, executionCtx)
		}
		var outcome executionResult
		select {
		case outcome = <-finished:
			if outcome.err != nil {
				var limitError interface {
					error
					RateLimited() bool
				}
				if selection.Mode == "auto" && !emitted.Load() && attempt < b.maxFailovers && errors.As(outcome.err, &limitError) && limitError.RateLimited() {
					if next, selectErr := chooseAlternate(); selectErr == nil {
						advance(next, "native provider reported a rate limit")
						continue
					}
				}
				failureClass := FailureClass(outcome.err)
				b.storeCompletion(requestKey, cachedCompletion{failed: true, selection: selectionKey})
				_ = b.persistMapping(frontendID, executor, outcome.value)
				if stream {
					_ = writeStreamError(w, executorErrorMessage(failureClass), failureClass)
					return
				}
				writeOpenAIErrorCode(w, http.StatusBadGateway, executorErrorMessage(failureClass), failureClass)
				return
			}
			result = outcome.value
		case reason := <-limit:
			finishedReceived := false
			select {
			case outcome = <-finished:
				finishedReceived = true
				if outcome.err == nil {
					result = outcome.value
					break
				}
			default:
			}
			if outcome.err == nil && outcome.value.ExecutorSessionID != "" {
				break
			}
			cancel()
			if !finishedReceived {
				outcome = <-finished
			}
			_ = b.persistMapping(frontendID, executor, outcome.value)
			if emitted.Load() {
				b.storeCompletion(requestKey, cachedCompletion{failed: true, selection: selectionKey})
				if stream {
					_ = writeStreamError(w, "IVOAI stopped after a quota change; partial output was not accepted", "executor_quota_changed")
					return
				}
				writeOpenAIError(w, http.StatusServiceUnavailable, "IVOAI stopped after a quota change")
				return
			}
			if attempt >= b.maxFailovers {
				b.storeCompletion(requestKey, cachedCompletion{failed: true, selection: selectionKey})
				if stream {
					_ = writeStreamError(w, "IVOAI quota failover limit reached", "executor_failover_limit")
					return
				}
				writeOpenAIError(w, http.StatusServiceUnavailable, "IVOAI quota failover limit reached")
				return
			}
			next, selectErr := chooseAlternate()
			if selectErr != nil || next == executor || !quota.Supported(quota.Provider(next)) {
				b.storeCompletion(requestKey, cachedCompletion{failed: true, selection: selectionKey})
				if stream {
					_ = writeStreamError(w, "IVOAI has no alternate subscription executor", "executor_unavailable")
					return
				}
				writeOpenAIError(w, http.StatusServiceUnavailable, "no alternate subscription executor")
				return
			}
			advance(next, reason)
			continue
		case <-r.Context().Done():
			cancel()
			outcome := <-finished
			_ = b.persistMapping(frontendID, executor, outcome.value)
			b.storeCompletion(requestKey, cachedCompletion{failed: true, selection: selectionKey})
			return
		}
		break
	}
	if err := b.persistMapping(frontendID, executor, result); err != nil {
		b.storeCompletion(requestKey, cachedCompletion{failed: true, selection: selectionKey})
		if stream {
			_ = writeStreamError(w, "IVOAI could not persist the executor session mapping", "session_mapping_failure")
			return
		}
		writeOpenAIError(w, http.StatusInternalServerError, "IVOAI could not persist the executor session mapping")
		return
	}
	content := strings.Join(chunks, "")
	b.storeCompletion(requestKey, cachedCompletion{content: content, replayable: len(content) <= 1<<20, selection: selectionKey})
	if stream {
		_ = writeFinish(w, request.Model)
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flush(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": "ivoai", "object": "chat.completion", "created": time.Now().Unix(), "model": request.Model,
		"choices": []map[string]any{{"index": 0, "message": map[string]string{"role": "assistant", "content": content}, "finish_reason": "stop"}},
	})
}

func (b *Bridge) persistMapping(frontendID, executor string, result ExecutorResult) error {
	if !safeID(result.ExecutorSessionID) {
		return nil
	}
	mapping := Mapping{
		AuthReference:     result.AuthReference,
		FrontendSessionID: frontendID, Executor: executor, ExecutorSessionID: result.ExecutorSessionID, Trace: result.Trace,
		CompressionUsed: result.CompressionUsed, CompressionProvider: result.CompressionProvider,
		SelectionMode: result.SelectionMode, RequestedModel: result.RequestedModel, EffectiveModel: result.Model,
		EffectiveEffort: result.Effort, CatalogRevision: result.CatalogRevision, ConfigurationSource: result.ConfigurationSource,
	}
	b.mu.Lock()
	if b.sessions[frontendID] == nil {
		b.sessions[frontendID] = map[string]Mapping{}
	}
	b.sessions[frontendID][executor] = mapping
	b.lastExecutor[frontendID] = executor
	if len(b.sessions) > 128 {
		for id := range b.sessions {
			if id != frontendID {
				delete(b.sessions, id)
				delete(b.lastExecutor, id)
				break
			}
		}
	}
	b.mu.Unlock()
	if b.mapping != nil {
		return b.mapping(mapping)
	}
	return nil
}

func (b *Bridge) storeCompletion(key string, value cachedCompletion) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.completed[key]; !exists {
		b.completedOrder = append(b.completedOrder, key)
	}
	if !value.replayable {
		value.content = ""
	}
	b.completed[key] = value
	for len(b.completedOrder) > 256 {
		delete(b.completed, b.completedOrder[0])
		b.completedOrder = b.completedOrder[1:]
	}
}

func writeCompletion(w http.ResponseWriter, model string, stream bool, content string) {
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_ = writeChunk(w, model, content)
		_ = writeFinish(w, model)
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flush(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": "ivoai", "object": "chat.completion", "created": time.Now().Unix(), "model": model, "choices": []map[string]any{{"index": 0, "message": map[string]string{"role": "assistant", "content": content}, "finish_reason": "stop"}}})
}

func lastUserText(messages []chatMessage) (string, error) {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role != "user" {
			continue
		}
		var text string
		if json.Unmarshal(messages[index].Content, &text) == nil {
			text = strings.TrimSpace(text)
			if text != "" && len(text) <= maxPromptBytes {
				return text, nil
			}
		}
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(messages[index].Content, &parts) == nil {
			var out strings.Builder
			for _, part := range parts {
				switch part.Type {
				case "text", "input_text":
					if part.Text == "" {
						return "", errors.New("request contains an empty text part")
					}
					out.WriteString(part.Text)
				default:
					return "", errors.New("managed AUTO does not silently discard file or image inputs")
				}
			}
			text = strings.TrimSpace(out.String())
			if text != "" && len(text) <= maxPromptBytes {
				return text, nil
			}
		}
	}
	return "", errors.New("request has no bounded user message")
}

func safeID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !(char == '-' || char == '_' || char == '.' || char >= '0' && char <= '9' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z') {
			return false
		}
	}
	return true
}

func writeChunk(w http.ResponseWriter, model, text string) error {
	delta := map[string]any{}
	if text != "" {
		delta["content"] = text
	} else {
		delta["role"] = "assistant"
	}
	return writeSSE(w, map[string]any{"id": "ivoai", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model, "choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": nil}}})
}

func writeFinish(w http.ResponseWriter, model string) error {
	return writeSSE(w, map[string]any{"id": "ivoai", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model, "choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}})
}

func writeStreamError(w http.ResponseWriter, message, code string) error {
	return writeSSE(w, map[string]any{"error": map[string]string{"message": message, "type": "ivoai_bridge_error", "code": code}})
}

func writeSSE(w http.ResponseWriter, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(w, "data: %s\n\n", body); err != nil {
		return err
	}
	flush(w)
	return nil
}

func flush(w http.ResponseWriter) {
	if value, ok := w.(http.Flusher); ok {
		value.Flush()
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeOpenAIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"message": message, "type": "ivoai_bridge_error"}})
}

func writeOpenAIErrorCode(w http.ResponseWriter, status int, message, code string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"message": message, "type": "ivoai_bridge_error", "code": code}})
}

// ScanJSONLines provides the bounded scanner used by official-client adapters.
func ScanJSONLines(reader io.Reader, handle func(map[string]any) error) error {
	counted := &countingReader{reader: reader}
	scanner := bufio.NewScanner(counted)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	seenProtocolEvent := false
	for scanner.Scan() {
		if counted.total > 8<<20 {
			return errors.New("executor output exceeds limit")
		}
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var value map[string]any
		if err := json.Unmarshal(line, &value); err != nil {
			// Official clients may emit bounded human startup noise before the
			// JSON event stream. Once the stream starts, or when a line claims
			// to be a JSON object, corruption is never silently discarded.
			if seenProtocolEvent || line[0] == '{' {
				return errors.New("malformed executor JSON event")
			}
			continue
		}
		seenProtocolEvent = true
		if err := handle(value); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("invalid or oversized executor output: %w", err)
	}
	return nil
}

type countingReader struct {
	reader io.Reader
	total  int64
}

func (r *countingReader) Read(body []byte) (int, error) {
	count, err := r.reader.Read(body)
	r.total += int64(count)
	return count, err
}
