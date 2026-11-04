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
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/secrets"
	"github.com/ivo-lopes/ivoai/internal/webauth"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

func outputHTTP(s *Server, token, target string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", s.origin+target, nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.AddCookie(&http.Cookie{Name: s.cookie, Value: token})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestAdministrativeCompleteSanitizedOutputACLExpiryAndNoReplay(t *testing.T) {
	a := fixtureApp(t)
	if err := a.Store.Save(config.Default()); err != nil {
		t.Fatal(err)
	}
	const secret = "private-unlabelled-store-credential"
	const once = "one_time_private_activation_0123456789"
	if err := (secrets.Store{Path: a.Store.Paths.Secrets}).Set("srv_fixture", secrets.ClientCredential{Token: secret}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	raw := strings.Repeat("verified administrative evidence line\n", 10000) + "TAIL-EXACT\n" + secret + "\nActivation code (shown once): " + once + "\n-----BEGIN PRIVATE KEY-----\nprivate-material\n-----END PRIVATE KEY-----\n"
	dispatch := func(_ context.Context, _ []string, _ io.Reader, out, _ io.Writer) error {
		calls++
		_, err := io.WriteString(out, raw)
		return err
	}
	opts := Options{App: a, Origin: "http://127.0.0.1:12345", Dispatch: dispatch}
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	token := mintAdminToken(t, s, ScopeRead, ScopeManage)
	body := map[string]any{"phase": "execute", "input": map[string]string{"name": "fixture"}, "request_id": "complete-output-request"}
	preview := actionHTTP(t, s, token, "mcp.test", map[string]any{"phase": "preview", "input": body["input"]}, true)
	body["confirmation"] = confirmationToken(t, preview)
	result := actionHTTP(t, s, token, "mcp.test", body, true)
	var value struct {
		Truncated bool                    `json:"truncated"`
		Output    string                  `json:"output"`
		Ref       administrativeOutputRef `json:"output_ref"`
	}
	if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &value) != nil || !value.Truncated || !value.Ref.Sanitized || !value.Ref.Result.Artifact.Complete {
		t.Fatalf("missing complete sanitized output: %d", result.Code)
	}
	if err := value.Ref.Result.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(value.Output) > maxResponseBytes/2 || strings.Contains(value.Output, "TAIL-EXACT") {
		t.Fatal("preview not bounded")
	}
	download := outputHTTP(s, token, value.Ref.DownloadURL)
	want := sanitizedAdministrativeOutput(raw, map[string]string{"name": "fixture"}, []string{secret})
	if download.Code != 200 || download.Body.String() != want || !strings.Contains(download.Body.String(), "TAIL-EXACT") || download.Header().Get("Content-Disposition") == "" {
		t.Fatal("complete sanitized projection cannot be recovered exactly")
	}
	for _, forbidden := range []string{secret, once, "private-material", token} {
		if strings.Contains(download.Body.String(), forbidden) {
			t.Fatal("private data exposed")
		}
		if err := filepath.WalkDir(filepath.Join(s.paths.StateDir, "admin", s.instance, "outputs"), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				data, e := os.ReadFile(path)
				if e != nil {
					return e
				}
				if strings.Contains(string(data), forbidden) {
					t.Fatal("private data persisted in artifact")
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	principal, err := s.auth.Authenticate(token, s.Resource(), ScopeRead)
	if err != nil {
		t.Fatal(err)
	}
	// The same OAuth client with a distinct grant/token cannot read old output.
	activation, err := s.auth.CreateActivation(time.Minute, []string{ScopeRead, ScopeManage})
	if err != nil {
		t.Fatal(err)
	}
	redirect := s.origin + "/auth/callback"
	verifier := strings.Repeat("n", 43)
	nonce, err := s.auth.BeginAuthorization(principal.ClientID, redirect, webauth.PKCEChallenge(verifier), "new-grant", s.Resource(), []string{ScopeRead, ScopeManage})
	if err != nil {
		t.Fatal(err)
	}
	code, _, _, err := s.auth.AuthorizeRequest(activation.Code, nonce)
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.auth.ExchangeCode(code, principal.ClientID, redirect, verifier, s.Resource())
	if err != nil {
		t.Fatal(err)
	}
	foreign := next.AccessToken
	if outputHTTP(s, foreign, value.Ref.DownloadURL).Code != 404 || outputHTTP(s, "", value.Ref.DownloadURL).Code != 401 {
		t.Fatal("output crossed credential ownership")
	}
	if outputHTTP(s, token, value.Ref.DownloadURL+"?token=ignored").Code != 404 {
		t.Fatal("query accepted")
	}
	if actionHTTP(t, s, token, "mcp.test", body, true).Code != 409 || calls != 1 {
		t.Fatal("output recovery replayed mutation")
	}
	// Recovery survives process restart but preserves the original token/resource ACL.
	restarted, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if outputHTTP(restarted, token, value.Ref.DownloadURL).Code != 200 {
		t.Fatal("authorized output not recoverable after restart")
	}
	clock := value.Ref.Result.Artifact.ExpiresAt.Add(time.Second)
	limits := workingcontext.DefaultLimits()
	limits.DefaultTTL = administrativeOutputTTL
	limits.MaxTTL = administrativeOutputTTL
	expired, err := workingcontext.NewLocalStore(filepath.Join(s.paths.StateDir, "admin", s.instance, "outputs"), workingcontext.LocalOptions{Limits: limits, Now: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}
	restarted.outputs = expired
	if outputHTTP(restarted, token, value.Ref.DownloadURL).Code != 404 {
		t.Fatal("expired output recovered")
	}
	if err := s.auth.RevokeToken(token); err != nil {
		t.Fatal(err)
	}
	if outputHTTP(s, token, value.Ref.DownloadURL).Code != 401 {
		t.Fatal("revoked grant recovered output")
	}
	if calls != 1 {
		t.Fatal("download invoked administrative dispatch")
	}
}

func TestAdministrativeCaptureLimitNeverClaimsCompleteOutput(t *testing.T) {
	a := fixtureApp(t)
	if err := a.Store.Save(config.Default()); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{App: a, Origin: "http://127.0.0.1:12345", Dispatch: func(_ context.Context, _ []string, _ io.Reader, out, _ io.Writer) error {
		_, err := io.WriteString(out, strings.Repeat("x", maxAdministrativeOutputBytes+1))
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	token := mintAdminToken(t, s, ScopeRead)
	w := actionHTTP(t, s, token, "version", map[string]any{"phase": "execute", "input": map[string]string{}, "request_id": "capture-limit"}, true)
	var result map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result["truncated"] != true || result["output_ref"] != nil || result["output_warning"] == nil {
		t.Fatal("incomplete capture presented as complete artifact")
	}
}

func TestAdministrativeOutputWithheldWhenPrivateRedactionContextIsInvalid(t *testing.T) {
	a := fixtureApp(t)
	if err := a.Store.Save(config.Default()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.Store.Paths.Secrets, []byte("not valid private JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{App: a, Origin: "http://127.0.0.1:12345", Dispatch: func(_ context.Context, _ []string, _ io.Reader, out, _ io.Writer) error {
		_, err := io.WriteString(out, "unlabelled-private-material")
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	token := mintAdminToken(t, s, ScopeRead)
	w := actionHTTP(t, s, token, "version", map[string]any{"phase": "execute", "input": map[string]string{}, "request_id": "invalid-redaction-context"}, true)
	if w.Code != 200 || strings.Contains(w.Body.String(), "unlabelled-private-material") || !strings.Contains(w.Body.String(), "withheld") {
		t.Fatal("invalid private context allowed raw result")
	}
	if got := sanitizedAdministrativeOutput("before\n-----BEGIN PRIVATE KEY-----\nunterminated-key-fragment", nil, nil); strings.Contains(got, "unterminated-key-fragment") {
		t.Fatal("incomplete private-key block exposed")
	}
}

func TestAdministrativeEscapedOutputFitsJSONAndRetainsFullProjection(t *testing.T) {
	a := fixtureApp(t)
	if err := a.Store.Save(config.Default()); err != nil {
		t.Fatal(err)
	}
	raw := strings.Repeat("<>&", 100000)
	s, err := New(Options{App: a, Origin: "http://127.0.0.1:12345", Dispatch: func(_ context.Context, _ []string, _ io.Reader, out, _ io.Writer) error {
		_, err := io.WriteString(out, raw)
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	token := mintAdminToken(t, s, ScopeRead)
	w := actionHTTP(t, s, token, "version", map[string]any{"phase": "execute", "input": map[string]string{}, "request_id": "escaped-output"}, true)
	var result struct {
		Truncated bool                    `json:"truncated"`
		Ref       administrativeOutputRef `json:"output_ref"`
	}
	if w.Code != 200 || w.Body.Len() > maxResponseBytes || json.Unmarshal(w.Body.Bytes(), &result) != nil || !result.Truncated || result.Ref.DownloadURL == "" {
		t.Fatal("JSON expansion hid successful result/ref", w.Code, w.Body.Len())
	}
	if got := outputHTTP(s, token, result.Ref.DownloadURL); got.Code != 200 || got.Body.String() != raw {
		t.Fatal("HTML-escaped content was not recovered as sanitized plain text")
	}
}

type unavailableOutputStore struct{ workingcontext.ArtifactStore }

func (s unavailableOutputStore) Put(context.Context, workingcontext.PutRequest, io.Reader) (workingcontext.ArtifactRef, error) {
	return workingcontext.ArtifactRef{}, context.Canceled
}

func TestAdministrativeRetentionFailureDoesNotReplayOrHideCommittedOutcome(t *testing.T) {
	a := fixtureApp(t)
	if err := a.Store.Save(config.Default()); err != nil {
		t.Fatal(err)
	}
	calls := 0
	s, err := New(Options{App: a, Origin: "http://127.0.0.1:12345", Dispatch: func(_ context.Context, _ []string, _ io.Reader, out, _ io.Writer) error {
		calls++
		_, err := io.WriteString(out, strings.Repeat("retention failure fixture\n", 20000))
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	s.outputs = unavailableOutputStore{s.outputs}
	token := mintAdminToken(t, s, ScopeRead, ScopeManage)
	input := map[string]string{"name": "fixture"}
	preview := actionHTTP(t, s, token, "mcp.test", map[string]any{"phase": "preview", "input": input}, true)
	body := map[string]any{"phase": "execute", "input": input, "confirmation": confirmationToken(t, preview), "request_id": "retention-failed-committed"}
	w := actionHTTP(t, s, token, "mcp.test", body, true)
	var result map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result["status"] != "completed" || result["output_warning"] == nil || result["output_ref"] != nil {
		t.Fatal("retention failure hid committed service outcome")
	}
	if actionHTTP(t, s, token, "mcp.test", body, true).Code != 409 || calls != 1 {
		t.Fatal("retention failure repeated mutation")
	}
}

func TestAdministrativeFastRedactionPreservesUnicodeAndSecretPatterns(t *testing.T) {
	for _, raw := range []string{"ſk-abcdefghijklmnop", "ſecret_key=private", "AcCeSs_ToKeN=private", "Cookie: private", "Authorization: Bearer private", "Activation code (shown once): private"} {
		got := sanitizeOutput(raw, nil)
		want := sensitiveLine.ReplaceAllString(platform.Redact(raw), "[REDACTED]")
		if got != want {
			t.Fatal("fast path changed existing redaction semantics")
		}
	}
	if potentiallySensitiveOutput(strings.Repeat("x", 100000)) {
		t.Fatal("plain long-line redaction fast path not used")
	}
}

func TestAdministrativeRedactionNormalizesControlsBeforeClassification(t *testing.T) {
	raw := "access\x1b_token=UNSAFE_TOKEN\n-----BEGIN\x1b PRIVATE KEY-----\nUNSAFE_KEY\n-----END PRIVATE KEY-----\nActivation co\x1bde (shown once): UNSAFE_CODE_1234567890\n"
	got := sanitizedAdministrativeOutput(raw, nil, nil)
	for _, secret := range []string{"UNSAFE_TOKEN", "UNSAFE_KEY", "UNSAFE_CODE"} {
		if strings.Contains(got, secret) {
			t.Fatal("control normalization exposed classified secret")
		}
	}
	if got := sanitizedAdministrativeOutput("opaque-with-control", nil, []string{"opaque-with-\x1bcontrol"}); strings.Contains(got, "opaque-with-control") {
		t.Fatal("normalization changed known private value without redacting it")
	}
}
