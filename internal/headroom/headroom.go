package headroom

import (
	"context"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

type Status struct {
	Installed         bool   `json:"installed"`
	Enabled           bool   `json:"enabled"`
	Version           string `json:"version"`
	Healthy           bool   `json:"healthy"`
	CodexCompatible   bool   `json:"codex_compatible"`
	ClaudeCompatible  bool   `json:"claude_compatible"`
	InteractiveLaunch string `json:"interactive_launch"`
}
type Manager struct {
	Runner platform.Runner
	Binary string
}

func (m Manager) Inspect(ctx context.Context, enabled bool) Status {
	status := Status{Enabled: enabled, InteractiveLaunch: "not_run"}
	path := m.Binary
	var err error
	if path == "" {
		path, err = m.Runner.LookPath("headroom")
	}
	if err != nil {
		return status
	}
	status.Installed = true
	r, err := m.Runner.Run(ctx, path, []string{"--version"}, platform.RunOptions{Timeout: 10 * time.Second})
	if err == nil {
		status.Version = strings.TrimSpace(r.Stdout)
		status.Healthy = true
	}
	status.CodexCompatible = m.supports(ctx, path, "codex")
	status.ClaudeCompatible = m.supports(ctx, path, "claude")
	return status
}

func (m Manager) supports(ctx context.Context, path, agent string) bool {
	_, err := m.Runner.Run(ctx, path, []string{"wrap", agent, "--help"}, platform.RunOptions{Timeout: 15 * time.Second})
	return err == nil
}
