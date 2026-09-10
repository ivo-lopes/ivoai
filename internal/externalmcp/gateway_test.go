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

const toolCall = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_projects","arguments":{}}}`

func TestWorkerScopeCannotBypassToolGrantEvenInFullMode(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`)
	}))
	defer upstream.Close()
	g, err := Start([]Target{{Name: "plane", URL: upstream.URL, Restricted: true, AllowedTools: []string{"list_projects"}}}, "full")
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	for _, body := range []string{toolCall, strings.ReplaceAll(toolCall, "list_projects", "delete_project"), `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{}}`} {
		response, err := gatewayRequest(context.Background(), g, 0, body)
		if err != nil {
			t.Fatal(err)
		}
		payload, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if body != toolCall && !strings.Contains(string(payload), "MCP_DENIED") {
			t.Fatal("scope denial missing")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("denied operation reached upstream")
	}
}

func TestExternalMCPDistinctTargetCredentials(t *testing.T) {
	targets := []Target{}
	for _, name := range []string{"a", "b"} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer fixture-"+name {
				t.Error("credential crossover")
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`)
		}))
		t.Cleanup(upstream.Close)
		targets = append(targets, Target{Name: name, URL: upstream.URL, Headers: http.Header{"Authorization": {"Bearer fixture-" + name}}})
	}
	g, err := Start(targets, "full")
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	for i := range targets {
		response, err := gatewayRequest(context.Background(), g, i, toolCall)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatal("target request failed")
		}
	}
}

func gatewayRequest(ctx context.Context, g *Gateway, index int, body string) (*http.Response, error) {
	request, _ := http.NewRequestWithContext(ctx, "POST", g.URL(index), strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+g.Token())
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	return http.DefaultClient.Do(request)
}

func TestFullAndInteractiveExternalMCPApproval(t *testing.T) {
	for _, mode := range []string{"full", "interactive"} {
		t.Run(mode, func(t *testing.T) {
			var count atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				if r.Header.Get("Authorization") != "Bearer fixture-private" || r.Header.Get("X-Workspace-Slug") != "fixture" {
					t.Error("wrong destination credential")
				}
				if r.Header.Get("Cookie") != "" || r.Header.Get("X-Forwarded-For") != "" {
					t.Error("untrusted headers crossed boundary")
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"fixture projects"}]}}`)
			}))
			defer upstream.Close()
			g, err := Start([]Target{{Name: "external", URL: upstream.URL, Headers: http.Header{"Authorization": {"Bearer fixture-private"}, "X-Workspace-Slug": {"fixture"}}}}, mode)
			if err != nil {
				t.Fatal(err)
			}
			defer g.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				response, err := gatewayRequest(ctx, g, 0, toolCall)
				if err == nil {
					body, _ := io.ReadAll(response.Body)
					response.Body.Close()
					if response.StatusCode != 200 || !strings.Contains(string(body), "fixture projects") {
						err = io.ErrUnexpectedEOF
					}
				}
				done <- err
			}()
			if mode == "interactive" {
				deadline := time.Now().Add(2 * time.Second)
				for len(g.Pending()) == 0 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				pending := g.Pending()
				if len(pending) != 1 || count.Load() != 0 {
					t.Fatal("interactive approval bypassed or unavailable")
				}
				if pending[0].Description != "external · list_projects" {
					t.Fatal("permission description not bounded metadata")
				}
				if err := g.Reply(pending[0].ID, true); err != nil {
					t.Fatal(err)
				}
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if len(g.Pending()) != 0 || count.Load() != 1 {
				t.Fatal("approval lifecycle incorrect")
			}
		})
	}
}

func TestExternalMCPDeniedCancelledAndUnauthorized(t *testing.T) {
	var count atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { count.Add(1) }))
	defer upstream.Close()
	for _, deny := range []bool{true, false} {
		g, err := Start([]Target{{Name: "external", URL: upstream.URL}}, "interactive")
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			response, err := gatewayRequest(ctx, g, 0, toolCall)
			if err == nil {
				io.Copy(io.Discard, response.Body)
				response.Body.Close()
			}
			close(done)
		}()
		deadline := time.Now().Add(2 * time.Second)
		for len(g.Pending()) == 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		pending := g.Pending()
		if len(pending) != 1 {
			t.Fatal("missing permission")
		}
		if deny {
			if err := g.Reply(pending[0].ID, false); err != nil {
				t.Fatal(err)
			}
		} else {
			cancel()
		}
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("cancel stuck")
		}
		cancel()
		g.Close()
	}
	if count.Load() != 0 {
		t.Fatal("denied/cancelled tool reached upstream")
	}
	g, err := Start([]Target{{Name: "external", URL: upstream.URL}}, "full")
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	response, err := http.Get(g.URL(0))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("local capability not enforced")
	}
}

func TestExternalMCPCredentialIsolationAndRedirect(t *testing.T) {
	var stolen atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { stolen.Add(1) }))
	defer sink.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-A" {
			t.Error("token crossover")
		}
		http.Redirect(w, r, sink.URL, 302)
	}))
	defer source.Close()
	g, err := Start([]Target{{Name: "A", URL: source.URL, Headers: http.Header{"Authorization": {"Bearer fixture-A"}}}, {Name: "B", URL: sink.URL, Headers: http.Header{"Authorization": {"Bearer fixture-B"}}}}, "full")
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	response, err := gatewayRequest(context.Background(), g, 0, toolCall)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 502 || stolen.Load() != 0 || strings.Contains(string(body), "fixture-A") || strings.Contains(string(body), "fixture-B") {
		t.Fatal("redirect leaked credential")
	}
}
