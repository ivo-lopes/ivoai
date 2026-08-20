package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/secrets"
)

const ProtocolVersion = 1
const maxResponse = 1 << 20

type Discovery struct {
	ProtocolVersion     int             `json:"protocol_version"`
	ServerVersion       string          `json:"server_version"`
	HealthEndpoint      string          `json:"health_endpoint"`
	ReadyEndpoint       string          `json:"ready_endpoint"`
	ContextMCPEndpoint  string          `json:"context_mcp_endpoint"`
	MemoryMCPEndpoint   string          `json:"memory_mcp_endpoint"`
	MemoryHooksEndpoint string          `json:"memory_hooks_endpoint"`
	EnrollmentEndpoint  string          `json:"enrollment_endpoint"`
	Features            map[string]bool `json:"features"`
}
type enrollmentRequest struct {
	Code            string   `json:"code"`
	ClientName      string   `json:"client_name"`
	RequestedScopes []string `json:"requested_scopes"`
}
type enrollmentResponse struct {
	Token     string    `json:"token"`
	ClientID  string    `json:"client_id"`
	Scopes    []string  `json:"scopes"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

type ConnectResult struct {
	Discovery      Discovery
	ContextMCPURL  string
	MemoryMCPURL   string
	MemoryHooksURL string
	Warnings       []string
}

type ServerConnector struct {
	Client  *http.Client
	Store   *config.Store
	Secrets secrets.Store
	Now     func() time.Time
}

func SecureHTTPClient() *http.Client {
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, IdleConnTimeout: 30 * time.Second}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		if len(via) > 0 && (req.URL.Scheme != via[0].URL.Scheme || !strings.EqualFold(req.URL.Host, via[0].URL.Host)) {
			return errors.New("cross-origin redirect refused")
		}
		return nil
	}}
}

func (s ServerConnector) Connect(ctx context.Context, baseURL, code, clientName string) (ConnectResult, error) {
	var result ConnectResult
	base, err := ValidateBaseURL(baseURL)
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(code) == "" {
		return result, errors.New("enrollment code is required")
	}
	if s.Client == nil {
		s.Client = SecureHTTPClient()
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	discovery, err := s.discover(ctx, base)
	if err != nil {
		return result, err
	}
	result.Discovery = discovery
	if discovery.ProtocolVersion != ProtocolVersion {
		return result, fmt.Errorf("incompatible server protocol %d; client supports %d", discovery.ProtocolVersion, ProtocolVersion)
	}
	contextEndpoint := resolveEndpoint(base, discovery.ContextMCPEndpoint)
	if contextEndpoint == "" {
		return result, errors.New("server discovery omitted a valid context MCP endpoint")
	}
	memoryEndpoint := ""
	memoryHooksEndpoint := ""
	if discovery.Features["memory"] {
		memoryEndpoint = resolveEndpoint(base, discovery.MemoryMCPEndpoint)
		if memoryEndpoint == "" {
			return result, errors.New("server advertises memory but omitted a valid memory MCP endpoint")
		}
		memoryHooksEndpoint = resolveEndpoint(base, discovery.MemoryHooksEndpoint)
		if memoryHooksEndpoint == "" {
			return result, errors.New("server advertises memory but omitted a valid memory hooks endpoint")
		}
	}
	result.ContextMCPURL = contextEndpoint
	result.MemoryMCPURL = memoryEndpoint
	result.MemoryHooksURL = memoryHooksEndpoint
	if err := s.health(ctx, base, discovery.HealthEndpoint); err != nil {
		return result, err
	}
	if err := s.ready(ctx, base, discovery.ReadyEndpoint); err != nil {
		return result, err
	}
	credential, err := s.enroll(ctx, base, discovery.EnrollmentEndpoint, code, clientName)
	if err != nil {
		return result, err
	}
	if credential.Token == "" || credential.ClientID == "" {
		return result, errors.New("server returned an incomplete client credential")
	}
	// Enrollment codes are one-time. Persist the scoped credential before
	// optional service probes so a transient MCP failure never strands it.
	secretData, err := s.Secrets.Load()
	if err != nil {
		return result, err
	}
	secretData.Server = &secrets.ClientCredential{Token: credential.Token, ClientID: credential.ClientID, Scopes: credential.Scopes, IssuedAt: s.Now().UTC(), ExpiresAt: credential.ExpiresAt}
	if err := s.Secrets.Save(secretData); err != nil {
		return result, err
	}
	c, err := s.Store.Load()
	if err != nil {
		return result, err
	}
	c.Connections.Server = config.Connection{Status: "connected", URL: strings.TrimRight(base.String(), "/"), Protocol: discovery.ProtocolVersion}
	c.MCP.Servers["ivoai-context"] = config.MCPServer{URL: contextEndpoint, Enabled: true, Kind: "context"}
	if memoryEndpoint != "" {
		c.MCP.Servers["ivoai-memory"] = config.MCPServer{URL: memoryEndpoint, HooksURL: memoryHooksEndpoint, Enabled: true, Kind: "memory"}
	} else {
		delete(c.MCP.Servers, "ivoai-memory")
	}
	if err := s.Store.Save(c); err != nil {
		return result, fmt.Errorf("save enrolled server state (credential was preserved for recovery): %w", err)
	}
	if err := s.probeMCP(ctx, contextEndpoint, credential.Token); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("context MCP validation degraded: %v", err))
	}
	if memoryEndpoint != "" {
		if err := s.probeMCP(ctx, memoryEndpoint, credential.Token); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("memory MCP validation degraded: %v", err))
		}
	}
	return result, nil
}

func (s ServerConnector) ready(ctx context.Context, base *url.URL, endpoint string) error {
	if endpoint == "" {
		return errors.New("server discovery omitted readiness endpoint")
	}
	var result struct {
		Status string `json:"status"`
	}
	if err := s.getJSON(ctx, resolveEndpoint(base, endpoint), "", &result); err != nil {
		return fmt.Errorf("server readiness check failed: %w", err)
	}
	if result.Status != "ready" && result.Status != "ready_degraded" {
		return fmt.Errorf("server is not ready: %s", result.Status)
	}
	return nil
}

func (s ServerConnector) probeMCP(ctx context.Context, endpoint, token string) error {
	if endpoint == "" {
		return errors.New("server discovery returned an invalid MCP endpoint")
	}
	payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponse))
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var result struct {
		JSONRPC string          `json:"jsonrpc"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if err := decodeLimited(resp.Body, &result); err != nil {
		return err
	}
	if result.JSONRPC != "2.0" || len(result.Error) != 0 {
		return errors.New("MCP endpoint returned an invalid JSON-RPC response")
	}
	return nil
}

