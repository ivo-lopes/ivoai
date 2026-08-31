package knowledgerouter

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/secrets"
	"github.com/ivo-lopes/ivoai/internal/serverpool"
)

const (
	maxRequestBytes  = 4 << 20
	maxResponseBytes = 16 << 20
	maxFailures      = 2
)

type Event struct {
	Operation     string
	SourceID      string
	SourceAlias   string
	Purpose       string
	SelectedCount int
	Failover      bool
	Partial       bool
	Duration      time.Duration
	State         string
	Reason        string
}

type Options struct {
	Selection   serverpool.Selection
	Credentials map[string]secrets.ClientCredential
	Client      *http.Client
	Timeout     time.Duration
	Now         func() time.Time
	Observe     func(Event)
}

type Router struct {
	selection serverpool.Selection
	creds     map[string]secrets.ClientCredential
	client    *http.Client
	timeout   time.Duration
	now       func() time.Time
	observe   func(Event)
	server    *http.Server
	listener  net.Listener
	token     string
	baseURL   string
	mu        sync.Mutex
	failures  map[string]failureState
}

type failureState struct {
	Count     int
	OpenUntil time.Time
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  struct {
		Name string `json:"name"`
	} `json:"params"`
}

type sourceResult struct {
	Source map[string]any `json:"source"`
	Result any            `json:"result,omitempty"`
	Error  any            `json:"error,omitempty"`
}

func Start(options Options) (*Router, error) {
	if options.Client == nil {
		options.Client = &http.Client{Timeout: 20 * time.Second}
	}
	client := *options.Client
	previousRedirect := client.CheckRedirect
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) == 0 || len(via) > 3 || request.URL.Scheme != via[0].URL.Scheme || !strings.EqualFold(request.URL.Host, via[0].URL.Host) {
			return errors.New("cross-origin redirect refused")
		}
		if previousRedirect != nil {
			return previousRedirect(request, via)
		}
		return nil
	}
	if options.Timeout <= 0 || options.Timeout > 30*time.Second {
		options.Timeout = 15 * time.Second
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start knowledge router: %w", err)
	}
	router := &Router{
		selection: options.Selection, creds: options.Credentials, client: &client,
		timeout: options.Timeout, now: options.Now, observe: options.Observe,
		listener: listener, token: token, baseURL: "http://" + listener.Addr().String(), failures: map[string]failureState{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp/context", router.authorize(router.handleContext))
	mux.HandleFunc("/mcp/memory", router.authorize(router.handleMemory))
	mux.HandleFunc("/memory/", router.authorize(router.handleMemoryHook))
	mux.HandleFunc("/memory", router.authorize(router.handleMemoryHook))
	router.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = router.server.Serve(listener) }()
	return router, nil
}

func (r *Router) BaseURL() string { return r.baseURL }
func (r *Router) Token() string   { return r.token }

func (r *Router) Close(ctx context.Context) error {
	r.mu.Lock()
	r.token = ""
	r.mu.Unlock()
	return r.server.Shutdown(ctx)
}

func (r *Router) authorize(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		r.mu.Lock()
		expected := r.token
		r.mu.Unlock()
		provided := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
		if expected == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, request)
	}
}

func (r *Router) handleContext(w http.ResponseWriter, request *http.Request) {
	r.handleMCP(w, request, "context")
}

func (r *Router) handleMemory(w http.ResponseWriter, request *http.Request) {
	r.handleMCP(w, request, "memory")
}

