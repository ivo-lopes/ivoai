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

func TestAdministrativeCatalogCoversEveryPublicMenuItemAndValidatedChoices(t *testing.T) {
	rows := admin.ActionCatalog()
	byID := map[string]admin.Action{}
	for _, a := range rows {
		if _, exists := byID[a.ID]; exists {
			t.Fatal("duplicate action", a.ID)
		}
		byID[a.ID] = a
		if a.Fields == nil {
			t.Fatal("null fields", a.ID)
		}
		if a.Kind != "read" && a.Kind != "mutation" && a.Kind != "information" && a.Kind != "native_handoff" {
			t.Fatal("unknown kind", a.ID)
		}
		if a.Confirmation != (a.Kind == "mutation") {
			t.Fatal("incorrect confirmation", a.ID)
		}
	}
	for _, id := range PublicMenuActionIDs() {
		if _, ok := byID[id]; !ok {
			t.Fatal("administrative menu item omitted", id)
		}
	}
	// Execute every enumerated configuration choice through the production CLI
	// parser and App validator, rather than accepting a catalog that only renders.
	root := t.TempDir()
	t.Setenv("HOME", root)
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, filepath.Join(root, key))
	}
	a, err := app.New("test", strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Store.Save(config.Default()); err != nil {
		t.Fatal(err)
	}
	keys := map[string]string{
		"config.headroom": "headroom.enabled", "config.memory": "memory.enabled", "config.ruflo": "orchestration.enabled", "config.auto": "orchestration.auto.enabled", "config.auto-plan": "orchestration.auto.plan_execution", "config.auto-planner": "orchestration.auto.default_planner", "config.auto-failover": "orchestration.auto.automatic_failover", "config.auto-checkpoint": "orchestration.auto.checkpoint_enabled", "config.auto-parallel": "orchestration.auto.optimization.parallelism", "config.auto-bootstrap": "orchestration.auto.optimization.shared_context_bootstrap", "config.auto-escalation": "orchestration.auto.optimization.progressive_escalation", "config.session-mode": "orchestration.default_mode", "config.primary": "orchestration.primary_executor", "config.reviewer": "orchestration.review_executor", "config.workers": "orchestration.max_workers", "policy.sources": "orchestration.auto.knowledge_routing", "policy.concurrency": "orchestration.auto.concurrency", "policy.provider": "orchestration.auto.provider_preference",
	}
	for id, key := range keys {
		for _, value := range byID[id].Fields[0].Options {
			if err := administrativeDispatch(a)(context.Background(), []string{"config", "set", key, value}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
				t.Fatalf("%s option %s: %v", id, value, err)
			}
		}
	}
}
func TestProductionAdministrativeHTTPDispatcherMutatesRealStoreAndProtectsCredentials(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, filepath.Join(root, key))
	}
	a, err := app.New("0.16.0", strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Store.Save(config.Default()); err != nil {
		t.Fatal(err)
	}
	origin := "http://127.0.0.1:12345"
	s, err := admin.New(admin.Options{App: a, Origin: origin, Dispatch: administrativeDispatch(a)})
	if err != nil {
		t.Fatal(err)
	}
	store, err := admin.AccessStore(a.Store.Paths)
	if err != nil {
		t.Fatal(err)
	}
	activation, err := store.CreateActivation(time.Hour, []string{admin.ScopeRead, admin.ScopeManage})
	if err != nil {
		t.Fatal(err)
	}
	redirect := origin + "/auth/callback"
	client, err := store.RegisterClient("test", []string{redirect})
	if err != nil {
		t.Fatal(err)
	}
	verifier := strings.Repeat("v", 43)
	nonce, err := store.BeginAuthorization(client.ID, redirect, webauth.PKCEChallenge(verifier), "test", s.Resource(), []string{admin.ScopeRead, admin.ScopeManage})
	if err != nil {
		t.Fatal(err)
	}
	code, _, _, err := store.AuthorizeRequest(activation.Code, nonce)
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.ExchangeCode(code, client.ID, redirect, verifier, s.Resource())
	if err != nil {
		t.Fatal(err)
	}
	// Cookie is intentionally private to the HTTP layer; recover its name from
	// the normal login flow's naming policy through a GET /auth/start response.
	start := httptest.NewRequest("GET", origin+"/auth/start", nil)
	start.RemoteAddr = "127.0.0.1:1"
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, start)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("flow cookie missing")
	}
	cookieName := strings.TrimSuffix(cookies[0].Name, "_flow")
	request := func(method, path string, body any, csrf string) *httptest.ResponseRecorder {
		var b []byte
		if body != nil {
			b, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, origin+path, bytes.NewReader(b))
		req.RemoteAddr = "127.0.0.1:1"
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: cookieName, Value: token.AccessToken})
		if csrf != "" {
			req.Header.Set("X-IVOAI-CSRF", csrf)
		}
		out := httptest.NewRecorder()
		s.Handler().ServeHTTP(out, req)
		return out
	}
	csrfResponse := request("GET", "/api/admin/v1/csrf", nil, "")
	var csrf struct {
		Token  string `json:"csrf_token"`
		Access string `json:"access"`
	}
	if json.Unmarshal(csrfResponse.Body.Bytes(), &csrf) != nil || csrf.Access != "manage" {
		t.Fatal("write scope not admitted")
	}
	input := map[string]string{"value": "false"}
	preview := request("POST", "/api/admin/v1/actions/config.auto", map[string]any{"phase": "preview", "input": input}, csrf.Token)
	var receipt struct {
		Token string `json:"confirmation"`
	}
	if json.Unmarshal(preview.Body.Bytes(), &receipt) != nil || receipt.Token == "" {
		t.Fatal("preview failed", preview.Body)
	}
	execute := map[string]any{"phase": "execute", "input": input, "confirmation": receipt.Token, "request_id": "production-request-1234"}
	result := request("POST", "/api/admin/v1/actions/config.auto", execute, csrf.Token)
	if result.Code != 200 || !strings.Contains(result.Body.String(), `"status":"completed"`) {
		t.Fatal("real operation failed", result.Body)
	}
	cfg, err := a.Store.Load()
	if err != nil || cfg.Orchestration.Auto.Enabled {
		t.Fatal("real store unchanged")
	}
	if replay := request("POST", "/api/admin/v1/actions/config.auto", execute, csrf.Token); replay.Code != 409 {
		t.Fatal("production replay accepted")
	}
	before, err := os.ReadFile(a.Store.Paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	read := request("POST", "/api/admin/v1/actions/config.show", map[string]any{"phase": "execute", "input": map[string]string{}}, csrf.Token)
	if read.Code != 200 || strings.Contains(read.Body.String(), "[mcp]") {
		t.Fatal("raw configuration exposed")
	}
	after, _ := os.ReadFile(a.Store.Paths.Config)
	if !bytes.Equal(before, after) {
		t.Fatal("read action mutated")
	}
}
