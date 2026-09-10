package app

import (
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
)

func TestAutoRejectsControlPlaneConfigOverrides(t *testing.T) {
	a := &App{}
	for _, args := range [][]string{
		{"-c", "sandbox_mode=\"danger-full-access\""},
		{"--config=mcp_servers.personal.enabled=true"},
		{"-cmcp_servers.personal.command=\"fixture\""},
		{"-c", `"mcp_servers".personal.enabled=true`},
		{"--config", "features.multi_agent=true"},
		{"--mcp-config", "personal.json"}, {"--allowedTools", "Bash"},
		{"--settings=personal.json"}, {"--add-dir", "/"},
		{"--profile", "unrestricted"},
	} {
		if _, err := a.autoBridgeArgs("codex", args, "fixture", t.TempDir(), "instructions", config.Default()); err == nil || !strings.Contains(err.Error(), "AUTO") {
			t.Fatalf("control-plane override was not rejected: %v", args)
		}
	}
}
