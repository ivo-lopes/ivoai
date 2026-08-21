package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contextsvc "github.com/ivo-lopes/ivoai/internal/context"
	"github.com/ivo-lopes/ivoai/internal/enrollment"
	"github.com/ivo-lopes/ivoai/internal/webauth"
)

func TestGatewayDiscoveryEnrollmentAuthAndMCP(t *testing.T) {
	contextService, err := contextsvc.NewService(contextsvc.DeterministicEmbedder{DimensionsN: 8}, contextsvc.NewMemoryStore(), contextsvc.NewMemoryCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if err := contextService.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	store := enrollment.NewStore(filepath.Join(t.TempDir(), "enrollment.json"))
	created, err := store.Create(5*time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := New(Config{ServerVersion: "0.1.0-test", Context: contextService, Enrollments: store})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(gateway.Handler())
	defer server.Close()
	client := server.Client()

	response, err := client.Get(server.URL + "/.well-known/ivoai")
	if err != nil {
		t.Fatal(err)
	}
	var discovery Discovery
	decode(t, response, &discovery)
	if discovery.ProtocolVersion != 1 || discovery.ContextMCPEndpoint != "/v1/mcp/context" || discovery.Features["memory"] {
		t.Fatalf("unexpected discovery: %#v", discovery)
	}

	unauthorized, _ := client.Get(server.URL + "/v1/remote/status")
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.StatusCode)
	}
	unauthorized.Body.Close()

	enrollmentBody, _ := json.Marshal(map[string]any{"code": created.Code, "client_name": "integration-client", "requested_scopes": []string{"context:read", "doctor:read"}})
	response, err = client.Post(server.URL+"/v1/enroll", "application/json", bytes.NewReader(enrollmentBody))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("enroll = %d %s", response.StatusCode, body)
	}
	var credential enrollment.ClientCredential
	decode(t, response, &credential)
	if credential.Token == "" {
		t.Fatal("missing client credential")
	}
	if len(credential.Scopes) != 2 {
		t.Fatalf("requested scopes not honored: %#v", credential.Scopes)
	}

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/remote/doctor", nil)
	request.Header.Set("Authorization", "Bearer "+credential.Token)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"arbitrary_commands":false`) {
		t.Fatalf("doctor = %d %s", response.StatusCode, body)
	}

	mcpBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"context_health","arguments":{}}}`
	request, _ = http.NewRequest(http.MethodPost, server.URL+"/v1/mcp/context", strings.NewReader(mcpBody))
	request.Header.Set("Authorization", "Bearer "+credential.Token)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `\"healthy\":true`) {
		t.Fatalf("MCP = %d %s", response.StatusCode, body)
	}

	response, _ = client.Post(server.URL+"/v1/enroll", "application/json", bytes.NewReader(enrollmentBody))
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reused enrollment = %d", response.StatusCode)
	}
	response.Body.Close()
}

func TestDiscoveryMemoryEndpointEndsInMCP(t *testing.T) {
	contextService, _ := contextsvc.NewService(contextsvc.DeterministicEmbedder{DimensionsN: 8}, contextsvc.NewMemoryStore(), contextsvc.NewMemoryCatalog())
	_ = contextService.Initialize(context.Background())
	gateway, err := New(Config{ServerVersion: "test", PublicBaseURL: "https://ai.example.com", Context: contextService,
		Enrollments: enrollment.NewStore(filepath.Join(t.TempDir(), "enrollment.json")), Memory: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	gateway.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/.well-known/ivoai", nil))
	var discovery Discovery
	if err := json.Unmarshal(recorder.Body.Bytes(), &discovery); err != nil {
		t.Fatal(err)
	}
	if discovery.MemoryMCPEndpoint != "/v1/memory/mcp" || !strings.HasSuffix(discovery.MemoryMCPEndpoint, "/mcp") || discovery.MemoryHooksEndpoint != "/v1/memory" || discovery.PublicBaseURL != "https://ai.example.com" {
		t.Fatalf("unexpected memory discovery: %#v", discovery)
	}
}

func TestWebMCPDiscoveryAndBearerChallenge(t *testing.T) {
	contextService, _ := contextsvc.NewService(contextsvc.DeterministicEmbedder{DimensionsN: 8}, contextsvc.NewMemoryStore(), contextsvc.NewMemoryCatalog())
	_ = contextService.Initialize(context.Background())
	oauth := &webauth.Server{Store: webauth.NewStore(filepath.Join(t.TempDir(), "oauth.json")), Issuer: "https://ai.example.com"}
	g, err := New(Config{ServerVersion: "test", PublicBaseURL: "https://ai.example.com", Context: contextService, Enrollments: enrollment.NewStore(filepath.Join(t.TempDir(), "enrollment.json")), WebOAuth: oauth, WebMCP: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })})
	if err != nil {
		t.Fatal(err)
	}
	discoveryRecorder := httptest.NewRecorder()
	g.Handler().ServeHTTP(discoveryRecorder, httptest.NewRequest("GET", "/.well-known/ivoai", nil))
	var discovery Discovery
	if err := json.Unmarshal(discoveryRecorder.Body.Bytes(), &discovery); err != nil {
		t.Fatal(err)
	}
	if discovery.WebMCPEndpoint != "/mcp" || discovery.OAuthMetadata == "" || !discovery.Features["oauth_pkce"] {
		t.Fatalf("web MCP not discovered: %#v", discovery)
	}
	request := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0"}`))
	recorder := httptest.NewRecorder()
	g.Handler().ServeHTTP(recorder, request)
	if recorder.Code != 401 || !strings.Contains(recorder.Header().Get("WWW-Authenticate"), "/.well-known/oauth-protected-resource") {
		t.Fatalf("challenge=%d %q", recorder.Code, recorder.Header().Get("WWW-Authenticate"))
	}
}

