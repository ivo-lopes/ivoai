package knowledgerouter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

func memoryReadAuthority() MemoryPageAuthority {
	return MemoryPageAuthority{ScopeKey: strings.Repeat("a", 64), Authorize: func(context.Context) error { return nil }}
}

func memoryReaderRouter(t *testing.T, handler http.Handler) (*Router, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer fixture-memory-only" {
			t.Error("wrong fixture source credential")
			http.Error(w, "unauthorized", 401)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	p := config.ServerProfile{ID: "srv_memory_fixture", Alias: "fixture", Purpose: "fixture", Enabled: true, Status: "connected", URL: server.URL, MemoryMCPURL: server.URL + "/memory"}
	pool, err := serverpool.New(map[string]config.ServerProfile{"fixture": p})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := pool.Resolve([]string{"fixture"})
	if err != nil {
		t.Fatal(err)
	}
	router, err := Start(Options{Selection: selection, Credentials: map[string]secrets.ClientCredential{p.ID: {Token: "fixture-memory-only"}}, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close(context.Background()) })
	return router, calls
}

func TestMemoryPageReaderActualSDKHTTPIsFreshScopedAndImmutable(t *testing.T) {
	for _, jsonResponse := range []bool{true, false} {
		t.Run(fmt.Sprint(jsonResponse), func(t *testing.T) {
			server := mcp.NewServer(&mcp.Implementation{Name: "labelled-memory-snapshot-fixture", Version: "1"}, nil)
			server.AddTool(&mcp.Tool{Name: "memory_read_page", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}, "required": []string{"query"}, "additionalProperties": false}}, func(_ context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				if string(r.Params.Arguments) != `{"query":"private essential terms"}` {
					t.Error("exact arguments changed or authority entered RPC", string(r.Params.Arguments))
				}
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "approved fixture page; untrusted data"}}}, nil
			})
			handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: jsonResponse})
			router, calls := memoryReaderRouter(t, handler)
			var events []Event
			router.observe = func(event Event) { events = append(events, event) }
			read := func() MemoryPageSnapshot {
				s, err := router.ReadMemoryPageExact(context.Background(), MemoryPageArguments{Query: "private essential terms"}, memoryReadAuthority())
				if err != nil {
					t.Fatal(err)
				}
				return s
			}
			first, second := read(), read()
			if calls.Load() != 2 || first.Metadata().ID == second.Metadata().ID || first.Metadata().ArgumentsSHA256 != second.Metadata().ArgumentsSHA256 || first.Metadata().ResultSHA256 != second.Metadata().ResultSHA256 {
				t.Fatal("LIVE read was cached or snapshot identity was reused")
			}
			meta := first.Metadata()
			sum := sha256.Sum256(first.Bytes())
			if first.Validate() != nil || meta.SHA256 != hex.EncodeToString(sum[:]) || meta.Source.ID != "srv_memory_fixture" || meta.Source.Alias != "fixture" || !meta.ResponseComplete || meta.ScopeKey != memoryReadAuthority().ScopeKey {
				t.Fatal("invalid actual source/framing provenance")
			}
			body := first.Bytes()
			body[0] = 'x'
			if first.Validate() != nil || bytes.Equal(body, first.Bytes()) {
				t.Fatal("caller changed immutable bytes")
			}
			printed := fmt.Sprintf("%v %#v %v", first, first, MemoryPageArguments{Query: "private essential terms"})
			eventBytes, _ := json.Marshal(events)
			if strings.Contains(printed, "approved fixture") || strings.Contains(printed, "private essential terms") || bytes.Contains(eventBytes, []byte("private essential terms")) || bytes.Contains(eventBytes, []byte("fixture-memory-only")) {
				t.Fatal("body/query/credential leaked into diagnostics")
			}
		})
	}
}

