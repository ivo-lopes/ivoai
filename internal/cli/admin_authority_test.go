package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/admin"
	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/webauth"
)

func TestReadOnlyGrantCannotRefreshMCPOrDoctorPersistedState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(k, filepath.Join(root, k))
	}
	a, err := app.New("regression", strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.MCP.Servers["fixture"] = config.MCPServer{ID: "mcp_0123456789abcdef0123456789abcdef", URL: "https://127.0.0.1:1/mcp", Enabled: true, Kind: "external", AuthMode: "none", Policy: "read_only", DirectPolicy: "read_only", Health: "HEALTHY"}
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	origin := "http://127.0.0.1:12345"
	s, err := admin.New(admin.Options{App: a, Origin: origin, Dispatch: administrativeDispatch(a)})
	if err != nil {
		t.Fatal(err)
	}
	auth, err := admin.AccessStore(a.Store.Paths)
	if err != nil {
		t.Fatal(err)
	}
	mint := func(scopes []string) string {
		act, err := auth.CreateActivation(time.Minute, scopes)
		if err != nil {
			t.Fatal(err)
		}
		redirect := origin + "/auth/callback"
		client, err := auth.RegisterClient("regression", []string{redirect})
		if err != nil {
			t.Fatal(err)
		}
		verifier := strings.Repeat("v", 43)
		nonce, err := auth.BeginAuthorization(client.ID, redirect, webauth.PKCEChallenge(verifier), "state", s.Resource(), scopes)
		if err != nil {
			t.Fatal(err)
		}
		code, _, _, err := auth.AuthorizeRequest(act.Code, nonce)
		if err != nil {
			t.Fatal(err)
		}
		token, err := auth.ExchangeCode(code, client.ID, redirect, verifier, s.Resource())
		if err != nil {
			t.Fatal(err)
		}
		return token.AccessToken
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
	request := func(token, method, path string, value any, csrf string) *httptest.ResponseRecorder {
		var body []byte
		if value != nil {
			body, _ = json.Marshal(value)
		}
		req := httptest.NewRequest(method, origin+path, bytes.NewReader(body))
		req.RemoteAddr = "127.0.0.1:1"
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		if csrf != "" {
			req.Header.Set("X-IVOAI-CSRF", csrf)
		}
		out := httptest.NewRecorder()
		s.Handler().ServeHTTP(out, req)
		return out
	}
	read := mint([]string{admin.ScopeRead})
	var csrf struct {
		Token string `json:"csrf_token"`
	}
	if json.Unmarshal(request(read, "GET", "/api/admin/v1/csrf", nil, "").Body.Bytes(), &csrf) != nil {
		t.Fatal("invalid CSRF response")
	}
	before, _ := os.ReadFile(a.Store.Paths.Config)
	for _, id := range []string{"mcp.test", "doctor"} {
		input := map[string]string{}
		if id == "mcp.test" {
			input["name"] = "fixture"
		}
		for _, phase := range []string{"preview", "execute"} {
			if denied := request(read, "POST", "/api/admin/v1/actions/"+id, map[string]any{"phase": phase, "input": input}, csrf.Token); denied.Code != 403 {
				t.Fatalf("read scope reached %s %s: %d %s", id, phase, denied.Code, denied.Body)
			}
		}
	}
	after, _ := os.ReadFile(a.Store.Paths.Config)
	if !bytes.Equal(before, after) {
		t.Fatal("read grant changed protected configuration")
	}
	if _, err := os.Stat(filepath.Join(a.Store.Paths.CacheDir, "capabilities.json")); !os.IsNotExist(err) {
		t.Fatal("read grant refreshed diagnostic cache")
	}
	manage := mint([]string{admin.ScopeRead, admin.ScopeManage})
	_ = json.Unmarshal(request(manage, "GET", "/api/admin/v1/csrf", nil, "").Body.Bytes(), &csrf)
	input := map[string]string{"name": "fixture"}
	preview := request(manage, "POST", "/api/admin/v1/actions/mcp.test", map[string]any{"phase": "preview", "input": input}, csrf.Token)
	var receipt struct {
		Token string `json:"confirmation"`
	}
	_ = json.Unmarshal(preview.Body.Bytes(), &receipt)
	if preview.Code != 200 || receipt.Token == "" {
		t.Fatal("manage preview failed")
	}
	execute := map[string]any{"phase": "execute", "input": input, "confirmation": receipt.Token, "request_id": "mcp-health-refresh-1234"}
	result := request(manage, "POST", "/api/admin/v1/actions/mcp.test", execute, csrf.Token)
	// An unavailable network service is a truthful failed probe, but updating
	// its health metadata is now an explicitly approved management operation.
	if result.Code != 200 || !strings.Contains(result.Body.String(), `"status":"failed"`) {
		t.Fatal("unexpected probe outcome", result.Body)
	}
	updated, _ := a.Store.Load()
	if updated.MCP.Servers["fixture"].ProbedAt == "" || updated.MCP.Servers["fixture"].Health == "HEALTHY" {
		t.Fatal("authorized probe did not retain legitimate health refresh")
	}
	if replay := request(manage, "POST", "/api/admin/v1/actions/mcp.test", execute, csrf.Token); replay.Code != 409 {
		t.Fatal("MCP refresh replay accepted")
	}
	_ = context.Background()
}
