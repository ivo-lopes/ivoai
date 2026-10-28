// Package admin exposes an explicitly started, local-owner, read-only view of
// the existing App. It neither schedules work nor grants agent capabilities.
package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/webauth"
)

const ScopeRead = "admin:read"
const maxResponseBytes = 512 << 10

//go:embed assets/index.html assets/admin.css assets/admin.js
var assets embed.FS

type Audit struct{ Action, Verdict, Actor string }
type Options struct {
	App    *app.App
	Origin string
	Audit  func(Audit)
}
type flow struct {
	verifier, client string
	expires          time.Time
}
type Server struct {
	app                                  *app.App
	auth                                 *webauth.Store
	oauth                                *webauth.Server
	origin, instance, cookie, flowCookie string
	paths                                config.Paths
	configRoot, stateRoot                os.FileInfo
	slots                                chan struct{}
	audit                                func(Audit)
	mu                                   sync.Mutex
	flows                                map[string]flow
	client                               string
	rateStart                            time.Time
	rateCount                            int
}

// AccessStore uses the existing OAuth store with a disjoint scope policy. Only
// its own private directory under the host StateRoot is created, never client
// configuration, credentials, or permission changes to an existing root.
func AccessStore(paths config.Paths) (*webauth.Store, error) {
	if !filepath.IsAbs(paths.StateDir) || !filepath.IsAbs(paths.ConfigDir) {
		return nil, errors.New("admin requires absolute host roots")
	}
	for _, path := range []string{paths.StateDir, paths.ConfigDir} {
		info, err := os.Lstat(path)
		if err != nil || !privateOwner(info) {
			return nil, errors.New("admin requires existing private host roots")
		}
	}
	dir := filepath.Join(paths.StateDir, "admin")
	if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return nil, errors.New("admin state unavailable")
	}
	info, err := os.Lstat(dir)
	if err != nil || !privateOwner(info) {
		return nil, errors.New("admin state must be a private directory")
	}
	dir = filepath.Join(dir, instanceID(paths))
	if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return nil, errors.New("admin state unavailable")
	}
	info, err = os.Lstat(dir)
	if err != nil || !privateOwner(info) {
		return nil, errors.New("admin state must be a private directory")
	}
	return webauth.NewScopedStore(filepath.Join(dir, "oauth.json"), []string{ScopeRead}, []string{ScopeRead})
}

func privateOwner(info os.FileInfo) bool {
	if info == nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Geteuid()
}
func instanceID(paths config.Paths) string {
	sum := sha256.Sum256([]byte(filepath.Clean(paths.ConfigDir) + "\x00" + filepath.Clean(paths.StateDir)))
	return hex.EncodeToString(sum[:16])
}

func New(opts Options) (*Server, error) {
	if opts.App == nil || opts.App.Store == nil {
		return nil, errors.New("admin requires the local App")
	}
	u, err := url.Parse(opts.Origin)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() || u.Port() == "" || u.Port() == "0" {
		return nil, errors.New("admin origin must be an explicit HTTP loopback listener")
	}
	store, err := AccessStore(opts.App.Store.Paths)
	if err != nil {
		return nil, err
	}
	paths := opts.App.Store.Paths
	cfgRoot, err := os.Lstat(paths.ConfigDir)
	if err != nil {
		return nil, errors.New("admin roots unavailable")
	}
	stateRoot, err := os.Lstat(paths.StateDir)
	if err != nil {
		return nil, errors.New("admin roots unavailable")
	}
	instance := instanceID(paths)
	cookieSum := sha256.Sum256([]byte(opts.Origin + instance))
	cookie := "ivoai_admin_" + hex.EncodeToString(cookieSum[:8])
	s := &Server{app: opts.App, auth: store, origin: opts.Origin, instance: instance, cookie: cookie, flowCookie: cookie + "_flow", paths: paths, configRoot: cfgRoot, stateRoot: stateRoot, slots: make(chan struct{}, 8), audit: opts.Audit, flows: map[string]flow{}}
	s.oauth = &webauth.Server{Store: store, Issuer: opts.Origin, ResourcePath: "/api/admin/v1/" + instance, CSRFCookieName: cookie + "_oauth_csrf"}
	return s, nil
}

