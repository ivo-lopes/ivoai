// Package externalmcp keeps upstream credentials in the IVOAI process and
// mediates tool approval independently of a headless executor's approval mode.
package externalmcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Target struct {
	Name, URL string
	Headers   http.Header
	// Restricted is used for task-local worker projections. Empty AllowedTools
	// denies every call; nil is not a wildcard. Primary/legacy targets retain
	// their existing transport and interactive approval contract.
	Restricted   bool
	AllowedTools []string
	// ApprovalTools must be confirmed even in full mode. Only IVOAI may mark
	// an exact task/tool as already approved through SetToolAdmission.
	ApprovalTools []string
}
type Permission struct{ ID, Description string }
type pending struct {
	view  Permission
	reply chan bool
}
type Gateway struct {
	url, token    string
	interactive   atomic.Bool
	server        *http.Server
	transport     *http.Transport
	mu            sync.Mutex
	pending       map[string]pending
	cancel        context.CancelFunc
	admission     func() bool
	toolAdmission func(server, tool string) (allowed, approved bool)
}

func (g *Gateway) SetToolAdmission(check func(string, string) (bool, bool)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.toolAdmission = check
}

// SetAdmission installs an additional control-plane gate, independent from
// Full/Interactive tool approvals. It is never configured by an MCP client.
func (g *Gateway) SetAdmission(check func() bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.admission = check
}

func Start(targets []Target, mode string) (*Gateway, error) {
	if mode != "full" && mode != "interactive" {
		return nil, errors.New("invalid external MCP permission mode")
	}
	token, err := randomID()
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	g := &Gateway{url: "http://" + listener.Addr().String(), token: token, pending: map[string]pending{}, cancel: cancel, transport: transport}
	g.interactive.Store(mode == "interactive")
	mux := http.NewServeMux()
	for index, target := range targets {
		if !safeName(target.Name) {
			listener.Close()
			cancel()
			return nil, errors.New("unsafe MCP name")
		}
		allowedTools := make(map[string]bool, len(target.AllowedTools))
		approvalTools := map[string]bool{}
		for _, name := range target.ApprovalTools {
			approvalTools[name] = true
		}
		if len(target.AllowedTools) > MaxInventoryTools {
			listener.Close()
			cancel()
			return nil, errors.New("worker MCP tool scope exceeds limit")
		}
		for _, name := range target.AllowedTools {
			if !safeName(name) {
				listener.Close()
				cancel()
				return nil, errors.New("invalid worker MCP tool scope")
			}
			allowedTools[name] = true
		}
		endpoint, err := url.Parse(target.URL)
		if err != nil || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Host == "" || (endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && (endpoint.Hostname() == "127.0.0.1" || endpoint.Hostname() == "localhost"))) {
			listener.Close()
			cancel()
			return nil, errors.New("invalid external MCP endpoint")
		}
		headers := target.Headers.Clone()
		proxy := &httputil.ReverseProxy{
			Rewrite: func(request *httputil.ProxyRequest) {
				request.Out.URL = cloneURL(endpoint)
				request.Out.Host = endpoint.Host
				// Never forward an executor credential, cookies or hop-by-hop metadata.
				request.Out.Header = http.Header{}
				for _, name := range []string{"Content-Type", "Accept", "Mcp-Session-Id", "Mcp-Protocol-Version", "Last-Event-ID"} {
					if value := request.In.Header.Get(name); value != "" {
						request.Out.Header.Set(name, value)
					}
				}
				for name, values := range headers {
					request.Out.Header[name] = append([]string(nil), values...)
				}
			}, Transport: transport, FlushInterval: -1, ErrorLog: log.New(io.Discard, "", 0),
			ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
				http.Error(w, "external MCP transport failed", http.StatusBadGateway)
			},
			ModifyResponse: func(response *http.Response) error {
				if response.StatusCode >= 300 && response.StatusCode < 400 {
					response.Body.Close()
					return errors.New("external MCP redirect refused")
				}
				response.Header.Del("Set-Cookie")
				if target.Restricted && response.Request.Context().Value(inventoryRequestKey{}) == true && response.StatusCode == http.StatusOK {
					return filterInventory(response, allowedTools)
				}
				response.Body = &boundedBody{ReadCloser: response.Body, remaining: 16 << 20}
				return nil
			},
		}
		path := g.Path(index)
		mux.Handle(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+g.token)) != 1 {
				http.Error(w, "unauthorized", 401)
				return
			}
			if r.URL.RawQuery != "" || r.Header.Get("Origin") != "" {
				http.Error(w, "invalid MCP origin or query", 403)
				return
			}
			requestCtx, done := context.WithTimeout(r.Context(), 3*time.Minute)
			defer done()
			r = r.WithContext(requestCtx)
			if r.Method == http.MethodPost {
				body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
				r.Body.Close()
				if err != nil {
					http.Error(w, "MCP request too large", 413)
					return
				}
				var message struct {
					JSONRPC string          `json:"jsonrpc"`
					ID      json.RawMessage `json:"id"`
					Method  string          `json:"method"`
					Params  struct {
						Name string `json:"name"`
					} `json:"params"`
				}
				if json.Unmarshal(body, &message) != nil || message.JSONRPC != "2.0" {
					http.Error(w, "invalid MCP request", 400)
					return
				}
				switch message.Method {
				case "tools/call":
					if len(message.ID) == 0 || !safeName(message.Params.Name) {
						http.Error(w, "invalid MCP tool call", 400)
						return
					}
					if target.Restricted && !allowedTools[message.Params.Name] {
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": "MCP_DENIED: tool is outside this worker's approved capability scope"}}}})
						return
					}
					g.mu.Lock()
					admission := g.admission
					toolAdmission := g.toolAdmission
					g.mu.Unlock()
					approved := false
					if toolAdmission != nil {
						allowed, priorApproval := toolAdmission(target.Name, message.Params.Name)
						if !allowed {
							writeDenied(w, message.ID, "MCP_DENIED: exact task/tool grant is absent or revoked")
							return
						}
						approved = priorApproval
					}
					if admission != nil && !admission() {
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": "PLAN_APPROVAL_REQUIRED: external work cannot execute before the IVOAI plan is admitted"}}}})
						return
					}
					if (g.interactive.Load() || approvalTools[message.Params.Name] && !approved) && !g.approve(requestCtx, target.Name+" · "+message.Params.Name) {
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": "IVOAI external MCP permission denied or cancelled"}}}})
						return
					}
				case "tools/list":
					r = r.WithContext(context.WithValue(r.Context(), inventoryRequestKey{}, true))
				case "initialize", "notifications/initialized", "notifications/cancelled", "ping":
				case "resources/list", "resources/templates/list", "resources/read", "prompts/list", "prompts/get":
					if target.Restricted {
						http.Error(w, "MCP_DENIED: method is outside worker scope", http.StatusForbidden)
						return
					}
				default:
					http.Error(w, "unsupported external MCP method", 400)
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
			} else if r.Method != http.MethodGet && r.Method != http.MethodDelete {
				http.Error(w, "method not allowed", 405)
				return
			}
			proxy.ServeHTTP(w, r)
		}))
	}
	g.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10, BaseContext: func(net.Listener) context.Context { return ctx }}
	go func() { _ = g.server.Serve(listener) }()
	return g, nil
}

