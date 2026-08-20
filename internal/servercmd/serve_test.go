package servercmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMemoryProxyAllowsOnlyStableOperationalRoutesAndStripsCredentials(t *testing.T) {
	t.Setenv("AI_MEMORY_AUTH_TOKEN", "internal-memory-token")
	type observed struct {
		path, authorization, cookie string
	}
	requests := make(chan observed, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- observed{r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Cookie")}
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
		if got.path != expected || got.authorization != "Bearer internal-memory-token" || got.cookie != "" {
			t.Fatalf("%s proxied as %#v, want path %s with only the internal credential", incoming, got, expected)
		}
	}
}
