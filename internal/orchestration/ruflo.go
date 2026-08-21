package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

const profileVersion = 2
const serverName = "ivoai-ruflo"

var safeTools = []string{
	"agent_list",
	"agent_status",
	"swarm_init",
	"swarm_status",
	"task_create",
	"task_list",
	"task_status",
	"task_cancel",
	"system_health",
	"system_info",
}

var providerVariables = []string{
	"ANTHROPIC_API_KEY",
	"OPENAI_API_KEY",
	"OPENROUTER_API_KEY",
	"OLLAMA_API_KEY",
	"GOOGLE_API_KEY",
	"GOOGLE_GEMINI_API_KEY",
	"GEMINI_API_KEY",
	"AZURE_OPENAI_API_KEY",
	"GROQ_API_KEY",
	"AWS_ACCESS_KEY_ID",
	"AWS_SECRET_ACCESS_KEY",
	"AWS_SESSION_TOKEN",
	"GOOGLE_APPLICATION_CREDENTIALS",
}

type Status struct {
	Installed         bool   `json:"installed"`
	Version           string `json:"version"`
	SafeMode          bool   `json:"safe_mode"`
	ProviderExecution bool   `json:"provider_execution"`
	DurableMemory     bool   `json:"durable_memory"`
}

type Profile struct {
	Version           int      `json:"version"`
	Tools             []string `json:"tools"`
	ProviderExecution bool     `json:"provider_execution"`
	DurableMemory     bool     `json:"durable_memory"`
}

type Manager struct {
	Runner       platform.Runner
	Binary       string
	CodexBinary  string
	ClaudeBinary string
	ProfileDir   string
}

