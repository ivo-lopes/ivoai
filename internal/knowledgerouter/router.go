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
	"mime"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/secrets"
	"github.com/ivo-lopes/ivoai/internal/serverpool"
)

const (
	maxRequestBytes   = 4 << 20
	maxResponseBytes  = 16 << 20
	maxFailures       = 2
	mcpAccept         = "application/json, text/event-stream"
	mcpProtocolHeader = "MCP-Protocol-Version"
	mcpSessionHeader  = "Mcp-Session-Id"
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
	if !acceptsMediaType(request.Header.Get("Accept"), "application/json") || !acceptsMediaType(request.Header.Get("Accept"), "text/event-stream") {
		http.Error(w, "MCP client must accept application/json and text/event-stream", http.StatusNotAcceptable)
		return
	}
	if !hasMediaType(request.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "MCP requests require application/json", http.StatusUnsupportedMediaType)
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
	write := kind == "memory" && memoryToolRequiresSingleDestination(rpc.Method, rpc.Params.Name)
	groups := r.groupsFor(kind)
	if len(groups) == 0 {
		writeRPCError(w, rpc.ID, -32021, kind+" is not available for the selected sources")
		return
	}
	if write && (r.selection.PurposeCount() != 1 || len(groups) != 1) {
		writeRPCError(w, rpc.ID, -32020, "memory write requires exactly one knowledge destination")
		return
	}
	if len(groups) > 1 {
		if request.Header.Get(mcpSessionHeader) != "" {
			writeRPCError(w, rpc.ID, -32023, "stateful MCP sessions are not supported across federated sources")
			return
		}
		if rpc.Method == "initialize" || rpc.Method == "notifications/initialized" {
			r.initializeFederation(w, request, rpc, kind, body, groups)
			return
		}
	}
	if write || len(groups) == 1 {
		response, profile, failover, err := r.callGroup(request.Context(), groups[0], kind, body, write, hasResponseID(rpc.ID), request.Header, maxResponseBytes)
		r.emit(Event{Operation: kind, SourceID: profile.ID, SourceAlias: profile.Alias, Purpose: profile.Purpose, SelectedCount: len(groups), Failover: failover, Duration: response.duration, State: state(err), Reason: boundedReason(err)})
		if err != nil {
			writeRPCError(w, rpc.ID, -32022, boundedReason(err))
			return
		}
		copyMCPResponse(w, response)
		return
	}
	if rpc.Method != "tools/call" {
		var lastErr error
		for _, group := range groups {
			response, profile, failover, err := r.callGroup(request.Context(), group, kind, body, false, hasResponseID(rpc.ID), request.Header, maxResponseBytes)
			r.emit(Event{Operation: kind, SourceID: profile.ID, SourceAlias: profile.Alias, Purpose: profile.Purpose, SelectedCount: len(groups), Failover: failover, Partial: err != nil, Duration: response.duration, State: state(err), Reason: boundedReason(err)})
			if err == nil {
				copyMCPResponse(w, response)
				return
			}
			lastErr = err
		}
		writeRPCError(w, rpc.ID, -32022, boundedReason(lastErr))
		return
	}
	r.federate(w, request, rpc, kind, body, groups)
}

