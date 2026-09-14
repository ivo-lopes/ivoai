package opencodebridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContinuityNativeOpenCodeSubmission(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "ivoai" || password != "fixture-private" {
			t.Error("missing scoped native authentication")
			w.WriteHeader(401)
			return
		}
		if r.Method != "POST" || r.URL.Query().Get("directory") != "/fixture" {
			t.Error("wrong native scope")
		}
		calls = append(calls, r.URL.Path)
		if r.URL.Path == "/session" {
			_, _ = w.Write([]byte(`{"id":"ses_fixture"}`))
			return
		}
		var body struct{ Parts []struct{ Type, Text string } }
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Parts) != 1 || body.Parts[0].Text != "fixture prompt" {
			t.Error("invalid native prompt payload")
		}
		w.WriteHeader(204)
	}))
	defer server.Close()
	m := &Managed{URL: server.URL, password: "fixture-private", AttachArgs: []string{"attach", server.URL}}
	if err := m.SubmitPrompt(context.Background(), "", "/fixture", "fixture prompt"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[1] != "/session/ses_fixture/prompt_async" || !strings.Contains(strings.Join(m.Args(), " "), "--session ses_fixture") {
		t.Fatal("native session not attached")
	}
	calls = nil
	if err := m.SubmitPrompt(context.Background(), "ses_fixture", "/fixture", "fixture prompt"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatal("resume created another native session")
	}
}
