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
	"github.com/ivo-lopes/ivoai/internal/serverpool"
)

const ProtocolVersion = 1
const maxResponse = 1 << 20

const (
	enrollmentAuthorizationScheme = "Ivoai-Enrollment "
	enrollmentClientNameHeader    = "X-Ivoai-Client-Name"
	enrollmentScopesHeader        = "X-Ivoai-Requested-Scopes"
)

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
	Code            string   `json:"code,omitempty"`
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
	Profile        config.ServerProfile
	Discovery      Discovery
	ContextMCPURL  string
	MemoryMCPURL   string
	MemoryHooksURL string
	Warnings       []string
}

type ConnectOptions struct {
	Alias           string
	Purpose         string
	RedundancyGroup string
	Priority        int
	BaseURL         string
	Code            string
	ClientName      string
}

type ProfileHealth struct {
	Reachable          bool `json:"reachable"`
	Ready              bool `json:"ready"`
	ProtocolCompatible bool `json:"protocol_compatible"`
	ContextAvailable   bool `json:"context_available"`
	MemoryAvailable    bool `json:"memory_available"`
}

type ServerConnector struct {
	Client     *http.Client
	Store      *config.Store
	Secrets    secrets.Store
	Now        func() time.Time
	SaveConfig func(config.Config) error
}

func (s ServerConnector) saveConfig(value config.Config) error {
	if s.SaveConfig != nil {
		return s.SaveConfig(value)
	}
	return s.Store.Save(value)
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
	return s.ConnectProfile(ctx, ConnectOptions{Alias: "default", Purpose: "default", BaseURL: baseURL, Code: code, ClientName: clientName})
}

