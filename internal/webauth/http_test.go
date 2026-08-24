package webauth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestOAuthHTTPFlowAndMetadata(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	approvedScopes := []string{ScopeContextRead, ScopeMemoryRead, ScopeMemoryWrite}
	activation, _ := store.CreateActivation(time.Minute, approvedScopes)
	server := &Server{Store: store, Issuer: "https://ivoai.example"}
	mux := http.NewServeMux()
	server.Register(mux)
	for _, endpoint := range []string{"/.well-known/oauth-authorization-server", "/.well-known/oauth-protected-resource"} {
		metadataRequest := httptest.NewRequest(http.MethodGet, endpoint, nil)
		metadataResponse := httptest.NewRecorder()
		mux.ServeHTTP(metadataResponse, metadataRequest)
		if metadataResponse.Code != http.StatusOK || strings.Contains(metadataResponse.Body.String(), ScopeMemoryDelete) {
			t.Fatalf("destructive scope advertised by %s: %d %s", endpoint, metadataResponse.Code, metadataResponse.Body.String())
		}
	}
	reg := httptest.NewRequest("POST", "/oauth/register", strings.NewReader(`{"client_name":"ChatGPT","redirect_uris":["https://chat.example/cb"],"token_endpoint_auth_method":"none","grant_types":["authorization_code","refresh_token"],"response_types":["code"],"client_uri":"https://chat.example"}`))
	reg.RemoteAddr = "192.0.2.1:123"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, reg)
	if rec.Code != 201 {
		t.Fatalf("register=%d %s", rec.Code, rec.Body.String())
	}
	var client struct {
		ID    string `json:"client_id"`
		Scope string `json:"scope"`
	}
	json.Unmarshal(rec.Body.Bytes(), &client)
	if client.Scope != strings.Join(DefaultScopes, " ") || strings.Contains(client.Scope, ScopeMemoryDelete) {
		t.Fatalf("unsafe registration defaults: %#v", client)
	}
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	missingResource := httptest.NewRequest("GET", "/oauth/authorize?response_type=code&client_id="+client.ID+"&redirect_uri="+url.QueryEscape("https://chat.example/cb")+"&scope=context%3Aread&state=s&code_challenge_method=S256&code_challenge="+PKCEChallenge(verifier), nil)
	missingResource.RemoteAddr = "192.0.2.1:122"
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, missingResource)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing resource status=%d", rec.Code)
	}
	authorizeURL := "/oauth/authorize?response_type=code&client_id=" + client.ID + "&redirect_uri=" + url.QueryEscape("https://chat.example/cb") + "&resource=" + url.QueryEscape(server.Resource()) + "&scope=" + url.QueryEscape(strings.Join(DefaultScopes, " ")) + "&state=s1&code_challenge_method=S256&code_challenge=" + PKCEChallenge(verifier)
	get := httptest.NewRequest("GET", authorizeURL, nil)
	get.RemoteAddr = "192.0.2.1:124"
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, get)
	if rec.Code != 200 || len(rec.Result().Cookies()) != 1 {
		t.Fatalf("authorize form=%d", rec.Code)
	}
	cookie := rec.Result().Cookies()[0]
	body := rec.Body.String()
	start := strings.Index(body, `name="csrf" value="`)
	if start < 0 {
		t.Fatal("CSRF hidden field missing")
	}
	start += len(`name="csrf" value="`)
	csrf := body[start : strings.Index(body[start:], `"`)+start]
	form := url.Values{"csrf": {csrf}, "activation_code": {activation.Code}}
	post := httptest.NewRequest("POST", "/oauth/authorize", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.AddCookie(cookie)
	post.RemoteAddr = "192.0.2.1:125"
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, post)
	if rec.Code != 302 {
		t.Fatalf("authorize=%d %s", rec.Code, rec.Body.String())
	}
	location, _ := url.Parse(rec.Header().Get("Location"))
	code := location.Query().Get("code")
	if location.Query().Get("state") != "s1" || code == "" || location.Query().Has("scope") {
		t.Fatalf("redirect state/code invalid or redundant scope returned: %s", location.String())
	}
	tokenForm := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {client.ID}, "redirect_uri": {"https://chat.example/cb"}, "code_verifier": {verifier}, "resource": {server.Resource()}}
	missingTokenResource := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {client.ID}, "redirect_uri": {"https://chat.example/cb"}, "code_verifier": {verifier}}
	badTokenReq := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(missingTokenResource.Encode()))
	badTokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	badTokenReq.RemoteAddr = "192.0.2.1:127"
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, badTokenReq)
	if rec.Code != 400 {
		t.Fatalf("missing token resource status=%d", rec.Code)
	}
	tokenReq := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(tokenForm.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenReq.RemoteAddr = "192.0.2.1:126"
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, tokenReq)
	if rec.Code != 200 {
		t.Fatalf("token=%d %s", rec.Code, rec.Body.String())
	}
	var tokens Tokens
	if err := json.Unmarshal(rec.Body.Bytes(), &tokens); err != nil || tokens.Scope != strings.Join(approvedScopes, " ") {
		t.Fatalf("token response over-granted scopes: %#v err=%v", tokens, err)
	}
	if strings.Contains(rec.Body.String(), activation.Code) {
		t.Fatal("activation leaked")
	}
}

