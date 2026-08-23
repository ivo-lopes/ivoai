package memory

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

type Configuration struct {
	MCPEndpoint  string
	HooksBaseURL string
	Token        string
	InstallMCP   bool
	InstallHooks bool
}

type Manager struct {
	Runner   platform.Runner
	Out, Err io.Writer
	HooksDir string
	Binary   string
}

// Configure delegates all third-party config merging to ai-memory's supported,
// idempotent installers. Authentication is passed by environment, never argv.
func (m Manager) Configure(ctx context.Context, serverURL, token string) error {
	return m.ConfigureWith(ctx, Configuration{
		MCPEndpoint:  serverURL,
		HooksBaseURL: serverURL,
		Token:        token,
		InstallMCP:   true,
		InstallHooks: true,
	})
}

// ConfigureHooks installs only lifecycle hooks. Remote MCP registrations are
// managed by ivoai through the official agent CLIs so discovery endpoints do
// not have to match ai-memory's conventional /mcp URL layout.
func (m Manager) ConfigureHooks(ctx context.Context, hooksBaseURL, token string) error {
	return m.ConfigureWith(ctx, Configuration{HooksBaseURL: hooksBaseURL, Token: token, InstallHooks: true})
}

func (m Manager) ConfigureWith(ctx context.Context, configuration Configuration) error {
	if os.Getenv("IVOAI_TEST_MODE") == "1" {
		return nil
	}
	path, err := m.binary()
	if err != nil {
		return err
	}
	if !configuration.InstallMCP && !configuration.InstallHooks {
		return nil
	}
	for _, agent := range []string{"codex", "claude-code"} {
		if configuration.InstallMCP {
			env := memoryEnv(configuration.MCPEndpoint, configuration.Token)
			if _, err := m.Runner.Run(ctx, path, []string{"install-mcp", "--client", agent, "--apply"}, platform.RunOptions{Env: env, Stdout: m.Out, Stderr: m.Err, Timeout: 2 * time.Minute}); err != nil {
				return fmt.Errorf("configure ai-memory MCP for %s: %w", agent, err)
			}
		}
		if configuration.InstallHooks {
			hookArgs := []string{"install-hooks", "--agent", agent, "--apply"}
			if m.HooksDir != "" {
				hookArgs = append(hookArgs, "--hooks-dir", m.HooksDir)
			}
			// Hook configuration is persistent. Never let the installer bake a
			// bearer into an agent settings command; ivoai supplies the token only
			// in the launched agent's process environment.
			env := memoryEnv(configuration.HooksBaseURL, "")
			if _, err := m.Runner.Run(ctx, path, hookArgs, platform.RunOptions{Env: env, Stdout: m.Out, Stderr: m.Err, Timeout: 2 * time.Minute}); err != nil {
				return fmt.Errorf("configure ai-memory hooks for %s: %w", agent, err)
			}
		}
	}
	return nil
}

func memoryEnv(serverURL, token string) []string {
	env := make([]string, 0, 2)
	if strings.TrimSpace(serverURL) != "" {
		env = append(env, "AI_MEMORY_SERVER_URL="+strings.TrimRight(serverURL, "/"))
	}
	if token != "" {
		env = append(env, "AI_MEMORY_AUTH_TOKEN="+token)
	}
	return env
}

// Disable removes only ai-memory-owned MCP entries and hooks. The upstream
// uninstaller is idempotent and preserves unrelated agent configuration.
func (m Manager) Disable(ctx context.Context) error {
	if os.Getenv("IVOAI_TEST_MODE") == "1" {
		return nil
	}
	path, err := m.binary()
	if err != nil {
		return err
	}
	if _, err := m.Runner.Run(ctx, path, []string{"uninstall", "--apply"}, platform.RunOptions{Stdout: m.Out, Stderr: m.Err, Timeout: 2 * time.Minute}); err != nil {
		return fmt.Errorf("remove ai-memory integration: %w", err)
	}
	return nil
}

func (m Manager) binary() (string, error) {
	if m.Binary != "" {
		return m.Binary, nil
	}
	path, err := m.Runner.LookPath("ai-memory")
	if err != nil || path == "" {
		return "", fmt.Errorf("ai-memory is not installed")
	}
	return path, nil
}

func (m Manager) Status(ctx context.Context) (string, error) {
	path := m.Binary
	var err error
	if path == "" {
		path, err = m.Runner.LookPath("ai-memory")
	}
	if err != nil {
		return "not-installed", nil
	}
	r, err := m.Runner.Run(ctx, path, []string{"status", "--json"}, platform.RunOptions{Timeout: 10 * time.Second})
	if err != nil {
		return "installed / server unavailable", nil
	}
	if r.Stdout == "" {
		return "installed", nil
	}
	return "ready", nil
}
