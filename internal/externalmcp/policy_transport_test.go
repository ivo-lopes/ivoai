package externalmcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPolicyTransportDeniesHiddenCallsDeleteAndLateResult(t *testing.T) {
	var calls atomic.Int32
	var allowed, revokeOnCall atomic.Bool
	target := Target{Name: "host_read", Restricted: true, AllowedTools: []string{"read"}, Local: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if revokeOnCall.Load() {
			allowed.Store(false)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"PRIVATE_LATE_READ"}]}}`)
	})}
	g, err := Start([]Target{target}, "full")
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	g.RequirePolicyJSON()
	g.SetToolAdmission(func(server, tool string) (bool, bool) {
		return allowed.Load() && server == "host_read" && tool == "read", false
	})
	request := func(method, body string) (int, string) {
		r, err := http.NewRequestWithContext(context.Background(), method, g.URL(0), strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+g.Token())
		r.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		return response.StatusCode, string(raw)
	}
	if status, _ := request("DELETE", ""); status != http.StatusMethodNotAllowed || calls.Load() != 0 {
		t.Fatal("DELETE reached policy-bound target")
	}
	for _, body := range []string{`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read"}}`, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"write"}}`, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"write","name":"read"}}`} {
		request("POST", body)
		if calls.Load() != 0 {
			t.Fatal("unadmitted or ambiguous call reached target")
		}
	}
	allowed.Store(true)
	if status, body := request("POST", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read"}}`); status != 200 || !strings.Contains(body, "PRIVATE_LATE_READ") || calls.Load() != 1 {
		t.Fatal("valid read unavailable")
	}
	revokeOnCall.Store(true)
	if _, body := request("POST", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read"}}`); strings.Contains(body, "PRIVATE_LATE_READ") || calls.Load() != 2 {
		t.Fatal("late result escaped revoked policy")
	}
}

func TestPolicyTransportRejectsRevocationAfterResponseHeaders(t *testing.T) {
	var allowed atomic.Bool
	allowed.Store(true)
	var checks atomic.Int32
	headerChecked := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-headerChecked:
		case <-r.Context().Done():
			return
		}
		allowed.Store(false)
		io.WriteString(w, "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"REVOKED_AFTER_HEADERS\"}]}}\n\n")
	}))
	defer upstream.Close()
	g, err := Start([]Target{{Name: "host_read", URL: upstream.URL, Restricted: true, AllowedTools: []string{"read"}}}, "full")
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	g.RequirePolicyJSON()
	g.SetToolAdmission(func(string, string) (bool, bool) {
		admitted := allowed.Load()
		if checks.Add(1) == 2 {
			close(headerChecked)
		}
		return admitted, false
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	response, err := gatewayRequest(ctx, g, 0, strings.ReplaceAll(toolCall, "list_projects", "read"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusBadGateway || strings.Contains(string(body), "REVOKED_AFTER_HEADERS") {
		t.Fatal("late response body escaped revocation after headers")
	}
}

func TestPolicyTransportPreservesFiniteResultsAndRejectsOversizeBeforeDelivery(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		wantStatus              int
	}{
		{"json", "application/json", `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`, http.StatusOK},
		{"sse", "text/event-stream", "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[]}}\n\n", http.StatusOK},
		{"oversize", "application/json", strings.Repeat("x", (16<<20)+1), http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, err := Start([]Target{{Name: "host_read", Restricted: true, AllowedTools: []string{"read"}, Local: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				io.WriteString(w, tc.body)
			})}}, "full")
			if err != nil {
				t.Fatal(err)
			}
			defer g.Close()
			g.RequirePolicyJSON()
			g.SetToolAdmission(func(string, string) (bool, bool) { return true, false })
			response, err := gatewayRequest(context.Background(), g, 0, strings.ReplaceAll(toolCall, "list_projects", "read"))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || response.StatusCode != tc.wantStatus {
				t.Fatal("unexpected finite result status")
			}
			if tc.wantStatus == http.StatusOK && string(body) != tc.body || tc.wantStatus != http.StatusOK && len(body) > 1024 {
				t.Fatal("result changed or oversized partial body escaped")
			}
		})
	}
}
