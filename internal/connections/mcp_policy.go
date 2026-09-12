package connections

import (
	"context"
	"errors"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
)

// Mutation boundaries reload the canonical registry and touch only one entry.
func (r Registry) updateMCP(name string, update func(*config.MCPServer) error) error {
	cfg, err := r.Store.Load()
	if err != nil {
		return err
	}
	entry, ok := cfg.MCP.Servers[name]
	if !ok || entry.Kind != "external" || IsManagedMCPName(name) {
		return errors.New("external MCP not found or managed identity protected")
	}
	if err := update(&entry); err != nil {
		return err
	}
	cfg.MCP.Servers[name] = entry
	return r.Store.Save(cfg)
}

func (r Registry) Enable(name string, enabled bool) error {
	return r.updateMCP(name, func(entry *config.MCPServer) error { entry.Enabled = enabled; return nil })
}

func (r Registry) SetPolicy(name, policy string, direct bool) error {
	return r.updateMCP(name, func(entry *config.MCPServer) error {
		if direct {
			if policy != "disabled" && policy != "read_only" {
				return errors.New("direct policy must be disabled or read_only")
			}
			entry.DirectPolicy = policy
		} else {
			if policy != "read_only" && policy != "read_auto_ask_mutating" {
				return errors.New("policy must be read_only or read_auto_ask_mutating")
			}
			entry.Policy = policy
		}
		return nil
	})
}

// Refresh is the explicit initialize/tools-list boundary; no tool is invoked.
// A failed refresh invalidates cached inventory rather than trusting stale grants.
func (r Registry) Refresh(ctx context.Context, name string) (int, error) {
	entries, err := r.List()
	if err != nil {
		return 0, err
	}
	entry, ok := entries[name]
	if !ok || entry.Kind != "external" || IsManagedMCPName(name) {
		return 0, errors.New("external MCP not found")
	}
	tools, probeErr := r.DiscoverTools(ctx, entry)
	err = r.updateMCP(name, func(current *config.MCPServer) error {
		if current.ID != entry.ID || current.URL != entry.URL || current.AuthMode != entry.AuthMode {
			return errors.New("MCP changed during discovery; test again")
		}
		current.Tools = nil
		current.ProbedAt = time.Now().UTC().Format(time.RFC3339)
		current.Health = "HEALTHY"
		if probeErr != nil {
			current.Health = "PROTOCOL_ERROR"
			var failure *ProbeError
			if errors.As(probeErr, &failure) {
				current.Health = failure.Health
			}
		} else {
			for _, tool := range tools {
				current.Tools = append(current.Tools, tool.Metadata)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(tools), probeErr
}
