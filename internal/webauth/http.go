package webauth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Server struct {
	Store  *Store
	Issuer string
	mu     sync.Mutex
	peers  map[string]rateWindow
}
type rateWindow struct {
	Start time.Time
	Count int
}

func (s *Server) Resource() string { return strings.TrimRight(s.Issuer, "/") + "/mcp" }

func (s *Server) allowed(r *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.peers == nil {
		s.peers = map[string]rateWindow{}
	}
	peer := r.RemoteAddr
	if host, _, err := net.SplitHostPort(peer); err == nil {
		peer = host
	}
	now := time.Now()
	if len(s.peers) >= 1024 {
		for key, window := range s.peers {
			if now.Sub(window.Start) >= time.Minute {
				delete(s.peers, key)
			}
		}
		if len(s.peers) >= 1024 {
			// A bounded global fallback prevents untrusted peer identifiers from
			// causing unbounded memory growth behind a large proxy fleet.
			peer = "_overflow"
		}
	}
	v := s.peers[peer]
	if now.Sub(v.Start) >= time.Minute {
		v = rateWindow{Start: now}
	}
	v.Count++
	s.peers[peer] = v
	return v.Count <= 60
}

func ValidateRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Fragment != "" || u.Host == "" {
		return errors.New("redirect URI must be an absolute URI without credentials or fragment")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1")) {
		return errors.New("redirect URI must use HTTPS (loopback HTTP is allowed)")
	}
	return nil
}
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.metadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", s.resourceMetadata)
	mux.HandleFunc("POST /oauth/register", s.register)
	mux.HandleFunc("GET /oauth/authorize", s.authorizeForm)
	mux.HandleFunc("POST /oauth/authorize", s.authorize)
	mux.HandleFunc("POST /oauth/token", s.token)
	mux.HandleFunc("POST /oauth/revoke", s.revoke)
}
func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (s *Server) metadata(w http.ResponseWriter, _ *http.Request) {
	jsonResponse(w, 200, map[string]any{"issuer": s.Issuer, "authorization_endpoint": s.Issuer + "/oauth/authorize", "token_endpoint": s.Issuer + "/oauth/token", "registration_endpoint": s.Issuer + "/oauth/register", "revocation_endpoint": s.Issuer + "/oauth/revoke", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"}, "scopes_supported": DefaultScopes})
}
func (s *Server) resourceMetadata(w http.ResponseWriter, _ *http.Request) {
	jsonResponse(w, 200, map[string]any{"resource": s.Resource(), "authorization_servers": []string{s.Issuer}, "scopes_supported": DefaultScopes, "bearer_methods_supported": []string{"header"}})
}
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if !s.allowed(r) {
		jsonResponse(w, http.StatusTooManyRequests, map[string]string{"error": "temporarily_unavailable"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var in struct {
		ClientName              string          `json:"client_name"`
		RedirectURIs            []string        `json:"redirect_uris"`
		TokenEndpointAuthMethod string          `json:"token_endpoint_auth_method"`
		GrantTypes              []string        `json:"grant_types,omitempty"`
		ResponseTypes           []string        `json:"response_types,omitempty"`
		ClientURI               string          `json:"client_uri,omitempty"`
		LogoURI                 string          `json:"logo_uri,omitempty"`
		Scope                   string          `json:"scope,omitempty"`
		Contacts                []string        `json:"contacts,omitempty"`
		TermsURI                string          `json:"tos_uri,omitempty"`
		PolicyURI               string          `json:"policy_uri,omitempty"`
		JWKsURI                 string          `json:"jwks_uri,omitempty"`
		JWKs                    json.RawMessage `json:"jwks,omitempty"`
		SoftwareID              string          `json:"software_id,omitempty"`
		SoftwareVersion         string          `json:"software_version,omitempty"`
	}
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || (in.TokenEndpointAuthMethod != "" && in.TokenEndpointAuthMethod != "none") || !supportedValues(in.GrantTypes, "authorization_code", "refresh_token") || !supportedValues(in.ResponseTypes, "code") {
		jsonResponse(w, 400, map[string]string{"error": "invalid_client_metadata"})
		return
	}
	if strings.TrimSpace(in.ClientName) == "" {
		in.ClientName = "Web MCP client"
	}
	c, err := s.Store.RegisterClient(in.ClientName, in.RedirectURIs)
	if err != nil {
		jsonResponse(w, 400, map[string]string{"error": "invalid_redirect_uri"})
		return
	}
	jsonResponse(w, 201, map[string]any{"client_id": c.ID, "client_name": c.Name, "redirect_uris": c.RedirectURIs, "token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}})
}

func supportedValues(values []string, allowed ...string) bool {
	for _, value := range values {
		found := false
		for _, candidate := range allowed {
			if value == candidate {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

var authorizePage = template.Must(template.New("authorize").Parse(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Authorize ivoai</title><style>body{font:16px system-ui;background:#111827;color:#e5e7eb;max-width:42rem;margin:3rem auto;padding:1rem}main{border:1px solid #374151;border-radius:12px;padding:2rem}input{box-sizing:border-box;width:100%;padding:.8rem;margin:.5rem 0 1rem;background:#0b1220;color:white;border:1px solid #4b5563;border-radius:7px}button{padding:.8rem 1.2rem;background:#06b6d4;color:#082f49;border:0;border-radius:7px;font-weight:bold}.scopes{color:#a78bfa}</style></head><body><main><h1>ivoai web access</h1><p>Authorize <strong>{{.Client}}</strong> to access:</p><p class="scopes">{{.Scopes}}</p><form method="post"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="state" value="{{.State}}"><label>One-time activation code</label><input type="password" name="activation_code" autocomplete="one-time-code" required><button type="submit">Authorize</button></form></main></body></html>`))

func (s *Server) parseAuthorize(r *http.Request) (Client, string, string, string, []string, error) {
	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirect := q.Get("redirect_uri")
	state := q.Get("state")
	challenge := q.Get("code_challenge")
	if len(q["resource"]) != 1 || q.Get("resource") != s.Resource() {
		return Client{}, "", "", "", nil, errors.New("exact OAuth resource is required")
	}
	if q.Get("response_type") != "" && q.Get("response_type") != "code" {
		return Client{}, "", "", "", nil, errors.New("unsupported response type")
	}
	if q.Get("code_challenge_method") != "S256" {
		return Client{}, "", "", "", nil, errors.New("PKCE S256 is required")
	}
	scopes := strings.Fields(q.Get("scope"))
	c, err := s.Store.Client(clientID, redirect)
	return c, state, redirect, challenge, scopes, err
}

func (s *Server) authorizeForm(w http.ResponseWriter, r *http.Request) {
	if !s.allowed(r) {
		http.Error(w, "Too many requests", http.StatusTooManyRequests)
		return
	}
	c, state, redirect, challenge, scopes, err := s.parseAuthorize(r)
	if err != nil {
		http.Error(w, "Invalid authorization request", 400)
		return
	}
	csrf, err := s.Store.BeginAuthorization(c.ID, redirect, challenge, state, s.Resource(), scopes)
	if err != nil {
		http.Error(w, "Invalid authorization request", 400)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "ivoai_oauth_csrf", Value: csrf, Path: "/oauth/authorize", HttpOnly: true, Secure: strings.HasPrefix(s.Issuer, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: 600})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
	_ = authorizePage.Execute(w, map[string]any{"Client": c.Name, "State": state, "CSRF": csrf, "Scopes": strings.Join(scopes, " ")})
}
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	if !s.allowed(r) {
		http.Error(w, "Too many requests", http.StatusTooManyRequests)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid authorization request", 400)
		return
	}
	cookie, err := r.Cookie("ivoai_oauth_csrf")
	if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(r.FormValue("csrf"))) != 1 {
		http.Error(w, "Invalid authorization request", 400)
		return
	}
	code, redirect, oauthState, err := s.Store.AuthorizeRequest(r.FormValue("activation_code"), cookie.Value)
	if err != nil {
		http.Error(w, "Invalid or expired activation code", 401)
		return
	}
	u, _ := url.Parse(redirect)
	q := u.Query()
	q.Set("code", code)
	q.Set("state", oauthState)
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}
func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if !s.allowed(r) {
		jsonResponse(w, 429, map[string]string{"error": "temporarily_unavailable"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		jsonResponse(w, 400, map[string]string{"error": "invalid_request"})
		return
	}
	var t Tokens
	var err error
	switch r.FormValue("grant_type") {
	case "authorization_code":
		if len(r.Form["resource"]) != 1 || r.FormValue("resource") != s.Resource() {
			err = errors.New("invalid resource")
			break
		}
		t, err = s.Store.ExchangeCode(r.FormValue("code"), r.FormValue("client_id"), r.FormValue("redirect_uri"), r.FormValue("code_verifier"), r.FormValue("resource"))
	case "refresh_token":
		if len(r.Form["resource"]) != 1 || r.FormValue("resource") != s.Resource() {
			err = errors.New("invalid resource")
			break
		}
		t, err = s.Store.Refresh(r.FormValue("refresh_token"), r.FormValue("client_id"), r.FormValue("resource"))
	default:
		err = errors.New("unsupported grant")
	}
	if err != nil {
		jsonResponse(w, 400, map[string]string{"error": "invalid_grant"})
		return
	}
	jsonResponse(w, 200, t)
}
func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	if !s.allowed(r) {
		w.WriteHeader(429)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	_ = r.ParseForm()
	token := r.FormValue("token")
	if len(token) > 4096 {
		http.Error(w, "invalid token", 400)
		return
	}
	_ = s.Store.RevokeToken(token)
	w.WriteHeader(200)
}
func Bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) < 8 || subtle.ConstantTimeCompare([]byte(strings.ToLower(h[:7])), []byte("bearer ")) != 1 {
		return ""
	}
	return h[7:]
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
