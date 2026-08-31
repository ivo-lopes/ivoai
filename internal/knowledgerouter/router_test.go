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
)

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
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, router.BaseURL()+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+router.Token())
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(response.Body)
	return payload, response.StatusCode
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
	if status != http.StatusOK || !bytes.Contains(payload, []byte(`"source_alias":"voicecorp"`)) || !bytes.Contains(payload, []byte(`"source_alias":"mindsite"`)) {
		t.Fatalf("status=%d payload=%s", status, payload)
	}
	if voice.wrongToken.Load() != 0 || mind.wrongToken.Load() != 0 {
		t.Fatalf("credential crossover voice=%d mind=%d", voice.wrongToken.Load(), mind.wrongToken.Load())
	}
	if bytes.Count(payload, []byte("projects/foo")) != 2 {
		t.Fatalf("same path from distinct sources was lost: %s", payload)
	}
	if bytes.Index(payload, []byte(`"source_alias":"mindsite"`)) > bytes.Index(payload, []byte(`"source_alias":"voicecorp"`)) {
		t.Fatalf("federated merge order is not deterministic: %s", payload)
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
			if status != http.StatusOK || !bytes.Contains(payload, []byte(`"partial":true`)) || !bytes.Contains(payload, []byte(`"source_alias":"mindsite"`)) {
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
	if targetRequests.Load() != 0 || !bytes.Contains(payload, []byte("cross-origin redirect refused")) {
		t.Fatalf("redirect was not fail-closed: target=%d payload=%s", targetRequests.Load(), payload)
	}
}