func (r *Router) handleMCP(w http.ResponseWriter, request *http.Request, kind string) {
	if request.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := readBounded(request.Body, maxRequestBytes)
	if err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	var rpc rpcRequest
	if err := json.Unmarshal(body, &rpc); err != nil || rpc.JSONRPC != "2.0" {
		writeRPCError(w, rpc.ID, -32600, "invalid JSON-RPC request")
		return
	}
	write := kind == "memory" && isWriteTool(rpc.Method, rpc.Params.Name)
	groups := r.groupsFor(kind)
	if len(groups) == 0 {
		writeRPCError(w, rpc.ID, -32021, kind+" is not available for the selected sources")
		return
	}
	if write && (r.selection.PurposeCount() != 1 || len(groups) != 1) {
		writeRPCError(w, rpc.ID, -32020, "memory write requires exactly one knowledge destination")
		return
	}
	if write || len(groups) == 1 || rpc.Method != "tools/call" {
		response, profile, failover, err := r.callGroup(request.Context(), groups[0], kind, body, write)
		r.emit(Event{Operation: kind, SourceID: profile.ID, SourceAlias: profile.Alias, Purpose: profile.Purpose, SelectedCount: len(groups), Failover: failover, Duration: response.duration, State: state(err), Reason: boundedReason(err)})
		if err != nil {
			writeRPCError(w, rpc.ID, -32022, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(response.status)
		_, _ = w.Write(response.body)
		return
	}
	r.federate(w, request, rpc, kind, body, groups)
}

func (r *Router) federate(w http.ResponseWriter, request *http.Request, rpc rpcRequest, kind string, body []byte, groups []serverpool.SourceGroup) {
	type outcome struct {
		index    int
		response upstreamResponse
		profile  config.ServerProfile
		failover bool
		err      error
	}
	channel := make(chan outcome, len(groups))
	for index, group := range groups {
		go func(index int, group serverpool.SourceGroup) {
			response, profile, failover, err := r.callGroup(request.Context(), group, kind, body, false)
			channel <- outcome{index: index, response: response, profile: profile, failover: failover, err: err}
		}(index, group)
	}
	outcomes := make([]outcome, len(groups))
	partial := false
	for range groups {
		value := <-channel
		outcomes[value.index] = value
		if value.err != nil {
			partial = true
		}
		r.emit(Event{Operation: kind, SourceID: value.profile.ID, SourceAlias: value.profile.Alias, Purpose: value.profile.Purpose, SelectedCount: len(groups), Failover: value.failover, Partial: value.err != nil, Duration: value.response.duration, State: state(value.err), Reason: boundedReason(value.err)})
	}
	results := make([]sourceResult, 0, len(outcomes))
	for _, value := range outcomes {
		metadata := sourceMetadata(value.profile)
		if value.err != nil {
			results = append(results, sourceResult{Source: metadata, Error: boundedReason(value.err)})
			continue
		}
		var envelope struct {
			Result any `json:"result"`
			Error  any `json:"error"`
		}
		if err := json.Unmarshal(value.response.body, &envelope); err != nil {
			results = append(results, sourceResult{Source: metadata, Error: "malformed upstream JSON-RPC response"})
			partial = true
			continue
		}
		results = append(results, sourceResult{Source: metadata, Result: envelope.Result, Error: envelope.Error})
	}
	writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(rpc.ID), "result": map[string]any{"federated": true, "partial": partial, "sources": results}})
}

func (r *Router) handleMemoryHook(w http.ResponseWriter, request *http.Request) {
	if r.selection.PurposeCount() != 1 || len(r.selection.Groups) != 1 {
		http.Error(w, "memory hook requires exactly one knowledge purpose", http.StatusConflict)
		return
	}
	body, err := readBounded(request.Body, maxRequestBytes)
	if err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	group := r.groupsFor("memory")
	if len(group) != 1 {
		http.Error(w, "memory hooks unavailable", http.StatusServiceUnavailable)
		return
	}
	profile := group[0].Profiles[0]
	endpoint := strings.TrimRight(profile.MemoryHooksURL, "/") + strings.TrimPrefix(request.URL.Path, "/memory")
	response, err := r.call(request.Context(), profile, endpoint, request.Method, body, request.Header.Get("Content-Type"))
	r.emit(Event{Operation: "memory_hook", SourceID: profile.ID, SourceAlias: profile.Alias, Purpose: profile.Purpose, SelectedCount: 1, Duration: response.duration, State: state(err), Reason: boundedReason(err)})
	if err != nil {
		http.Error(w, "memory upstream unavailable", http.StatusBadGateway)
		return
	}
	copyResponse(w, response)
}

type upstreamResponse struct {
	status   int
	body     []byte
	header   http.Header
	duration time.Duration
}

func (r *Router) callGroup(ctx context.Context, group serverpool.SourceGroup, kind string, body []byte, write bool) (upstreamResponse, config.ServerProfile, bool, error) {
	var last error
	for index, profile := range group.Profiles {
		if r.circuitOpen(profile.ID) {
			last = errors.New("source circuit is open")
			continue
		}
		endpoint := profile.ContextMCPURL
		if kind == "memory" {
			endpoint = profile.MemoryMCPURL
		}
		response, err := r.call(ctx, profile, endpoint, http.MethodPost, body, "application/json")
		if err == nil && response.status >= 200 && response.status < 300 {
			if validationErr := validateRPCResponse(response.body); validationErr == nil {
				r.recordSuccess(profile.ID)
				return response, profile, index > 0, nil
			} else {
				err = validationErr
			}
		}
		if err == nil {
			err = fmt.Errorf("upstream HTTP %d", response.status)
		}
		r.recordFailure(profile.ID)
		last = err
		if write {
			// A write may have reached the primary. Never retry it automatically.
			break
		}
	}
	profile := config.ServerProfile{}
	if len(group.Profiles) > 0 {
		profile = group.Profiles[0]
	}
	return upstreamResponse{}, profile, false, last
}

