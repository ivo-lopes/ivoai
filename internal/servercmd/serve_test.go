package servercmd

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOAuthAuditLogsNoSecrets(t *testing.T) {
	var output bytes.Buffer
	handler := oauthAudit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "contains-secret", http.StatusBadRequest)
	}), &output)
	request := httptest.NewRequest(http.MethodPost, "https://gateway/oauth/token?code=query-secret", strings.NewReader("code=body-secret"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	got := output.String()
	if got != "ivoai gateway: oauth audit method=POST path=/oauth/token status=400 location_host=none referer_host=none fetch_site=other fetch_mode=other fetch_dest=other ua=other\n" {
		t.Fatalf("unexpected audit log: %q", got)
	}
	for _, secret := range []string{"query-secret", "body-secret", "contains-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("audit leaked %q: %s", secret, got)
		}
	}
}

func TestTrustedHTTPSProxyOnlyValidatesPeerAndScheme(t *testing.T) {
	_, network, err := net.ParseCIDR("192.0.2.10/32")
	if err != nil {
		t.Fatal(err)
	}
	handler := trustedHTTPSProxyOnly(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), []*net.IPNet{network})

	for _, test := range []struct {
		name       string
		remoteAddr string
		proto      string
		want       int
	}{
		{"trusted HTTPS proxy", "192.0.2.10:43123", "https", http.StatusNoContent},
		{"untrusted peer", "192.0.2.11:43123", "https", http.StatusForbidden},
		{"plaintext forwarding", "192.0.2.10:43123", "http", http.StatusForbidden},
		{"spoofed forwarded address", "192.0.2.11:43123", "https", http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://gateway/health", nil)
			request.RemoteAddr = test.remoteAddr
			request.Header.Set("X-Forwarded-Proto", test.proto)
			request.Header.Set("X-Forwarded-For", "192.0.2.10")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
		})
	}
}

func TestMemoryProxyAllowsOnlyStableOperationalRoutesAndStripsCredentials(t *testing.T) {
	t.Setenv("AI_MEMORY_AUTH_TOKEN", "internal-memory-token")
	type observed struct {
		path, authorization, cookie, host string
	}
	requests := make(chan observed, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- observed{r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Cookie"), r.Host}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer upstream.Close()
	proxy, err := memoryProxy(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	for incoming, expected := range map[string]string{
		"/v1/memory/mcp":        "/mcp",
		"/v1/memory/hook":       "/hook",
		"/v1/memory/hook/batch": "/hook/batch",
		"/v1/memory/handoff":    "/handoff",
	} {
		request := httptest.NewRequest(http.MethodPost, incoming, nil)
		request.Header.Set("Authorization", "Bearer client-secret")
		request.Header.Set("Cookie", "private=cookie")
		proxy.ServeHTTP(httptest.NewRecorder(), request)
		got := <-requests
		if got.path != expected || got.authorization != "Bearer internal-memory-token" || got.cookie != "" || got.host != strings.TrimPrefix(upstream.URL, "http://") {
			t.Fatalf("%s proxied as %#v, want path %s with only the internal credential", incoming, got, expected)
		}
	}
}

func TestMemoryMCPProbeUsesAuthenticatedToolsList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/mcp" || r.Header.Get("Authorization") != "Bearer internal-token" || !strings.Contains(r.Header.Get("Accept"), "application/json") {
			http.Error(w, "invalid probe", http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`)
	}))
	defer server.Close()
	if status := probeMemoryMCP(context.Background(), server.URL+"/mcp", "internal-token"); status != "healthy" {
		t.Fatalf("probe status = %s", status)
	}
	if status := probeMemoryMCP(context.Background(), server.URL+"/mcp", "wrong-token"); status != "unhealthy" {
		t.Fatalf("invalid credential probe status = %s", status)
	}
}
