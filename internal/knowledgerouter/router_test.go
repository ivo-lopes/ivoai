package knowledgerouter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/secrets"
	"github.com/ivo-lopes/ivoai/internal/serverpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

type fakeSource struct {
	server     *httptest.Server
	token      string
	purpose    string
	delay      time.Duration
	malformed  atomic.Bool
	oversized  atomic.Bool
	requests   atomic.Int32
	wrongToken atomic.Int32
	down       atomic.Bool
	mu         sync.Mutex
	bodies     [][]byte
	response   []byte
	sessionID  string
}

func newFakeSource(t *testing.T, purpose, token string) *fakeSource {
	t.Helper()
	value := &fakeSource{token: token, purpose: purpose}
	value.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		value.requests.Add(1)
		if request.Header.Get("Authorization") != "Bearer "+token {
			value.wrongToken.Add(1)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if value.down.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		if value.delay > 0 {
			select {
			case <-time.After(value.delay):
			case <-request.Context().Done():
				return
			}
		}
		body, _ := io.ReadAll(request.Body)
		value.mu.Lock()
		value.bodies = append(value.bodies, body)
		value.mu.Unlock()
		if strings.HasPrefix(request.URL.Path, "/hooks") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"hook":"` + purpose + `"}`))
			return
		}
		if value.malformed.Load() {
			_, _ = w.Write([]byte(`{"jsonrpc":`))
			return
		}
		if value.oversized.Load() {
			_, _ = w.Write(bytes.Repeat([]byte("x"), maxResponseBytes+1))
			return
		}
		if value.response != nil {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(value.response)
			return
		}
		var rpc struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &rpc)
		switch rpc.Method {
		case "initialize":
			if value.sessionID != "" {
				w.Header().Set(mcpSessionHeader, value.sessionID)
			}
			writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "result": map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "fixture", "version": "1"}}})
			return
		case "tools/list":
			name := "context_search"
			if strings.Contains(request.URL.Path, "memory") {
				name = "memory_read_page"
			}
			writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "result": map[string]any{"tools": []any{map[string]any{"name": name, "inputSchema": map[string]any{"type": "object"}}}}})
			return
		}
		writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": purpose + ":projects/foo"}}}})
	}))
	t.Cleanup(value.server.Close)
	return value
}

func TestSingleSourceAuthoritativeResponseIsByteExact(t *testing.T) {
	voice := newFakeSource(t, "voicecorp", "token-a")
	original := []byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"byte-exact\\nline-2\"}]}}")
	voice.response = original
	profiles := map[string]config.ServerProfile{"voicecorp": profile("voicecorp", "voicecorp", "", 0, voice)}
	router := startTestRouter(t, profiles, []string{"voicecorp"}, map[string]*fakeSource{"voicecorp": voice})
	payload, status := callRouter(t, router, "/mcp/context", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"context_search"}}`)
	if status != http.StatusOK || !bytes.Equal(payload, original) || sha256.Sum256(payload) != sha256.Sum256(original) {
		t.Fatalf("authoritative response changed: status=%d got=%q want=%q", status, payload, original)
	}
}

func profile(alias, purpose, group string, priority int, source *fakeSource) config.ServerProfile {
	return config.ServerProfile{
		ID: "srv_id_" + strings.ReplaceAll(alias, "-", "_"), Alias: alias, URL: source.server.URL, Purpose: purpose,
		RedundancyGroup: group, Priority: priority, Enabled: true, Status: "connected",
		ContextMCPURL: source.server.URL + "/context", MemoryMCPURL: source.server.URL + "/memory", MemoryHooksURL: source.server.URL + "/hooks",
	}
}

func startTestRouter(t *testing.T, profiles map[string]config.ServerProfile, selectors []string, sources map[string]*fakeSource) *Router {
	return startTestRouterWithOptions(t, profiles, selectors, sources, time.Second, nil)
}

func startTestRouterWithOptions(t *testing.T, profiles map[string]config.ServerProfile, selectors []string, sources map[string]*fakeSource, timeout time.Duration, observe func(Event)) *Router {
	t.Helper()
	pool, err := serverpool.New(profiles)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := pool.Resolve(selectors)
	if err != nil {
		t.Fatal(err)
	}
	credentials := map[string]secrets.ClientCredential{}
	for alias, profile := range profiles {
		credentials[profile.ID] = secrets.ClientCredential{Token: sources[alias].token}
	}
	router, err := Start(Options{Selection: selection, Credentials: credentials, Timeout: timeout, Observe: observe})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close(context.Background()) })
	return router
}

func callRouter(t *testing.T, router *Router, path, body string) ([]byte, int) {
	payload, status, _ := callRouterWithHeaders(t, router, path, body, nil)
	return payload, status
}

func callRouterWithHeaders(t *testing.T, router *Router, path, body string, headers http.Header) ([]byte, int, http.Header) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, router.BaseURL()+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+router.Token())
	request.Header.Set("Content-Type", "application/json")
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(response.Body)
	return payload, response.StatusCode, response.Header.Clone()
}

