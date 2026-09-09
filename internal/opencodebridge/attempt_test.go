package opencodebridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPreThreadAttemptPersistsBeforeExecutionAndAfterFailure(t *testing.T) {
	for _, code := range []int{1, 2, 0} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			root := t.TempDir()
			body := "#!/bin/sh\nprintf '%s\\n' 'Not inside a trusted directory and --skip-git-repo-check was not specified. token=DO_NOT_PERSIST' >&2\nexit " + fmt.Sprint(code) + "\n"
			if code == 0 {
				body = "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"fixture_thread\"}' '{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"OK\"}}' '{\"type\":\"turn.completed\"}'\n"
			}
			var attempts []TurnAttempt
			bridge, err := Start(Options{Token: "fixture", PreferredExecutor: "codex", Status: func() Status { return Status{Frontend: "opencode"} }, Runner: CLIRunner{Codex: ExecutorSpec{Path: fixtureExecutable(t, root, "codex", body), Dir: root}}, Select: func(context.Context, string) (string, error) { return "codex", nil }, Attempt: func(a TurnAttempt) error { attempts = append(attempts, a); return nil }})
			if err != nil {
				t.Fatal(err)
			}
			request, _ := http.NewRequest(http.MethodPost, bridge.URL()+"/v1/chat/completions", strings.NewReader(`{"model":"auto","messages":[{"role":"user","content":"PRIVATE_PROMPT"}]}`))
			request.Header.Set("Authorization", "Bearer fixture")
			request.Header.Set("X-IVOAI-OpenCode-Session", "fixture_frontend")
			request.Header.Set("X-IVOAI-OpenCode-Message", "fixture_message")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			payload, _ := io.ReadAll(response.Body)
			response.Body.Close()
			// Closing a healthy frontend/transport must not rewrite failed turns.
			if err := bridge.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(attempts) != 2 || attempts[0].State != "running" || attempts[0].EndedAt != nil || attempts[0].NativeSessionID != "" {
				t.Fatalf("missing start record: %+v", attempts)
			}
			last := attempts[1]
			if last.ID != attempts[0].ID || last.EndedAt == nil || last.ExitCode == nil || *last.ExitCode != code {
				t.Fatalf("missing outcome: %+v", last)
			}
			if code != 0 && (last.State != "failed" || last.NativeSessionID != "" || last.Failure != "non_git_repository_gate" || last.FinalResponse || !strings.Contains(string(payload), "outside a Git repository")) {
				t.Fatalf("failure lost: %+v %s", last, payload)
			}
			if code == 0 && (last.State != "completed" || !last.FinalResponse || last.NativeSessionID == "") {
				t.Fatalf("success lost: %+v", last)
			}
			saved, _ := json.Marshal(attempts)
			for _, secret := range []string{"PRIVATE_PROMPT", "DO_NOT_PERSIST"} {
				if strings.Contains(string(saved)+string(payload), secret) {
					t.Fatal("private content leaked")
				}
			}
		})
	}
}
