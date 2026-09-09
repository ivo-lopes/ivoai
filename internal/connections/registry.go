package connections

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/secrets"
)

type Registry struct{ Store *config.Store }

func (r Registry) Add(name string, server config.MCPServer) error {
	name = strings.TrimSpace(name)
	if !validMCPName(name) {
		return fmt.Errorf("invalid MCP name %q", name)
	}
	if IsManagedMCPName(name) {
		return fmt.Errorf("MCP name is reserved for the IVOAI control plane")
	}
	u, err := url.Parse(server.URL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("invalid MCP URL")
	}
	ip := net.ParseIP(u.Hostname())
	loopback := strings.EqualFold(u.Hostname(), "localhost") || (ip != nil && ip.IsLoopback())
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return fmt.Errorf("MCP URL must use HTTPS (HTTP only on loopback)")
	}
	c, err := r.Store.Load()
	if err != nil {
		return err
	}
	if c.MCP.Servers == nil {
		c.MCP.Servers = map[string]config.MCPServer{}
	}
	if old, exists := c.MCP.Servers[name]; exists {
		if old.Kind != "external" {
			return fmt.Errorf("cannot replace managed MCP")
		}
		if old.URL != server.URL && old.AuthMode != "" && old.AuthMode != "none" {
			return fmt.Errorf("remove authentication before changing MCP endpoint")
		}
		server.ID, server.AuthMode = old.ID, old.AuthMode
	}
	if server.ID == "" {
		id, err := newMCPID()
		if err != nil {
			return err
		}
		server.ID = id
	}
	c.MCP.Servers[name] = server
	return r.Store.Save(c)
}

func (r Registry) Remove(name string) error {
	c, err := r.Store.Load()
	if err != nil {
		return err
	}
	entry, exists := c.MCP.Servers[name]
	if !exists {
		return nil
	}
	if entry.Kind != "external" {
		return fmt.Errorf("cannot remove managed MCP through external registry")
	}
	if entry.ID != "" {
		store := secrets.Store{Path: r.Store.Paths.Secrets}
		data, err := store.Load()
		if err != nil {
			return err
		}
		delete(data.MCP, entry.ID)
		if err := store.Save(data); err != nil {
			return err
		}
	}
	delete(c.MCP.Servers, name)
	return r.Store.Save(c)
}

func validMCPName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

func IsManagedMCPName(name string) bool {
	switch name {
	case "ivoai-memory", "ivoai-context", "ivoai-orchestrator":
		return true
	}
	return false
}

func newMCPID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return "mcp_" + hex.EncodeToString(bytes[:]), nil
}

func (r Registry) List() (map[string]config.MCPServer, error) {
	c, err := r.Store.Load()
	if err != nil {
		return nil, err
	}
	result := make(map[string]config.MCPServer, len(c.MCP.Servers))
	for k, v := range c.MCP.Servers {
		result[k] = v
	}
	return result, nil
}