func federatedText(t *testing.T, payload []byte) []byte {
	t.Helper()
	var envelope struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil || len(envelope.Result.Content) != 1 || envelope.Result.Content[0].Type != "text" {
		t.Fatalf("invalid federated CallToolResult: err=%v payload=%s", err, payload)
	}
	return []byte(envelope.Result.Content[0].Text)
}

func TestRouterForwardsStreamableHTTPNegotiationHeaders(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if request.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
			return
		}
		accept := request.Header.Get("Accept")
		if !strings.Contains(accept, "application/json") || !strings.Contains(accept, "text/event-stream") {
			http.Error(w, "not acceptable", http.StatusNotAcceptable)
			return
		}
		if request.Header.Get(mcpProtocolHeader) != "2025-06-18" || request.Header.Get(mcpSessionHeader) != "session-fixture" {
			http.Error(w, "missing MCP session headers", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set(mcpSessionHeader, "upstream-session")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"fixture","version":"1"}}}`))
	}))
	t.Cleanup(upstream.Close)

	profile := config.ServerProfile{
		ID: "srv_negotiation", Alias: "negotiation", URL: upstream.URL, Purpose: "test",
		Enabled: true, Status: "connected", MemoryMCPURL: upstream.URL + "/memory",
	}
	pool, err := serverpool.New(map[string]config.ServerProfile{"negotiation": profile})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := pool.Resolve([]string{"negotiation"})
	if err != nil {
		t.Fatal(err)
	}
	router, err := Start(Options{
		Selection: selection,
		Credentials: map[string]secrets.ClientCredential{
			profile.ID: {Token: "fixture-token"},
		},
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close(context.Background()) })

	payload, status, responseHeaders := callRouterWithHeaders(t, router, "/mcp/memory", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}`, http.Header{
		mcpProtocolHeader: {"2025-06-18"},
		mcpSessionHeader:  {"session-fixture"},
	})
	if status != http.StatusOK || !bytes.Contains(payload, []byte(`"protocolVersion":"2025-06-18"`)) {
		t.Fatalf("streamable HTTP negotiation failed: status=%d payload=%s", status, payload)
	}
	if responseHeaders.Get(mcpSessionHeader) != "upstream-session" || responseHeaders.Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("MCP response headers were not preserved: %v", responseHeaders)
	}
	if requests.Load() != 1 {
		t.Fatalf("unexpected upstream request count: %d", requests.Load())
	}
}