func (r *Router) initializeFederation(w http.ResponseWriter, request *http.Request, rpc rpcRequest, kind string, body []byte, groups []serverpool.SourceGroup) {
	type outcome struct {
		response upstreamResponse
		profile  config.ServerProfile
		err      error
	}
	channel := make(chan outcome, len(groups))
	limit := maxResponseBytes / int64(len(groups))
	for _, group := range groups {
		go func(group serverpool.SourceGroup) {
			response, profile, _, err := r.callGroup(request.Context(), group, kind, body, false, hasResponseID(rpc.ID), request.Header, limit)
			channel <- outcome{response: response, profile: profile, err: err}
		}(group)
	}
	var first *upstreamResponse
	var lastErr error
	for range groups {
		value := <-channel
		r.emit(Event{Operation: kind, SourceID: value.profile.ID, SourceAlias: value.profile.Alias, Purpose: value.profile.Purpose, SelectedCount: len(groups), Partial: value.err != nil, Duration: value.response.duration, State: state(value.err), Reason: boundedReason(value.err)})
		if value.err != nil {
			lastErr = value.err
			continue
		}
		if value.response.header.Get(mcpSessionHeader) != "" {
			writeRPCError(w, rpc.ID, -32023, "stateful upstream MCP sessions are not supported for federation")
			return
		}
		if first == nil {
			copy := value.response
			first = &copy
		}
	}
	if first == nil {
		writeRPCError(w, rpc.ID, -32022, boundedReason(lastErr))
		return
	}
	if !hasResponseID(rpc.ID) {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	first.header.Del(mcpSessionHeader)
	copyMCPResponse(w, *first)
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
			response, profile, failover, err := r.callGroup(request.Context(), group, kind, body, false, true, request.Header, maxResponseBytes/int64(len(groups)))
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
		payload, err := rpcResponsePayload(value.response.body, value.response.header.Get("Content-Type"))
		if err != nil {
			results = append(results, sourceResult{Source: metadata, Error: "malformed upstream JSON-RPC response"})
			partial = true
			continue
		}
		var envelope struct {
			Result any `json:"result"`
			Error  any `json:"error"`
		}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			results = append(results, sourceResult{Source: metadata, Error: "malformed upstream JSON-RPC response"})
			partial = true
			continue
		}
		results = append(results, sourceResult{Source: metadata, Result: envelope.Result, Error: envelope.Error})
	}
	federated := map[string]any{"federated": true, "partial": partial, "sources": results}
	text, err := json.Marshal(federated)
	if err != nil {
		writeRPCError(w, rpc.ID, -32603, "failed to encode federated result")
		return
	}
	if len(text) > maxResponseBytes {
		writeRPCError(w, rpc.ID, -32022, "federated MCP result exceeds limit")
		return
	}
	// tools/call must always return a CallToolResult. The previous federation
	// envelope put the source collection directly under result, which was valid
	// JSON-RPC but not valid MCP and was rejected by Codex as an unexpected
	// response type. Keep the machine-readable federation value as text: the
	// upstream tool's output schema describes one source and therefore must not
	// be paired with a different federated structuredContent shape.
	writeJSON(w, map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(rpc.ID),
		"result":  map[string]any{"content": []any{map[string]any{"type": "text", "text": string(text)}}},
	})
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
	response, err := r.call(request.Context(), profile, endpoint, request.Method, body, request.Header.Get("Content-Type"), nil, maxResponseBytes)
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

func (r *Router) callGroup(ctx context.Context, group serverpool.SourceGroup, kind string, body []byte, write, expectsResponse bool, headers http.Header, responseLimit int64) (upstreamResponse, config.ServerProfile, bool, error) {
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
		response, err := r.call(ctx, profile, endpoint, http.MethodPost, body, "application/json", headers, responseLimit)
		if err == nil && response.status >= 200 && response.status < 300 {
			if validationErr := validateRPCResponse(response.body, response.header.Get("Content-Type"), expectsResponse); validationErr == nil {
				r.recordSuccess(profile.ID)
				return response, profile, index > 0, nil
			} else {
				err = validationErr
			}
		}
		if err == nil {
			if response.status == http.StatusNotAcceptable {
				err = errors.New("upstream rejected MCP HTTP content negotiation (HTTP 406)")
			} else {
				err = fmt.Errorf("upstream HTTP %d", response.status)
			}
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

func validateRPCResponse(body []byte, contentType string, expectsResponse bool) error {
	if !expectsResponse && len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	_, err := rpcResponsePayload(body, contentType)
	return err
}

func rpcResponsePayload(body []byte, contentType string) ([]byte, error) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, errors.New("upstream MCP response has invalid Content-Type")
	}
	if mediaType == "application/json" {
		if err := validateJSONRPCResponse(body); err != nil {
			return nil, err
		}
		return body, nil
	}
	if mediaType != "text/event-stream" {
		return nil, errors.New("upstream MCP response has unsupported Content-Type")
	}
	for _, event := range bytes.Split(body, []byte("\n\n")) {
		var data []byte
		for _, line := range bytes.Split(event, []byte("\n")) {
			line = bytes.TrimSuffix(line, []byte("\r"))
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			value := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			if len(data) != 0 {
				data = append(data, '\n')
			}
			data = append(data, value...)
		}
		if len(data) != 0 && validateJSONRPCResponse(data) == nil {
			return data, nil
		}
	}
	return nil, errors.New("malformed upstream MCP event stream")
}

