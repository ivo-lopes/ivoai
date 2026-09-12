package connections

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/secrets"
)

func validHeaderValue(value string) bool {
	if len(value) == 0 || len(value) > 8192 {
		return false
	}
	for _, c := range value {
		if c < 32 || c > 126 {
			return false
		}
	}
	return true
}

func validExternalHeader(name string) bool {
	if len(name) == 0 || len(name) > 128 {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	switch strings.ToLower(name) {
	case "authorization", "proxy-authorization", "host", "cookie", "set-cookie", "connection", "transfer-encoding", "content-length", "content-type", "accept", "upgrade", "te", "trailer", "keep-alive", "mcp-session-id", "mcp-protocol-version":
		return false
	}
	return true
}

func (r Registry) SetBearer(name, token string) error {
	if !validHeaderValue(token) || strings.Contains(token, " ") {
		return errors.New("invalid bearer credential (empty, oversized or unsafe characters)")
	}
	return r.changeAuth(name, func(value *secrets.MCPCredential) { value.Bearer = token })
}

func (r Registry) SetHeader(name, header, value string) error {
	if !validExternalHeader(header) || !validHeaderValue(value) {
		return errors.New("invalid external MCP header name or value; use bearer authentication for Authorization")
	}
	return r.changeAuth(name, func(credential *secrets.MCPCredential) {
		if credential.Headers == nil {
			credential.Headers = map[string]string{}
		}
		credential.Headers[http.CanonicalHeaderKey(header)] = value
	})
}

func (r Registry) ClearAuth(name string) error {
	return r.changeAuth(name, func(credential *secrets.MCPCredential) { credential.Bearer = ""; credential.Headers = nil })
}

func (r Registry) changeAuth(name string, change func(*secrets.MCPCredential)) error {
	cfg, err := r.Store.Load()
	if err != nil {
		return err
	}
	entry, ok := cfg.MCP.Servers[name]
	if !ok || entry.Kind != "external" || !validMCPName(name) || IsManagedMCPName(name) {
		return errors.New("external MCP not found")
	}
	if entry.ID == "" {
		entry.ID, err = newMCPID()
		if err != nil {
			return err
		}
	}
	private := secrets.Store{Path: r.Store.Paths.Secrets}
	data, err := private.Load()
	if err != nil {
		return err
	}
	if data.MCP == nil {
		data.MCP = map[string]secrets.MCPCredential{}
	}
	credential := data.MCP[entry.ID]
	if credential.Endpoint != "" && credential.Endpoint != entry.URL {
		return errors.New("MCP credential endpoint mismatch; remove and re-register entry")
	}
	credential.Endpoint = entry.URL
	entry.Tools, entry.Health, entry.ProbedAt = nil, "UNKNOWN", ""
	change(&credential)
	if len(credential.Headers) > 16 {
		return errors.New("external MCP header limit exceeded")
	}
	entry.AuthMode = "none"
	if len(credential.Headers) > 0 {
		entry.AuthMode = "header"
	}
	if credential.Bearer != "" {
		entry.AuthMode = "bearer"
	}
	if entry.AuthMode == "none" {
		delete(data.MCP, entry.ID)
	} else {
		data.MCP[entry.ID] = credential
	}
	// Persist the non-secret ID first. An interrupted initial save fails closed;
	// rotation retains the previous credential until the private save succeeds.
	cfg.MCP.Servers[name] = entry
	if err := r.Store.Save(cfg); err != nil {
		return err
	}
	return private.Save(data)
}

// Headers must only be called at the HTTP or process-local execution boundary.
func (r Registry) Headers(entry config.MCPServer) (http.Header, error) {
	result := http.Header{}
	if entry.AuthMode == "" || entry.AuthMode == "none" {
		return result, nil
	}
	if entry.AuthMode != "bearer" && entry.AuthMode != "header" {
		return nil, errors.New("unsupported external MCP authentication mode")
	}
	data, err := (secrets.Store{Path: r.Store.Paths.Secrets}).Load()
	if err != nil {
		return nil, err
	}
	credential, ok := data.MCP[entry.ID]
	if !ok || entry.ID == "" || credential.Endpoint != entry.URL {
		return nil, errors.New("external MCP credential missing or endpoint mismatch; configure authentication")
	}
	if entry.AuthMode == "bearer" {
		if !validHeaderValue(credential.Bearer) || strings.Contains(credential.Bearer, " ") {
			return nil, errors.New("invalid stored MCP bearer credential")
		}
		result.Set("Authorization", "Bearer "+credential.Bearer)
	}
	for name, value := range credential.Headers {
		if !validExternalHeader(name) || !validHeaderValue(value) {
			return nil, errors.New("invalid stored MCP header")
		}
		result.Set(name, value)
	}
	return result, nil
}