func TestRouterValidatesClientAcceptMediaRanges(t *testing.T) {
	source := newFakeSource(t, "negotiation", "fixture-token")
	profiles := map[string]config.ServerProfile{"negotiation": profile("negotiation", "test", "", 0, source)}
	router := startTestRouter(t, profiles, []string{"negotiation"}, map[string]*fakeSource{"negotiation": source})
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`

	for _, test := range []struct {
		name   string
		accept string
		want   int
	}{
		{name: "canonical", accept: "application/json, text/event-stream", want: http.StatusOK},
		{name: "reverse order", accept: "text/event-stream, application/json", want: http.StatusOK},
		{name: "whitespace", accept: " application/json ,  text/event-stream ", want: http.StatusOK},
		{name: "quality values", accept: "application/json; q=0.8, text/event-stream; q=0.9", want: http.StatusOK},
		{name: "wildcard", accept: "*/*", want: http.StatusOK},
		{name: "unsupported", accept: "text/plain", want: http.StatusNotAcceptable},
		{name: "json only", accept: "application/json", want: http.StatusNotAcceptable},
		{name: "event stream disabled", accept: "application/json, text/event-stream; q=0", want: http.StatusNotAcceptable},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, status, _ := callRouterWithHeaders(t, router, "/mcp/memory", body, http.Header{"Accept": {test.accept}})
			if status != test.want {
				t.Fatalf("status=%d want=%d", status, test.want)
			}
		})
	}
}

func TestRPCResponseContentTypes(t *testing.T) {
	jsonBody := []byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)
	sseBody := append([]byte("event: message\ndata: "), append(jsonBody, []byte("\n\n")...)...)
	for _, test := range []struct {
		name        string
		contentType string
		body        []byte
		wantError   bool
	}{
		{name: "json", contentType: "application/json", body: jsonBody},
		{name: "json charset", contentType: "application/json; charset=utf-8", body: jsonBody},
		{name: "event stream", contentType: "text/event-stream", body: sseBody},
		{name: "event stream charset", contentType: "text/event-stream; charset=utf-8", body: sseBody},
		{name: "unsupported", contentType: "text/plain", body: jsonBody, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload, err := rpcResponsePayload(test.body, test.contentType)
			if (err != nil) != test.wantError {
				t.Fatalf("error=%v wantError=%t", err, test.wantError)
			}
			if !test.wantError && !bytes.Equal(payload, jsonBody) {
				t.Fatalf("payload=%q want=%q", payload, jsonBody)
			}
		})
	}
}

func TestRouterAcceptsEmptyNotificationResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(upstream.Close)
	profile := config.ServerProfile{ID: "srv_notification", Alias: "notification", URL: upstream.URL, Purpose: "test", Enabled: true, Status: "connected", MemoryMCPURL: upstream.URL + "/memory"}
	pool, err := serverpool.New(map[string]config.ServerProfile{"notification": profile})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := pool.Resolve([]string{"notification"})
	if err != nil {
		t.Fatal(err)
	}
	router, err := Start(Options{Selection: selection, Credentials: map[string]secrets.ClientCredential{profile.ID: {Token: "fixture-token"}}, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close(context.Background()) })
	_, status, _ := callRouterWithHeaders(t, router, "/mcp/memory", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, http.Header{"Accept": {mcpAccept}})
	if status != http.StatusAccepted {
		t.Fatalf("notification status=%d want=%d", status, http.StatusAccepted)
	}
}

func TestExplicitFederationPreservesSourceAndCredentialIsolation(t *testing.T) {
	voice := newFakeSource(t, "voicecorp", "token-a")
	mind := newFakeSource(t, "mindsite", "token-b")
	profiles := map[string]config.ServerProfile{
		"voicecorp": profile("voicecorp", "voicecorp", "", 0, voice),
		"mindsite":  profile("mindsite", "mindsite", "", 0, mind),
	}
	router := startTestRouter(t, profiles, []string{"voicecorp", "mindsite"}, map[string]*fakeSource{"voicecorp": voice, "mindsite": mind})
	payload, status := callRouter(t, router, "/mcp/context", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"context_search"}}`)
	federated := federatedText(t, payload)
	if status != http.StatusOK || !bytes.Contains(federated, []byte(`"source_alias":"voicecorp"`)) || !bytes.Contains(federated, []byte(`"source_alias":"mindsite"`)) {
		t.Fatalf("status=%d payload=%s", status, payload)
	}
	if voice.wrongToken.Load() != 0 || mind.wrongToken.Load() != 0 {
		t.Fatalf("credential crossover voice=%d mind=%d", voice.wrongToken.Load(), mind.wrongToken.Load())
	}
	if bytes.Count(federated, []byte("projects/foo")) != 2 {
		t.Fatalf("same path from distinct sources was lost: %s", federated)
	}
	if bytes.Index(federated, []byte(`"source_alias":"mindsite"`)) > bytes.Index(federated, []byte(`"source_alias":"voicecorp"`)) {
		t.Fatalf("federated merge order is not deterministic: %s", federated)
	}
}

func TestImplicitSelectionFederatesEveryEnabledSourceAndDegradesSafely(t *testing.T) {
	voice := newFakeSource(t, "voicecorp", "token-a")
	mind := newFakeSource(t, "mindsite", "token-b")
	research := newFakeSource(t, "research", "token-c")
	mind.down.Store(true)
	profiles := map[string]config.ServerProfile{
		"voicecorp": profile("voicecorp", "voicecorp", "", 0, voice),
		"mindsite":  profile("mindsite", "mindsite", "", 0, mind),
		"research":  profile("research", "research", "", 0, research),
	}
	router := startTestRouter(t, profiles, nil, map[string]*fakeSource{"voicecorp": voice, "mindsite": mind, "research": research})
	payload, status := callRouter(t, router, "/mcp/context", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"context_search"}}`)
	federated := federatedText(t, payload)
	if status != http.StatusOK || !bytes.Contains(federated, []byte(`"partial":true`)) {
		t.Fatalf("implicit federation did not preserve healthy sources: status=%d payload=%s", status, payload)
	}
	for _, alias := range []string{"mindsite", "research", "voicecorp"} {
		if !bytes.Contains(federated, []byte(`"source_alias":"`+alias+`"`)) {
			t.Fatalf("source provenance missing for %s: %s", alias, payload)
		}
	}
	if !bytes.Contains(federated, []byte("voicecorp:projects/foo")) || !bytes.Contains(federated, []byte("research:projects/foo")) {
		t.Fatalf("healthy implicit sources were lost: %s", payload)
	}
	if voice.wrongToken.Load() != 0 || mind.wrongToken.Load() != 0 || research.wrongToken.Load() != 0 {
		t.Fatalf("credential crossover: voice=%d mind=%d research=%d", voice.wrongToken.Load(), mind.wrongToken.Load(), research.wrongToken.Load())
	}
}

func TestFederatedToolDiscoveryDegradesToHealthySource(t *testing.T) {
	a := newFakeSource(t, "a", "ta")
	b := newFakeSource(t, "b", "tb")
	a.down.Store(true)
	profiles := map[string]config.ServerProfile{"a": profile("a", "a", "", 0, a), "b": profile("b", "b", "", 0, b)}
	router := startTestRouter(t, profiles, nil, map[string]*fakeSource{"a": a, "b": b})
	payload, status := callRouter(t, router, "/mcp/context", `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	if status != http.StatusOK || !bytes.Contains(payload, []byte("context_search")) || a.requests.Load() != 1 || b.requests.Load() != 1 {
		t.Fatalf("tool discovery did not degrade safely: status=%d requests=%d/%d payload=%s", status, a.requests.Load(), b.requests.Load(), payload)
	}
}