func (s ServerConnector) Disconnect() error {
	c, err := s.Store.Load()
	if err != nil {
		return err
	}
	c.Connections.Server = config.Connection{Status: "not-connected"}
	delete(c.MCP.Servers, "ivoai-context")
	delete(c.MCP.Servers, "ivoai-memory")
	if err := s.Store.Save(c); err != nil {
		return err
	}
	return s.Secrets.RemoveServer()
}

func (s ServerConnector) discover(ctx context.Context, base *url.URL) (Discovery, error) {
	var result Discovery
	if err := s.getJSON(ctx, resolveEndpoint(base, "/.well-known/ivoai"), "", &result); err != nil {
		return result, fmt.Errorf("server discovery failed: %w", err)
	}
	return result, nil
}
func (s ServerConnector) health(ctx context.Context, base *url.URL, endpoint string) error {
	var result struct {
		Status string `json:"status"`
	}
	if err := s.getJSON(ctx, resolveEndpoint(base, endpoint), "", &result); err != nil {
		return fmt.Errorf("server health check failed: %w", err)
	}
	if result.Status != "healthy" && result.Status != "ok" {
		return fmt.Errorf("server is not healthy: %s", result.Status)
	}
	return nil
}
func (s ServerConnector) enroll(ctx context.Context, base *url.URL, endpoint, code, clientName string) (enrollmentResponse, error) {
	payload, _ := json.Marshal(enrollmentRequest{Code: code, ClientName: clientName, RequestedScopes: []string{"context:read", "memory:read", "memory:write", "status:read", "doctor:read", "connector:read"}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolveEndpoint(base, endpoint), bytes.NewReader(payload))
	if err != nil {
		return enrollmentResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := s.Client.Do(req)
	if err != nil {
		return enrollmentResponse{}, fmt.Errorf("enrollment request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponse))
		return enrollmentResponse{}, fmt.Errorf("enrollment refused with HTTP %d", resp.StatusCode)
	}
	var result enrollmentResponse
	if err := decodeLimited(resp.Body, &result); err != nil {
		return result, err
	}
	return result, nil
}
func (s ServerConnector) getJSON(ctx context.Context, endpoint, token string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponse))
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return decodeLimited(resp.Body, target)
}
func decodeLimited(r io.Reader, target any) error {
	return json.NewDecoder(io.LimitReader(r, maxResponse)).Decode(target)
}

// ValidateBaseURL validates an ivoai server origin. Plain HTTP is accepted
// only for a loopback development server.
func ValidateBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}
	if u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("server URL must be a base origin without credentials, query, or fragment")
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	loopback := strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return nil, errors.New("server URL must use HTTPS (HTTP is allowed only for loopback development)")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u, nil
}
func resolveEndpoint(base *url.URL, endpoint string) string {
	ref, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	if ref.IsAbs() {
		if ref.Scheme != base.Scheme || !strings.EqualFold(ref.Host, base.Host) {
			return ""
		}
		return ref.String()
	}
	if !strings.HasPrefix(endpoint, "/") {
		endpoint = "/" + endpoint
	}
	copy := *base
	copy.Path = strings.TrimRight(base.Path, "/") + endpoint
	copy.RawQuery = ""
	copy.Fragment = ""
	return copy.String()
}
