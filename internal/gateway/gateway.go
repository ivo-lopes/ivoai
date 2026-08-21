// Package gateway exposes the versioned ivoai control, enrollment, and MCP API.
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	contextsvc "github.com/ivo-lopes/ivoai/internal/context"
	"github.com/ivo-lopes/ivoai/internal/enrollment"
	"github.com/ivo-lopes/ivoai/internal/webauth"
)

const ProtocolVersion = 1

const (
	enrollmentAuthorizationScheme = "Ivoai-Enrollment "
	enrollmentClientNameHeader    = "X-Ivoai-Client-Name"
	enrollmentScopesHeader        = "X-Ivoai-Requested-Scopes"
)

type Config struct {
	ServerVersion   string
	PublicBaseURL   string
	Context         *contextsvc.Service
	Enrollments     *enrollment.Store
	Memory          http.Handler
	MemoryHealth    func(context.Context) error
	Now             func() time.Time
	EnrollmentAudit func(EnrollmentAudit)
	WebOAuth        *webauth.Server
	WebMCP          http.Handler
}

// EnrollmentAudit contains only non-secret request metadata. Code contents,
// hashes, issued credentials, and client names are deliberately excluded.
type EnrollmentAudit struct {
	ID          string
	CodeLength  int
	FormatValid bool
	Peer        string
	Accepted    bool
	Reason      string
}

type Gateway struct {
	config      Config
	mux         *http.ServeMux
	authSlots   chan struct{}
	enrollSlots chan struct{}
}

type Discovery struct {
	ProtocolVersion     int             `json:"protocol_version"`
	ServerVersion       string          `json:"server_version"`
	PublicBaseURL       string          `json:"public_base_url,omitempty"`
	HealthEndpoint      string          `json:"health_endpoint"`
	ReadyEndpoint       string          `json:"ready_endpoint"`
	ContextMCPEndpoint  string          `json:"context_mcp_endpoint"`
	MemoryMCPEndpoint   string          `json:"memory_mcp_endpoint,omitempty"`
	MemoryHooksEndpoint string          `json:"memory_hooks_endpoint,omitempty"`
	EnrollmentEndpoint  string          `json:"enrollment_endpoint"`
	WebMCPEndpoint      string          `json:"web_mcp_endpoint,omitempty"`
	OAuthMetadata       string          `json:"oauth_authorization_server_metadata,omitempty"`
	Features            map[string]bool `json:"features"`
}

func New(config Config) (*Gateway, error) {
	if config.ServerVersion == "" || config.Context == nil || config.Enrollments == nil {
		return nil, errors.New("gateway requires version, context service, and enrollment store")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	gateway := &Gateway{config: config, mux: http.NewServeMux(), authSlots: make(chan struct{}, 32), enrollSlots: make(chan struct{}, 4)}
	gateway.routes()
	return gateway, nil
}

func (g *Gateway) Handler() http.Handler { return securityHeaders(g.mux) }

func (g *Gateway) routes() {
	g.mux.HandleFunc("GET /health", g.health)
	g.mux.HandleFunc("GET /ready", g.ready)
	g.mux.HandleFunc("GET /.well-known/ivoai", g.discovery)
	g.mux.HandleFunc("POST /v1/enroll", g.enroll)
	if g.config.WebOAuth != nil && g.config.WebMCP != nil {
		g.config.WebOAuth.Register(g.mux)
		g.mux.Handle("POST /mcp", g.authorizeWeb(g.config.WebMCP))
	}
	g.mux.Handle("POST /v1/mcp/context", g.authorize(enrollment.ScopeContextRead, contextsvc.MCPHandler{Service: g.config.Context}))
	if g.config.Memory != nil {
		g.mux.Handle("POST /v1/memory/mcp", g.authorizeAll([]enrollment.Scope{enrollment.ScopeMemoryRead, enrollment.ScopeMemoryWrite}, g.config.Memory))
		g.mux.Handle("POST /v1/memory/hook", g.authorize(enrollment.ScopeMemoryWrite, g.config.Memory))
		g.mux.Handle("POST /v1/memory/hook/batch", g.authorize(enrollment.ScopeMemoryWrite, g.config.Memory))
		g.mux.Handle("GET /v1/memory/handoff", g.authorize(enrollment.ScopeMemoryRead, g.config.Memory))
	}
	g.mux.Handle("GET /v1/remote/status", g.authorize(enrollment.ScopeStatusRead, http.HandlerFunc(g.remoteStatus)))
	g.mux.Handle("GET /v1/remote/doctor", g.authorize(enrollment.ScopeDoctorRead, http.HandlerFunc(g.remoteDoctor)))
	g.mux.Handle("GET /v1/remote/connectors", g.authorize(enrollment.ScopeConnectorRead, http.HandlerFunc(g.remoteConnectors)))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (g *Gateway) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "healthy", "protocol_version": ProtocolVersion})
}

