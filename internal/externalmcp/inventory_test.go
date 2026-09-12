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

func TestExactInventoryJSONAndSSE(t *testing.T) {
	for _, sse := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body := `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"read_tool","inputSchema":{"type":"object"}},{"name":"write_tool"},{"name":"unknown_tool"},{"name":"delete_tool"}]}}`
			if sse {
				w.Header().Set("Content-Type", "text/event-stream")
				body = "event: message\ndata: " + body + "\n\n"
			} else {
				w.Header().Set("Content-Type", "application/json")
			}
			io.WriteString(w, body)
		}))
		g, err := Start([]Target{{Name: "a", URL: server.URL, Restricted: true, AllowedTools: []string{"read_tool"}}}, "full")
		if err != nil {
			t.Fatal(err)
		}
		response, err := gatewayRequest(context.Background(), g, 0, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != 200 || !strings.Contains(string(body), "read_tool") || strings.Contains(string(body), "write_tool") || strings.Contains(string(body), "unknown_tool") || strings.Contains(string(body), "delete_tool") {
			t.Fatalf("incorrect filtered inventory: %s", body)
		}
		g.Close()
		server.Close()
	}
}

func TestExactGrantApprovalCrossWorkerAndRevocation(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`)
	}))
	defer upstream.Close()
	g, err := Start([]Target{{Name: "plane", URL: upstream.URL, Restricted: true, AllowedTools: []string{"read_tool", "write_tool", "unknown_tool"}, ApprovalTools: []string{"write_tool", "unknown_tool"}}}, "full")
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	call := func(name string) string { return strings.ReplaceAll(toolCall, "list_projects", name) }
	for _, name := range []string{"read_tool", "delete_tool"} {
		r, err := gatewayRequest(context.Background(), g, 0, call(name))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if name == "delete_tool" && !strings.Contains(string(body), "MCP_DENIED") {
			t.Fatal("delete allowed")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("denied delete reached upstream")
	}
	for _, name := range []string{"write_tool", "unknown_tool"} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		done := make(chan string, 1)
		go func() {
			r, err := gatewayRequest(ctx, g, 0, call(name))
			if err != nil {
				done <- err.Error()
				return
			}
			body, _ := io.ReadAll(r.Body)
			r.Body.Close()
			done <- string(body)
		}()
		deadline := time.Now().Add(time.Second)
		for len(g.Pending()) == 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		pending := g.Pending()
		if len(pending) != 1 || calls.Load() != 1 {
			t.Fatal("full bypassed approval")
		}
		allow := name == "write_tool"
		if err := g.Reply(pending[0].ID, allow); err != nil {
			t.Fatal(err)
		}
		body := <-done
		cancel()
		if !allow && !strings.Contains(body, "denied") {
			t.Fatal("unknown auto-approved")
		}
		if allow {
			if calls.Load() != 2 {
				t.Fatal("approved write did not reach upstream exactly once")
			}
			calls.Store(1)
			// The next fixture operation starts with a fresh observation baseline.
		}
	}
	b, err := Start([]Target{{Name: "github", URL: upstream.URL, Restricted: true, AllowedTools: []string{"read_tool"}}}, "full")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	req, _ := http.NewRequest("POST", b.URL(0), strings.NewReader(call("read_tool")))
	req.Header.Set("Authorization", "Bearer "+g.Token())
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 401 {
		t.Fatal("worker token crossover")
	}
	g.Close()
	if r, err := gatewayRequest(context.Background(), g, 0, call("read_tool")); err == nil {
		r.Body.Close()
		t.Fatal("revoked listener active")
	}
}
