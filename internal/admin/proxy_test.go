package admin

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAdminHTTPSReverseProxyPrefixPKCEAndCookieIsolation(t *testing.T) {
	app := fixtureApp(t)
	upstream := httptest.NewUnstartedServer(nil)
	target, _ := url.Parse("http://" + upstream.Listener.Addr().String())
	reverse := httputil.NewSingleHostReverseProxy(target)
	front := httptest.NewUnstartedServer(reverse)
	publicOrigin := "https://" + front.Listener.Addr().String()
	public := publicOrigin + "/ivoai"
	s, err := New(Options{App: app, Origin: target.String(), PublicURL: public, TrustedProxies: []string{"127.0.0.1/32", "::1/128"}})
	if err != nil {
		t.Fatal(err)
	}
	upstream.Config.Handler = s.Handler()
	upstream.Start()
	defer upstream.Close()
	front.StartTLS()
	defer front.Close()
	c := front.Client()
	c.Jar, _ = cookiejar.New(nil)
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	status, _, h := request(t, c, "GET", public, "", "")
	if status != 308 || h.Get("Location") != "/ivoai/" {
		t.Fatalf("prefix redirect=%d %s", status, h.Get("Location"))
	}
	status, _, _ = request(t, c, "GET", publicOrigin+"/api/admin/v1/meta", "", "")
	if status != 404 {
		t.Fatalf("outside mount=%d", status)
	}
	status, _, h = request(t, c, "GET", public+"/auth/start", "", "")
	if status != 303 {
		t.Fatalf("start=%d", status)
	}
	authorize := h.Get("Location")
	if !strings.HasPrefix(authorize, public+"/oauth/authorize?") {
		t.Fatal("authorization escaped mount", authorize)
	}
	activation, err := s.auth.CreateActivation(time.Minute, []string{ScopeRead, ScopeManage})
	if err != nil {
		t.Fatal(err)
	}
	status, page, h := request(t, c, "GET", authorize, "", "")
	if status != 200 {
		t.Fatalf("form=%d %s", status, page)
	}
	for _, raw := range h.Values("Set-Cookie") {
		cookie, err := http.ParseSetCookie(raw)
		if err != nil || !cookie.Secure || !cookie.HttpOnly || cookie.Path != "/ivoai/oauth/authorize" {
			t.Fatalf("invalid OAuth cookie: %s", raw)
		}
	}
	match := csrfPattern.FindSubmatch(page)
	if len(match) != 2 {
		t.Fatal("CSRF missing")
	}
	form := url.Values{"csrf": {string(match[1])}, "activation_code": {activation.Code}}.Encode()
	status, _, h = request(t, c, "POST", authorize, form, publicOrigin)
	if status != 303 {
		t.Fatalf("authorize=%d", status)
	}
	callback := h.Get("Location")
	if !strings.HasPrefix(callback, public+"/auth/callback?") {
		t.Fatal("callback escaped mount")
	}
	status, _, h = request(t, c, "GET", callback, "", "")
	if status != 303 || h.Get("Location") != "/ivoai/" {
		t.Fatalf("callback=%d location=%s", status, h.Get("Location"))
	}
	sessionCookie := false
	for _, raw := range h.Values("Set-Cookie") {
		cookie, err := http.ParseSetCookie(raw)
		if err != nil {
			t.Fatal(err)
		}
		if cookie.Name == s.cookie {
			sessionCookie = true
			if cookie.Path != "/ivoai/" || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
				t.Fatal("session cookie boundary lost")
			}
		}
	}
	if !sessionCookie {
		t.Fatal("missing cookie")
	}
	status, body, _ := request(t, c, "GET", public+"/api/admin/v1/csrf", "", "")
	if status != 200 || !strings.Contains(string(body), `"access":"manage"`) {
		t.Fatalf("managed proxy access=%d %s", status, body)
	}
	status, _, _ = request(t, c, "GET", public+"/api/admin/v1/overview", "", "https://evil.example")
	if status != 403 {
		t.Fatal("cross-origin read accepted")
	}
	status, _, _ = request(t, c, "POST", public+"/auth/logout", "", publicOrigin)
	if status != 200 {
		t.Fatal("proxy logout failed", status)
	}
	status, _, _ = request(t, c, "GET", public+"/api/admin/v1/overview", "", "")
	if status != 401 {
		t.Fatal("proxy logout not revoked")
	}
}
func TestProxyNeverTrustsForwardedIdentityOrEscapedPaths(t *testing.T) {
	a := fixtureApp(t)
	s, err := New(Options{App: a, Origin: "http://127.0.0.1:12345", PublicURL: "https://admin.example/ivoai", TrustedProxies: []string{"127.0.0.1/32"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		peer, host, path string
		want             int
	}{
		{"192.0.2.1:5555", "admin.example", "/ivoai/api/admin/v1/meta", 403},
		{"127.0.0.1:5555", "evil.example", "/ivoai/api/admin/v1/meta", 403},
		{"127.0.0.1:5555", "admin.example", "/ivoai/../api/admin/v1/meta", 400},
		{"127.0.0.1:5555", "admin.example", "/ivoai/%2e%2e/api/admin/v1/meta", 400},
		{"127.0.0.1:5555", "admin.example", "/ivoai%2fapi/admin/v1/meta", 400},
		{"127.0.0.1:5555", "admin.example", "/ivoai/api/admin/v1/meta", 200},
	} {
		r := httptest.NewRequest("GET", "https://"+tc.host+tc.path, nil)
		r.RemoteAddr = tc.peer
		r.Header.Set("X-Forwarded-For", "127.0.0.1")
		r.Header.Set("X-Forwarded-Host", "admin.example")
		r.Header.Set("Forwarded", "for=127.0.0.1;host=admin.example;proto=https")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s %s: %d want%d", tc.peer, tc.path, w.Code, tc.want)
		}
	}
	for _, u := range []string{"http://admin.example/ivoai", "https://user:pass@admin.example/ivoai", "https://admin.example/a/../ivoai", "https://admin.example/ivoai?secret=x", "https://admin.example/ivoai#fragment", "https://admin.example//ivoai"} {
		if _, err := New(Options{App: a, Origin: "http://127.0.0.1:12345", PublicURL: u, TrustedProxies: []string{"127.0.0.1/32"}}); err == nil {
			t.Fatal("unsafe public URL accepted", u)
		}
	}
	if _, err := New(Options{App: a, Origin: "http://127.0.0.1:12345", PublicURL: "https://admin.example/ivoai"}); err == nil {
		t.Fatal("implicit proxy trust accepted")
	}
}
