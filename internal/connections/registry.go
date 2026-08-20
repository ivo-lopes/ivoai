package connections

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/config"
)

type Registry struct{ Store *config.Store }

func (r Registry) Add(name string, server config.MCPServer) error {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, " /\\") {
		return fmt.Errorf("invalid MCP name %q", name)
	}
	u, err := url.Parse(server.URL)
	if err != nil || u.Host == "" || u.User != nil {
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
	c.MCP.Servers[name] = server
	return r.Store.Save(c)
}

func (r Registry) Remove(name string) error {
	c, err := r.Store.Load()
	if err != nil {
		return err
	}
	delete(c.MCP.Servers, name)
	return r.Store.Save(c)
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
