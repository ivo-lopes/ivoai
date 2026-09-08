package opencodebridge

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/routing"
)

func TestNativePermissionRelayIsAuthenticatedAndFailClosed(t *testing.T) {
	replies := 0
	bridge, err := Start(Options{Runner: &fakeRunner{}, Select: func(context.Context, string) (string, error) { return "opencode", nil }, Status: func() Status { return Status{} }, NativePermissions: func() []PermissionView { return []PermissionView{{ID: "per_fixture", Description: "bash: git status"}} }, ReplyNativePermission: func(_ context.Context, id string, allow bool) error {
		if id != "per_fixture" {
			return errors.New("expired")
		}
		replies++
		if !allow {
			return nil
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	for _, tc := range []struct {
		body, token string
		status      int
	}{
		{`{"id":"per_fixture","allow":true}`, "", 401},
		{`{"id":"per_fixture","allow":true}`, bridge.Token(), 204},
		{`{"id":"per_fixture","allow":false}`, bridge.Token(), 204},
		{`{"id":"per_fixture","allow":true,"session":"other"}`, bridge.Token(), 400},
		{`{"id":"per_missing","allow":true}`, bridge.Token(), 409},
	} {
		request, _ := http.NewRequest("POST", bridge.URL()+"/native-permissions/reply", strings.NewReader(tc.body))
		request.Header.Set("Authorization", "Bearer "+tc.token)
		request.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != tc.status {
			t.Fatalf("permission reply status=%d want=%d", response.StatusCode, tc.status)
		}
	}
	if replies != 2 {
		t.Fatal("unexpected permission replies", replies)
	}
}

type fixtureNativeLimit struct{}

func (fixtureNativeLimit) Error() string     { return "fixture native provider limit" }
func (fixtureNativeLimit) RateLimited() bool { return true }

type limitThenNativeRunner struct {
	calls      []string
	alwaysFail bool
}

func (r *limitThenNativeRunner) Run(_ context.Context, request ExecutorRequest, emit func(string) error) (ExecutorResult, error) {
	r.calls = append(r.calls, request.Executor)
	if len(r.calls) == 1 || r.alwaysFail {
		return ExecutorResult{}, fixtureNativeLimit{}
	}
	return ExecutorResult{ExecutorSessionID: "ses_native_done"}, emit("alternate final response")
}
func TestNativeRateLimitFailoverAndExplicitSelection(t *testing.T) {
	for _, initial := range []string{"codex", "opencode"} {
		t.Run(initial, func(t *testing.T) {
			runner := &limitThenNativeRunner{}
			next := "opencode"
			if initial == "opencode" {
				next = "codex"
			}
			bridge, err := Start(Options{Runner: runner, Select: func(context.Context, string) (string, error) { return initial, nil }, SelectAlternate: func(_ context.Context, from string, attempted []string) (string, error) {
				if from != initial || len(attempted) != 1 {
					t.Error("bad bounded failover state")
				}
				return next, nil
			}, Status: func() Status { return Status{} }})
			if err != nil {
				t.Fatal(err)
			}
			defer bridge.Close(context.Background())
			request := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewBufferString(`{"model":"auto","messages":[{"role":"user","content":"fixture"}]}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-IVOAI-OpenCode-Session", "ses_fixture")
			request.Header.Set("X-IVOAI-OpenCode-Message", "msg_fixture")
			response := httptest.NewRecorder()
			bridge.chat(response, request)
			if response.Code != 200 || !strings.Contains(response.Body.String(), "alternate final response") || len(runner.calls) != 2 || runner.calls[1] != next {
				t.Fatal("native rate-limit failover failed", response.Code, runner.calls)
			}
		})
	}
	t.Run("explicit-native-no-fallback", func(t *testing.T) {
		runner := &limitThenNativeRunner{alwaysFail: true}
		catalog := CatalogFromRegistry(routing.Registry{Providers: map[string]routing.ProviderCapability{"opencode": {Provider: "opencode", Authenticated: true, Models: []routing.ModelCapability{{Name: "fixture/model", Source: routing.SourceRuntimeVerified}}}}})
		modelID := ""
		for _, entry := range catalog.Entries() {
			if entry.Executor == "opencode" {
				modelID = entry.ID
			}
		}
		bridge, err := Start(Options{Runner: runner, Catalog: catalog, Select: func(context.Context, string) (string, error) { return "opencode", nil }, SelectAlternate: func(context.Context, string, []string) (string, error) {
			t.Error("explicit selection fell back")
			return "codex", nil
		}, Status: func() Status { return Status{} }})
		if err != nil {
			t.Fatal(err)
		}
		defer bridge.Close(context.Background())
		request := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"`+modelID+`","messages":[{"role":"user","content":"fixture"}]}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-IVOAI-OpenCode-Session", "ses_explicit")
		request.Header.Set("X-IVOAI-OpenCode-Message", "msg_explicit")
		response := httptest.NewRecorder()
		bridge.chat(response, request)
		if len(runner.calls) != 1 || runner.calls[0] != "opencode" || !strings.Contains(response.Body.String(), "ivoai_bridge_error") {
			t.Fatal("explicit native failure was hidden", response.Code, runner.calls)
		}
	})
}
