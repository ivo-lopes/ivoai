package connections

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/secrets"
)

type authRunner struct {
	mu     sync.Mutex
	logged bool
	calls  [][]string
}

func (r *authRunner) LookPath(name string) (string, error) { return "/fake/" + name, nil }
func (r *authRunner) Run(_ context.Context, command string, args []string, _ platform.RunOptions) (platform.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, append([]string{command}, args...))
	if len(args) >= 2 && args[len(args)-1] == "status" {
		if r.logged {
			return platform.Result{Stdout: `{"loggedIn": true}`}, nil
		}
		return platform.Result{Stdout: `{"loggedIn": false}`}, nil
	}
	r.logged = true
	return platform.Result{}, nil
}

func TestAuthenticationStatusRejectsNegativeAndUnknownOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{name: "json camel case with whitespace", output: `{"loggedIn": false}`},
		{name: "nested json positive", output: `{"auth":{"authenticated": true}}`, want: true},
		{name: "status word negative", output: `{"status":"NOT_LOGGED_IN"}`},
		{name: "human negative", output: "Claude is not authenticated"},
		{name: "human positive", output: "Logged in using ChatGPT", want: true},
		{name: "unknown successful output", output: "ok"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := AuthenticationStatus(platform.Result{Stdout: test.output}, nil); got != test.want {
				t.Fatalf("AuthenticationStatus(%q) = %t, want %t", test.output, got, test.want)
			}
		})
	}
}
func connStore(root string) *config.Store {
	p := config.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), Config: filepath.Join(root, "config", "config.toml"), State: filepath.Join(root, "state", "state.toml"), Secrets: filepath.Join(root, "config", "secrets.json"), Ownership: filepath.Join(root, "state", "ownership.toml"), HooksDir: filepath.Join(root, "data", "hooks")}
	return config.NewStore(p)
}

func TestAgentAuthUsesOfficialCommands(t *testing.T) {
	store := connStore(t.TempDir())
	if err := store.Save(config.Default()); err != nil {
		t.Fatal(err)
	}
	runner := &authRunner{}
	a := AgentAuth{Runner: runner, Store: store, In: strings.NewReader(""), Out: io.Discard, Err: io.Discard}
	if err := a.Connect(context.Background(), "chatgpt"); err != nil {
		t.Fatal(err)
	}
	c, _ := store.Load()
	if c.Connections.ChatGPT.Status != "connected" {
		t.Fatal("state not connected")
	}
	joined := ""
	for _, call := range runner.calls {
		joined += strings.Join(call, " ") + "\n"
	}
	if !strings.Contains(joined, "/fake/codex login status") || !strings.Contains(joined, "/fake/codex login") {
		t.Fatalf("unexpected calls:\n%s", joined)
	}
}

