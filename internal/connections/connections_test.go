package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

func TestProbeMCPUsesStreamableHTTPNegotiation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		accept := request.Header.Get("Accept")
		if !strings.Contains(accept, "application/json") || !strings.Contains(accept, "text/event-stream") {
			http.Error(w, "not acceptable", http.StatusNotAcceptable)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
	}))
	t.Cleanup(server.Close)
	connector := ServerConnector{Client: server.Client()}
	if err := connector.probeMCP(context.Background(), server.URL+"/mcp", "fixture-token"); err != nil {
		t.Fatal(err)
	}
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

func TestClaudeAuthUsesSubscriptionLoginAndReportsProgress(t *testing.T) {
	store := connStore(t.TempDir())
	if err := store.Save(config.Default()); err != nil {
		t.Fatal(err)
	}
	runner := &authRunner{}
	var output strings.Builder
	a := AgentAuth{Runner: runner, Store: store, In: strings.NewReader(""), Out: io.Discard, Err: &output}
	if err := a.Connect(context.Background(), "claude"); err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, call := range runner.calls {
		joined += strings.Join(call, " ") + "\n"
	}
	if !strings.Contains(joined, "/fake/claude auth login --claudeai") {
		t.Fatalf("official subscription login was not used:\n%s", joined)
	}
	for _, message := range []string{"Checking", "Starting", "Validating", "connection is ready"} {
		if !strings.Contains(output.String(), message) {
			t.Fatalf("missing %q in progress output: %q", message, output.String())
		}
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
			if request.Code != "" || r.Header.Get("Authorization") != "Ivoai-Enrollment "+code || r.Header.Get(enrollmentClientNameHeader) != "host:test" || !strings.Contains(r.Header.Get(enrollmentScopesHeader), "context:read") || used {
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

func TestMultiServerEnrollmentIsIndependentAndSelective(t *testing.T) {
	newServer := func(token string, reject bool) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/.well-known/ivoai":
				json.NewEncoder(w).Encode(Discovery{ProtocolVersion: ProtocolVersion, HealthEndpoint: "/health", ReadyEndpoint: "/ready", ContextMCPEndpoint: "/context", MemoryMCPEndpoint: "/memory", MemoryHooksEndpoint: "/hooks", EnrollmentEndpoint: "/enroll", Features: map[string]bool{"context": true, "memory": true}})
			case "/health":
				json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
			case "/ready":
				json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
			case "/enroll":
				if reject {
					http.Error(w, "refused", http.StatusUnauthorized)
					return
				}
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(enrollmentResponse{Token: token, ClientID: "client-" + token, Scopes: []string{"context:read", "memory:read"}})
			case "/context", "/memory":
				if r.Header.Get("Authorization") != "Bearer "+token {
					http.Error(w, "wrong credential", http.StatusUnauthorized)
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"tools": []any{}}})
			default:
				http.NotFound(w, r)
			}
		}))
	}
	a := newServer("token-a", false)
	b := newServer("token-b", false)
	c := newServer("token-c", true)
	defer a.Close()
	defer b.Close()
	defer c.Close()
	store := connStore(t.TempDir())
	if err := store.Save(config.Default()); err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.Store{Path: store.Paths.Secrets}
	connector := ServerConnector{Store: store, Secrets: secretStore}
	for _, options := range []ConnectOptions{
		{Alias: "voicecorp", Purpose: "voicecorp", BaseURL: a.URL, Code: "a", ClientName: "test"},
		{Alias: "mindsite", Purpose: "mindsite", BaseURL: b.URL, Code: "b", ClientName: "test"},
	} {
		if _, err := connector.ConnectProfile(context.Background(), options); err != nil {
			t.Fatal(err)
		}
	}
	before, _ := store.Load()
	voiceID := before.Connections.Servers["voicecorp"].ID
	mindID := before.Connections.Servers["mindsite"].ID
	if voiceID == mindID || len(before.Connections.Servers) != 2 {
		t.Fatalf("profiles=%#v", before.Connections.Servers)
	}
	credentials, _ := secretStore.Load()
	if credentials.Servers[voiceID].Token != "token-a" || credentials.Servers[mindID].Token != "token-b" {
		t.Fatalf("credentials=%#v", credentials.Servers)
	}
	if _, err := connector.ConnectProfile(context.Background(), ConnectOptions{Alias: "voicecorp", Purpose: "voicecorp", BaseURL: a.URL, Code: "a2", ClientName: "test"}); err != nil {
		t.Fatal(err)
	}
	reconnected, _ := store.Load()
	if reconnected.Connections.Servers["voicecorp"].ID != voiceID || reconnected.Connections.Servers["mindsite"].ID != mindID {
		t.Fatal("reconnect changed stable identities or unrelated profile")
	}
	if _, err := connector.ConnectProfile(context.Background(), ConnectOptions{Alias: "failed", Purpose: "failed", BaseURL: c.URL, Code: "c", ClientName: "test"}); err == nil {
		t.Fatal("failed enrollment was accepted")
	}
	afterFailure, _ := store.Load()
	if len(afterFailure.Connections.Servers) != 2 {
		t.Fatal("failed enrollment changed existing profiles")
	}
	if err := connector.DisconnectProfile("voicecorp"); err != nil {
		t.Fatal(err)
	}
	after, _ := store.Load()
	remaining, ok := after.Connections.Servers["mindsite"]
	if !ok || remaining.ID != mindID {
		t.Fatalf("unrelated profile changed: %#v", after.Connections.Servers)
	}
	credentials, _ = secretStore.Load()
	if _, ok := credentials.Servers[voiceID]; ok || credentials.Servers[mindID].Token != "token-b" {
		t.Fatalf("selective credential removal failed: %#v", credentials.Servers)
	}
}