func TestMemoryFailureDoesNotTakeGatewayOffline(t *testing.T) {
	contextService, _ := contextsvc.NewService(contextsvc.DeterministicEmbedder{DimensionsN: 8}, contextsvc.NewMemoryStore(), contextsvc.NewMemoryCatalog())
	_ = contextService.Initialize(context.Background())
	store := enrollment.NewStore(filepath.Join(t.TempDir(), "enrollment.json"))
	gateway, err := New(Config{ServerVersion: "test", Context: contextService, Enrollments: store,
		MemoryHealth: func(context.Context) error { return io.ErrUnexpectedEOF }})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	gateway.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "ready_degraded") {
		t.Fatalf("memory failure took gateway offline: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestMemoryHookRoutesEnforceReadWriteScopes(t *testing.T) {
	contextService, _ := contextsvc.NewService(contextsvc.DeterministicEmbedder{DimensionsN: 8}, contextsvc.NewMemoryStore(), contextsvc.NewMemoryCatalog())
	_ = contextService.Initialize(context.Background())
	store := enrollment.NewStore(filepath.Join(t.TempDir(), "enrollment.json"))
	created, _ := store.Create(time.Minute, []enrollment.Scope{enrollment.ScopeMemoryRead})
	credential, _ := store.Consume(created.Code, "read-only")
	memoryCalls := 0
	gateway, err := New(Config{ServerVersion: "test", Context: contextService, Enrollments: store,
		Memory: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { memoryCalls++; w.WriteHeader(http.StatusOK) })})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/memory/handoff", nil)
	request.Header.Set("Authorization", "Bearer "+credential.Token)
	recorder := httptest.NewRecorder()
	gateway.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || memoryCalls != 1 {
		t.Fatalf("read-scoped handoff failed: %d calls=%d", recorder.Code, memoryCalls)
	}
	request = httptest.NewRequest(http.MethodPost, "/v1/memory/hook", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer "+credential.Token)
	recorder = httptest.NewRecorder()
	gateway.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized || memoryCalls != 1 {
		t.Fatalf("read-only token wrote memory: %d calls=%d", recorder.Code, memoryCalls)
	}
}

func TestGatewayRejectsAuthenticationAndEnrollmentOverload(t *testing.T) {
	contextService, _ := contextsvc.NewService(contextsvc.DeterministicEmbedder{DimensionsN: 8}, contextsvc.NewMemoryStore(), contextsvc.NewMemoryCatalog())
	gateway, err := New(Config{ServerVersion: "0.1.0", Context: contextService, Enrollments: enrollment.NewStore(filepath.Join(t.TempDir(), "state.json"))})
	if err != nil {
		t.Fatal(err)
	}
	for acquire(gateway.authSlots) {
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/mcp/context", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer syntactically-valid-but-invalid")
	gateway.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("auth overload status = %d", recorder.Code)
	}
	for len(gateway.authSlots) > 0 {
		release(gateway.authSlots)
	}
	for acquire(gateway.enrollSlots) {
	}
	recorder = httptest.NewRecorder()
	gateway.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/enroll", strings.NewReader(`{}`)))
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("enrollment overload status = %d", recorder.Code)
	}
}