func validateJSONRPCResponse(body []byte) error {
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

func (r *Router) call(ctx context.Context, profile config.ServerProfile, endpoint, method string, body []byte, contentType string, mcpHeaders http.Header, responseLimit int64) (upstreamResponse, error) {
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
	if mcpHeaders == nil {
		req.Header.Set("Accept", "application/json")
	} else {
		req.Header.Set("Accept", mcpAccept)
		for _, name := range []string{mcpProtocolHeader, mcpSessionHeader} {
			if value := mcpHeaders.Get(name); value != "" && len(value) <= 1024 {
				req.Header.Set(name, value)
			}
		}
	}
	req.Header.Set("Authorization", "Bearer "+credential.Token)
	resp, err := r.client.Do(req)
	if err != nil {
		return upstreamResponse{duration: r.now().Sub(started)}, err
	}
	defer resp.Body.Close()
	if responseLimit <= 0 || responseLimit > maxResponseBytes {
		responseLimit = maxResponseBytes
	}
	payload, err := readBounded(resp.Body, responseLimit)
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

// memoryToolRequiresSingleDestination fails closed for newly introduced or
// unrecognized Memory tools. An unknown tool may mutate state; treating it as a
// write prevents cross-purpose fan-out and retry onto a redundancy peer until
// its read-only semantics are explicitly reviewed.
func memoryToolRequiresSingleDestination(method, name string) bool {
	if method != "tools/call" {
		return false
	}
	switch name {
	case "memory_query", "memory_recent", "memory_read_page", "memory_status":
		return false
	default:
		return true
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

func hasResponseID(id json.RawMessage) bool {
	value := bytes.TrimSpace(id)
	return len(value) != 0 && !bytes.Equal(value, []byte("null"))
}

func hasMediaType(header, target string) bool {
	mediaType, _, err := mime.ParseMediaType(header)
	return err == nil && strings.EqualFold(mediaType, target)
}

func acceptsMediaType(header, target string) bool {
	if strings.TrimSpace(header) == "" {
		return true
	}
	targetType, targetSubtype, ok := strings.Cut(strings.ToLower(target), "/")
	if !ok {
		return false
	}
	for _, item := range strings.Split(header, ",") {
		mediaType, parameters, err := mime.ParseMediaType(strings.TrimSpace(item))
		if err != nil {
			continue
		}
		quality := 1.0
		if raw, exists := parameters["q"]; exists {
			quality, err = strconv.ParseFloat(raw, 64)
			if err != nil || quality <= 0 || quality > 1 {
				continue
			}
		}
		candidateType, candidateSubtype, ok := strings.Cut(strings.ToLower(mediaType), "/")
		if ok && (candidateType == "*" || candidateType == targetType) && (candidateSubtype == "*" || candidateSubtype == targetSubtype) {
			return true
		}
	}
	return false
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

func copyMCPResponse(w http.ResponseWriter, response upstreamResponse) {
	for _, name := range []string{"Content-Type", mcpSessionHeader} {
		if value := response.header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
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
	if errors.Is(err, context.DeadlineExceeded) {
		return "source request timed out"
	}
	var networkError *url.Error
	if errors.As(err, &networkError) {
		return "source transport unavailable"
	}
	value := err.Error()
	for _, allowed := range []string{
		"source circuit is open", "source endpoint is not exposed", "source endpoint is invalid",
		"source credential is unavailable", "upstream response exceeds limit",
		"upstream rejected MCP HTTP content negotiation (HTTP 406)",
		"upstream MCP response has invalid Content-Type", "upstream MCP response has unsupported Content-Type",
		"malformed upstream MCP event stream", "malformed upstream JSON-RPC response",
	} {
		if value == allowed {
			return allowed
		}
	}
	if strings.HasPrefix(value, "upstream HTTP ") {
		return value
	}
	return "source request failed"
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