func TestImplicitFederationNeverBroadcastsMemoryWrites(t *testing.T) {
	a := newFakeSource(t, "a", "token-a")
	b := newFakeSource(t, "b", "token-b")
	profiles := map[string]config.ServerProfile{"a": profile("a", "a", "", 0, a), "b": profile("b", "b", "", 0, b)}
	router := startTestRouter(t, profiles, nil, map[string]*fakeSource{"a": a, "b": b})
	payload, _ := callRouter(t, router, "/mcp/memory", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_write_page"}}`)
	if !bytes.Contains(payload, []byte("exactly one knowledge destination")) || a.requests.Load() != 0 || b.requests.Load() != 0 {
		t.Fatalf("implicit federation broadcast a write: %s a=%d b=%d", payload, a.requests.Load(), b.requests.Load())
	}
}

func TestAmbiguousCrossPurposeWriteFailsBeforeUpstream(t *testing.T) {
	a := newFakeSource(t, "a", "token-a")
	b := newFakeSource(t, "b", "token-b")
	profiles := map[string]config.ServerProfile{"a": profile("a", "a", "", 0, a), "b": profile("b", "b", "", 0, b)}
	router := startTestRouter(t, profiles, []string{"a", "b"}, map[string]*fakeSource{"a": a, "b": b})
	payload, _ := callRouter(t, router, "/mcp/memory", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_write_page"}}`)
	if !bytes.Contains(payload, []byte("exactly one knowledge destination")) || a.requests.Load() != 0 || b.requests.Load() != 0 {
		t.Fatalf("write was not fail-closed: %s a=%d b=%d", payload, a.requests.Load(), b.requests.Load())
	}
}

func TestUnknownMemoryToolFailsClosedAsPotentialWrite(t *testing.T) {
	a := newFakeSource(t, "a", "token-a")
	b := newFakeSource(t, "b", "token-b")
	profiles := map[string]config.ServerProfile{"a": profile("a", "a", "", 0, a), "b": profile("b", "b", "", 0, b)}
	router := startTestRouter(t, profiles, []string{"a", "b"}, map[string]*fakeSource{"a": a, "b": b})
	payload, _ := callRouter(t, router, "/mcp/memory", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"future_memory_tool"}}`)
	if !bytes.Contains(payload, []byte("exactly one knowledge destination")) || a.requests.Load() != 0 || b.requests.Load() != 0 {
		t.Fatalf("unknown Memory tool fanned out: %s a=%d b=%d", payload, a.requests.Load(), b.requests.Load())
	}
}

func TestUnknownMemoryToolDoesNotFailOverAcrossReplicas(t *testing.T) {
	primary := newFakeSource(t, "mindsite-primary", "token-primary")
	secondary := newFakeSource(t, "mindsite-secondary", "token-secondary")
	primary.down.Store(true)
	profiles := map[string]config.ServerProfile{
		"mindsite-1": profile("mindsite-1", "mindsite", "prod", 10, primary),
		"mindsite-2": profile("mindsite-2", "mindsite", "prod", 20, secondary),
	}
	router := startTestRouter(t, profiles, []string{"mindsite"}, map[string]*fakeSource{"mindsite-1": primary, "mindsite-2": secondary})
	_, _ = callRouter(t, router, "/mcp/memory", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"future_memory_tool"}}`)
	if primary.requests.Load() != 1 || secondary.requests.Load() != 0 {
		t.Fatalf("unknown Memory tool retried after uncertain failure: primary=%d secondary=%d", primary.requests.Load(), secondary.requests.Load())
	}
}