func decode(t *testing.T, response *http.Response, output any) {
	t.Helper()
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(output); err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentAuditExcludesSecretsAndCorrelatesRejection(t *testing.T) {
	contextService, err := contextsvc.NewService(contextsvc.DeterministicEmbedder{DimensionsN: 8}, contextsvc.NewMemoryStore(), contextsvc.NewMemoryCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if err := contextService.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	store := enrollment.NewStore(filepath.Join(t.TempDir(), "state.json"))
	var audit EnrollmentAudit
	g, err := New(Config{ServerVersion: "test", Context: contextService, Enrollments: store, EnrollmentAudit: func(event EnrollmentAudit) { audit = event }})
	if err != nil {
		t.Fatal(err)
	}
	code := "ivoai-enroll_0123456789abcdef_secret-fixture"
	body, _ := json.Marshal(map[string]any{"client_name": "test", "requested_scopes": []string{"context:read"}})
	request := httptest.NewRequest(http.MethodPost, "/v1/enroll", bytes.NewReader(body))
	request.Header.Set("Authorization", "Ivoai-Enrollment "+code)
	request.RemoteAddr = "192.0.2.20:43123"
	response := httptest.NewRecorder()
	g.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
	if audit.ID != "0123456789abcdef" || audit.CodeLength != len(code) || !audit.FormatValid || audit.Accepted || audit.Reason != "invalid_or_expired" || audit.Peer != request.RemoteAddr {
		t.Fatalf("unexpected safe audit metadata: %#v", audit)
	}
	encoded, _ := json.Marshal(audit)
	if bytes.Contains(encoded, []byte("secret-fixture")) {
		t.Fatal("audit event exposed enrollment secret")
	}
}

func TestEnrollmentAcceptsProxyResilientHeadersWithoutBody(t *testing.T) {
	contextService, err := contextsvc.NewService(contextsvc.DeterministicEmbedder{DimensionsN: 8}, contextsvc.NewMemoryStore(), contextsvc.NewMemoryCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if err := contextService.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	store := enrollment.NewStore(filepath.Join(t.TempDir(), "state.json"))
	created, err := store.Create(time.Minute, []enrollment.Scope{enrollment.ScopeContextRead, enrollment.ScopeDoctorRead})
	if err != nil {
		t.Fatal(err)
	}
	g, err := New(Config{ServerVersion: "test", Context: contextService, Enrollments: store})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/enroll", nil)
	request.Header.Set("Authorization", enrollmentAuthorizationScheme+created.Code)
	request.Header.Set(enrollmentClientNameHeader, "host:proxy-test")
	request.Header.Set(enrollmentScopesHeader, "context:read, doctor:read")
	response := httptest.NewRecorder()
	g.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var credential enrollment.ClientCredential
	if err := json.NewDecoder(response.Body).Decode(&credential); err != nil {
		t.Fatal(err)
	}
	if credential.ClientID == "" || len(credential.Scopes) != 2 {
		t.Fatalf("unexpected credential: %#v", credential)
	}
}

func TestEnrollmentReportsStateAvailabilityFailure(t *testing.T) {
	contextService, err := contextsvc.NewService(contextsvc.DeterministicEmbedder{DimensionsN: 8}, contextsvc.NewMemoryStore(), contextsvc.NewMemoryCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if err := contextService.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	var audit EnrollmentAudit
	// A directory is intentionally not a valid state file, simulating an
	// unreadable or incorrectly-owned deployment without involving a secret.
	store := enrollment.NewStore(t.TempDir())
	g, err := New(Config{ServerVersion: "test", Context: contextService, Enrollments: store, EnrollmentAudit: func(event EnrollmentAudit) { audit = event }})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/enroll", nil)
	request.Header.Set("Authorization", enrollmentAuthorizationScheme+"ivoai-enroll_0123456789abcdef_secret-fixture")
	request.Header.Set(enrollmentClientNameHeader, "host:state-test")
	request.Header.Set(enrollmentScopesHeader, "context:read")
	response := httptest.NewRecorder()
	g.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || audit.Reason != "state_unavailable" {
		t.Fatalf("status=%d audit=%#v body=%s", response.Code, audit, response.Body.String())
	}
}
