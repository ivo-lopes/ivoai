package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
)

func TestSharedKnowledgeContractUsesOfficialClientInstructionChannels(t *testing.T) {
	cfg := config.Default()
	cfg.MCP.Servers["ivoai-memory"] = config.MCPServer{Enabled: true, Kind: "memory"}
	cfg.MCP.Servers["ivoai-context"] = config.MCPServer{Enabled: true, Kind: "context"}
	for _, executor := range []string{"codex", "claude"} {
		args := sharedKnowledgeAgentArgs(executor, []string{"original"}, cfg)
		joined := strings.Join(args, "\n")
		for _, expected := range []string{"memory_query", "memory_write_page", "context_search", "original"} {
			if !strings.Contains(joined, expected) {
				t.Fatalf("%s instructions missing %q: %q", executor, expected, joined)
			}
		}
		if executor == "codex" && !strings.Contains(joined, "developer_instructions=") {
			t.Fatalf("Codex instructions=%q", joined)
		}
		if executor == "codex" {
			for _, expected := range []string{
				`mcp_servers.ivoai-memory.tools.memory_query.approval_mode="approve"`,
				`mcp_servers.ivoai-memory.tools.memory_read_page.approval_mode="approve"`,
				`mcp_servers.ivoai-context.tools.context_search.approval_mode="approve"`,
			} {
				if !strings.Contains(joined, expected) {
					t.Fatalf("Codex read approval missing %q: %q", expected, joined)
				}
			}
		}
		if executor == "claude" && args[0] != "--append-system-prompt" {
			t.Fatalf("Claude instructions=%q", joined)
		}
	}
}

func TestSharedKnowledgeContractDefinesBoundedOneCallRecall(t *testing.T) {
	for _, expected := range []string{
		"one-call fast path",
		`memory_read_page once with only {"query":"essential terms"}`,
		"answer immediately and stop",
		"call memory_query once",
		"do not make further memory calls for a simple recall",
		"never pass id, page_id, scope",
		"write exactly one canonical page",
		"Never duplicate the same fact across scopes",
		"Context/RAG is read-only",
	} {
		if !strings.Contains(sharedKnowledgeInstructions, expected) {
			t.Fatalf("shared-knowledge fast-path contract missing %q", expected)
		}
	}
}

func TestCodexKnowledgeArgsDoNotCreateUnregisteredMCPServers(t *testing.T) {
	joined := strings.Join(sharedKnowledgeAgentArgs("codex", nil, config.Default()), "\n")
	if strings.Contains(joined, "approval_mode") {
		t.Fatalf("unregistered optional MCP server received an override: %q", joined)
	}
}

func TestManagedCodexAcceptsExactVerifiedCompanion(t *testing.T) {
	root := t.TempDir()
	codex := filepath.Join(root, "codex")
	host := filepath.Join(root, "codex-code-mode-host")
	for _, path := range []string{codex, host} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	state := config.State{Components: map[string]config.ComponentState{
		"codex":                {Installed: true, Managed: true, Version: "1.0.0", Path: codex},
		"codex-code-mode-host": {Installed: true, Managed: true, Version: "1.0.0", Path: host},
	}}
	if err := validateManagedAgentRuntime("codex", state); err != nil {
		t.Fatalf("verified companion refused: %v", err)
	}
}

func TestAutomaticInstructionsIncludeSharedKnowledgeContract(t *testing.T) {
	instructions := automaticInstructions(true)
	for _, expected := range []string{"memory_query", "memory_write_page", "context_search", "orchestration_checkpoint"} {
		if !strings.Contains(instructions, expected) {
			t.Fatalf("automatic instructions missing %q", expected)
		}
	}
}
