package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
)

func TestExternalMCPProjectionKeepsPATOnlyInIVOAI(t *testing.T) {
	a := autoTestApp(t, t.TempDir(), "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	if err := a.MCPAdd("plane", "https://example.invalid/mcp"); err != nil {
		t.Fatal(err)
	}
	if err := a.MCPSetBearer("plane", "fixture-private-pat"); err != nil {
		t.Fatal(err)
	}
	if err := a.MCPSetHeader("plane", "X-Workspace-Slug", "fixture-private-header"); err != nil {
		t.Fatal(err)
	}
	cfg, err := a.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, executor := range []string{"codex", "claude", "opencode"} {
		t.Run(executor, func(t *testing.T) {
			runtime := t.TempDir()
			knowledge, err := a.prepareSessionKnowledgeWithApprovals(context.Background(), cfg, nil, executor, runtime, nil, nil, true)
			if err != nil {
				t.Fatal(err)
			}
			defer knowledge.close()
			combined := strings.Join(knowledge.args, "\n") + strings.Join(knowledge.environment, "\n")
			files, _ := filepath.Glob(filepath.Join(runtime, "*.json"))
			for _, path := range files {
				body, _ := os.ReadFile(path)
				combined += string(body)
			}
			if strings.Contains(combined, "fixture-private") || strings.Contains(combined, "https://example.invalid") {
				t.Fatal("upstream secret/endpoint escaped parent")
			}
			if !strings.HasPrefix(knowledge.config.MCP.Servers["plane"].URL, "http://127.0.0.1:") {
				t.Fatal("external MCP was not projected")
			}
			if cfg.MCP.Servers["plane"].URL != "https://example.invalid/mcp" {
				t.Fatal("caller config mutated")
			}
			if strings.Contains(strings.Join(managedFrontendEnvironment(knowledge.environment), "\n"), externalMCPTokenEnvironment) {
				t.Fatal("frontend received executor capability")
			}
			if executor == "codex" && !strings.Contains(combined, `.default_tools_approval_mode="approve"`) {
				t.Fatal("headless never policy not separated from gateway approval")
			}
			if executor == "opencode" {
				state := config.State{Components: map[string]config.ComponentState{"opencode": {Installed: true, Managed: true, Version: "1.18.25", Path: appExecutable(t, t.TempDir(), "opencode", "#!/bin/sh\nexit 0\n")}}}
				native := a.nativeOpenCode(knowledge.config, state, t.TempDir(), runtime, knowledge.environment, false)
				if native == nil || native.Options.NativeMCP["plane"] == nil || native.Options.NativePermissions["plane_*"] != "allow" {
					t.Fatal("native managed projection absent")
				}
				worker := a.nativeOpenCode(knowledge.config, state, t.TempDir(), runtime, knowledge.environment, true)
				if worker == nil || worker.Options.NativeMCP["plane"] != nil || strings.Contains(strings.Join(worker.Options.Environment, "\n"), externalMCPTokenEnvironment) {
					t.Fatal("advisory worker inherited arbitrary external MCP")
				}
			}
		})
	}
	persisted, err := a.Store.Load()
	if err != nil || persisted.MCP.Servers["plane"].URL != cfg.MCP.Servers["plane"].URL || persisted.MCP.Servers["plane"].SessionApproved {
		t.Fatal("runtime projection persisted")
	}
}

func TestExternalMCPDirectInteractiveRetainsNativeApproval(t *testing.T) {
	a := autoTestApp(t, t.TempDir(), "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	if err := a.MCPAdd("external", "https://example.invalid/mcp"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := a.Store.Load()
	cfg.OpenCode.PermissionMode = "interactive"
	knowledge, err := a.prepareSessionKnowledge(context.Background(), cfg, nil, "codex", t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer knowledge.close()
	if strings.Contains(strings.Join(knowledge.args, "\n"), "approval_mode") {
		t.Fatal("native interactive approval silently disabled")
	}
}

func TestExternalMCPCodexNamesAndCollision(t *testing.T) {
	cfg := config.Config{}
	entry := config.MCPServer{Enabled: true, Kind: "external", URL: "http://127.0.0.1:1234/mcp/0", HeaderEnv: map[string]string{"Authorization": externalMCPTokenEnvironment}}
	cfg.MCP.Servers = map[string]config.MCPServer{"plane_mcp_mindsite": entry}
	args, err := processLocalExternalMCPArgs("codex", t.TempDir(), cfg)
	if err != nil || !strings.Contains(strings.Join(args, "\n"), "mcp_servers.plane_mcp_mindsite.url=") || strings.Contains(strings.Join(args, "\n"), `mcp_servers."`) {
		t.Fatal("Codex override changed MCP identity")
	}
	cfg.MCP.Servers = map[string]config.MCPServer{"company.a": entry, "company_a": entry}
	if _, err := processLocalExternalMCPArgs("codex", t.TempDir(), cfg); err == nil {
		t.Fatal("colliding aliases accepted")
	}
}

func TestExternalMCPSafeEndpointDisplay(t *testing.T) {
	value := safeMCPDisplayURL("https://user:password@example.invalid/mcp?secret=fixture#token")
	for _, private := range []string{"password", "secret", "fixture", "token"} {
		if strings.Contains(value, private) {
			t.Fatal("unsafe endpoint display")
		}
	}
}
