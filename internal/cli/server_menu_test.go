package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/connections"
	"github.com/ivo-lopes/ivoai/internal/secrets"
)

const menuEnrollmentFixture = "ivoai-enroll_0123456789abcdef_fixture-not-a-real-code"

func menuServerFixture(t *testing.T, token string, reject bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/ivoai":
			_ = json.NewEncoder(w).Encode(connections.Discovery{ProtocolVersion: 1, HealthEndpoint: "/health", ReadyEndpoint: "/ready", ContextMCPEndpoint: "/context", MemoryMCPEndpoint: "/memory", MemoryHooksEndpoint: "/hooks", EnrollmentEndpoint: "/enroll", Features: map[string]bool{"context": true, "memory": true}})
		case "/health", "/ready":
			status := "ready"
			if r.URL.Path == "/health" {
				status = "healthy"
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
		case "/enroll":
			calls.Add(1)
			if reject || r.Header.Get("Authorization") != "Ivoai-Enrollment "+menuEnrollmentFixture {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"token": token, "client_id": "fixture-client", "scopes": []string{"memory:read", "context:read"}})
		case "/context", "/memory":
			if r.Header.Get("Authorization") != "Bearer "+token {
				t.Error("credential crossover in server probe")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"tools": []any{}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s, calls
}

func TestMultiServerTUICompleteLifecycleWithoutOverwrite(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	for _, item := range []string{"CONFIG", "DATA", "STATE", "CACHE"} {
		t.Setenv("XDG_"+item+"_HOME", filepath.Join(root, strings.ToLower(item)))
	}
	t.Setenv("IVOAI_TEST_MODE", "1")
	t.Setenv("NO_COLOR", "1")
	aServer, aCalls := menuServerFixture(t, "fixture-private-a", false)
	bServer, _ := menuServerFixture(t, "fixture-private-b", false)
	cServer, _ := menuServerFixture(t, "fixture-private-c", true)
	var output bytes.Buffer
	newApp := func(input string) *app.App {
		a, err := app.New("test", strings.NewReader(input), &output, &output)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	a := newApp("")
	cfg := config.Default()
	cfg.Memory.Enabled = false
	cfg.MCP.Servers["personal"] = config.MCPServer{URL: "https://personal.example.invalid/mcp", Enabled: true, Kind: "external"}
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	runMenu := func(input string) {
		t.Helper()
		// Full public launcher -> Connections -> IVOAI Servers, not a UI helper.
		if err := Run(context.Background(), newApp("4\n6\n"+input+"0\n0\n0\n"), nil); err != nil {
			t.Fatal(err)
		}
	}
	add := func(alias, endpoint string) string {
		return "2\n" + alias + "\n" + alias + "\n" + endpoint + "\n" + menuEnrollmentFixture + "\n"
	}
	runMenu("1\n" + add("company-a", aServer.URL) + add("company-b", bServer.URL) + "1\n")
	before, err := a.Store.Load()
	if err != nil || len(before.Connections.Servers) != 2 {
		t.Fatal("second TUI enrollment did not preserve two profiles", err)
	}
	first, second := before.Connections.Servers["company-a"], before.Connections.Servers["company-b"]
	if first.ID == second.ID {
		t.Fatal("duplicate profile identity")
	}
	private := secrets.Store{Path: a.Store.Paths.Secrets}
	credentials, err := private.Load()
	if err != nil || credentials.Servers[first.ID].Token != "fixture-private-a" || credentials.Servers[second.ID].Token != "fixture-private-b" {
		t.Fatal("credential binding mismatch", err)
	}
	// Reload/new App on every menu invocation proves persistence independently.
	runMenu("3\n1\n0\n4\n1\n0\n")
	if !strings.Contains(output.String(), "Health: healthy") {
		t.Fatal("individual authenticated test results missing")
	}
	runMenu("3\n2\n0\n")
	after, _ := a.Store.Load()
	if after.Connections.Servers["company-a"].Enabled || !reflect.DeepEqual(after.Connections.Servers["company-b"], second) {
		t.Fatal("disable changed another profile")
	}
	if now, _ := private.Load(); !reflect.DeepEqual(now, credentials) {
		t.Fatal("disable modified credentials")
	}
	runMenu("3\n2\n0\n")
	runMenu("4\n5\nREMOVE company-b\n")
	after, _ = a.Store.Load()
	if len(after.Connections.Servers) != 1 || !reflect.DeepEqual(after.Connections.Servers["company-a"], first) {
		t.Fatal("selective TUI removal changed A")
	}
	if now, _ := private.Load(); len(now.Servers) != 1 || now.Servers[first.ID].Token != "fixture-private-a" {
		t.Fatal("selective credential removal failed")
	}
	runMenu(add("company-b", bServer.URL))
	baselineConfig, _ := os.ReadFile(a.Store.Paths.Config)
	baselineSecrets, _ := os.ReadFile(a.Store.Paths.Secrets)
	runMenu(add("company-c", cServer.URL))
	assertUnchanged := func() {
		t.Helper()
		gotConfig, _ := os.ReadFile(a.Store.Paths.Config)
		gotSecrets, _ := os.ReadFile(a.Store.Paths.Secrets)
		if !bytes.Equal(gotConfig, baselineConfig) || !bytes.Equal(gotSecrets, baselineSecrets) {
			t.Fatal("failed/cancelled action changed profiles or credentials")
		}
	}
	assertUnchanged()
	count := aCalls.Load()
	runMenu("2\ncompany-a\n") // Collision rejected before asking for another code.
	assertUnchanged()
	if aCalls.Load() != count {
		t.Fatal("alias collision consumed an enrollment")
	}
	runMenu("3\n5\nwrong confirmation\n0\n")
	assertUnchanged()
	// Explicit re-enrollment preserves metadata, identity and other profiles.
	runMenu("3\n3\nadministrative\n-\n15\n4\n\nRE-ENROLL company-a\n" + menuEnrollmentFixture + "\n0\n")
	after, _ = a.Store.Load()
	if p := after.Connections.Servers["company-a"]; p.ID != first.ID || p.Purpose != "administrative" || p.Priority != 15 {
		t.Fatal("metadata/identity lost across explicit re-enrollment")
	}
	if !reflect.DeepEqual(after.MCP.Servers["personal"], cfg.MCP.Servers["personal"]) {
		t.Fatal("personal MCP modified")
	}
	for _, secret := range []string{menuEnrollmentFixture, "fixture-private-a", "fixture-private-b", "fixture-private-c"} {
		public, _ := os.ReadFile(a.Store.Paths.Config)
		if strings.Contains(output.String(), secret) || bytes.Contains(public, []byte(secret)) {
			t.Fatal("secret leaked into public config or TUI")
		}
	}
	if info, _ := os.Stat(a.Store.Paths.Secrets); info.Mode().Perm() != 0o600 {
		t.Fatal("secret store mode not private")
	}
	for _, command := range [][]string{{"connect", "server", "disable", "company-b"}, {"connect", "server", "enable", "company-b"}, {"connect", "server", "edit", "company-b", "--purpose", "company-b"}, {"connect", "server", "list", "--json"}} {
		if err := Run(context.Background(), newApp(""), command); err != nil {
			t.Fatal(err)
		}
	}
	if err := Run(context.Background(), newApp(menuEnrollmentFixture), []string{"connect", "server", "add", "company-a", "--url", aServer.URL, "--code-stdin"}); err == nil {
		t.Fatal("CLI add silently replaced existing alias")
	}
	if aCalls.Load() != count+1 {
		t.Fatal("unexpected enrollment from duplicate CLI add")
	}
}

func TestServerMenuLegacyDefaultAndStaleHealth(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	for _, item := range []string{"CONFIG", "DATA", "STATE", "CACHE"} {
		t.Setenv("XDG_"+item+"_HOME", filepath.Join(root, item))
	}
	var output bytes.Buffer
	a, _ := app.New("test", strings.NewReader(""), &output, &output)
	cfg := config.Default()
	cfg.Memory.Enabled = false
	cfg.Connections.Server = config.Connection{Status: "connected", URL: "https://legacy.example.invalid", Protocol: 1}
	cfg.MCP.Servers["ivoai-context"] = config.MCPServer{URL: "https://legacy.example.invalid/context", Kind: "context", Enabled: true}
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	private := secrets.Store{Path: a.Store.Paths.Secrets}
	if err := private.Set(config.LegacyServerID, secrets.ClientCredential{Token: "fixture-legacy", ClientID: "legacy"}); err != nil {
		t.Fatal(err)
	}
	server, _ := menuServerFixture(t, "fixture-new", false)
	if err := Run(context.Background(), a, []string{"connect", "server", "add", "company-b", "--url", server.URL, "--enrollment-code", menuEnrollmentFixture}); err != nil {
		t.Fatal(err)
	}
	values, _ := a.ListServers()
	if len(values) != 2 {
		t.Fatal("legacy default was overwritten")
	}
	value, _ := a.ShowServer("default")
	s := menuSession{serverHealth: map[string]serverObservation{value.ID: {value: value, at: time.Now().Add(-2 * time.Minute)}}}
	old := s.serverHealth[value.ID]
	old.value.Health = connections.ProfileHealth{State: "healthy", Probed: true}
	s.serverHealth[value.ID] = old
	if got := s.observedServer(value); got.Health.State != "not_probed" || got.Health.Probed {
		t.Fatal("stale health was presented as current")
	}
	if err := a.SetServerEnabled("default", false); err != nil {
		t.Fatal(err)
	}
	if err := a.SetServerEnabled("default", true); err != nil {
		t.Fatal(err)
	}
	if c, ok, _ := private.Get(config.LegacyServerID); !ok || c.Token != "fixture-legacy" {
		t.Fatal("legacy credential not preserved")
	}
	if err := a.DisconnectServerProfile(context.Background(), "company-b", false); err != nil {
		t.Fatal(err)
	}
	values, _ = a.ListServers()
	if len(values) != 1 || values[0].ID != config.LegacyServerID {
		t.Fatal("default removed with another profile")
	}
	fmt.Fprintln(&output, "legacy default preserved")
}