func (g *Gateway) readiness(ctx context.Context) (map[string]any, bool) {
	contextStatus := g.config.Context.Status(ctx)
	memory := "disabled"
	degraded := false
	if g.config.MemoryHealth != nil {
		if err := g.config.MemoryHealth(ctx); err != nil {
			memory = "unavailable"
			degraded = true
		} else {
			memory = "healthy"
		}
	}
	ready := contextStatus.Healthy
	status := "ready"
	if !ready {
		status = "not_ready"
	} else if degraded {
		status = "ready_degraded"
	}
	return map[string]any{"status": status, "context": contextStatus, "memory": memory}, ready
}

func (g *Gateway) ready(w http.ResponseWriter, r *http.Request) {
	result, healthy := g.readiness(r.Context())
	status := http.StatusOK
	if !healthy {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, result)
}

func (g *Gateway) discovery(w http.ResponseWriter, _ *http.Request) {
	discovery := Discovery{ProtocolVersion: ProtocolVersion, ServerVersion: g.config.ServerVersion,
		HealthEndpoint: "/health", ReadyEndpoint: "/ready", ContextMCPEndpoint: "/v1/mcp/context",
		EnrollmentEndpoint: "/v1/enroll", PublicBaseURL: g.config.PublicBaseURL, Features: map[string]bool{"context": true, "memory": g.config.Memory != nil, "memory_hooks": g.config.Memory != nil, "remote_admin_read_only": true}}
	if g.config.WebOAuth != nil && g.config.WebMCP != nil {
		discovery.WebMCPEndpoint = "/mcp"
		discovery.OAuthMetadata = "/.well-known/oauth-authorization-server"
		discovery.Features["web_mcp"] = true
		discovery.Features["oauth_pkce"] = true
	}
	if g.config.Memory != nil {
		discovery.MemoryMCPEndpoint = "/v1/memory/mcp"
		discovery.MemoryHooksEndpoint = "/v1/memory"
	}
	writeJSON(w, http.StatusOK, discovery)
}