// Configure registers a least-privilege Ruflo MCP profile through the official
// agent CLIs. The wrapper strips every supported PAYG provider credential,
// advertises only coordination tools, and uses process-local memory so durable
// cross-session state remains exclusively owned by ai-memory.
func (m Manager) Configure(ctx context.Context, enabled bool) error {
	if !enabled {
		return m.Disable(ctx)
	}
	if m.Binary == "" {
		return errors.New("ruflo is not installed")
	}
	if m.ProfileDir == "" {
		return errors.New("ruflo profile directory is not configured")
	}
	dir := m.profileDirectory()
	if err := platform.EnsurePrivateDir(dir); err != nil {
		return err
	}
	profile := Profile{Version: profileVersion, Tools: append([]string(nil), safeTools...)}
	b, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	if err := platform.AtomicWritePrivate(append(b, '\n'), m.profilePath()); err != nil {
		return err
	}
	if err := platform.AtomicWritePrivate([]byte(m.wrapper()), m.wrapperPath()); err != nil {
		return err
	}
	if err := os.Chmod(m.wrapperPath(), 0o700); err != nil {
		return err
	}
	if os.Getenv("IVOAI_TEST_MODE") == "1" {
		return nil
	}
	var failures []error
	if err := m.registerCodex(ctx); err != nil {
		failures = append(failures, err)
	}
	if err := m.registerClaude(ctx); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func (m Manager) Disable(ctx context.Context) error {
	var failures []error
	if os.Getenv("IVOAI_TEST_MODE") != "1" {
		if path, err := m.binary("codex", m.CodexBinary); err == nil {
			_, _ = m.Runner.Run(ctx, path, []string{"mcp", "remove", serverName}, platform.RunOptions{Timeout: 30 * time.Second})
		}
		if path, err := m.binary("claude", m.ClaudeBinary); err == nil {
			_, _ = m.Runner.Run(ctx, path, []string{"mcp", "remove", "--scope", "user", serverName}, platform.RunOptions{Timeout: 30 * time.Second})
		}
	}
	for _, path := range []string{m.wrapperPath(), m.profilePath()} {
		if path == "" {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (m Manager) Inspect(ctx context.Context) Status {
	status := Status{}
	path := m.Binary
	var err error
	if path == "" {
		path, err = m.Runner.LookPath("ruflo")
	}
	if err != nil || path == "" {
		return status
	}
	status.Installed = true
	if result, runErr := m.Runner.Run(ctx, path, []string{"--version"}, platform.RunOptions{Timeout: 10 * time.Second}); runErr == nil {
		status.Version = strings.TrimSpace(result.Stdout)
	}
	profile, err := m.loadProfile()
	if err == nil {
		status.ProviderExecution = profile.ProviderExecution
		status.DurableMemory = profile.DurableMemory
	}
	if err == nil && profile.Version == profileVersion && !profile.ProviderExecution && !profile.DurableMemory && slices.Equal(profile.Tools, safeTools) {
		if info, statErr := os.Stat(m.wrapperPath()); statErr == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o077 == 0 && info.Mode().Perm()&0o100 != 0 {
			status.SafeMode = true
		}
	}
	return status
}

func (m Manager) registerCodex(ctx context.Context) error {
	if err := ensureAgentConfigDirectory("CODEX_HOME", ".codex"); err != nil {
		return fmt.Errorf("prepare Codex configuration directory: %w", err)
	}
	path, err := m.binary("codex", m.CodexBinary)
	if err != nil {
		return err
	}
	_, _ = m.Runner.Run(ctx, path, []string{"mcp", "remove", serverName}, platform.RunOptions{Timeout: 30 * time.Second})
	args := []string{"mcp", "add", serverName, "--env", "CLAUDE_FLOW_MCP_TOOLS=" + strings.Join(safeTools, ","), "--", m.wrapperPath()}
	if _, err := m.Runner.Run(ctx, path, args, platform.RunOptions{Timeout: 30 * time.Second}); err != nil {
		return fmt.Errorf("register Ruflo in Codex: %w", err)
	}
	return nil
}

func (m Manager) registerClaude(ctx context.Context) error {
	if err := ensureAgentConfigDirectory("CLAUDE_CONFIG_DIR", ".claude"); err != nil {
		return fmt.Errorf("prepare Claude configuration directory: %w", err)
	}
	path, err := m.binary("claude", m.ClaudeBinary)
	if err != nil {
		return err
	}
	_, _ = m.Runner.Run(ctx, path, []string{"mcp", "remove", "--scope", "user", serverName}, platform.RunOptions{Timeout: 30 * time.Second})
	args := []string{"mcp", "add", "--scope", "user", serverName, "--env", "CLAUDE_FLOW_MCP_TOOLS=" + strings.Join(safeTools, ","), "--", m.wrapperPath()}
	if _, err := m.Runner.Run(ctx, path, args, platform.RunOptions{Timeout: 30 * time.Second}); err != nil {
		return fmt.Errorf("register Ruflo in Claude: %w", err)
	}
	return nil
}

func ensureAgentConfigDirectory(environment, fallback string) error {
	directory := strings.TrimSpace(os.Getenv(environment))
	if directory == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		directory = filepath.Join(home, fallback)
	}
	if !filepath.IsAbs(directory) {
		return fmt.Errorf("%s must be an absolute path", environment)
	}
	return platform.EnsurePrivateDir(directory)
}

func (m Manager) wrapper() string {
	var body strings.Builder
	body.WriteString("#!/bin/sh\nset -eu\n")
	body.WriteString("unset " + strings.Join(providerVariables, " ") + "\n")
	body.WriteString("export RUFLO_PROVIDER=ivoai-disabled\n")
	body.WriteString("export CLAUDE_FLOW_HOOKS_ENABLED=false\n")
	body.WriteString("export CLAUDE_FLOW_MEMORY_BACKEND=memory\n")
	body.WriteString("export CLAUDE_FLOW_MCP_TOOLS=" + shellQuote(strings.Join(safeTools, ",")) + "\n")
	body.WriteString("exec " + shellQuote(m.Binary) + " mcp start --tools \"$CLAUDE_FLOW_MCP_TOOLS\"\n")
	return body.String()
}

func (m Manager) loadProfile() (Profile, error) {
	var profile Profile
	b, err := os.ReadFile(m.profilePath())
	if err != nil {
		return profile, err
	}
	if err := json.Unmarshal(b, &profile); err != nil {
		return profile, err
	}
	return profile, nil
}

func (m Manager) binary(name, configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	path, err := m.Runner.LookPath(name)
	if err != nil || path == "" {
		return "", fmt.Errorf("%s is not installed", name)
	}
	return path, nil
}

func (m Manager) profileDirectory() string {
	if m.ProfileDir == "" {
		return ""
	}
	return filepath.Join(m.ProfileDir, "orchestration")
}

func (m Manager) profilePath() string {
	if m.profileDirectory() == "" {
		return ""
	}
	return filepath.Join(m.profileDirectory(), "ruflo-safe-profile.json")
}

func (m Manager) wrapperPath() string {
	if m.profileDirectory() == "" {
		return ""
	}
	return filepath.Join(m.profileDirectory(), "ruflo-safe")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