func validateRPCResponse(body []byte) error {
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.JSONRPC != "2.0" || len(envelope.Result) == 0 && len(envelope.Error) == 0 {
		return errors.New("malformed upstream JSON-RPC response")
	}
	return nil
}

func (r *Router) call(ctx context.Context, profile config.ServerProfile, endpoint, method string, body []byte, contentType string) (upstreamResponse, error) {
	started := r.now()
	if endpoint == "" {
		return upstreamResponse{}, errors.New("source endpoint is not exposed")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return upstreamResponse{}, errors.New("source endpoint is invalid")
	}
	credential, ok := r.creds[profile.ID]
	if !ok || credential.Token == "" {
		return upstreamResponse{}, errors.New("source credential is unavailable")
	}
	callContext, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callContext, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return upstreamResponse{}, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+credential.Token)
	resp, err := r.client.Do(req)
	if err != nil {
		return upstreamResponse{duration: r.now().Sub(started)}, err
	}
	defer resp.Body.Close()
	payload, err := readBounded(resp.Body, maxResponseBytes)
	if err != nil {
		return upstreamResponse{status: resp.StatusCode, duration: r.now().Sub(started)}, errors.New("upstream response exceeds limit")
	}
	return upstreamResponse{status: resp.StatusCode, body: payload, header: resp.Header.Clone(), duration: r.now().Sub(started)}, nil
}

func (r *Router) groupsFor(kind string) []serverpool.SourceGroup {
	result := make([]serverpool.SourceGroup, 0, len(r.selection.Groups))
	for _, group := range r.selection.Groups {
		members := make([]config.ServerProfile, 0, len(group.Profiles))
		for _, profile := range group.Profiles {
			if kind == "context" && profile.ContextMCPURL != "" || kind == "memory" && profile.MemoryMCPURL != "" {
				members = append(members, profile)
			}
		}
		if len(members) > 0 {
			group.Profiles = members
			result = append(result, group)
		}
	}
	return result
}

func (r *Router) circuitOpen(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failures[id].OpenUntil.After(r.now())
}

func (r *Router) recordFailure(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value := r.failures[id]
	value.Count++
	if value.Count >= maxFailures {
		value.OpenUntil = r.now().Add(30 * time.Second)
	}
	r.failures[id] = value
}

func (r *Router) recordSuccess(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.failures, id)
}

func (r *Router) emit(event Event) {
	if r.observe != nil {
		r.observe(event)
	}
}

func isWriteTool(method, name string) bool {
	if method != "tools/call" {
		return false
	}
	switch name {
	case "memory_write_page", "memory_delete_page", "memory_feedback":
		return true
	default:
		return false
	}
}

func sourceMetadata(profile config.ServerProfile) map[string]any {
	return map[string]any{"source_id": profile.ID, "source_alias": profile.Alias, "purpose": profile.Purpose, "redundancy_group": profile.RedundancyGroup}
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	value, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(value)) > limit {
		return nil, errors.New("size limit exceeded")
	}
	return value, nil
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func copyResponse(w http.ResponseWriter, response upstreamResponse) {
	w.Header().Set("Content-Type", response.header.Get("Content-Type"))
	w.WriteHeader(response.status)
	_, _ = w.Write(response.body)
}

func state(err error) string {
	if err != nil {
		return "failed"
	}
	return "success"
}

func boundedReason(err error) string {
	if err == nil {
		return ""
	}
	value := err.Error()
	if len(value) > 160 {
		value = value[:160]
	}
	return value
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate session capability: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

// SortedSourceAliases exposes only bounded, non-secret routing metadata.
func (r *Router) SortedSourceAliases() []string {
	aliases := []string{}
	for _, group := range r.selection.Groups {
		for _, profile := range group.Profiles {
			aliases = append(aliases, profile.Alias)
		}
	}
	sort.Strings(aliases)
	return aliases
}