func TestSamePurposeIndependentWriteIsAmbiguous(t *testing.T) {
	a := newFakeSource(t, "mindsite-a", "token-a")
	b := newFakeSource(t, "mindsite-b", "token-b")
	profiles := map[string]config.ServerProfile{"a": profile("a", "mindsite", "", 0, a), "b": profile("b", "mindsite", "", 0, b)}
	router := startTestRouter(t, profiles, []string{"a", "b"}, map[string]*fakeSource{"a": a, "b": b})
	payload, _ := callRouter(t, router, "/mcp/memory", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_write_page"}}`)
	if !bytes.Contains(payload, []byte("exactly one knowledge destination")) || a.requests.Load() != 0 || b.requests.Load() != 0 {
		t.Fatalf("same-purpose multi-destination write was not fail-closed: %s a=%d b=%d", payload, a.requests.Load(), b.requests.Load())
	}
}

func TestRedundancyReadFailoverIsPriorityOrdered(t *testing.T) {
	primary := newFakeSource(t, "mindsite-primary", "token-primary")
	secondary := newFakeSource(t, "mindsite-secondary", "token-secondary")
	primary.down.Store(true)
	profiles := map[string]config.ServerProfile{
		"mindsite-1": profile("mindsite-1", "mindsite", "prod", 10, primary),
		"mindsite-2": profile("mindsite-2", "mindsite", "prod", 20, secondary),
	}
	router := startTestRouter(t, profiles, []string{"mindsite"}, map[string]*fakeSource{"mindsite-1": primary, "mindsite-2": secondary})
	payload, _ := callRouter(t, router, "/mcp/context", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"context_search"}}`)
	if !bytes.Contains(payload, []byte("mindsite-secondary")) || primary.requests.Load() != 1 || secondary.requests.Load() != 1 {
		t.Fatalf("failover failed: %s primary=%d secondary=%d", payload, primary.requests.Load(), secondary.requests.Load())
	}
}

func TestRedundancyCircuitBreakerRecoversDeterministically(t *testing.T) {
	primary := newFakeSource(t, "mindsite-primary", "token-primary")
	secondary := newFakeSource(t, "mindsite-secondary", "token-secondary")
	primary.down.Store(true)
	profiles := map[string]config.ServerProfile{
		"mindsite-1": profile("mindsite-1", "mindsite", "prod", 10, primary),
		"mindsite-2": profile("mindsite-2", "mindsite", "prod", 20, secondary),
	}
	pool, err := serverpool.New(profiles)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := pool.Resolve([]string{"mindsite"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	router, err := Start(Options{Selection: selection, Credentials: map[string]secrets.ClientCredential{
		profiles["mindsite-1"].ID: {Token: primary.token}, profiles["mindsite-2"].ID: {Token: secondary.token},
	}, Timeout: time.Second, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close(context.Background()) })
	requestBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"context_search"}}`
	for range 2 {
		payload, _ := callRouter(t, router, "/mcp/context", requestBody)
		if !bytes.Contains(payload, []byte("mindsite-secondary")) {
			t.Fatalf("initial failover failed: %s", payload)
		}
	}
	primary.down.Store(false)
	_, _ = callRouter(t, router, "/mcp/context", requestBody)
	if primary.requests.Load() != 2 {
		t.Fatalf("open circuit retried primary: requests=%d", primary.requests.Load())
	}
	now = now.Add(31 * time.Second)
	payload, _ := callRouter(t, router, "/mcp/context", requestBody)
	if primary.requests.Load() != 3 || !bytes.Contains(payload, []byte("mindsite-primary")) {
		t.Fatalf("recovered primary was not restored: requests=%d payload=%s", primary.requests.Load(), payload)
	}
}

func TestConcurrentSessionsNeverCrossSources(t *testing.T) {
	voice := newFakeSource(t, "voicecorp", "token-a")
	mind := newFakeSource(t, "mindsite", "token-b")
	profiles := map[string]config.ServerProfile{"voicecorp": profile("voicecorp", "voicecorp", "", 0, voice), "mindsite": profile("mindsite", "mindsite", "", 0, mind)}
	sources := map[string]*fakeSource{"voicecorp": voice, "mindsite": mind}
	voiceRouter := startTestRouter(t, profiles, []string{"voicecorp"}, sources)
	mindRouter := startTestRouter(t, profiles, []string{"mindsite"}, sources)
	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			payload, _ := callRouter(t, voiceRouter, "/mcp/context", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"context_search"}}`)
			if bytes.Contains(payload, []byte("mindsite")) {
				t.Errorf("voicecorp session crossed source: %s", payload)
			}
		}()
		go func() {
			defer wg.Done()
			payload, _ := callRouter(t, mindRouter, "/mcp/context", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"context_search"}}`)
			if bytes.Contains(payload, []byte("voicecorp")) {
				t.Errorf("mindsite session crossed source: %s", payload)
			}
		}()
	}
	wg.Wait()
	if voice.requests.Load() != 25 || mind.requests.Load() != 25 || voice.wrongToken.Load() != 0 || mind.wrongToken.Load() != 0 {
		t.Fatalf("requests voice=%d mind=%d crossover=%d/%d", voice.requests.Load(), mind.requests.Load(), voice.wrongToken.Load(), mind.wrongToken.Load())
	}
}

func TestLocalCapabilityIsNotForwardedAndExpiresOnClose(t *testing.T) {
	source := newFakeSource(t, "voicecorp", "upstream-token")
	profiles := map[string]config.ServerProfile{"voicecorp": profile("voicecorp", "voicecorp", "", 0, source)}
	router := startTestRouter(t, profiles, []string{"voicecorp"}, map[string]*fakeSource{"voicecorp": source})
	localToken := router.Token()
	if localToken == "upstream-token" || len(localToken) < 32 {
		t.Fatal("session capability is not independent")
	}
	if err := router.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodPost, router.BaseURL()+"/mcp/context", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+localToken)
	if response, err := http.DefaultClient.Do(request); err == nil {
		response.Body.Close()
		t.Fatal("closed session router remained reachable")
	}
}