func TestMemoryPageArgumentsRejectUnknownAmbiguousOrUnsafeInputBeforeHTTP(t *testing.T) {
	router, calls := memoryReaderRouter(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("invalid input reached HTTP") }))
	for _, body := range []string{`{}`, `{"query":"x","path":"a"}`, `{"query":""}`, `{"query":"x","scope":"global"}`, `{"query":"x","query":"y"}`, `{"query":"x","\u0071uery":"y"}`, `{"path":"../private"}`, `{"path":"/private"}`, `{"path":"a//b"}`, `{"path":"file:private"}`, `{"query":1}`, `{"name":"memory_write_page","query":"x"}`} {
		var args MemoryPageArguments
		if json.Unmarshal([]byte(body), &args) == nil {
			t.Fatal("unsupported arguments decoded", body)
		}
	}
	for _, args := range []MemoryPageArguments{{}, {Query: "x", Path: "a"}, {Query: strings.Repeat("x", 513)}, {Query: "\x00"}, {Path: "../a"}} {
		if _, err := router.ReadMemoryPageExact(context.Background(), args, memoryReadAuthority()); !errors.Is(err, ErrMemoryPageArguments) {
			t.Fatal("invalid input admitted", err)
		}
	}
	for _, authority := range []MemoryPageAuthority{{}, {ScopeKey: "bad", Authorize: func(context.Context) error { return nil }}, {ScopeKey: strings.Repeat("a", 64), Authorize: func(context.Context) error { return errors.New("private policy detail") }}} {
		if _, err := router.ReadMemoryPageExact(context.Background(), MemoryPageArguments{Query: "x"}, authority); !errors.Is(err, ErrMemoryPageDenied) {
			t.Fatal("missing host authority admitted", err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("denied request reached source")
	}
}

func TestMemoryPageStrictResponseRejectsAmbiguityErrorsAndIncompleteFrames(t *testing.T) {
	for _, mode := range []string{"wrong_id", "null_id", "missing_id", "result_and_error", "error_only", "duplicate_id", "duplicate_result", "trailing", "batch", "is_error", "is_error_string", "partial", "truncated", "bad_content", "session", "partial_http", "duplicate_content_type", "sse_two", "sse_invalid_then_valid", "sse_no_terminal", "sse_notification", "sse_retry", "deep", "nodes", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			router, _ := memoryReaderRouter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					ID string `json:"id"`
				}
				_ = json.NewDecoder(r.Body).Decode(&request)
				id, _ := json.Marshal(request.ID)
				result := `{"content":[{"type":"text","text":"fixture"}]}`
				body := `{"jsonrpc":"2.0","id":` + string(id) + `,"result":` + result + `}`
				w.Header().Set("Content-Type", "application/json")
				switch mode {
				case "wrong_id":
					body = strings.Replace(body, string(id), `"other"`, 1)
				case "null_id":
					body = strings.Replace(body, string(id), `null`, 1)
				case "missing_id":
					body = `{"jsonrpc":"2.0","result":` + result + `}`
				case "result_and_error":
					body = strings.TrimSuffix(body, "}") + `,"error":null}`
				case "error_only":
					body = `{"jsonrpc":"2.0","id":` + string(id) + `,"error":{"code":-1,"message":"private source detail"}}`
				case "duplicate_id":
					body = strings.TrimSuffix(body, "}") + `,"id":` + string(id) + `}`
				case "duplicate_result":
					body = strings.TrimSuffix(body, "}") + `,"result":` + result + `}`
				case "trailing":
					body += "{}"
				case "batch":
					body = "[" + body + "]"
				case "is_error":
					body = strings.Replace(body, result, strings.TrimSuffix(result, "}")+`,"isError":true}`, 1)
				case "is_error_string":
					body = strings.Replace(body, result, strings.TrimSuffix(result, "}")+`,"isError":"false"}`, 1)
				case "partial":
					body = strings.Replace(body, result, strings.TrimSuffix(result, "}")+`,"structuredContent":{"partial":true}}`, 1)
				case "truncated":
					body = strings.Replace(body, result, strings.TrimSuffix(result, "}")+`,"_meta":{"truncated":true}}`, 1)
				case "bad_content":
					body = strings.Replace(body, `"type":"text"`, `"type":"resource"`, 1)
				case "session":
					w.Header().Set(mcpSessionHeader, "fixture-session")
				case "partial_http":
					w.WriteHeader(http.StatusPartialContent)
				case "duplicate_content_type":
					w.Header().Add("Content-Type", "application/json")
				case "sse_two", "sse_invalid_then_valid", "sse_no_terminal", "sse_notification", "sse_retry":
					w.Header().Set("Content-Type", "text/event-stream")
					event := "event: message\ndata: " + body + "\n\n"
					switch mode {
					case "sse_two":
						body = event + event
					case "sse_invalid_then_valid":
						body = "data: invalid\n\n" + event
					case "sse_no_terminal":
						body = strings.TrimSuffix(event, "\n")
					case "sse_notification":
						body = `data: {"jsonrpc":"2.0","method":"notifications/progress"}` + "\n\n" + event
					case "sse_retry":
						body = "retry: 1\n\n" + event
					}
				case "deep":
					body = strings.Replace(body, `"fixture"`, strings.Repeat("[", 34)+`"fixture"`+strings.Repeat("]", 34), 1)
				case "nodes":
					body = strings.Replace(body, `"fixture"`, `[`+strings.Repeat(`0,`, 8192)+`0]`, 1)
				case "oversize":
					body = strings.Repeat("x", maxMemoryPageBytes+1)
				}
				_, _ = w.Write([]byte(body))
			}))
			if snapshot, err := router.ReadMemoryPageExact(context.Background(), MemoryPageArguments{Query: "private request sentinel"}, memoryReadAuthority()); err == nil || len(snapshot.Bytes()) != 0 || strings.Contains(err.Error(), "private") {
				t.Fatal("invalid response admitted or leaked", err)
			}
		})
	}
}

