package admin_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/admin"
	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/cli"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/platform"
	projectpkg "github.com/ivo-lopes/ivoai/internal/project"
	"github.com/ivo-lopes/ivoai/internal/secrets"
	"github.com/ivo-lopes/ivoai/internal/server"
	"github.com/ivo-lopes/ivoai/internal/servercmd"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/skillcatalog"
	"github.com/ivo-lopes/ivoai/internal/skills"
	"github.com/ivo-lopes/ivoai/internal/webauth"
)

type isolatedAdministrativeRunner struct{}

func (isolatedAdministrativeRunner) LookPath(string) (string, error) { return "", os.ErrNotExist }
func (isolatedAdministrativeRunner) Run(context.Context, string, []string, platform.RunOptions) (platform.Result, error) {
	return platform.Result{Stdout: `{"status":"healthy"}`, ExitCode: 0}, nil
}

type noNetworkTransport struct{}

func (noNetworkTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("isolated dependency unavailable")
}

type operationRecord struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	Status        string   `json:"status"`
	HTTP          int      `json:"http_status"`
	DispatchError string   `json:"dispatch_error,omitempty"`
	Changed       []string `json:"changed_protected_files"`
	Scope         string   `json:"scope"`
}

// Every applicable public action traverses the actual HTTP admission and CLI
// parser/service adapter with independent temporary state. Native process calls
// and server lifecycle are explicit fixture boundaries, not certified providers
// or systemd. A domain rejection is recorded as unavailable, never a completion.
func TestAdministrativeOperationCatalogThroughActualCLIAndIsolatedServices(t *testing.T) {
	t.Setenv("IVOAI_TEST_MODE", "1")
	t.Setenv("NO_COLOR", "1")
	rpc := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/ivoai":
			fmt.Fprint(w, `{"protocol_version":1,"health_endpoint":"/health","ready_endpoint":"/ready","context_mcp_endpoint":"/context","memory_mcp_endpoint":"/memory","memory_hooks_endpoint":"/hooks","enrollment_endpoint":"/enroll","features":{"context":true,"memory":true}}`)
			return
		case "/health":
			fmt.Fprint(w, `{"status":"healthy"}`)
			return
		case "/ready":
			fmt.Fprint(w, `{"status":"ready"}`)
			return
		case "/enroll":
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"token":"fixture-private-token","client_id":"fixture","scopes":["memory:read","context:read"]}`)
			return
		}
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			w.WriteHeader(400)
			return
		}
		if req.ID == nil {
			w.WriteHeader(202)
			return
		}
		var result any = map[string]any{}
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "isolated-fixture", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "fixture_lookup", "description": "Fixture lookup", "inputSchema": map[string]any{"type": "object"}, "annotations": map[string]any{"readOnlyHint": true}}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer rpc.Close()
	discovery := httptest.NewServer(rpc.Config.Handler)
	defer discovery.Close()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // Isolated TLS fixture only.
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || port == "6333" || port == "8080" || port == "49374" {
			return nil, errors.New("isolated dependency unavailable")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = previous; transport.CloseIdleConnections() }()
	cli.RegisterServerRunner(servercmd.New("0.16.0"))
	defer cli.RegisterServerRunner(nil)
	records := []operationRecord{}
	for _, action := range admin.ActionCatalog() {
		if action.Kind != "read" && action.Kind != "mutation" {
			continue
		}
		t.Run(action.ID, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", filepath.Join(root, "home"))
			t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
			t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))
			for _, key := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
				t.Setenv(key, filepath.Join(root, key))
			}
			serverRoot := filepath.Join(root, "server")
			t.Setenv("IVOAI_SERVER_ROOT", serverRoot)
			t.Setenv("QDRANT__SERVICE__API_KEY", "")
			t.Setenv("API_KEY", "")
			project := filepath.Join(root, "project")
			if err := os.Mkdir(project, 0700); err != nil {
				t.Fatal(err)
			}
			t.Chdir(project)
			if action.ID == "project.init" {
				cmd := exec.Command("git", "init", "-q", project)
				if err := cmd.Run(); err != nil {
					t.Fatal(err)
				}
			}
			a, err := app.New("0.16.0", strings.NewReader(""), io.Discard, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			a.Runner = isolatedAdministrativeRunner{}
			a.HTTPClient = &http.Client{Transport: noNetworkTransport{}}
			cfg := config.Default()
			cfg.Memory.Enabled = false
			if action.ID == "capability.enable" || action.ID == "capability.unpin" {
				cfg.Skills.Sources = map[string]config.SkillSourcePolicy{skillcatalog.NativeIDs()[0]: {Disabled: action.ID == "capability.enable", Pinned: action.ID == "capability.unpin"}}
			}
			endpoint := discovery.URL
			cfg.MCP.Servers["fixture"] = config.MCPServer{ID: "mcp_0123456789abcdef0123456789abcdef", URL: rpc.URL + "/mcp", Enabled: true, Kind: "external", AuthMode: "none", Policy: "read_only", DirectPolicy: "read_only"}
			cfg.Connections.Servers["fixture"] = config.ServerProfile{ID: "srv_0123456789abcdef0123456789abcdef", Alias: "fixture", URL: endpoint, Status: "connected", Enabled: true, Purpose: "fixture", Protocol: 1, ContextMCPURL: endpoint + "/context", MemoryMCPURL: endpoint + "/memory"}
			if err := a.Store.Save(cfg); err != nil {
				t.Fatal(err)
			}
			if err := (secrets.Store{Path: a.Store.Paths.Secrets}).Set("srv_0123456789abcdef0123456789abcdef", secrets.ClientCredential{Token: "fixture-private-token", ClientID: "fixture", Scopes: []string{"memory:read", "context:read"}}); err != nil {
				t.Fatal(err)
			}
			if action.ID == "mcp.auth.remove" || action.ID == "mcp.header" {
				if err := a.MCPSetBearer("fixture", "existing-fixture-secret"); err != nil {
					t.Fatal(err)
				}
			}
			id, _ := session.NewID()
			now := time.Now().UTC()
			value := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeDirect, PrimaryExecutor: "codex", WorkingDirectory: project, PrimaryModel: session.UnknownModel(), MaxWorkers: 1, State: session.StateRunning, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected"}
			if err := (session.Store{Root: a.Store.Paths.SessionsDir}).Create(value); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(root, "corpus")
			if err := os.Mkdir(source, 0700); err != nil {
				t.Fatal(err)
			}
			_ = os.WriteFile(filepath.Join(source, "approved.md"), []byte("distinctive approved administrative document"), 0600)
			runServer := servercmd.New("0.16.0")
			if err := runServer(context.Background(), []string{"setup"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
				t.Fatal(err)
			}
			_ = runServer(context.Background(), []string{"connector", "add", "--name", "fixture", "--type", "filesystem", "--path", source}, strings.NewReader(""), io.Discard, io.Discard)
			layout := server.DefaultLayout(serverRoot)
			backup := filepath.Join(root, "fixture-backup.tar.gz")
			if err := server.Backup(layout, backup, now); err != nil {
				t.Fatal(err)
			}
			var created bytes.Buffer
			_ = runServer(context.Background(), []string{"enrollment", "create", "--ttl", "5m"}, strings.NewReader(""), &created, io.Discard)
			enrollmentID := regexp.MustCompile(`Enrollment ID: ([a-f0-9]+)`).FindStringSubmatch(created.String())
			created.Reset()
			_ = runServer(context.Background(), []string{"web-access", "create", "--ttl", "5m"}, strings.NewReader(""), &created, io.Discard)
			webID := regexp.MustCompile(`Web access ID: ([a-f0-9]+)`).FindStringSubmatch(created.String())
			input := map[string]string{}
			for _, f := range action.Fields {
				if !f.Required {
					continue
				}
				if len(f.Options) > 0 {
					input[f.Name] = f.Options[len(f.Options)-1]
					continue
				}
				input[f.Name] = "fixture"
			}
			for key := range input {
				switch key {
				case "value":
					boolChoice := false
					for _, f := range action.Fields {
						if f.Name == key && len(f.Options) > 0 {
							boolChoice = true
						}
					}
					if boolChoice {
						continue
					}
					input[key] = "2"
					if action.ID == "policy.low-quota" {
						input[key] = "20"
					}
				case "id":
					input[key] = id
				case "source":
					input[key] = skillcatalog.NativeIDs()[0]
				case "alias":
					input[key] = "fixture"
				case "url":
					input[key] = endpoint
				case "purpose":
					input[key] = "updated"
				case "priority":
					input[key] = "2"
				case "code":
					input[key] = "ivoai-enroll_0123456789abcdef_fixture-not-a-real-code"
				case "name":
					input[key] = "fixture"
				case "header":
					input[key] = "X-Fixture"
				case "secret":
					input[key] = "isolated-private-fixture"
				case "ttl":
					input[key] = "5m"
				case "listen":
					input[key] = "127.0.0.1:7744"
				case "path":
					input[key] = source
				case "query":
					input[key] = "distinctive approved administrative document"
				case "project_root":
					input[key] = project
				}
			}
			if action.ID == "mcp.add" {
				input["name"] = "new-mcp"
				input["url"] = rpc.URL + "/mcp"
			}
			if action.ID == "servers.add" {
				input["alias"] = "new-server"
			}
			if action.ID == "server.connector.add" {
				input["name"] = "new-connector"
			}
			if action.ID == "server.gateway.configure" {
				input["url"] = "https://fixture.example"
			}
			if action.ID == "server.backup" {
				input["path"] = filepath.Join(root, "created-backup.tar.gz")
			}
			if action.ID == "server.restore" {
				input["path"] = backup
			}
			if action.ID == "session.scope.restore" {
				input["path"] = filepath.Join(root, "missing-scope-backup.json")
			}
			if action.ID == "server.enrollment.revoke" && len(enrollmentID) == 2 {
				input["id"] = enrollmentID[1]
			}
			if action.ID == "server.web-access.revoke" && len(webID) == 2 {
				input["id"] = webID[1]
			}
			if action.ID == "servers.re-enroll" {
				a.HTTPClient = discovery.Client()
			}
			if action.ID == "servers.add" || action.ID == "servers.test" {
				a.HTTPClient = discovery.Client()
			}
			if action.ID == "profile.restore" {
				if err := a.Store.ApplyAutomationProfile("economic", nil); err != nil {
					t.Fatal(err)
				}
			}
			var dispatchedErr error
			dispatchCalls := 0
			origin := "http://127.0.0.1:12345"
			s, err := admin.New(admin.Options{App: a, Origin: origin, Dispatch: func(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) error {
				dispatchCalls++
				if len(args) > 1 && args[0] == "server" && (args[1] == "start" || args[1] == "stop" || args[1] == "restart" || args[1] == "logs") {
					dispatchedErr = errors.New("isolated systemd controller unavailable")
					return dispatchedErr
				}
				operation := *a
				operation.In, operation.Out, operation.Err = in, out, errOut
				dispatchedErr = cli.Run(ctx, &operation, args)
				return dispatchedErr
			}})
			if err != nil {
				t.Fatal(err)
			}
			auth, err := admin.AccessStore(a.Store.Paths)
			if err != nil {
				t.Fatal(err)
			}
			activation, err := auth.CreateActivation(time.Minute, []string{admin.ScopeRead, admin.ScopeManage})
			if err != nil {
				t.Fatal(err)
			}
			redirect := origin + "/auth/callback"
			client, err := auth.RegisterClient("isolated test", []string{redirect})
			if err != nil {
				t.Fatal(err)
			}
			verifier := strings.Repeat("v", 43)
			nonce, err := auth.BeginAuthorization(client.ID, redirect, webauth.PKCEChallenge(verifier), "fixture", s.Resource(), []string{admin.ScopeRead, admin.ScopeManage})
			if err != nil {
				t.Fatal(err)
			}
			code, _, _, err := auth.AuthorizeRequest(activation.Code, nonce)
			if err != nil {
				t.Fatal(err)
			}
			token, err := auth.ExchangeCode(code, client.ID, redirect, verifier, s.Resource())
			if err != nil {
				t.Fatal(err)
			}
			start := httptest.NewRequest("GET", origin+"/auth/start", nil)
			start.RemoteAddr = "127.0.0.1:1"
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, start)
			cookies := w.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatal("missing flow cookie")
			}
			cookieName := strings.TrimSuffix(cookies[0].Name, "_flow")
			request := func(method, path string, body any, csrf string) *httptest.ResponseRecorder {
				b, _ := json.Marshal(body)
				r := httptest.NewRequest(method, origin+path, bytes.NewReader(b))
				r.RemoteAddr = "127.0.0.1:1"
				r.Header.Set("Origin", origin)
				r.Header.Set("Content-Type", "application/json")
				r.AddCookie(&http.Cookie{Name: cookieName, Value: token.AccessToken})
				r.Header.Set("X-IVOAI-CSRF", csrf)
				out := httptest.NewRecorder()
				s.Handler().ServeHTTP(out, r)
				return out
			}
			var csrf struct {
				Token string `json:"csrf_token"`
			}
			_ = json.Unmarshal(request("GET", "/api/admin/v1/csrf", nil, "").Body.Bytes(), &csrf)
			snapshot := func() map[string]string {
				m := map[string]string{}
				for name, path := range map[string]string{"config": a.Store.Paths.Config, "state": a.Store.Paths.State, "secrets": a.Store.Paths.Secrets, "connectors": filepath.Join(layout.ConfigDir, "connectors.json"), "enrollment": filepath.Join(layout.DataDir, "enrollment/state.json"), "web_access": filepath.Join(layout.DataDir, "web-oauth/state.json"), "gateway": server.GatewayConfigPath(layout), "skills_registry": skills.RegistryPath(a.Store.Paths.StateDir), "project_marker": filepath.Join(project, projectpkg.MarkerName), "backup": input["path"]} {
					if path == "" {
						continue
					}
					b, err := os.ReadFile(path)
					if err == nil {
						h := sha256.Sum256(b)
						m[name] = hex.EncodeToString(h[:])
					}
				}
				return m
			}
			before := snapshot()
			body := map[string]any{"phase": "execute", "input": input, "request_id": "isolated-operation-" + strings.ReplaceAll(action.ID, ".", "_")}
			path := "/api/admin/v1/actions/" + action.ID
			if action.Kind == "mutation" {
				preview := request("POST", path, map[string]any{"phase": "preview", "input": input}, csrf.Token)
				if preview.Code == 403 && os.Geteuid() != 0 {
					records = append(records, operationRecord{ID: action.ID, Kind: action.Kind, Status: "unavailable_existing_OS_privileges", HTTP: 403, Changed: []string{}, Scope: "not_executed"})
					return
				}
				var receipt struct {
					Token string `json:"confirmation"`
				}
				_ = json.Unmarshal(preview.Body.Bytes(), &receipt)
				if preview.Code != 200 || receipt.Token == "" {
					t.Fatalf("typed preview rejected %d %s input=%v", preview.Code, preview.Body, input)
				}
				body["confirmation"] = receipt.Token
			}
			result := request("POST", path, body, csrf.Token)
			if result.Code == 403 && os.Geteuid() != 0 && (action.ID == "server.context.document" || action.ID == "server.context.search") {
				records = append(records, operationRecord{ID: action.ID, Kind: action.Kind, Status: "unavailable_existing_OS_privileges", HTTP: 403, Changed: []string{}, Scope: "not_executed"})
				return
			}
			var decoded struct {
				Status string `json:"status"`
				Output any    `json:"output"`
				Error  string `json:"error"`
			}
			_ = json.Unmarshal(result.Body.Bytes(), &decoded)
			after := snapshot()
			changed := []string{}
			for name, digest := range after {
				if before[name] != digest {
					changed = append(changed, name)
				}
			}
			for name := range before {
				if _, exists := after[name]; !exists {
					changed = append(changed, name)
				}
			}
			sort.Strings(changed)
			record := operationRecord{ID: action.ID, Kind: action.Kind, Status: decoded.Status, HTTP: result.Code, Changed: changed, Scope: "HTTP_actual_CLI_App_isolated_services_TEST_MODE_no_provider_inference"}
			if dispatchedErr != nil {
				record.DispatchError = platform.Redact(dispatchedErr.Error())
				for _, bad := range []string{"unknown skills command", "unknown MCP action", "unknown session action", "unknown server command", "flag provided but not defined", "usage: ivoai"} {
					if strings.Contains(record.DispatchError, bad) {
						t.Fatalf("catalog maps to invalid CLI contract: %s", record.DispatchError)
					}
				}
			}
			if result.Code != 200 && !(action.ID == "uninstall" && (result.Code == 401 || result.Code == 503)) {
				t.Fatalf("execution did not reach a valid service outcome: %d %s", result.Code, result.Body)
			}
			if action.Kind == "read" && len(changed) != 0 {
				t.Fatalf("read action persisted protected state: %v", changed)
			}
			if action.ID == "uninstall" && result.Code == http.StatusUnauthorized {
				if dispatchedErr != nil {
					t.Fatal("uninstall failed before invalidating its authority", dispatchedErr)
				}
				if _, err := os.Stat(a.Store.Paths.Config); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("uninstall did not remove isolated configuration", err)
				}
				retry := request("POST", path, body, csrf.Token)
				if (retry.Code != http.StatusUnauthorized && retry.Code != http.StatusServiceUnavailable) || dispatchCalls != 1 {
					t.Fatal("removed authority remained valid for a second dispatch", retry.Code, dispatchCalls)
				}
				record.Status = "authority_revoked_after_authorized_uninstall"
				record.Scope = "isolated_App_uninstall_authority_removal_and_replay_denial"
			}
			if decoded.Status == "failed" {
				want, expected := unavailableAdministrativePreconditions[action.ID]
				if !expected || !strings.Contains(record.DispatchError, want) {
					t.Fatalf("unexpected service rejection must be investigated: %q", record.DispatchError)
				}
				record.Scope = "dependency_or_precondition_unavailable_not_functional_certification"
			}
			if decoded.Status == "completed" {
				observed, err := a.Store.Load()
				if err != nil {
					t.Fatal("completed operation left unreadable configuration", err)
				}
				if key, ok := expectedConfigurationTargets[action.ID]; ok {
					got := configurationValue(observed, key)
					if got != input["value"] {
						t.Fatalf("wrong setting modified: %s=%s want%s", key, got, input["value"])
					}
				}
				switch action.ID {
				case "mcp.add":
					if observed.MCP.Servers["new-mcp"].URL != input["url"] {
						t.Fatal("MCP entry not created")
					}
				case "mcp.remove":
					if _, ok := observed.MCP.Servers["fixture"]; ok {
						t.Fatal("MCP entry not removed")
					}
				case "mcp.enable":
					if observed.MCP.Servers["fixture"].Enabled != (input["state"] == "enable") {
						t.Fatal("MCP enabled state unchanged")
					}
				case "mcp.policy":
					if observed.MCP.Servers["fixture"].Policy != input["value"] {
						t.Fatal("MCP policy not updated")
					}
				case "mcp.direct-policy":
					if observed.MCP.Servers["fixture"].DirectPolicy != input["value"] {
						t.Fatal("direct MCP policy not updated")
					}
				case "mcp.test":
					if observed.MCP.Servers["fixture"].ProbedAt == "" || len(observed.MCP.Servers["fixture"].Tools) != 1 {
						t.Fatal("MCP discovery did not persist inventory")
					}
				case "mcp.auth", "mcp.header", "mcp.auth.remove":
					private, err := (secrets.Store{Path: a.Store.Paths.Secrets}).Load()
					if err != nil {
						t.Fatal(err)
					}
					credential := private.MCP[observed.MCP.Servers["fixture"].ID]
					if action.ID == "mcp.auth" && credential.Bearer != input["secret"] {
						t.Fatal("private bearer was not stored")
					}
					if action.ID == "mcp.header" && (credential.Headers[input["header"]] != input["secret"] || credential.Bearer != "existing-fixture-secret") {
						t.Fatal("private header changed existing bearer or was not stored")
					}
					if action.ID == "mcp.auth.remove" && (credential.Bearer != "" || len(credential.Headers) != 0) {
						t.Fatal("private MCP auth not cleared")
					}
				case "servers.add":
					if observed.Connections.Servers["new-server"].Alias != "new-server" || len(observed.Connections.Servers) != 2 {
						t.Fatal("server add overwrote another profile")
					}
				case "servers.edit":
					if observed.Connections.Servers["fixture"].Purpose != "updated" || observed.Connections.Servers["fixture"].Priority != 2 {
						t.Fatal("server metadata not updated")
					}
				case "servers.toggle":
					if observed.Connections.Servers["fixture"].Enabled != (input["state"] == "enable") {
						t.Fatal("server enabled state not updated")
					}
				case "servers.remove":
					if _, ok := observed.Connections.Servers["fixture"]; ok {
						t.Fatal("server profile not removed")
					}
				case "capability.enable", "capability.disable", "capability.pin", "capability.unpin":
					policy := observed.Skills.Sources[input["source"]]
					if action.ID == "capability.enable" && policy.Disabled || action.ID == "capability.disable" && !policy.Disabled || action.ID == "capability.pin" && !policy.Pinned || action.ID == "capability.unpin" && policy.Pinned {
						t.Fatal("capability policy not updated")
					}
				case "profile.apply":
					if config.EffectiveAutomationProfile(observed) != input["profile"] {
						t.Fatal("profile apply mismatch")
					}
				case "profile.restore":
					if config.EffectiveAutomationProfile(observed) != config.EffectiveAutomationProfile(cfg) {
						t.Fatal("profile restore mismatch")
					}
				case "session.stop":
					current, err := (session.Store{Root: a.Store.Paths.SessionsDir}).Get(id)
					if err != nil || current.Active() {
						t.Fatal("session stop did not finalize process-free session")
					}
				case "project.init":
					if _, err := os.Stat(filepath.Join(project, projectpkg.MarkerName)); err != nil {
						t.Fatal("project marker not created")
					}
				case "server.connector.add", "server.connector.remove":
					data, err := os.ReadFile(filepath.Join(layout.ConfigDir, "connectors.json"))
					if err != nil {
						t.Fatal(err)
					}
					var connectors []struct {
						Name string `json:"name"`
					}
					if err := json.Unmarshal(data, &connectors); err != nil {
						t.Fatal(err)
					}
					found := false
					for _, entry := range connectors {
						found = found || entry.Name == input["name"]
					}
					if found != (action.ID == "server.connector.add") {
						t.Fatal("connector registry did not reflect the requested mutation")
					}
				case "server.gateway.configure":
					gateway, err := server.LoadGatewayConfig(layout)
					if err != nil || gateway.PublicURL != input["url"] || gateway.ListenAddress != input["listen"] {
						t.Fatal("gateway configuration mismatch", err)
					}
				case "server.enrollment.create", "server.enrollment.revoke", "server.web-access.create", "server.web-access.revoke":
					registry := "enrollment"
					if strings.HasPrefix(action.ID, "server.web-access.") {
						registry = "web_access"
					}
					if before[registry] == after[registry] {
						t.Fatal("authorized enrollment or access mutation did not update its registry")
					}
				case "server.backup":
					if info, err := os.Stat(input["path"]); err != nil || info.Size() == 0 {
						t.Fatal("backup file not created")
					}
				}
			}
			records = append(records, record)
		})
	}
	encoded, _ := json.MarshalIndent(map[string]any{"schema_version": 1, "scope": "isolated_semantic_HTTP_contract_matrix", "official_provider_inference": false, "production_systemd": false, "records": records}, "", "  ")
	if path := os.Getenv("IVOAI_ADMIN_OPERATION_MATRIX_OUTPUT"); path != "" {
		if err := os.WriteFile(path, append(encoded, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("applicable_action_records=%d; completed/dependency/unprivileged cases remain separate", len(records))
}

// These are concrete fixture boundaries, not a generic allowance for failures.
// A changed error or a newly failing action requires investigation.
var unavailableAdministrativePreconditions = map[string]string{
	"capability.rollback":     "no previous native revision is available",
	"doctor.repair-hooks":     "memory hooks are disabled",
	"remote.connector.list":   "no ivoai server is connected",
	"remote.doctor":           "no ivoai server is connected",
	"remote.status":           "no ivoai server is connected",
	"rollback":                "inspect rollback binary:",
	"server.context.document": "context document unavailable",
	"server.context.ingest":   "context ingestion failed or already in progress",
	"server.context.search":   "context search failed",
	"server.logs":             "isolated systemd controller unavailable",
	"server.memory.status":    "ai-memory is unavailable",
	"server.restart":          "isolated systemd controller unavailable",
	"server.restore":          "lookup ivoai service user:",
	"server.start":            "isolated systemd controller unavailable",
	"server.stop":             "isolated systemd controller unavailable",
	"session.scope.adopt":     "SCOPE_ADOPTION_REQUIRES_INACTIVE_MANAGED_CONVERSATION",
	"session.scope.restore":   "SCOPE_ADOPTION_REQUIRES_INACTIVE_MANAGED_CONVERSATION",
	"update":                  "isolated dependency unavailable",
	"update.dry-run":          "isolated dependency unavailable",
}

var expectedConfigurationTargets = map[string]string{
	"capabilities.ponytail": "skills.ponytail", "config.headroom": "headroom.enabled", "config.memory": "memory.enabled", "config.ruflo": "orchestration.enabled", "config.auto": "orchestration.auto.enabled", "config.auto-plan": "orchestration.auto.plan_execution", "config.auto-planner": "orchestration.auto.default_planner", "config.auto-failover": "orchestration.auto.automatic_failover", "config.auto-checkpoint": "orchestration.auto.checkpoint_enabled", "config.auto-parallel": "orchestration.auto.optimization.parallelism", "config.auto-bootstrap": "orchestration.auto.optimization.shared_context_bootstrap", "config.auto-escalation": "orchestration.auto.optimization.progressive_escalation", "config.session-mode": "orchestration.default_mode", "config.primary": "orchestration.primary_executor", "config.reviewer": "orchestration.review_executor", "config.workers": "orchestration.max_workers", "policy.sources": "orchestration.auto.knowledge_routing", "policy.concurrency": "orchestration.auto.concurrency", "policy.worker-cap": "orchestration.auto.worker_cap", "policy.writes": "orchestration.auto.parallel_writes", "policy.provider": "orchestration.auto.provider_preference", "policy.low-quota": "orchestration.auto.low_quota_threshold",
}

func configurationValue(c config.Config, key string) string {
	value := reflect.ValueOf(c)
	for _, part := range strings.Split(key, ".") {
		found := false
		for i := 0; i < value.NumField(); i++ {
			if strings.Split(value.Type().Field(i).Tag.Get("toml"), ",")[0] == part {
				value = value.Field(i)
				found = true
				break
			}
		}
		if !found {
			return "<missing>"
		}
	}
	return fmt.Sprint(value.Interface())
}