func TestFederatedResponseIsMachineReadable(t *testing.T) {
	a := newFakeSource(t, "a", "ta")
	b := newFakeSource(t, "b", "tb")
	profiles := map[string]config.ServerProfile{"a": profile("a", "a", "", 0, a), "b": profile("b", "b", "", 0, b)}
	router := startTestRouter(t, profiles, []string{"a", "b"}, map[string]*fakeSource{"a": a, "b": b})
	payload, _ := callRouter(t, router, "/mcp/context", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"context_search"}}`)
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("invalid response: %v: %s", err, payload)
	}
}

func TestFederatedResponseIsAConformantMCPToolResult(t *testing.T) {
	a := newFakeSource(t, "a", "ta")
	b := newFakeSource(t, "b", "tb")
	profiles := map[string]config.ServerProfile{"a": profile("a", "a", "", 0, a), "b": profile("b", "b", "", 0, b)}
	router := startTestRouter(t, profiles, []string{"a", "b"}, map[string]*fakeSource{"a": a, "b": b})
	authClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		clone := request.Clone(request.Context())
		clone.Header = request.Header.Clone()
		clone.Header.Set("Authorization", "Bearer "+router.Token())
		return http.DefaultTransport.RoundTrip(clone)
	})}
	client := mcp.NewClient(&mcp.Implementation{Name: "federation-conformance", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: router.BaseURL() + "/mcp/context", HTTPClient: authClient, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatalf("initialize through federated router: %v", err)
	}
	defer session.Close()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "context_search", Arguments: map[string]any{"query": "fixture"}})
	if err != nil {
		t.Fatalf("federated tools/call is not a valid MCP CallToolResult: %v", err)
	}
	if len(result.Content) != 1 || result.StructuredContent != nil {
		t.Fatalf("federated result must use schema-compatible text content: %#v", result)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(text.Text, `"source_alias":"a"`) || !strings.Contains(text.Text, `"source_alias":"b"`) {
		t.Fatalf("federated result lost source provenance: %#v", result.Content)
	}
}

func TestFederatedInitializeReachesEveryStatelessSource(t *testing.T) {
	a := newFakeSource(t, "a", "ta")
	b := newFakeSource(t, "b", "tb")
	profiles := map[string]config.ServerProfile{"a": profile("a", "a", "", 0, a), "b": profile("b", "b", "", 0, b)}
	router := startTestRouter(t, profiles, []string{"a", "b"}, map[string]*fakeSource{"a": a, "b": b})
	payload, status, headers := callRouterWithHeaders(t, router, "/mcp/context", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}`, http.Header{"Accept": {mcpAccept}})
	if status != http.StatusOK || !bytes.Contains(payload, []byte(`"protocolVersion":"2025-06-18"`)) || headers.Get(mcpSessionHeader) != "" || a.requests.Load() != 1 || b.requests.Load() != 1 {
		t.Fatalf("federated initialize drifted: status=%d headers=%v requests=%d/%d payload=%s", status, headers, a.requests.Load(), b.requests.Load(), payload)
	}
}