func (s ServerConnector) ConnectProfile(ctx context.Context, options ConnectOptions) (ConnectResult, error) {
	var result ConnectResult
	if options.Alias == "" {
		options.Alias = "default"
	}
	if err := serverpool.ValidateAlias(options.Alias); err != nil {
		return result, err
	}
	if options.Purpose == "" {
		options.Purpose = options.Alias
	}
	if err := serverpool.ValidateLabel("server purpose", options.Purpose); err != nil {
		return result, err
	}
	if options.RedundancyGroup != "" {
		if err := serverpool.ValidateLabel("redundancy group", options.RedundancyGroup); err != nil {
			return result, err
		}
	}
	base, err := ValidateBaseURL(options.BaseURL)
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(options.Code) == "" {
		return result, errors.New("enrollment code is required")
	}
	c, err := s.Store.Load()
	if err != nil {
		return result, err
	}
	if _, err := serverpool.New(c.Connections.Servers); err != nil {
		return result, fmt.Errorf("validate existing server profiles: %w", err)
	}
	originalConfig := cloneConnectionConfig(c)
	originalSecrets, err := s.Secrets.Load()
	if err != nil {
		return result, err
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
	profileID := ""
	if previous, exists := c.Connections.Servers[options.Alias]; exists {
		profileID = previous.ID
	}
	if profileID == "" && options.Alias == "default" {
		profileID = config.LegacyServerID
	}
	if profileID == "" {
		profileID, err = serverpool.NewID()
		if err != nil {
			return result, err
		}
	}
	profile := config.ServerProfile{
		ID: profileID, Alias: options.Alias, URL: strings.TrimRight(base.String(), "/"),
		Status: "enrolling", Enabled: false, Purpose: options.Purpose,
		RedundancyGroup: options.RedundancyGroup, Priority: options.Priority,
		Protocol: discovery.ProtocolVersion, ContextMCPURL: contextEndpoint,
		MemoryMCPURL: memoryEndpoint, MemoryHooksURL: memoryHooksEndpoint,
		ServerVersion: discovery.ServerVersion, Features: discovery.Features,
	}
	if c.Connections.Servers == nil {
		c.Connections.Servers = map[string]config.ServerProfile{}
	}
	c.Connections.Servers[options.Alias] = profile
	if options.Alias == "default" {
		c.Connections.Server = config.Connection{Status: "not-connected"}
		delete(c.MCP.Servers, "ivoai-context")
		delete(c.MCP.Servers, "ivoai-memory")
	}
	// Persist a fail-closed enrollment marker before consuming the one-time code.
	// A crash can leave this profile unavailable, but can never pair a new token
	// with the previous server URL.
	if err := s.saveConfig(c); err != nil {
		return result, fmt.Errorf("prepare server enrollment: %w", err)
	}
	credential, err := s.enroll(ctx, base, discovery.EnrollmentEndpoint, options.Code, options.ClientName)
	if err != nil {
		_ = s.saveConfig(originalConfig)
		return result, err
	}
	if credential.Token == "" || credential.ClientID == "" {
		_ = s.saveConfig(originalConfig)
		return result, errors.New("server returned an incomplete client credential")
	}
	storedCredential := secrets.ClientCredential{Token: credential.Token, ClientID: credential.ClientID, Scopes: credential.Scopes, IssuedAt: s.Now().UTC(), ExpiresAt: credential.ExpiresAt}
	if err := s.Secrets.Set(profileID, storedCredential); err != nil {
		rollbackErr := s.saveConfig(originalConfig)
		return result, errors.Join(err, rollbackErr)
	}
	profile.Status = "connected"
	profile.Enabled = true
	c.Connections.Servers[options.Alias] = profile
	// Preserve the v0.5 singleton and MCP entries only as the default profile's
	// rollback bridge. They are never used to route a second upstream token.
	if options.Alias == "default" {
		c.Connections.Server = config.Connection{Status: "connected", URL: profile.URL, Protocol: profile.Protocol}
		c.MCP.Servers["ivoai-context"] = config.MCPServer{URL: contextEndpoint, Enabled: true, Kind: "context"}
		if memoryEndpoint != "" {
			c.MCP.Servers["ivoai-memory"] = config.MCPServer{URL: memoryEndpoint, HooksURL: memoryHooksEndpoint, Enabled: true, Kind: "memory"}
		} else {
			delete(c.MCP.Servers, "ivoai-memory")
		}
	}
	if err := s.saveConfig(c); err != nil {
		secretErr := s.Secrets.Save(originalSecrets)
		configErr := s.saveConfig(originalConfig)
		return result, errors.Join(fmt.Errorf("commit server enrollment: %w", err), secretErr, configErr)
	}
	result.Profile = profile
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
	req.Header.Set("Accept", "application/json, text/event-stream")
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
	return s.DisconnectProfile("default")
}

func (s ServerConnector) DisconnectProfile(alias string) error {
	if err := serverpool.ValidateAlias(alias); err != nil {
		return err
	}
	c, err := s.Store.Load()
	if err != nil {
		return err
	}
	if _, err := serverpool.New(c.Connections.Servers); err != nil {
		return fmt.Errorf("validate existing server profiles: %w", err)
	}
	profile, exists := c.Connections.Servers[alias]
	if !exists {
		return fmt.Errorf("server profile %q is not connected", alias)
	}
	delete(c.Connections.Servers, alias)
	if alias == "default" {
		c.Connections.Server = config.Connection{Status: "not-connected"}
		delete(c.MCP.Servers, "ivoai-context")
		delete(c.MCP.Servers, "ivoai-memory")
	}
	secretData, err := s.Secrets.Load()
	if err != nil {
		return err
	}
	originalSecrets := cloneSecrets(secretData)
	delete(secretData.Servers, profile.ID)
	if profile.ID == config.LegacyServerID {
		secretData.Server = nil
	}
	if err := s.Secrets.Save(secretData); err != nil {
		return err
	}
	if err := s.saveConfig(c); err != nil {
		return errors.Join(err, s.Secrets.Save(originalSecrets))
	}
	return nil
}

func (s ServerConnector) DisconnectAll() error {
	c, err := s.Store.Load()
	if err != nil {
		return err
	}
	if _, err := serverpool.New(c.Connections.Servers); err != nil {
		return fmt.Errorf("validate existing server profiles: %w", err)
	}
	ids := make([]string, 0, len(c.Connections.Servers))
	for _, profile := range c.Connections.Servers {
		ids = append(ids, profile.ID)
	}
	c.Connections.Servers = map[string]config.ServerProfile{}
	c.Connections.Server = config.Connection{Status: "not-connected"}
	delete(c.MCP.Servers, "ivoai-context")
	delete(c.MCP.Servers, "ivoai-memory")
	secretData, err := s.Secrets.Load()
	if err != nil {
		return err
	}
	originalSecrets := cloneSecrets(secretData)
	for _, id := range ids {
		delete(secretData.Servers, id)
	}
	secretData.Server = nil
	if err := s.Secrets.Save(secretData); err != nil {
		return err
	}
	if err := s.saveConfig(c); err != nil {
		return errors.Join(err, s.Secrets.Save(originalSecrets))
	}
	return nil
}

func (s ServerConnector) TestProfile(ctx context.Context, profile config.ServerProfile, credential secrets.ClientCredential) (ProfileHealth, error) {
	result := ProfileHealth{}
	if !profile.Enabled || profile.Status != "connected" {
		return result, errors.New("server profile is disabled or disconnected")
	}
	base, err := ValidateBaseURL(profile.URL)
	if err != nil {
		return result, err
	}
	if s.Client == nil {
		s.Client = SecureHTTPClient()
	}
	discovery, err := s.discover(ctx, base)
	if err != nil {
		return result, err
	}
	result.Reachable = true
	result.ProtocolCompatible = discovery.ProtocolVersion == ProtocolVersion
	if !result.ProtocolCompatible {
		return result, fmt.Errorf("incompatible server protocol %d", discovery.ProtocolVersion)
	}
	if err := s.health(ctx, base, discovery.HealthEndpoint); err != nil {
		return result, err
	}
	if err := s.ready(ctx, base, discovery.ReadyEndpoint); err != nil {
		return result, err
	}
	result.Ready = true
	contextEndpoint := resolveEndpoint(base, discovery.ContextMCPEndpoint)
	if contextEndpoint != "" {
		if err := s.probeMCP(ctx, contextEndpoint, credential.Token); err == nil {
			result.ContextAvailable = true
		}
	}
	if discovery.Features["memory"] {
		memoryEndpoint := resolveEndpoint(base, discovery.MemoryMCPEndpoint)
		if memoryEndpoint != "" {
			if err := s.probeMCP(ctx, memoryEndpoint, credential.Token); err == nil {
				result.MemoryAvailable = true
			}
		}
	}
	return result, nil
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
	// Keep the one-time credential out of proxy-parsed JSON. Authorization is
	// already the required transport for issued client credentials and is less
	// likely to be rewritten by reverse-proxy request-body protections.
	payload, _ := json.Marshal(enrollmentRequest{ClientName: clientName, RequestedScopes: []string{"context:read", "memory:read", "memory:write", "status:read", "doctor:read", "connector:read"}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolveEndpoint(base, endpoint), bytes.NewReader(payload))
	if err != nil {
		return enrollmentResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", enrollmentAuthorizationScheme+code)
	// Repeat non-secret enrollment metadata in headers. Some reverse proxies
	// apply request-body inspection rules that can discard a small JSON body;
	// the gateway treats these explicit headers as the authoritative transport
	// while retaining the JSON body for compatibility with older servers.
	req.Header.Set(enrollmentClientNameHeader, clientName)
	req.Header.Set(enrollmentScopesHeader, strings.Join([]string{"context:read", "memory:read", "memory:write", "status:read", "doctor:read", "connector:read"}, ","))
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
	body, err := io.ReadAll(io.LimitReader(r, maxResponse+1))
	if err != nil {
		return err
	}
	if len(body) > maxResponse {
		return errors.New("server response exceeds size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("server response contains multiple JSON values")
		}
		return fmt.Errorf("server response contains trailing data: %w", err)
	}
	return nil
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
	if err != nil || ref.User != nil || ref.Fragment != "" || ref.RawQuery != "" {
		return ""
	}
	if ref.IsAbs() {
		if ref.Scheme != base.Scheme || !strings.EqualFold(ref.Host, base.Host) {
			return ""
		}
		return ref.String()
	}
	if ref.Host != "" {
		return ""
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

func cloneConnectionConfig(value config.Config) config.Config {
	copy := value
	copy.Connections.Servers = make(map[string]config.ServerProfile, len(value.Connections.Servers))
	for alias, profile := range value.Connections.Servers {
		profileCopy := profile
		profileCopy.Features = make(map[string]bool, len(profile.Features))
		for feature, enabled := range profile.Features {
			profileCopy.Features[feature] = enabled
		}
		copy.Connections.Servers[alias] = profileCopy
	}
	copy.MCP.Servers = make(map[string]config.MCPServer, len(value.MCP.Servers))
	for name, server := range value.MCP.Servers {
		copy.MCP.Servers[name] = server
	}
	return copy
}

func cloneSecrets(value secrets.Data) secrets.Data {
	copy := value
	copy.Servers = make(map[string]secrets.ClientCredential, len(value.Servers))
	for id, credential := range value.Servers {
		credentialCopy := credential
		credentialCopy.Scopes = append([]string(nil), credential.Scopes...)
		copy.Servers[id] = credentialCopy
	}
	if value.Server != nil {
		credential := *value.Server
		credential.Scopes = append([]string(nil), value.Server.Scopes...)
		copy.Server = &credential
	}
	return copy
}
