package connections

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

const ServerTokenEnvironment = "IVOAI_SERVER_TOKEN"

type AgentMCP struct {
	Runner       platform.Runner
	CodexBinary  string
	ClaudeBinary string
}

// ConfigureRemote registers discovered HTTP MCP endpoints through each
// agent's official CLI. Config files contain only an environment reference;
// the scoped bearer credential is injected by ivoai when launching the agent.
func (m AgentMCP) ConfigureRemote(ctx context.Context, servers map[string]config.MCPServer) error {
	var failures []error
	names := make([]string, 0, len(servers))
	for name, server := range servers {
		if server.Enabled && (server.Kind == "context" || server.Kind == "memory") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		server := servers[name]
		if err := m.configureCodex(ctx, name, server.URL); err != nil {
			failures = append(failures, err)
		}
		if err := m.configureClaude(ctx, name, server.URL); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (m AgentMCP) RemoveRemote(ctx context.Context) error {
	var failures []error
	for _, name := range []string{"ivoai-context", "ivoai-memory"} {
		if err := m.remove(ctx, "codex", m.CodexBinary, name); err != nil {
			failures = append(failures, err)
		}
		if err := m.remove(ctx, "claude", m.ClaudeBinary, name); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (m AgentMCP) configureCodex(ctx context.Context, name, endpoint string) error {
	path, err := m.binary("codex", m.CodexBinary)
	if err != nil {
		return err
	}
	_, _ = m.Runner.Run(ctx, path, []string{"mcp", "remove", name}, platform.RunOptions{Timeout: 30 * time.Second})
	args := []string{"mcp", "add", name, "--url", endpoint, "--bearer-token-env-var", ServerTokenEnvironment}
	if _, err := m.Runner.Run(ctx, path, args, platform.RunOptions{Timeout: 30 * time.Second}); err != nil {
		return fmt.Errorf("register %s in Codex: %w", name, err)
	}
	return nil
}

func (m AgentMCP) configureClaude(ctx context.Context, name, endpoint string) error {
	path, err := m.binary("claude", m.ClaudeBinary)
	if err != nil {
		return err
	}
	_, _ = m.Runner.Run(ctx, path, []string{"mcp", "remove", "--scope", "user", name}, platform.RunOptions{Timeout: 30 * time.Second})
	header := "Authorization: Bearer ${" + ServerTokenEnvironment + "}"
	args := []string{"mcp", "add", "--scope", "user", "--transport", "http", name, endpoint, "--header", header}
	if _, err := m.Runner.Run(ctx, path, args, platform.RunOptions{Timeout: 30 * time.Second}); err != nil {
		return fmt.Errorf("register %s in Claude: %w", name, err)
	}
	return nil
}

func (m AgentMCP) remove(ctx context.Context, executable, configuredPath, name string) error {
	path, err := m.binary(executable, configuredPath)
	if err != nil {
		return err
	}
	args := []string{"mcp", "remove", name}
	if executable == "claude" {
		args = []string{"mcp", "remove", "--scope", "user", name}
	}
	result, err := m.Runner.Run(ctx, path, args, platform.RunOptions{Timeout: 30 * time.Second})
	if err != nil {
		output := strings.ToLower(result.Stdout + "\n" + result.Stderr)
		// Official CLIs return a non-zero exit code when an idempotent removal
		// targets an entry that does not exist.
		if strings.Contains(output, "no mcp server named") || strings.Contains(output, "not found") || strings.Contains(output, "does not exist") {
			return nil
		}
		return fmt.Errorf("remove %s from %s: %w", name, executable, err)
	}
	return nil
}

func (m AgentMCP) binary(executable, configuredPath string) (string, error) {
	if configuredPath != "" {
		return configuredPath, nil
	}
	path, err := m.Runner.LookPath(executable)
	if err != nil || path == "" {
		return "", fmt.Errorf("%s is not installed", executable)
	}
	return path, nil
}