func TestFederationRejectsStatefulUpstreamSessionsWithoutCrossover(t *testing.T) {
	a := newFakeSource(t, "a", "ta")
	b := newFakeSource(t, "b", "tb")
	a.sessionID = "session-a"
	b.sessionID = "session-b"
	profiles := map[string]config.ServerProfile{"a": profile("a", "a", "", 0, a), "b": profile("b", "b", "", 0, b)}
	router := startTestRouter(t, profiles, []string{"a", "b"}, map[string]*fakeSource{"a": a, "b": b})
	payload, status, _ := callRouterWithHeaders(t, router, "/mcp/memory", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}`, http.Header{"Accept": {mcpAccept}})
	if status != http.StatusOK || !bytes.Contains(payload, []byte("stateful upstream MCP sessions are not supported for federation")) || a.requests.Load() != 1 || b.requests.Load() != 1 {
		t.Fatalf("stateful federation was not fail-closed: status=%d requests=%d/%d payload=%s", status, a.requests.Load(), b.requests.Load(), payload)
	}
	beforeA, beforeB := a.requests.Load(), b.requests.Load()
	payload, _, _ = callRouterWithHeaders(t, router, "/mcp/memory", `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`, http.Header{"Accept": {mcpAccept}, mcpSessionHeader: {"session-a"}})
	if !bytes.Contains(payload, []byte("stateful MCP sessions are not supported across federated sources")) || a.requests.Load() != beforeA || b.requests.Load() != beforeB {
		t.Fatalf("client session identifier crossed sources: requests=%d/%d payload=%s", a.requests.Load(), b.requests.Load(), payload)
	}
}

func TestFederatedResponseBudgetIsGlobal(t *testing.T) {
	a := newFakeSource(t, "a", "ta")
	b := newFakeSource(t, "b", "tb")
	a.response = append([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"`), bytes.Repeat([]byte("x"), maxResponseBytes/2+1)...)
	a.response = append(a.response, []byte(`"}]}}`)...)
	profiles := map[string]config.ServerProfile{"a": profile("a", "a", "", 0, a), "b": profile("b", "b", "", 0, b)}
	router := startTestRouter(t, profiles, []string{"a", "b"}, map[string]*fakeSource{"a": a, "b": b})
	payload, status := callRouter(t, router, "/mcp/context", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"context_search"}}`)
	text := federatedText(t, payload)
	if status != http.StatusOK || len(payload) > maxResponseBytes || !bytes.Contains(text, []byte("upstream response exceeds limit")) {
		t.Fatalf("federated result budget failed: status=%d bytes=%d text=%s", status, len(payload), text)
	}
}

func TestVoicehubPromptMemoryAndContextRemainConformantAcrossFederation(t *testing.T) {
	const prompt = "Consulte o contexto e memória e comente um pouco sobre o projeto Voicehub."
	a := newFakeSource(t, "company-a", "token-a")
	b := newFakeSource(t, "company-b", "token-b")
	profiles := map[string]config.ServerProfile{"company-a": profile("company-a", "company-a", "", 0, a), "company-b": profile("company-b", "company-b", "", 0, b)}
	router := startTestRouter(t, profiles, []string{"company-a", "company-b"}, map[string]*fakeSource{"company-a": a, "company-b": b})
	authClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		clone := request.Clone(request.Context())
		clone.Header = request.Header.Clone()
		clone.Header.Set("Authorization", "Bearer "+router.Token())
		return http.DefaultTransport.RoundTrip(clone)
	})}

	for _, test := range []struct {
		path, tool, argument string
	}{
		{path: "/mcp/memory", tool: "memory_read_page", argument: "Voicehub"},
		{path: "/mcp/context", tool: "context_search", argument: prompt},
	} {
		client := mcp.NewClient(&mcp.Implementation{Name: "voicehub-regression", Version: "1"}, nil)
		session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: router.BaseURL() + test.path, HTTPClient: authClient, DisableStandaloneSSE: true}, nil)
		if err != nil {
			t.Fatalf("%s initialize failed: %v", test.tool, err)
		}
		listed, err := session.ListTools(context.Background(), nil)
		if err != nil || len(listed.Tools) != 1 || listed.Tools[0].Name != test.tool {
			_ = session.Close()
			t.Fatalf("%s discovery failed: result=%#v err=%v", test.tool, listed, err)
		}
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: test.tool, Arguments: map[string]any{"query": test.argument}})
		_ = session.Close()
		if err != nil || len(result.Content) != 1 {
			t.Fatalf("%s returned an incompatible CallToolResult: result=%#v err=%v", test.tool, result, err)
		}
		text, ok := result.Content[0].(*mcp.TextContent)
		if !ok || !strings.Contains(text.Text, `"source_alias":"company-a"`) || !strings.Contains(text.Text, `"source_alias":"company-b"`) {
			t.Fatalf("%s lost federated provenance: %#v", test.tool, result.Content)
		}
	}
	if a.wrongToken.Load() != 0 || b.wrongToken.Load() != 0 {
		t.Fatal("credential crossover in the Voicehub Memory/Context regression")
	}
}

func TestFederationMakesPartialFailuresVisible(t *testing.T) {
	for _, test := range []struct {
		name      string
		configure func(*fakeSource)
		timeout   time.Duration
	}{
		{name: "http failure", configure: func(source *fakeSource) { source.down.Store(true) }, timeout: time.Second},
		{name: "malformed JSON-RPC", configure: func(source *fakeSource) { source.malformed.Store(true) }, timeout: time.Second},
		{name: "timeout", configure: func(source *fakeSource) { source.delay = 100 * time.Millisecond }, timeout: 10 * time.Millisecond},
		{name: "oversized response", configure: func(source *fakeSource) { source.oversized.Store(true) }, timeout: time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := newFakeSource(t, "voicecorp", "token-a")
			b := newFakeSource(t, "mindsite", "token-b")
			test.configure(b)
			profiles := map[string]config.ServerProfile{"voicecorp": profile("voicecorp", "voicecorp", "", 0, a), "mindsite": profile("mindsite", "mindsite", "", 0, b)}
			sources := map[string]*fakeSource{"voicecorp": a, "mindsite": b}
			events := []Event{}
			router := startTestRouterWithOptions(t, profiles, []string{"voicecorp", "mindsite"}, sources, test.timeout, func(event Event) { events = append(events, event) })
			payload, status := callRouter(t, router, "/mcp/context", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"context_search"}}`)
			federated := federatedText(t, payload)
			if status != http.StatusOK || !bytes.Contains(federated, []byte(`"partial":true`)) || !bytes.Contains(federated, []byte(`"source_alias":"mindsite"`)) {
				t.Fatalf("partial failure was not attributed: status=%d payload=%s", status, payload)
			}
			if len(events) != 2 || events[1].SourceAlias != "voicecorp" && events[1].SourceAlias != "mindsite" {
				t.Fatalf("unexpected bounded events: %+v", events)
			}
			failed := false
			for _, event := range events {
				if event.SourceAlias == "mindsite" && event.State == "failed" {
					failed = true
				}
			}
			if !failed {
				t.Fatalf("failed source was not observable: %+v", events)
			}
			if a.wrongToken.Load() != 0 || b.wrongToken.Load() != 0 {
				t.Fatal("credential crossover during partial failure")
			}
		})
	}
}