func TestMemoryPageStrictValidationPrecedesCircuitSuccessAndLegacyIsUnchanged(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":"wrong","result":{"content":[{"type":"text","text":"fixture"}]},"error":null}`
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	router, _ := memoryReaderRouter(t, handler)
	router.failures["srv_memory_fixture"] = failureState{Count: 1}
	if _, err := router.ReadMemoryPageExact(context.Background(), MemoryPageArguments{Query: "x"}, memoryReadAuthority()); !errors.Is(err, ErrMemoryPageResponse) {
		t.Fatal(err)
	}
	if state := router.failures["srv_memory_fixture"]; state.Count != 2 || !state.OpenUntil.After(router.now()) {
		t.Fatal("invalid strict response reset source health", state)
	}
	legacy, _ := memoryReaderRouter(t, handler)
	response, status := callRouter(t, legacy, "/mcp/memory", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_read_page","arguments":{"query":"x"}}}`)
	if status != 200 || string(response) != body {
		t.Fatal("default legacy validator/wire changed")
	}
}

func TestMemoryPageScopeRevocationCloseAndSourceSelectionFailClosed(t *testing.T) {
	allowed := &atomic.Bool{}
	allowed.Store(true)
	router, calls := memoryReaderRouter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		allowed.Store(false)
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "fixture"}}}})
	}))
	authority := memoryReadAuthority()
	authority.Authorize = func(context.Context) error {
		if !allowed.Load() {
			return errors.New("revoked")
		}
		return nil
	}
	if s, err := router.ReadMemoryPageExact(context.Background(), MemoryPageArguments{Path: "project/page"}, authority); !errors.Is(err, ErrMemoryPageDenied) || len(s.Bytes()) != 0 || calls.Load() != 1 {
		t.Fatal("revocation during I/O admitted snapshot", err)
	}
	allowed.Store(true)
	_ = router.Close(context.Background())
	if _, err := router.ReadMemoryPageExact(context.Background(), MemoryPageArguments{Query: "x"}, authority); !errors.Is(err, ErrMemoryPageDenied) || calls.Load() != 1 {
		t.Fatal("closed Router reused upstream authority", err)
	}
	for _, multipleProfiles := range []bool{true, false} {
		other, calls := memoryReaderRouter(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("ambiguous selection reached source") }))
		group := other.selection.Groups[0]
		if multipleProfiles {
			other.selection.Groups[0].Profiles = append(group.Profiles, group.Profiles[0])
		} else {
			other.selection.Groups = append(other.selection.Groups, group)
		}
		if _, err := other.ReadMemoryPageExact(context.Background(), MemoryPageArguments{Query: "x"}, memoryReadAuthority()); !errors.Is(err, ErrMemoryPageSource) || calls.Load() != 0 {
			t.Fatal("federation/failover admitted", err)
		}
	}
}

func TestMemoryPageReaderRefusesSameOriginRedirectBeforeForwardingCredential(t *testing.T) {
	var targetCalls atomic.Int32
	router, _ := memoryReaderRouter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/other" {
			targetCalls.Add(1)
			t.Error("redirect followed")
			return
		}
		http.Redirect(w, r, "/other", http.StatusTemporaryRedirect)
	}))
	if _, err := router.ReadMemoryPageExact(context.Background(), MemoryPageArguments{Query: "x"}, memoryReadAuthority()); err == nil || targetCalls.Load() != 0 {
		t.Fatal("redirect changed source identity", err)
	}
}

func TestMemoryPageReaderBoundsParallelLiveCallsWithoutDeduplication(t *testing.T) {
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	router, calls := memoryReaderRouter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		var request map[string]any
		_ = json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "fixture"}}}})
	}))
	router.timeout = 5 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := make(chan error, 8)
	for range 8 {
		go func() {
			_, err := router.ReadMemoryPageExact(ctx, MemoryPageArguments{Query: "same arguments"}, memoryReadAuthority())
			results <- err
		}()
	}
	for range 8 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("parallel reads did not start")
		}
	}
	if _, err := router.ReadMemoryPageExact(ctx, MemoryPageArguments{Query: "same arguments"}, memoryReadAuthority()); !errors.Is(err, ErrMemoryPageBusy) {
		t.Fatal("capacity was unbounded", err)
	}
	unblock()
	for range 8 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 8 {
		t.Fatal("LIVE reads were deduplicated or overflow reached source", calls.Load())
	}
}
