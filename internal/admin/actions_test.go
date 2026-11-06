package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/webauth"
)

func mintAdminToken(t *testing.T, s *Server, scopes ...string) string {
	t.Helper()
	activation, err := s.auth.CreateActivation(time.Hour, scopes)
	if err != nil {
		t.Fatal(err)
	}
	redirect := s.origin + s.basePath + "/auth/callback"
	client, err := s.auth.RegisterClient("test", []string{redirect})
	if err != nil {
		t.Fatal(err)
	}
	verifier := strings.Repeat("v", 43)
	nonce, err := s.auth.BeginAuthorization(client.ID, redirect, webauth.PKCEChallenge(verifier), "test-state", s.Resource(), scopes)
	if err != nil {
		t.Fatal(err)
	}
	code, _, _, err := s.auth.AuthorizeRequest(activation.Code, nonce)
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.auth.ExchangeCode(code, client.ID, redirect, verifier, s.Resource())
	if err != nil {
		t.Fatal(err)
	}
	return token.AccessToken
}
func actionHTTP(t *testing.T, s *Server, token, id string, body any, csrf bool) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", s.origin+s.basePath+"/api/admin/v1/actions/"+id, strings.NewReader(string(b)))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", s.origin)
	r.AddCookie(&http.Cookie{Name: s.cookie, Value: token})
	if csrf {
		r.Header.Set("X-IVOAI-CSRF", s.csrfValue(r))
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func confirmationToken(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("preview status=%d %s", w.Code, w.Body)
	}
	var v struct {
		Confirmation string `json:"confirmation"`
	}
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Confirmation == "" {
		t.Fatal("missing confirmation")
	}
	return v.Confirmation
}
func TestAdministrativeMutationRequiresScopeCSRFExactPreviewAndDurableClaim(t *testing.T) {
	a := fixtureApp(t)
	if err := a.Store.Save(config.Default()); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	s, err := New(Options{App: a, Origin: "http://127.0.0.1:12345", Dispatch: func(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) error {
		calls.Add(1)
		return a.ConfigSet(args[2], args[3])
	}})
	if err != nil {
		t.Fatal(err)
	}
	read := mintAdminToken(t, s, ScopeRead)
	manage := mintAdminToken(t, s, ScopeRead, ScopeManage)
	input := map[string]string{"value": "false"}
	preview := map[string]any{"phase": "preview", "input": input}
	if w := actionHTTP(t, s, read, "config.memory", preview, true); w.Code != 403 {
		t.Fatalf("read mutated: %d", w.Code)
	}
	if w := actionHTTP(t, s, manage, "config.memory", preview, false); w.Code != 403 {
		t.Fatalf("no CSRF: %d", w.Code)
	}
	w := actionHTTP(t, s, manage, "config.memory", preview, true)
	token := confirmationToken(t, w)
	changed := map[string]any{"phase": "execute", "input": map[string]string{"value": "true"}, "confirmation": token, "request_id": "request-12345678"}
	if w := actionHTTP(t, s, manage, "config.memory", changed, true); w.Code != 409 {
		t.Fatalf("changed params: %d", w.Code)
	}
	if calls.Load() != 0 {
		t.Fatal("preview/invalid confirmation executed")
	}
	token = confirmationToken(t, actionHTTP(t, s, manage, "config.memory", preview, true))
	execute := map[string]any{"phase": "execute", "input": input, "confirmation": token, "request_id": "request-12345678"}
	if w := actionHTTP(t, s, manage, "config.memory", execute, true); w.Code != 200 {
		t.Fatalf("execute=%d %s", w.Code, w.Body)
	}
	cfg, _ := a.Store.Load()
	if cfg.Memory.Enabled || calls.Load() != 1 {
		t.Fatal("controlled real config mutation failed")
	}
	if w := actionHTTP(t, s, manage, "config.memory", execute, true); w.Code != 409 {
		t.Fatal("confirmation replay accepted")
	}
	token = confirmationToken(t, actionHTTP(t, s, manage, "config.memory", preview, true))
	execute["confirmation"] = token
	if w := actionHTTP(t, s, manage, "config.memory", execute, true); w.Code != 409 {
		t.Fatal("duplicate request with fresh confirmation accepted")
	}
	if calls.Load() != 1 {
		t.Fatal("duplicate dispatch")
	}
	receipts, err := os.ReadDir(filepath.Join(filepath.Dir(s.auth.Path), "operations"))
	if err != nil || len(receipts) != 1 {
		t.Fatal("missing durable claim")
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(s.auth.Path), "operations", receipts[0].Name()))
	if err != nil || !strings.Contains(string(b), "completed") || strings.Contains(string(b), manage) {
		t.Fatal("invalid or sensitive receipt")
	}
	// An independent HTTP server instance cannot replay an already claimed ID.
	restarted, err := New(Options{App: a, Origin: s.origin, Dispatch: s.dispatch})
	if err != nil {
		t.Fatal(err)
	}
	token = confirmationToken(t, actionHTTP(t, restarted, manage, "config.memory", preview, true))
	execute["confirmation"] = token
	if w := actionHTTP(t, restarted, manage, "config.memory", execute, true); w.Code != 409 {
		t.Fatal("process-restart replay accepted")
	}
}
func TestAdministrativeConfirmationInvalidatesOnPolicyChangeAndRevocation(t *testing.T) {
	a := fixtureApp(t)
	_ = a.Store.Save(config.Default())
	calls := 0
	s, err := New(Options{App: a, Origin: "http://127.0.0.1:12345", Dispatch: func(context.Context, []string, io.Reader, io.Writer, io.Writer) error { calls++; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	token := mintAdminToken(t, s, ScopeRead, ScopeManage)
	input := map[string]string{"value": "false"}
	preview := map[string]any{"phase": "preview", "input": input}
	receipt := confirmationToken(t, actionHTTP(t, s, token, "config.memory", preview, true))
	if err := a.ConfigSet("orchestration.auto.plan_execution", "immediate"); err != nil {
		t.Fatal(err)
	}
	execute := map[string]any{"phase": "execute", "input": input, "confirmation": receipt, "request_id": "policy-change-1234"}
	if w := actionHTTP(t, s, token, "config.memory", execute, true); w.Code != 409 {
		t.Fatal("stale policy accepted")
	}
	receipt = confirmationToken(t, actionHTTP(t, s, token, "config.memory", preview, true))
	execute["confirmation"] = receipt
	if err := s.auth.RevokeToken(token); err != nil {
		t.Fatal(err)
	}
	if w := actionHTTP(t, s, token, "config.memory", execute, true); w.Code != 401 {
		t.Fatal("revoked principal accepted")
	}
	if calls != 0 {
		t.Fatal("unauthorized mutation ran")
	}
}
func TestAdministrativeInputAndOutputDoNotBecomeInstructionsOrSecrets(t *testing.T) {
	a := fixtureApp(t)
	_ = a.Store.Save(config.Default())
	calls := 0
	s, err := New(Options{App: a, Origin: "http://127.0.0.1:12345", Dispatch: func(_ context.Context, args []string, in io.Reader, out, errOut io.Writer) error {
		calls++
		b, _ := io.ReadAll(in)
		_, _ = out.Write([]byte("authorization: bearer " + string(b)))
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	token := mintAdminToken(t, s, ScopeRead, ScopeManage)
	for _, input := range []map[string]string{{"name": "--evil", "secret": "private"}, {"name": "safe", "secret": "abc\nrun"}, {"name": "safe", "secret": "private", "args": "sh -c bad"}} {
		if w := actionHTTP(t, s, token, "mcp.auth", map[string]any{"phase": "preview", "input": input}, true); w.Code != 400 {
			t.Fatal("unsafe args accepted")
		}
	}
	input := map[string]string{"name": "safe", "secret": "private-secret-do-not-echo"}
	receipt := confirmationToken(t, actionHTTP(t, s, token, "mcp.auth", map[string]any{"phase": "preview", "input": input}, true))
	w := actionHTTP(t, s, token, "mcp.auth", map[string]any{"phase": "execute", "input": input, "confirmation": receipt, "request_id": "private-request-1234"}, true)
	if w.Code != 200 || strings.Contains(w.Body.String(), input["secret"]) || calls != 1 {
		t.Fatal("secret in response or wrong execution count")
	}
}

func TestAdministrativeRevocationCancelsInFlightOperationWithoutReplay(t *testing.T) {
	a := fixtureApp(t)
	_ = a.Store.Save(config.Default())
	started, finished := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s, err := New(Options{App: a, Origin: "http://127.0.0.1:12345", Dispatch: func(ctx context.Context, _ []string, _ io.Reader, _ io.Writer, _ io.Writer) error {
		calls.Add(1)
		close(started)
		<-ctx.Done()
		close(finished)
		return ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	token := mintAdminToken(t, s, ScopeRead, ScopeManage)
	input := map[string]string{"value": "false"}
	receipt := confirmationToken(t, actionHTTP(t, s, token, "config.memory", map[string]any{"phase": "preview", "input": input}, true))
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- actionHTTP(t, s, token, "config.memory", map[string]any{"phase": "execute", "input": input, "confirmation": receipt, "request_id": "revoke-request-1234"}, true)
	}()
	<-started
	if err := s.auth.RevokeToken(token); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("revocation did not cancel operation")
	}
	result := <-done
	if result.Code != 401 || calls.Load() != 1 {
		t.Fatal("revoked output returned or operation replayed")
	}
}

func TestAdminInitializePreservesExistingPermissionsAndState(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{ConfigDir: filepath.Join(root, "config"), StateDir: filepath.Join(root, "state")}
	if err := Initialize(paths); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(paths.ConfigDir, "private-marker")
	if err := os.WriteFile(marker, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(paths); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(marker); string(b) != "preserve" {
		t.Fatal("existing state changed")
	}
	if err := os.Chmod(paths.ConfigDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(paths); err == nil {
		t.Fatal("public root silently repaired")
	}
	info, _ := os.Stat(paths.ConfigDir)
	if info.Mode().Perm() != 0755 {
		t.Fatal("existing permissions changed")
	}
}
func TestAdministrativeConfirmationRefusesSymlinkStateAndWrongProjectScope(t *testing.T) {
	a := fixtureApp(t)
	_ = a.Store.Save(config.Default())
	calls := 0
	s, err := New(Options{App: a, Origin: "http://127.0.0.1:12345", Dispatch: func(context.Context, []string, io.Reader, io.Writer, io.Writer) error { calls++; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	token := mintAdminToken(t, s, ScopeRead, ScopeManage)
	if w := actionHTTP(t, s, token, "project.init", map[string]any{"phase": "preview", "input": map[string]string{"project_root": t.TempDir()}}, true); w.Code != 409 {
		t.Fatal("wrong project scope admitted")
	}
	outside := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(outside, []byte("do not read or write"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(a.Store.Paths.Config); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, a.Store.Paths.Config); err != nil {
		t.Fatal(err)
	}
	if w := actionHTTP(t, s, token, "config.memory", map[string]any{"phase": "preview", "input": map[string]string{"value": "false"}}, true); w.Code != 503 {
		t.Fatal("symlink configuration admitted")
	}
	actual, _ := os.ReadFile(outside)
	if string(actual) != "do not read or write" || calls != 0 {
		t.Fatal("symlink or wrong scope had effects")
	}
}

func TestAntigravityProjectionDoesNotEnableUnsupportedRouting(t *testing.T) {
	value := session.Session{PrimaryExecutor: "antigravity"}
	projected := projectSession(value)
	if projected.Executor != "antigravity" || len(projected.Quotas) != 0 {
		t.Fatal("observed executor hidden or quota fabricated")
	}
	for _, a := range ActionCatalog() {
		if a.ID == "config.primary" || a.ID == "config.reviewer" || a.ID == "config.auto-planner" || a.ID == "policy.provider" {
			for _, f := range a.Fields {
				for _, option := range f.Options {
					if option == "antigravity" {
						t.Fatal("experimental headless identity enabled unsupported global routing")
					}
				}
			}
		}
	}
}

func TestEveryAdministrativeFormRejectsMissingInputsAndFlagInjection(t *testing.T) {
	for _, action := range ActionCatalog() {
		if action.Kind != "read" && action.Kind != "mutation" {
			continue
		}
		t.Run(action.ID, func(t *testing.T) {
			valid := map[string]string{}
			fields := map[string]Field{}
			for _, f := range action.Fields {
				fields[f.Name] = f
				valid[f.Name] = "isolated-fixture-" + f.Name
				if len(f.Options) > 0 {
					valid[f.Name] = f.Options[0]
				}
			}
			if err := validateActionInput(action, valid); err != nil {
				t.Fatal("catalog cannot admit its own typed form", err)
			}
			for _, f := range action.Fields {
				input := map[string]string{}
				for k, v := range valid {
					input[k] = v
				}
				if f.Required {
					delete(input, f.Name)
					if validateActionInput(action, input) == nil {
						t.Fatal("required field omitted", f.Name)
					}
				}
				if f.Type != "password" {
					for _, payload := range []string{"--help", "--root=/outside", "line\ncommand", "nul\x00value"} {
						input[f.Name] = payload
						if validateActionInput(action, input) == nil {
							t.Fatal("injection admitted", f.Name)
						}
					}
				}
				for _, option := range f.Options {
					input[f.Name] = option
					if err := validateActionInput(action, input); err != nil {
						t.Fatal("advertised option rejected", f.Name, err)
					}
				}
			}
			args, stdin := actionArguments(action, valid)
			for name, flag := range action.optionalFlags {
				if _, ok := fields[name]; !ok {
					t.Fatal("CLI flag has no typed field", name)
				}
				found := flag == ""
				for i, arg := range args {
					if arg == flag && i+1 < len(args) && args[i+1] == valid[name] {
						found = true
					}
				}
				if !found {
					t.Fatal("typed optional field did not reach its CLI flag", name)
				}
			}
			if action.stdin != "" {
				if fields[action.stdin].Type != "password" || stdin != valid[action.stdin]+"\n" {
					t.Fatal("secret input not exclusively forwarded through stdin")
				}
				for _, arg := range args {
					if arg == valid[action.stdin] {
						t.Fatal("secret copied to CLI arguments")
					}
				}
			}
		})
	}
}
