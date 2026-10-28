package admin

import (
	"bytes"
	"net/http"
	"os"
	"testing"
)

func TestAdminExposesOnlyBrowserAuthorizationOAuthRoutes(t *testing.T) {
	s, ts, client := fixtureServer(t, fixtureApp(t))
	login(t, s, ts, client)
	before, err := os.ReadFile(s.auth.Path)
	if err != nil {
		t.Fatal(err)
	}
	unauthenticated := *client
	unauthenticated.Jar = nil
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/oauth/register", `{"client_name":"unused external client","redirect_uris":["http://127.0.0.1/callback"]}`},
		{"POST", "/oauth/token", "grant_type=refresh_token&refresh_token=unused"},
		{"POST", "/oauth/revoke", "token=unused"},
		{"GET", "/.well-known/oauth-authorization-server", ""},
		{"GET", "/.well-known/oauth-protected-resource", ""},
		{"GET", "/.well-known/oauth-protected-resource" + s.oauth.ResourcePath, ""},
	} {
		for _, caller := range []*http.Client{client, &unauthenticated} {
			status, _, headers := request(t, caller, tc.method, ts.URL+tc.path, tc.body, ts.URL)
			if status != http.StatusNotFound || len(headers.Values("Set-Cookie")) != 0 {
				t.Fatalf("unused OAuth endpoint exposed: %s %s status=%d", tc.method, tc.path, status)
			}
		}
	}
	after, err := os.ReadFile(s.auth.Path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("unused OAuth requests changed authorization state")
	}
	status, _, _ := request(t, client, "GET", ts.URL+"/api/admin/v1/overview", "", "")
	if status != http.StatusOK {
		t.Fatal("owned browser authorization stopped working")
	}
}
