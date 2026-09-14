package opencodebridge

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConsoleActionsRequireExactConfirmedDomain(t *testing.T) {
	calls := 0
	b := &Bridge{consoleAction: func(_ context.Context, action string) error {
		calls++
		if action == "hooks.repair" {
			return errors.New("Bearer private-diagnostic-canary")
		}
		return nil
	}}
	for _, tc := range []struct {
		body   string
		status int
		calls  int
	}{
		{`{"action":"profile.quality","confirm":false}`, 400, 0},
		{`{"action":"shell.execute","confirm":true}`, 400, 0},
		{`{"action":"profile.quality","confirm":true,"token":"canary"}`, 400, 0},
		{`{"action":"profile.quality","confirm":true}`, 200, 1},
		{`{"action":"hooks.repair","confirm":true}`, 409, 2},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/console/action", strings.NewReader(tc.body))
		b.consoleAct(w, r)
		if w.Code != tc.status || calls != tc.calls {
			t.Fatal("console action boundary mismatch")
		}
		if strings.Contains(w.Body.String(), "private-diagnostic-canary") {
			t.Fatal("diagnostic escaped")
		}
	}
}

func TestConsoleResumeKeepsOwnershipAndRequiresConfirmation(t *testing.T) {
	selected := ""
	b := &Bridge{consoleSessions: func() []ConsoleSession {
		return []ConsoleSession{{ID: "sess_fixture", NativeID: "ses_native", Resumable: true}}
	}, selectConversation: func(id string) error { selected = id; return nil }}
	call := func(body string, status int) {
		t.Helper()
		w := httptest.NewRecorder()
		b.consoleResume(w, httptest.NewRequest("POST", "/console/resume", strings.NewReader(body)))
		if w.Code != status {
			t.Fatalf("resume status=%d", w.Code)
		}
	}
	call(`{"id":"sess_fixture","confirm":false}`, 400)
	b.writer.Lock()
	call(`{"id":"sess_fixture","confirm":true}`, 409)
	b.writer.Unlock()
	if selected != "" {
		t.Fatal("active writer displaced")
	}
	call(`{"id":"foreign","confirm":true}`, 409)
	call(`{"id":"sess_fixture","confirm":true}`, 200)
	if selected != "ses_native" {
		t.Fatal("native identity not preserved")
	}
}
