package opencodebridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestConversationResumeRequiresProvenAuthContinuity(t *testing.T) {
	for _, tc := range []struct {
		name, previous, current, want string
		probeErr                      error
	}{
		{"same account restart", "account_A", "account_A", "thread_previous", nil},
		{"account switch", "account_A", "account_B", "", nil},
		{"unknown identity", "account_A", "", "", nil},
		{"legacy mapping", "", "account_A", "", nil},
		{"login again", "account_A_epoch1", "account_A_epoch2", "", nil},
		{"logout", "account_A", "", "", errors.New("not authenticated")},
		{"stale probe", "account_A", "account_A", "", errors.New("probe unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner := &fakeRunner{result: ExecutorResult{ExecutorSessionID: "thread_next"}}
			var saved Mapping
			bridge, err := Start(Options{Runner: runner, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{} },
				AuthReference: func(context.Context, string) (string, error) { return tc.current, tc.probeErr },
				LookupMapping: func(frontend string) []Mapping {
					return []Mapping{{FrontendSessionID: frontend, Executor: "codex", ExecutorSessionID: "thread_previous", AuthReference: tc.previous}}
				},
				Mapping: func(m Mapping) error { saved = m; return nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			defer bridge.Close(context.Background())
			response := bridgeRequest(t, bridge, false)
			response.Body.Close()
			if tc.probeErr != nil {
				if response.StatusCode != http.StatusServiceUnavailable || len(runner.requests) != 0 {
					t.Fatal("unverified auth dispatched")
				}
				return
			}
			if response.StatusCode != http.StatusOK || len(runner.requests) != 1 || runner.requests[0].ExecutorSessionID != tc.want {
				t.Fatalf("status=%d requests=%+v", response.StatusCode, runner.requests)
			}
			if saved.AuthReference != tc.current {
				t.Fatal("identity association not persisted")
			}
		})
	}
}

func TestLiveMappingInvalidatedAfterExternalAccountTransition(t *testing.T) {
	// Official probe metadata fixture only. No actual account or credential
	// store is changed. The same frontend and bridge survive every transition.
	var identity atomic.Value
	identity.Store("account_A")
	runner := &fakeRunner{result: ExecutorResult{ExecutorSessionID: "thread_fixture"}}
	bridge, err := Start(Options{Runner: runner,
		Select: func(context.Context, string) (string, error) { return "codex", nil },
		Status: func() Status { return Status{} },
		AuthReference: func(context.Context, string) (string, error) {
			value := identity.Load().(string)
			if value == "logout" {
				return "", errors.New("not authenticated")
			}
			return value, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	for index, step := range []struct {
		identity, resume string
		status           int
	}{
		{"account_A", "", http.StatusOK},
		{"account_A", "thread_fixture", http.StatusOK},
		{"account_B", "", http.StatusOK},
		{"logout", "", http.StatusServiceUnavailable},
		{"account_B_epoch2", "", http.StatusOK},
		{"", "", http.StatusOK},
		{"", "", http.StatusOK},
	} {
		identity.Store(step.identity)
		request, err := http.NewRequest(http.MethodPost, bridge.URL()+"/v1/chat/completions", strings.NewReader(`{"model":"auto","messages":[{"role":"user","content":"non-sensitive transition fixture"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+bridge.Token())
		request.Header.Set("X-IVOAI-OpenCode-Session", "oc_account_transition")
		request.Header.Set("X-IVOAI-OpenCode-Message", fmt.Sprintf("msg_%d", index))
		runner.mu.Lock()
		before := len(runner.requests)
		runner.mu.Unlock()
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != step.status {
			t.Fatalf("step %d status=%d", index, response.StatusCode)
		}
		runner.mu.Lock()
		count := len(runner.requests)
		resume := ""
		if count > before {
			resume = runner.requests[count-1].ExecutorSessionID
		}
		runner.mu.Unlock()
		if step.status != http.StatusOK {
			if count != before {
				t.Fatal("logout dispatched a conversation")
			}
		} else if count != before+1 || resume != step.resume {
			t.Fatalf("step %d incorrectly reused native conversation", index)
		}
	}
}
