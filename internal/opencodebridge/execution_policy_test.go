package opencodebridge

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/core"
)

func TestHostExecutionPolicyPreventsRateLimitFailoverAndCompletionReplay(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, rateLimited := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%v/limit=%v", stream, rateLimited), func(t *testing.T) {
				result := ExecutorResult{Policy: core.ExecutionPolicy{NoReplay: true, NoAutomaticFailover: true}, ExecutorSessionID: "real-primary-thread", Model: "real-model"}
				r := &fakeRunner{result: result, text: "PRIMARY_POLICY_FINAL"}
				if rateLimited {
					r.text, r.err = "", fixtureNativeLimit{}
				}
				var alternates atomic.Int32
				b, err := Start(Options{Runner: r, Select: func(context.Context, string) (string, error) { return "codex", nil }, SelectAlternate: func(context.Context, string, []string) (string, error) { alternates.Add(1); return "claude", nil }, Status: func() Status { return Status{} }})
				if err != nil {
					t.Fatal(err)
				}
				defer b.Close(context.Background())
				request := func() *httptest.ResponseRecorder {
					req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(fmt.Sprintf(`{"model":"auto","stream":%v,"messages":[{"role":"user","content":"question"}]}`, stream)))
					req.Header.Set("X-IVOAI-OpenCode-Session", "policy-session")
					req.Header.Set("X-IVOAI-OpenCode-Message", "policy-message")
					response := httptest.NewRecorder()
					b.chat(response, req)
					return response
				}
				first := request()
				if !rateLimited && (first.Code != 200 || !strings.Contains(first.Body.String(), "PRIMARY_POLICY_FINAL")) {
					t.Fatal("successful host-policy turn lost its final")
				}
				if rateLimited && strings.Contains(first.Body.String(), "PRIMARY_POLICY_FINAL") {
					t.Fatal("limited turn fabricated final")
				}
				second := request()
				if second.Code != 409 || len(r.requests) != 1 || alternates.Load() != 0 || strings.Contains(second.Body.String(), "PRIMARY_POLICY_FINAL") {
					t.Fatal("host policy allowed failover, replay or reexecution")
				}
			})
		}
	}
}