func TestServerEnrollmentAndOneTimeCode(t *testing.T) {
	var mu sync.Mutex
	used := false
	const code = "single-use-code"
	const token = "ivo_super_secret_client_token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/ivoai":
			json.NewEncoder(w).Encode(Discovery{ProtocolVersion: ProtocolVersion, ServerVersion: "0.1.0", HealthEndpoint: "/health", ReadyEndpoint: "/ready", ContextMCPEndpoint: "/mcp/context", MemoryMCPEndpoint: "/v1/memory/mcp", MemoryHooksEndpoint: "/v1/memory", EnrollmentEndpoint: "/enroll", Features: map[string]bool{"context": true, "memory": true}})
		case "/health":
			json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
		case "/ready":
			json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
		case "/mcp/context":
			if r.Header.Get("Authorization") != "Bearer "+token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"tools": []any{}}})
		case "/v1/memory/mcp":
			if r.Header.Get("Authorization") != "Bearer "+token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"tools": []any{}}})
		case "/enroll":
			var request enrollmentRequest
			json.NewDecoder(r.Body).Decode(&request)
			mu.Lock()
			defer mu.Unlock()
			if request.Code != "" || r.Header.Get("Authorization") != "Ivoai-Enrollment "+code || used {
				http.Error(w, "refused", http.StatusUnauthorized)
				return
			}
			used = true
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(enrollmentResponse{Token: token, ClientID: "client-1", Scopes: []string{"context:read"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	store := connStore(root)
	if err := store.Save(config.Default()); err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.Store{Path: store.Paths.Secrets}
	connector := ServerConnector{Client: server.Client(), Store: store, Secrets: secretStore}
	result, err := connector.Connect(context.Background(), server.URL, code, "host:test")
	if err != nil {
		t.Fatal(err)
	}
	if result.MemoryMCPURL != server.URL+"/v1/memory/mcp" || result.MemoryHooksURL != server.URL+"/v1/memory" {
		t.Fatalf("wrong resolved memory endpoints: %#v", result)
	}
	c, _ := store.Load()
	if c.Connections.Server.Status != "connected" || c.MCP.Servers["ivoai-context"].URL != server.URL+"/mcp/context" || c.MCP.Servers["ivoai-memory"].URL != server.URL+"/v1/memory/mcp" || c.MCP.Servers["ivoai-memory"].HooksURL != server.URL+"/v1/memory" {
		t.Fatalf("bad config %#v", c.Connections)
	}
	b, _ := os.ReadFile(store.Paths.Config)
	if strings.Contains(string(b), token) || strings.Contains(string(b), code) {
		t.Fatal("credential leaked to config")
	}
	info, _ := os.Stat(store.Paths.Secrets)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("secret mode %o", info.Mode().Perm())
	}
	if _, err := connector.Connect(context.Background(), server.URL, code, "host:test"); err == nil {
		t.Fatal("one-time code reused")
	}
	if err := connector.Disconnect(); err != nil {
		t.Fatal(err)
	}
	data, _ := secretStore.Load()
	if data.Server != nil {
		t.Fatal("server credential retained")
	}
}

func TestServerRejectsInsecureRemoteAndProtocolMismatch(t *testing.T) {
	if _, err := ValidateBaseURL("http://example.com"); err == nil {
		t.Fatal("accepted remote HTTP")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/ivoai" {
			json.NewEncoder(w).Encode(Discovery{ProtocolVersion: 99})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	store := connStore(t.TempDir())
	_ = store.Save(config.Default())
	_, err := (ServerConnector{Client: server.Client(), Store: store, Secrets: secrets.Store{Path: store.Paths.Secrets}}).Connect(context.Background(), server.URL, "code", "client")
	if err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("got %v", err)
	}
}

func TestEnrollmentCredentialSurvivesDegradedMCPProbe(t *testing.T) {
	const token = "scoped-token-never-logged"
	used := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/ivoai":
			json.NewEncoder(w).Encode(Discovery{ProtocolVersion: ProtocolVersion, HealthEndpoint: "/health", ReadyEndpoint: "/ready", ContextMCPEndpoint: "/broken-mcp", EnrollmentEndpoint: "/enroll", Features: map[string]bool{"context": true}})
		case "/health":
			json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
		case "/ready":
			json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
		case "/enroll":
			if r.Header.Get("Authorization") != "Ivoai-Enrollment one-time" {
				http.Error(w, "missing enrollment authorization", http.StatusBadRequest)
				return
			}
			if used {
				http.Error(w, "already used", http.StatusUnauthorized)
				return
			}
			used = true
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(enrollmentResponse{Token: token, ClientID: "client-degraded", Scopes: []string{"context:read"}})
		case "/broken-mcp":
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	store := connStore(t.TempDir())
	if err := store.Save(config.Default()); err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.Store{Path: store.Paths.Secrets}
	connector := ServerConnector{Client: server.Client(), Store: store, Secrets: secretStore}
	result, err := connector.Connect(context.Background(), server.URL, "one-time", "host:test")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 1 || strings.Contains(strings.Join(result.Warnings, " "), token) {
		t.Fatalf("unexpected warnings: %#v", result.Warnings)
	}
	cfg, _ := store.Load()
	credential, _ := secretStore.Load()
	if cfg.Connections.Server.Status != "connected" || credential.Server == nil || credential.Server.Token != token {
		t.Fatalf("recoverable connection state not preserved: %#v %#v", cfg.Connections.Server, credential.Server)
	}
}

type mcpRunner struct {
	calls [][]string
}

func (r *mcpRunner) LookPath(name string) (string, error) { return "/managed/" + name, nil }
func (r *mcpRunner) Run(_ context.Context, command string, args []string, _ platform.RunOptions) (platform.Result, error) {
	r.calls = append(r.calls, append([]string{command}, args...))
	return platform.Result{}, nil
}

func TestAgentMCPUsesDiscoveryURLsAndTokenEnvironmentReference(t *testing.T) {
	runner := &mcpRunner{}
	manager := AgentMCP{Runner: runner, CodexBinary: "/managed/codex", ClaudeBinary: "/managed/claude"}
	servers := map[string]config.MCPServer{
		"ivoai-context": {URL: "https://ai.example.com/v1/mcp/context", Enabled: true, Kind: "context"},
		"ivoai-memory":  {URL: "https://ai.example.com/v1/memory/mcp", Enabled: true, Kind: "memory"},
	}
	if err := manager.ConfigureRemote(context.Background(), servers); err != nil {
		t.Fatal(err)
	}
	joined := make([]string, 0, len(runner.calls))
	for _, call := range runner.calls {
		joined = append(joined, strings.Join(call, " "))
	}
	all := strings.Join(joined, "\n")
	for _, expected := range []string{
		"/managed/codex mcp add ivoai-context --url https://ai.example.com/v1/mcp/context --bearer-token-env-var " + ServerTokenEnvironment,
		"/managed/codex mcp add ivoai-memory --url https://ai.example.com/v1/memory/mcp --bearer-token-env-var " + ServerTokenEnvironment,
		"Authorization: Bearer ${" + ServerTokenEnvironment + "}",
		"https://ai.example.com/v1/memory/mcp",
	} {
		if !strings.Contains(all, expected) {
			t.Fatalf("missing %q in calls:\n%s", expected, all)
		}
	}
	if strings.Contains(all, "scoped-token") {
		t.Fatal("literal token leaked into MCP configuration argv")
	}
	runner.calls = nil
	if err := manager.RemoveRemote(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 4 {
		t.Fatalf("expected four remove calls, got %d", len(runner.calls))
	}
}