func (s *Server) Resource() string { return s.oauth.Resource() }
func (s *Server) rootValid() bool {
	if s.app.Store.Paths != s.paths {
		return false
	}
	for _, pair := range []struct {
		path string
		info os.FileInfo
	}{{s.paths.ConfigDir, s.configRoot}, {s.paths.StateDir, s.stateRoot}} {
		now, err := os.Lstat(pair.path)
		if err != nil || !privateOwner(now) || !os.SameFile(now, pair.info) {
			return false
		}
	}
	return true
}
func (s *Server) emit(action, verdict, actor string) {
	if s.audit != nil {
		if actor != "" {
			digest := sha256.Sum256([]byte(actor))
			actor = hex.EncodeToString(digest[:8])
		}
		s.audit(Audit{action, verdict, actor})
	}
}
func errorJSON(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
func response(w http.ResponseWriter, value any) {
	body, err := json.Marshal(value)
	if err != nil || len(body) > maxResponseBytes {
		errorJSON(w, 503, "snapshot_unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func (s *Server) admitRate() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if now.Sub(s.rateStart) >= time.Minute {
		s.rateStart = now
		s.rateCount = 0
	}
	s.rateCount++
	return s.rateCount <= 240
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// This browser backend registers clients and exchanges/revokes tokens through
	// the existing Store. Expose only its authorization form, rather than the
	// Web MCP service's public registration, token and discovery endpoints.
	authorization := http.NewServeMux()
	s.oauth.Register(authorization)
	mux.Handle("GET /oauth/authorize", authorization)
	mux.Handle("POST /oauth/authorize", authorization)
	mux.HandleFunc("GET /oauth/", http.NotFound)
	mux.HandleFunc("POST /oauth/", http.NotFound)
	mux.HandleFunc("GET /auth/start", s.start)
	mux.HandleFunc("GET /auth/callback", s.callback)
	mux.HandleFunc("POST /auth/logout", s.logout)
	mux.HandleFunc("GET /api/admin/v1/meta", func(w http.ResponseWriter, r *http.Request) {
		response(w, map[string]any{"schema_version": 1, "authentication": "oauth_pkce_cookie", "access": "local_owner_read_only"})
	})
	mux.HandleFunc("GET /api/admin/v1/overview", s.protected("overview", s.overview))
	mux.HandleFunc("GET /api/admin/v1/sessions", s.protected("sessions", s.sessions))
	mux.HandleFunc("GET /api/admin/v1/sessions/{id}", s.protected("session", s.session))
	mux.HandleFunc("GET /api/admin/v1/sessions/{id}/events", s.protected("events", s.events))
	mux.HandleFunc("GET /api/admin/v1/components", s.protected("components", s.components))
	mux.HandleFunc("GET /api/admin/v1/connections", s.protected("connections", s.connections))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		name := ""
		switch r.URL.Path {
		case "/":
			name = "index.html"
		case "/assets/admin.js":
			name = "admin.js"
		case "/assets/admin.css":
			name = "admin.css"
		default:
			http.NotFound(w, r)
			return
		}
		body, err := fs.ReadFile(assets, "assets/"+name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		switch name {
		case "admin.js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		case "admin.css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}
		_, _ = w.Write(body)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		// OAuth's form installs its own narrow CSP. Requests must originate at the
		// listener itself: forwarded headers never confer authority.
		peer, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || net.ParseIP(peer) == nil || !net.ParseIP(peer).IsLoopback() || r.Host != strings.TrimPrefix(s.origin, "http://") || r.Header.Get("Origin") != "" && r.Header.Get("Origin") != s.origin || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			s.emit("boundary", "deny", "")
			errorJSON(w, 403, "forbidden")
			return
		}
		if !s.admitRate() {
			errorJSON(w, 429, "rate_exceeded")
			return
		}
		if len(r.URL.RawQuery) > 4096 || len(r.URL.Path) > 256 {
			errorJSON(w, 400, "invalid_request")
			return
		}
		if !s.rootValid() {
			s.emit("instance", "deny", "")
			errorJSON(w, 503, "instance_unavailable")
			return
		}
		select {
		case s.slots <- struct{}{}:
			defer func() { <-s.slots }()
		default:
			errorJSON(w, 429, "capacity_exceeded")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		// Chromium serializes Origin as null for a navigation POST from a
		// no-referrer document. Only the OAuth form may retain same-origin
		// referrers; its query contains PKCE/state, never access tokens.
		// Callback/API responses remain no-referrer, and null is still denied.
		if r.Method == http.MethodGet && r.URL.Path == "/oauth/authorize" {
			w.Header().Set("Referrer-Policy", "same-origin")
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) principal(r *http.Request) (webauth.Principal, error) {
	cookie, err := r.Cookie(s.cookie)
	if err != nil || len(cookie.Value) > 256 {
		return webauth.Principal{}, errors.New("unauthorized")
	}
	return s.auth.Authenticate(cookie.Value, s.Resource(), ScopeRead)
}
func (s *Server) protected(action string, next func(*http.Request) (any, int)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := s.principal(r)
		if err != nil {
			s.emit(action, "deny", "")
			errorJSON(w, 401, "unauthorized")
			return
		}
		value, status := next(r)
		// Read-side revalidation detects revocation/root changes during collection;
		// it does not claim an atomic transaction against a privileged local owner.
		if _, err = s.principal(r); err != nil || !s.rootValid() {
			s.emit(action, "deny", principal.ClientID)
			errorJSON(w, 401, "unauthorized")
			return
		}
		if r.Context().Err() != nil {
			status = 503
		}
		if status != 200 {
			s.emit(action, "unavailable", principal.ClientID)
			errorJSON(w, status, "snapshot_unavailable")
			return
		}
		s.emit(action, "allow", principal.ClientID)
		response(w, value)
	}
}
func randomString() (string, error) {
	var b [32]byte
	_, err := rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:]), err
}
func (s *Server) cookieValue(w http.ResponseWriter, name, value, path string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: path, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: strings.HasPrefix(s.origin, "https://"), MaxAge: maxAge})
}
func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	state, err := randomString()
	if err != nil {
		errorJSON(w, 503, "access_unavailable")
		return
	}
	verifier, err := randomString()
	if err != nil {
		errorJSON(w, 503, "access_unavailable")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.flows {
		if now.After(v.expires) {
			delete(s.flows, k)
		}
	}
	if len(s.flows) >= 32 {
		errorJSON(w, 429, "capacity_exceeded")
		return
	}
	var clientID = s.client
	if clientID == "" {
		client, err := s.auth.RegisterClient("IVOAI local admin", []string{s.origin + "/auth/callback"})
		if err != nil {
			errorJSON(w, 503, "access_unavailable")
			return
		}
		s.client = client.ID
		clientID = client.ID
	}
	if clientID == "" {
		errorJSON(w, 503, "access_unavailable")
		return
	}
	if !s.rootValid() {
		errorJSON(w, 503, "instance_unavailable")
		return
	}
	s.flows[state] = flow{verifier: verifier, client: clientID, expires: now.Add(10 * time.Minute)}
	s.cookieValue(w, s.flowCookie, state, "/auth/callback", 600)
	query := url.Values{"client_id": {clientID}, "redirect_uri": {s.origin + "/auth/callback"}, "response_type": {"code"}, "resource": {s.Resource()}, "scope": {ScopeRead}, "code_challenge_method": {"S256"}, "code_challenge": {webauth.PKCEChallenge(verifier)}, "state": {state}}
	http.Redirect(w, r, s.origin+"/oauth/authorize?"+query.Encode(), http.StatusSeeOther)
}
func (s *Server) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cookie, err := r.Cookie(s.flowCookie)
	state := q.Get("state")
	if err != nil || len(q["state"]) != 1 || len(q["code"]) != 1 || len(state) != 43 || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		s.emit("login", "deny", "")
		errorJSON(w, 403, "invalid_authorization")
		return
	}
	s.mu.Lock()
	pending, ok := s.flows[state]
	delete(s.flows, state)
	s.mu.Unlock()
	s.cookieValue(w, s.flowCookie, "", "/auth/callback", -1)
	if !ok || time.Now().After(pending.expires) || len(q.Get("code")) > 256 {
		errorJSON(w, 403, "invalid_authorization")
		return
	}
	if !s.rootValid() {
		errorJSON(w, 503, "instance_unavailable")
		return
	}
	tokens, err := s.auth.ExchangeCode(q.Get("code"), pending.client, s.origin+"/auth/callback", pending.verifier, s.Resource())
	if err != nil {
		errorJSON(w, 403, "invalid_authorization")
		return
	}
	if !s.rootValid() {
		_ = s.auth.RevokeToken(tokens.AccessToken)
		errorJSON(w, 503, "instance_unavailable")
		return
	}
	// No refresh token is exposed to the browser or retained by this layer.
	s.cookieValue(w, s.cookie, tokens.AccessToken, "/", int(tokens.ExpiresIn))
	s.emit("login", "allow", pending.client)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.origin {
		errorJSON(w, 403, "forbidden")
		return
	}
	principal, err := s.principal(r)
	if err != nil {
		errorJSON(w, 401, "unauthorized")
		return
	}
	c, _ := r.Cookie(s.cookie)
	if s.auth.RevokeToken(c.Value) != nil {
		errorJSON(w, 503, "access_unavailable")
		return
	}
	s.cookieValue(w, s.cookie, "", "/", -1)
	s.emit("logout", "allow", principal.ClientID)
	response(w, map[string]bool{"signed_out": true})
}

// ServeListener requires a pre-bound numeric loopback socket. Caller owns the
// explicit startup; there is no background service, public bind, or App launch.
func (s *Server) ServeListener(ctx context.Context, listener net.Listener) error {
	host, _, err := net.SplitHostPort(listener.Addr().String())
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || "http://"+listener.Addr().String() != s.origin {
		return errors.New("admin listener does not match instance origin")
	}
	server := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errors.New("admin listener unavailable")
	case <-ctx.Done():
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(stop)
	}
}