func TestDestructiveScopeRemainsExplicitlyAvailable(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "state.json"))
	activation, err := store.CreateActivation(time.Minute, []string{ScopeMemoryDelete})
	if err != nil || !contains(activation.Scopes, ScopeMemoryDelete) {
		t.Fatalf("explicit destructive activation unavailable: %#v err=%v", activation, err)
	}
}

func TestRedirectValidation(t *testing.T) {
	for _, raw := range []string{"http://example.com/cb", "https://user@example.com/cb", "javascript:alert(1)"} {
		if ValidateRedirectURI(raw) == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestRegistrationDistinguishesClientErrorsFromStoreFailures(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := os.Mkdir(statePath, 0o700); err != nil {
		t.Fatal(err)
	}
	server := &Server{Store: NewStore(statePath), Issuer: "https://ivoai.example"}
	mux := http.NewServeMux()
	server.Register(mux)

	for _, test := range []struct {
		name       string
		body       string
		wantStatus int
		wantError  string
	}{
		{
			name:       "invalid redirect",
			body:       `{"client_name":"ChatGPT","redirect_uris":["http://example.com/callback"]}`,
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid_redirect_uri",
		},
		{
			name:       "persistent store failure",
			body:       `{"client_name":"ChatGPT","redirect_uris":["https://example.com/callback"]}`,
			wantStatus: http.StatusInternalServerError,
			wantError:  "server_error",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(test.body))
			request.RemoteAddr = "192.0.2.50:1234"
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), `"error":"`+test.wantError+`"`) {
				t.Fatalf("registration response=%d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestRateLimiterHandlesIPv6AndBoundsPeers(t *testing.T) {
	server := &Server{}
	for i := 0; i < 60; i++ {
		request := httptest.NewRequest("GET", "/", nil)
		request.RemoteAddr = "[2001:db8::1]:" + strconv.Itoa(1000+i)
		if !server.allowed(request) {
			t.Fatalf("IPv6 request %d rejected early", i)
		}
	}
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "[2001:db8::1]:9999"
	if server.allowed(request) {
		t.Fatal("IPv6 peer rate limit not enforced")
	}
	server = &Server{}
	for i := 0; i < 1500; i++ {
		request := httptest.NewRequest("GET", "/", nil)
		request.RemoteAddr = fmt.Sprintf("192.0.%d.%d:1", (i/250)%250, i%250)
		_ = server.allowed(request)
	}
	if len(server.peers) > 1025 {
		t.Fatalf("peer map grew to %d", len(server.peers))
	}
}