func (g *Gateway) enroll(w http.ResponseWriter, r *http.Request) {
	if !acquire(g.enrollSlots) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "enrollment capacity temporarily exhausted"})
		return
	}
	defer release(g.enrollSlots)
	var request struct {
		Code            string   `json:"code"`
		ClientName      string   `json:"client_name"`
		RequestedScopes []string `json:"requested_scopes,omitempty"`
	}
	header := r.Header.Get("Authorization")
	headerClientName := strings.TrimSpace(r.Header.Get(enrollmentClientNameHeader))
	if headerClientName != "" {
		// Header metadata is authoritative for the proxy-resilient transport.
		// The JSON body remains present only so newer clients can enroll against
		// older gateways. Neither metadata header carries a secret.
		request.ClientName = headerClientName
		scopes := strings.TrimSpace(r.Header.Get(enrollmentScopesHeader))
		if scopes != "" {
			for _, scope := range strings.Split(scopes, ",") {
				scope = strings.TrimSpace(scope)
				if scope == "" {
					g.auditEnrollment("", r.RemoteAddr, false, "invalid_request")
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "valid enrollment code and client name are required"})
					return
				}
				request.RequestedScopes = append(request.RequestedScopes, scope)
			}
		}
	} else {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || request.ClientName == "" {
			g.auditEnrollment(request.Code, r.RemoteAddr, false, "invalid_request")
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "valid enrollment code and client name are required"})
			return
		}
	}
	if header != "" {
		if request.Code != "" || !strings.HasPrefix(header, enrollmentAuthorizationScheme) || strings.ContainsAny(header, "\r\n") {
			g.auditEnrollment(request.Code, r.RemoteAddr, false, "ambiguous_or_invalid_transport")
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "valid enrollment code and client name are required"})
			return
		}
		request.Code = strings.TrimPrefix(header, enrollmentAuthorizationScheme)
	}
	if request.Code == "" {
		g.auditEnrollment(request.Code, r.RemoteAddr, false, "missing_code")
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "valid enrollment code and client name are required"})
		return
	}
	requested := make([]enrollment.Scope, len(request.RequestedScopes))
	for index, scope := range request.RequestedScopes {
		requested[index] = enrollment.Scope(scope)
	}
	if request.RequestedScopes == nil {
		requested = nil
	}
	credential, err := g.config.Enrollments.ConsumeScoped(request.Code, request.ClientName, requested)
	if err != nil {
		reason := "invalid_or_expired"
		status := http.StatusUnauthorized
		message := "invalid or expired enrollment code"
		if !errors.Is(err, enrollment.ErrInvalidEnrollmentCode) && !strings.Contains(err.Error(), "scope") && !strings.Contains(err.Error(), "client name") {
			reason = "state_unavailable"
			status = http.StatusServiceUnavailable
			message = "enrollment service temporarily unavailable"
		} else if strings.Contains(err.Error(), "scope") {
			reason = "scope_not_allowed"
		} else if strings.Contains(err.Error(), "client name") {
			reason = "invalid_client_name"
		}
		g.auditEnrollment(request.Code, r.RemoteAddr, false, reason)
		// Invalid credentials receive a deliberately uniform response. Internal
		// storage failures are availability errors and must never masquerade as
		// an invalid user-supplied code.
		writeJSON(w, status, map[string]string{"error": message})
		return
	}
	g.auditEnrollment(request.Code, r.RemoteAddr, true, "accepted")
	writeJSON(w, http.StatusCreated, credential)
}

func (g *Gateway) auditEnrollment(code, peer string, accepted bool, reason string) {
	if g.config.EnrollmentAudit == nil {
		return
	}
	id, valid := enrollment.CodeID(code)
	g.config.EnrollmentAudit(EnrollmentAudit{ID: id, CodeLength: len(code), FormatValid: valid, Peer: peer, Accepted: accepted, Reason: reason})
}

type principalKey struct{}

func (g *Gateway) authorize(scope enrollment.Scope, next http.Handler) http.Handler {
	return g.authorizeAll([]enrollment.Scope{scope}, next)
}

func (g *Gateway) authorizeAll(scopes []enrollment.Scope, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") || strings.ContainsAny(header, "\r\n") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "valid bearer credential required"})
			return
		}
		if !acquire(g.authSlots) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "authentication capacity temporarily exhausted"})
			return
		}
		principal, err := g.config.Enrollments.Authenticate(strings.TrimPrefix(header, "Bearer "), scopes...)
		release(g.authSlots)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "valid bearer credential required"})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, principal)))
	})
}

func (g *Gateway) authorizeWeb(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g.config.WebOAuth == nil || g.config.WebOAuth.Store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "web access unavailable"})
			return
		}
		if !acquire(g.authSlots) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "authentication capacity temporarily exhausted"})
			return
		}
		principal, err := g.config.WebOAuth.Store.Authenticate(webauth.Bearer(r), g.config.WebOAuth.Resource())
		release(g.authSlots)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+strings.TrimRight(g.config.PublicBaseURL, "/")+`/.well-known/oauth-protected-resource"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_token"})
			return
		}
		next.ServeHTTP(w, r.WithContext(webauth.WithPrincipal(r.Context(), principal)))
	})
}

func acquire(slots chan struct{}) bool {
	select {
	case slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func release(slots chan struct{}) { <-slots }

func (g *Gateway) remoteStatus(w http.ResponseWriter, r *http.Request) {
	status, ready := g.readiness(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"ready": ready, "services": status, "server_version": g.config.ServerVersion, "protocol_version": ProtocolVersion})
}

func (g *Gateway) remoteDoctor(w http.ResponseWriter, r *http.Request) {
	status, ready := g.readiness(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"healthy": ready, "checks": status, "security": map[string]any{"database_public": false, "arbitrary_commands": false, "context_tools_read_only": true},
	})
}

func (g *Gateway) remoteConnectors(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"connectors": g.config.Context.ConnectorNames()})
}