func TestPurposeWritesReachOnlySelectedServer(t *testing.T) {
	voice := newFakeSource(t, "voicecorp", "token-a")
	mind := newFakeSource(t, "mindsite", "token-b")
	profiles := map[string]config.ServerProfile{"voicecorp": profile("voicecorp", "voicecorp", "", 0, voice), "mindsite": profile("mindsite", "mindsite", "", 0, mind)}
	sources := map[string]*fakeSource{"voicecorp": voice, "mindsite": mind}
	router := startTestRouter(t, profiles, []string{"mindsite"}, sources)
	payload, status := callRouter(t, router, "/mcp/memory", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_write_page"}}`)
	if status != http.StatusOK || !bytes.Contains(payload, []byte("mindsite")) || voice.requests.Load() != 0 || mind.requests.Load() != 1 {
		t.Fatalf("write routing crossed purpose: status=%d payload=%s voice=%d mind=%d", status, payload, voice.requests.Load(), mind.requests.Load())
	}
}

func TestRedundancyWriteDoesNotRetryAfterUncertainPrimaryFailure(t *testing.T) {
	primary := newFakeSource(t, "mindsite-primary", "token-primary")
	secondary := newFakeSource(t, "mindsite-secondary", "token-secondary")
	primary.down.Store(true)
	profiles := map[string]config.ServerProfile{
		"mindsite-1": profile("mindsite-1", "mindsite", "prod", 10, primary),
		"mindsite-2": profile("mindsite-2", "mindsite", "prod", 20, secondary),
	}
	router := startTestRouter(t, profiles, []string{"mindsite"}, map[string]*fakeSource{"mindsite-1": primary, "mindsite-2": secondary})
	payload, _ := callRouter(t, router, "/mcp/memory", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_write_page"}}`)
	if !bytes.Contains(payload, []byte("upstream HTTP 503")) || primary.requests.Load() != 1 || secondary.requests.Load() != 0 {
		t.Fatalf("uncertain write was retried or hidden: %s primary=%d secondary=%d", payload, primary.requests.Load(), secondary.requests.Load())
	}
}

func TestMemoryHookUsesSessionSourceAndLocalCapability(t *testing.T) {
	voice := newFakeSource(t, "voicecorp", "token-a")
	mind := newFakeSource(t, "mindsite", "token-b")
	profiles := map[string]config.ServerProfile{"voicecorp": profile("voicecorp", "voicecorp", "", 0, voice), "mindsite": profile("mindsite", "mindsite", "", 0, mind)}
	router := startTestRouter(t, profiles, []string{"voicecorp"}, map[string]*fakeSource{"voicecorp": voice, "mindsite": mind})
	payload, status := callRouter(t, router, "/memory/checkpoint", `{"metadata":"bounded"}`)
	if status != http.StatusOK || !bytes.Contains(payload, []byte(`"hook":"voicecorp"`)) || mind.requests.Load() != 0 || voice.wrongToken.Load() != 0 {
		t.Fatalf("memory hook source isolation failed: status=%d payload=%s voice=%d mind=%d", status, payload, voice.requests.Load(), mind.requests.Load())
	}
}

func TestRouterRefusesCrossOriginRedirectBeforeCredentialCrossover(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		targetRequests.Add(1)
		if request.Header.Get("Authorization") == "Bearer token-a" {
			t.Error("token A reached redirect target B")
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, target.URL+"/context", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	profile := config.ServerProfile{ID: "srv_redirect", Alias: "voicecorp", URL: redirect.URL, Status: "connected", Enabled: true, Purpose: "voicecorp", ContextMCPURL: redirect.URL + "/context"}
	pool, err := serverpool.New(map[string]config.ServerProfile{"voicecorp": profile})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := pool.Resolve([]string{"voicecorp"})
	if err != nil {
		t.Fatal(err)
	}
	router, err := Start(Options{Selection: selection, Credentials: map[string]secrets.ClientCredential{"srv_redirect": {Token: "token-a"}}, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close(context.Background()) })
	payload, _ := callRouter(t, router, "/mcp/context", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"context_search"}}`)
	if targetRequests.Load() != 0 || !bytes.Contains(payload, []byte("source transport unavailable")) || bytes.Contains(payload, []byte(redirect.URL)) {
		t.Fatalf("redirect was not fail-closed: target=%d payload=%s", targetRequests.Load(), payload)
	}
}
