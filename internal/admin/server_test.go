package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/webauth"
)

func fixtureApp(t *testing.T) *app.App {
	t.Helper()
	root := t.TempDir()
	paths := config.Paths{ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"), StateDir: filepath.Join(root, "state"), CacheDir: filepath.Join(root, "cache"), BinDir: filepath.Join(root, "bin"), Config: filepath.Join(root, "config/config.toml"), State: filepath.Join(root, "state/state.toml"), Secrets: filepath.Join(root, "config/secrets.json"), Ownership: filepath.Join(root, "state/ownership.toml"), HooksDir: filepath.Join(root, "data/hooks"), SessionsDir: filepath.Join(root, "state/sessions"), QuotaDir: filepath.Join(root, "state/quota")}
	store := config.NewStore(paths)
	if err := store.Ensure(); err != nil {
		t.Fatal(err)
	}
	return &app.App{Version: "0.13.0", Store: store, Out: io.Discard, Err: io.Discard}
}
func fixtureServer(t *testing.T, a *app.App) (*Server, *httptest.Server, *http.Client) {
	t.Helper()
	ts := httptest.NewUnstartedServer(nil)
	s, err := New(Options{App: a, Origin: "http://" + ts.Listener.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	ts.Config.Handler = s.Handler()
	ts.Start()
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	client := ts.Client()
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return s, ts, client
}
func request(t *testing.T, c *http.Client, method, target, body, origin string) (int, []byte, http.Header) {
	t.Helper()
	req, err := http.NewRequest(method, target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	response, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	b, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > maxResponseBytes {
		t.Fatal("response exceeded cap")
	}
	return response.StatusCode, b, response.Header
}

var csrfPattern = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

func login(t *testing.T, s *Server, ts *httptest.Server, c *http.Client) webauth.CreatedActivation {
	t.Helper()
	activation, err := s.auth.CreateActivation(time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	status, _, h := request(t, c, "GET", ts.URL+"/auth/start", "", "")
	if status != 303 {
		t.Fatalf("start=%d", status)
	}
	authorize := h.Get("Location")
	status, page, formHeaders := request(t, c, "GET", authorize, "", "")
	if formHeaders.Get("Referrer-Policy") != "same-origin" {
		t.Fatal("OAuth form browser POST origin would be opaque")
	}
	if status != 200 {
		t.Fatalf("form=%d", status)
	}
	match := csrfPattern.FindSubmatch(page)
	if len(match) != 2 {
		t.Fatal("missing CSRF")
	}
	form := url.Values{"csrf": {string(match[1])}, "activation_code": {activation.Code}}.Encode()
	bad := url.Values{"csrf": {"wrong"}, "activation_code": {activation.Code}}.Encode()
	status, _, _ = request(t, c, "POST", authorize, form, "null")
	if status != 403 {
		t.Fatal("opaque Origin accepted")
	}
	status, _, _ = request(t, c, "POST", authorize, bad, ts.URL)
	if status != 400 {
		t.Fatalf("CSRF=%d", status)
	}
	status, _, h = request(t, c, "POST", authorize, form, ts.URL)
	if status != 303 {
		t.Fatalf("authorize=%d", status)
	}
	callback := h.Get("Location")
	status, body, h := request(t, c, "GET", callback, "", "")
	if status != 303 || strings.Contains(string(body), "ivoai-oauth_") {
		t.Fatal("callback failed or exposed token")
	}
	found := false
	for _, v := range h.Values("Set-Cookie") {
		if strings.Contains(v, s.cookie+"=") {
			found = strings.Contains(v, "HttpOnly") && strings.Contains(v, "SameSite=Strict")
		}
	}
	if !found {
		t.Fatal("missing secure session semantics")
	}
	status, _, _ = request(t, c, "GET", callback, "", "")
	if status != 403 {
		t.Fatal("callback state/code replayed")
	}
	return activation
}
func TestAdminHTTPPKCERevocationAndBoundaries(t *testing.T) {
	a := fixtureApp(t)
	s, ts, c := fixtureServer(t, a)
	status, _, _ := request(t, c, "GET", ts.URL+"/api/admin/v1/overview", "", "")
	if status != 401 {
		t.Fatal(status)
	}
	activation := login(t, s, ts, c)
	for _, path := range []string{"overview", "sessions", "components", "connections"} {
		status, _, _ = request(t, c, "GET", ts.URL+"/api/admin/v1/"+path, "", "")
		if status != 200 {
			t.Fatal(path, status)
		}
	}
	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		status, _, _ = request(t, c, method, ts.URL+"/api/admin/v1/sessions", "", ts.URL)
		if status != 405 {
			t.Fatal(method, status)
		}
	}
	status, _, _ = request(t, c, "GET", ts.URL+"/api/admin/v1/sessions", "", "https://attacker.invalid")
	if status != 403 {
		t.Fatal("origin", status)
	}
	req, _ := http.NewRequest("GET", ts.URL+"/api/admin/v1/sessions", nil)
	req.Host = "attacker.invalid"
	r, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("host accepted")
	}
	req, _ = http.NewRequest("GET", ts.URL+"/api/admin/v1/sessions", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	r, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("fetch accepted")
	}
	status, _, _ = request(t, c, "POST", ts.URL+"/auth/logout", "", "")
	if status != 403 {
		t.Fatal("logout CSRF accepted")
	}
	if err := s.auth.RevokeActivation(activation.ID); err != nil {
		t.Fatal(err)
	}
	status, _, _ = request(t, c, "GET", ts.URL+"/api/admin/v1/sessions", "", "")
	if status != 401 {
		t.Fatal("revoked grant accepted", status)
	}
	login(t, s, ts, c)
	status, _, _ = request(t, c, "POST", ts.URL+"/auth/logout", "", ts.URL)
	if status != 200 {
		t.Fatal(status)
	}
	status, _, _ = request(t, c, "GET", ts.URL+"/api/admin/v1/overview", "", "")
	if status != 401 {
		t.Fatal("logout retained access")
	}
}
func directToken(t *testing.T, store *webauth.Store, resource string, scopes []string) string {
	t.Helper()
	activation, err := store.CreateActivation(time.Minute, scopes)
	if err != nil {
		t.Fatal(err)
	}
	client, err := store.RegisterClient("fixture", []string{"http://127.0.0.1/callback"})
	if err != nil {
		t.Fatal(err)
	}
	verifier := strings.Repeat("v", 43)
	nonce, err := store.BeginAuthorization(client.ID, client.RedirectURIs[0], webauth.PKCEChallenge(verifier), "state", resource, scopes)
	if err != nil {
		t.Fatal(err)
	}
	code, redirect, _, err := store.AuthorizeRequest(activation.Code, nonce)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := store.ExchangeCode(code, client.ID, redirect, verifier, resource)
	if err != nil {
		t.Fatal(err)
	}
	return tokens.AccessToken
}
func TestAdminRejectsOtherAudienceScopeInstanceAndBearer(t *testing.T) {
	s, ts, c := fixtureServer(t, fixtureApp(t))
	u, _ := url.Parse(ts.URL)
	wrongScope, err := webauth.NewScopedStore(s.auth.Path, []string{"admin:other"}, []string{"admin:other"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []string{directToken(t, s.auth, ts.URL+"/mcp", nil), directToken(t, wrongScope, s.Resource(), nil), "connector-or-bridge-capability"}
	for _, token := range cases {
		c.Jar.SetCookies(u, []*http.Cookie{{Name: s.cookie, Value: token, Path: "/"}})
		status, _, _ := request(t, c, "GET", ts.URL+"/api/admin/v1/overview", "", "")
		if status != 401 {
			t.Fatal("wrong authority accepted", status)
		}
	}
	token := directToken(t, s.auth, s.Resource(), nil)
	req, _ := http.NewRequest("GET", ts.URL+"/api/admin/v1/overview", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	c.Jar = nil
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("bearer transport implicitly enabled")
	}
	other, otherTS, otherC := fixtureServer(t, fixtureApp(t))
	otherURL, _ := url.Parse(otherTS.URL)
	otherC.Jar.SetCookies(otherURL, []*http.Cookie{{Name: other.cookie, Value: token, Path: "/"}})
	status, _, _ := request(t, otherC, "GET", otherTS.URL+"/api/admin/v1/overview", "", "")
	if status != 401 {
		t.Fatal("cross-instance token accepted")
	}
}
func addSession(t *testing.T, a *app.App) session.Session {
	t.Helper()
	id, _ := session.NewID()
	now := time.Now().UTC()
	value := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeDirect, PrimaryExecutor: "codex", WorkingDirectory: "/private/CANARY_DIR", PrimaryModel: session.ModelInfo{Name: "gpt-5-codex", Source: session.ModelRuntimeVerified}, Workers: []session.Worker{}, MaxWorkers: 2, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected", State: session.StateRunning, ExecutorTrace: json.RawMessage(`{"resolved_executable":"/private/CANARY_EXEC","working_directory":"CANARY_PROMPT","usage_status":"reported","usage":{"input_tokens":100,"output_tokens":2,"cached_input_tokens":60,"basis":"client_reported","source":"codex_exec_turn_completed","scope":"cli_invocation"}}`)}
	if err := session.AppendObservation(&value, observability.Event{Category: observability.CategoryWorker, Operation: observability.OperationWorkerLifecycle, State: observability.StateCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := (session.Store{Root: a.Store.Paths.SessionsDir}).Create(value); err != nil {
		t.Fatal(err)
	}
	return value
}
func TestAdminDTONoSecretsXSSAndReportedUsage(t *testing.T) {
	a := fixtureApp(t)
	value := addSession(t, a)
	s, ts, c := fixtureServer(t, a)
	login(t, s, ts, c)
	cfg, err := a.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.MCP.Servers["CANARY_ALIAS"] = config.MCPServer{URL: "https://CANARY_HOST.invalid/mcp", Kind: "external", Enabled: true}
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"overview", "sessions", "sessions/" + value.SessionID, "sessions/" + value.SessionID + "/events?after=0", "components", "connections"} {
		status, body, _ := request(t, c, "GET", ts.URL+"/api/admin/v1/"+path, "", "")
		if status != 200 {
			t.Fatal(path, status)
		}
		if strings.Contains(string(body), "CANARY") || strings.Contains(string(body), "resolved_executable") || strings.Contains(string(body), "working_directory") || strings.Contains(string(body), "<script") {
			t.Fatal("unsafe projection", path)
		}
	}
	status, body, _ := request(t, c, "GET", ts.URL+"/api/admin/v1/sessions/"+value.SessionID, "", "")
	if status != 200 || !strings.Contains(string(body), `"scope":"cli_invocation"`) || !strings.Contains(string(body), `"cached_input_included_in_input":true`) {
		t.Fatal("usage provenance lost")
	}
	for _, raw := range []string{`{"usage_status":"reported","usage":{"input_tokens":100,"output_tokens":2,"basis":"client_reported","source":"codex_exec_turn_completed","scope":"session"}}`, `{"usage_status":"invalid","usage":{"input_tokens":0,"output_tokens":0}}`, `{}`} {
		value.ExecutorTrace = json.RawMessage(raw)
		if usage(value) != nil {
			t.Fatal("unknown usage treated as report")
		}
	}
	value.PrimaryModel = session.ModelInfo{Name: "<script>alert('CANARY')</script>", Source: session.ModelConfigured}
	if projectSession(value).Model.Name != "unknown" {
		t.Fatal("XSS label admitted")
	}
	status, _, _ = request(t, c, "GET", ts.URL+"/api/admin/v1/sessions/invalid/events?after=bad", "", "")
	if status != 400 {
		t.Fatal("bad cursor", status)
	}
}
func TestAdminConcurrentSnapshotsAndReplacedRoot(t *testing.T) {
	a := fixtureApp(t)
	addSession(t, a)
	s, ts, c := fixtureServer(t, a)
	login(t, s, ts, c)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := c.Get(ts.URL + "/api/admin/v1/sessions")
			if err != nil {
				t.Error(err)
				return
			}
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if res.StatusCode != 200 && res.StatusCode != 429 {
				t.Error(res.StatusCode)
			}
		}()
	}
	wg.Wait()
	original := a.Store.Paths.ConfigDir
	if err := os.Rename(original, original+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(original, 0700); err != nil {
		t.Fatal(err)
	}
	status, _, _ := request(t, c, "GET", ts.URL+"/api/admin/v1/overview", "", "")
	if status != 503 {
		t.Fatal("replaced instance accepted", status)
	}
}

func TestAdminCallbackRechecksRootAfterCodeExchange(t *testing.T) {
	a := fixtureApp(t)
	s, ts, c := fixtureServer(t, a)
	activation, err := s.auth.CreateActivation(time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	status, _, headers := request(t, c, "GET", ts.URL+"/auth/start", "", "")
	if status != 303 {
		t.Fatal("start", status)
	}
	authorize := headers.Get("Location")
	status, page, _ := request(t, c, "GET", authorize, "", "")
	match := csrfPattern.FindSubmatch(page)
	if status != 200 || len(match) != 2 {
		t.Fatal("form unavailable")
	}
	form := url.Values{"csrf": {string(match[1])}, "activation_code": {activation.Code}}.Encode()
	status, _, headers = request(t, c, "POST", authorize, form, ts.URL)
	if status != 303 {
		t.Fatal("authorize", status)
	}
	callback := headers.Get("Location")
	// The store's clock runs inside the code exchange. Changing a host root
	// there exercises the post-exchange check, after middleware admission.
	var once sync.Once
	s.auth.Clock = func() time.Time {
		once.Do(func() {
			root := a.Store.Paths.ConfigDir
			if err := os.Rename(root, root+".previous"); err != nil {
				t.Error(err)
				return
			}
			if err := os.Mkdir(root, 0700); err != nil {
				t.Error(err)
			}
		})
		return time.Now()
	}
	status, _, headers = request(t, c, "GET", callback, "", "")
	if status != 503 {
		t.Fatal("changed root admitted after exchange", status)
	}
	for _, raw := range headers.Values("Set-Cookie") {
		if strings.HasPrefix(raw, s.cookie+"=") {
			t.Fatal("access cookie issued for changed host root")
		}
	}
}

func TestAdminReferrerPolicyIsLimitedToOAuthForm(t *testing.T) {
	_, ts, c := fixtureServer(t, fixtureApp(t))
	for _, path := range []string{"/", "/assets/admin.js", "/api/admin/v1/meta", "/auth/start", "/auth/callback"} {
		_, _, headers := request(t, c, "GET", ts.URL+path, "", "")
		if headers.Get("Referrer-Policy") != "no-referrer" {
			t.Fatal("referrer policy widened outside OAuth form", path)
		}
	}
}

// TestLiveAdminBrowserFixture is an explicit opt-in local integration fixture.
// The only secret output is an operator-authorized 0600 temporary rendezvous;
// no personal HOME/config/provider auth or network is used.
func TestLiveAdminBrowserFixture(t *testing.T) {
	path := os.Getenv("IVOAI_ADMIN_BROWSER_FIXTURE")
	if path == "" {
		t.Skip("opt-in isolated browser fixture")
	}
	if !filepath.IsAbs(path) || !strings.HasPrefix(filepath.Clean(path), "/tmp/") {
		t.Fatal("fixture output must be under /tmp")
	}
	a := fixtureApp(t)
	addSession(t, a)
	s, ts, _ := fixtureServer(t, a)
	activation, err := s.auth.CreateActivation(5*time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"url": ts.URL, "activation_code": activation.Code})
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("fixture rendezvous unavailable")
	}
	if _, err = file.Write(body); err != nil {
		t.Fatal(err)
	}
	file.Close()
	defer os.Remove(path)
	timer := time.NewTimer(4 * time.Minute)
	defer timer.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-timer.C:
			t.Fatal("browser fixture timed out")
		case <-ticker.C:
			if _, err := os.Stat(path + ".revoke"); err == nil {
				if err := s.auth.RevokeActivation(activation.ID); err != nil {
					t.Fatal("fixture grant revocation failed")
				}
				os.Remove(path + ".revoke")
				if err := os.WriteFile(path+".revoked", []byte("revoked\n"), 0600); err != nil {
					t.Fatal("fixture revocation acknowledgement unavailable")
				}
			}
			if _, err := os.Stat(path + ".stop"); err == nil {
				os.Remove(path + ".stop")
				os.Remove(path + ".revoked")
				return
			}
		case <-context.Background().Done():
			return
		}
	}
}

func TestAdminExpiredAccessAndSharedStateRootStayIsolated(t *testing.T) {
	a := fixtureApp(t)
	s, ts, c := fixtureServer(t, a)
	login(t, s, ts, c)
	s.auth.Clock = func() time.Time { return time.Now().Add(2 * time.Hour) }
	status, _, _ := request(t, c, "GET", ts.URL+"/api/admin/v1/overview", "", "")
	if status != 401 {
		t.Fatal("expired access accepted")
	}
	s.auth.Clock = time.Now
	otherPaths := a.Store.Paths
	otherPaths.ConfigDir = filepath.Join(t.TempDir(), "other-config")
	if err := os.Mkdir(otherPaths.ConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	other, err := AccessStore(otherPaths)
	if err != nil {
		t.Fatal(err)
	}
	if other.Path == s.auth.Path {
		t.Fatal("different config instances shared activations")
	}
	activation, err := s.auth.CreateActivation(time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	client, err := other.RegisterClient("isolated", []string{"http://127.0.0.1/callback"})
	if err != nil {
		t.Fatal(err)
	}
	nonce, err := other.BeginAuthorization(client.ID, client.RedirectURIs[0], webauth.PKCEChallenge(strings.Repeat("v", 43)), "state", s.Resource(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := other.AuthorizeRequest(activation.Code, nonce); err == nil {
		t.Fatal("activation crossed ConfigRoot boundary")
	}
}
func TestAdminRequestAndResponseBounds(t *testing.T) {
	s, ts, c := fixtureServer(t, fixtureApp(t))
	login(t, s, ts, c)
	for _, path := range []string{"/api/admin/v1/sessions?unknown=x", "/api/admin/v1/sessions/invalid/events?unexpected=x", "/api/admin/v1/sessions/invalid/events?after=1&after=2", "/" + strings.Repeat("x", 260)} {
		status, _, _ := request(t, c, "GET", ts.URL+path, "", "")
		if status != 400 {
			t.Fatal("unbounded/unknown request accepted", status)
		}
	}
	recorder := httptest.NewRecorder()
	response(recorder, map[string]string{"payload": strings.Repeat("x", maxResponseBytes+1)})
	if recorder.Code != 503 || strings.Contains(recorder.Body.String(), "payload") {
		t.Fatal("oversized response written")
	}
	s.mu.Lock()
	s.rateStart = time.Now()
	s.rateCount = 240
	s.mu.Unlock()
	status, _, _ := request(t, c, "GET", ts.URL+"/api/admin/v1/meta", "", "")
	if status != 429 {
		t.Fatal("rate bound absent")
	}
}