func TestReconnectCommitFailureRestoresProfileAndCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/.well-known/ivoai":
			json.NewEncoder(w).Encode(Discovery{ProtocolVersion: ProtocolVersion, HealthEndpoint: "/health", ReadyEndpoint: "/ready", ContextMCPEndpoint: "/context", EnrollmentEndpoint: "/enroll", Features: map[string]bool{"context": true}})
		case "/health":
			json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
		case "/ready":
			json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
		case "/enroll":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(enrollmentResponse{Token: "new-token", ClientID: "new-client"})
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	store := connStore(t.TempDir())
	cfg := config.Default()
	cfg.Connections.Servers["voicecorp"] = config.ServerProfile{ID: "srv_stable_voicecorp", Alias: "voicecorp", URL: "https://old.example.invalid", Status: "connected", Enabled: true, Purpose: "voicecorp", ContextMCPURL: "https://old.example.invalid/context"}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.Store{Path: store.Paths.Secrets}
	if err := secretStore.Set("srv_stable_voicecorp", secrets.ClientCredential{Token: "old-token", ClientID: "old-client"}); err != nil {
		t.Fatal(err)
	}
	saves := 0
	connector := ServerConnector{Client: server.Client(), Store: store, Secrets: secretStore, SaveConfig: func(value config.Config) error {
		saves++
		if saves == 2 {
			return errors.New("injected final config failure")
		}
		return store.Save(value)
	}}
	if _, err := connector.ConnectProfile(context.Background(), ConnectOptions{Alias: "voicecorp", Purpose: "voicecorp", BaseURL: server.URL, Code: "one-time", ClientName: "test"}); err == nil {
		t.Fatal("injected commit failure was hidden")
	}
	reloaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	credential, ok, err := secretStore.Get("srv_stable_voicecorp")
	if err != nil || !ok {
		t.Fatalf("credential unavailable after rollback: ok=%v err=%v", ok, err)
	}
	if reloaded.Connections.Servers["voicecorp"].URL != "https://old.example.invalid" || credential.Token != "old-token" {
		t.Fatalf("reconnect failure crossed state: profile=%+v credential=%+v", reloaded.Connections.Servers["voicecorp"], credential)
	}
}

func TestCredentialOperationsRejectDuplicateServerIdentity(t *testing.T) {
	store := connStore(t.TempDir())
	cfg := config.Default()
	for _, alias := range []string{"voicecorp", "mindsite"} {
		cfg.Connections.Servers[alias] = config.ServerProfile{ID: "srv_duplicate_identity", Alias: alias, URL: "https://" + alias + ".example.invalid", Status: "connected", Enabled: true, Purpose: alias, ContextMCPURL: "https://" + alias + ".example.invalid/context"}
	}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	connector := ServerConnector{Store: store, Secrets: secrets.Store{Path: store.Paths.Secrets}}
	if err := connector.DisconnectProfile("mindsite"); err == nil {
		t.Fatal("disconnect accepted duplicate server identity")
	}
	if _, err := connector.ConnectProfile(context.Background(), ConnectOptions{Alias: "voicecorp", Purpose: "voicecorp", BaseURL: "https://voicecorp.example.invalid", Code: "unused"}); err == nil {
		t.Fatal("reconnect accepted duplicate server identity")
	}
}

func TestDecodeLimitedRejectsOversizedAndTrailingJSON(t *testing.T) {
	var target map[string]any
	oversized := append([]byte(`{"ok":true}`), bytes.Repeat([]byte(" "), maxResponse)...)
	if err := decodeLimited(bytes.NewReader(oversized), &target); err == nil {
		t.Fatal("oversized JSON prefix accepted")
	}
	if err := decodeLimited(strings.NewReader(`{"ok":true}{"second":true}`), &target); err == nil {
		t.Fatal("multiple JSON values accepted")
	}
}

func TestResolveEndpointRejectsUserinfoAndCrossOrigin(t *testing.T) {
	base, err := ValidateBaseURL("https://voicecorp.example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"https://user:secret@voicecorp.example.invalid/context", "https://mindsite.example.invalid/context", "//mindsite.example.invalid/context", "/context?token=secret"} {
		if got := resolveEndpoint(base, endpoint); got != "" {
			t.Fatalf("unsafe endpoint %q resolved to %q", endpoint, got)
		}
	}
}

type mcpRunner struct {
	calls              [][]string
	missingRemoveEntry bool
}

func (r *mcpRunner) LookPath(name string) (string, error) { return "/managed/" + name, nil }
func (r *mcpRunner) Run(_ context.Context, command string, args []string, _ platform.RunOptions) (platform.Result, error) {
	r.calls = append(r.calls, append([]string{command}, args...))
	if r.missingRemoveEntry && strings.Contains(strings.Join(args, " "), "mcp remove") {
		return platform.Result{Stderr: "No MCP server named fixture in user scope", ExitCode: 1}, errors.New("fixture exit 1")
	}
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

func TestAgentMCPRemovalIgnoresAlreadyAbsentEntries(t *testing.T) {
	runner := &mcpRunner{missingRemoveEntry: true}
	manager := AgentMCP{Runner: runner, CodexBinary: "/managed/codex", ClaudeBinary: "/managed/claude"}
	if err := manager.RemoveRemote(context.Background()); err != nil {
		t.Fatalf("idempotent removal failed: %v", err)
	}
}