func (g *Gateway) URL(index int) string  { return g.url + g.Path(index) }
func (g *Gateway) Path(index int) string { return "/mcp/" + strconv.Itoa(index) }
func (g *Gateway) Token() string         { return g.token }

// UseNativeApprovals is used only before a degraded fallback launches its native
// TUI; that executor must retain its own interactive approval configuration.
func (g *Gateway) UseNativeApprovals() { g.interactive.Store(false) }
func (g *Gateway) Close()              { g.cancel(); _ = g.server.Close(); g.transport.CloseIdleConnections() }
func (g *Gateway) Pending() []Permission {
	g.mu.Lock()
	defer g.mu.Unlock()
	result := []Permission{}
	for _, value := range g.pending {
		result = append(result, value.view)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
func (g *Gateway) Reply(id string, allow bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	value, ok := g.pending[id]
	if !ok {
		return errors.New("external MCP permission no longer pending")
	}
	delete(g.pending, id)
	value.reply <- allow
	return nil
}
func (g *Gateway) approve(ctx context.Context, description string) bool {
	id, err := randomID()
	if err != nil {
		return false
	}
	id = "ext_" + id
	value := pending{view: Permission{ID: id, Description: description}, reply: make(chan bool, 1)}
	g.mu.Lock()
	if len(g.pending) >= 16 {
		g.mu.Unlock()
		return false
	}
	g.pending[id] = value
	g.mu.Unlock()
	defer func() { g.mu.Lock(); delete(g.pending, id); g.mu.Unlock() }()
	select {
	case allow := <-value.reply:
		return allow
	case <-ctx.Done():
		return false
	}
}
func randomID() (string, error) {
	var b [24]byte
	_, err := rand.Read(b[:])
	return hex.EncodeToString(b[:]), err
}
func cloneURL(value *url.URL) *url.URL { copy := *value; return &copy }
func safeName(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_.-", r)) {
			return false
		}
	}
	return true
}

type boundedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, errors.New("MCP response limit exceeded")
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	return n, err
}
