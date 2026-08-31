package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/observability"
)

func TestOpenCodeUsesPrivateProcessLocalInstructionConfig(t *testing.T) {
	root := t.TempDir()
	environment, cleanup, err := openCodeInstructionEnvironment([]string{`OPENCODE_CONFIG_CONTENT={"model":"provider/model","instructions":["existing.md"]}`}, root, "managed skill body")
	if err != nil {
		t.Fatal(err)
	}
	value := environmentValue(environment, "OPENCODE_CONFIG_CONTENT")
	var configValue struct {
		Model        string   `json:"model"`
		Instructions []string `json:"instructions"`
	}
	if err := json.Unmarshal([]byte(value), &configValue); err != nil {
		t.Fatal(err)
	}
	if configValue.Model != "provider/model" || len(configValue.Instructions) != 2 || configValue.Instructions[0] != "existing.md" {
		t.Fatalf("config=%+v", configValue)
	}
	path := configValue.Instructions[1]
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "managed skill body" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v err=%v", info.Mode(), err)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("ephemeral instruction survived cleanup: %v", err)
	}
}

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

func TestSharedKnowledgeContractRequiresMemoryAndContextBeforeWeb(t *testing.T) {
	for _, expected := range []string{
		"mandatory source order",
		"(1) ivoai-memory, (2) ivoai-context, (3) web",
		"Before the first web search",
		`memory_read_page once with only {"query":"essential terms"}`,
		"call context_search once",
		"Do not skip them because a question appears general",
		"only then continue to external research",
		"never pass id, page_id, scope",
		"write exactly one canonical page",
		"Never duplicate the same fact across scopes",
		"Context/RAG is read-only",
	} {
		if !strings.Contains(sharedKnowledgeInstructions, expected) {
			t.Fatalf("shared-knowledge fast-path contract missing %q", expected)
		}
	}
	memory := strings.Index(sharedKnowledgeInstructions, "(1) ivoai-memory")
	context := strings.Index(sharedKnowledgeInstructions, "(2) ivoai-context")
	web := strings.Index(sharedKnowledgeInstructions, "(3) web")
	if memory < 0 || context <= memory || web <= context {
		t.Fatalf("research source order is not memory -> context -> web: %q", sharedKnowledgeInstructions)
	}
}

func TestCodexKnowledgeArgsDoNotCreateUnregisteredMCPServers(t *testing.T) {
	joined := strings.Join(sharedKnowledgeAgentArgs("codex", nil, config.Default()), "\n")
	if strings.Contains(joined, "approval_mode") {
		t.Fatalf("unregistered optional MCP server received an override: %q", joined)
	}
}

func TestResearchPriorityStatusRequiresBothInternalSources(t *testing.T) {
	cfg := config.Default()
	if got := researchPriorityStatus(cfg); got.Text != "unavailable / connect server" {
		t.Fatalf("unconfigured research status=%+v", got)
	}
	cfg.MCP.Servers["ivoai-memory"] = config.MCPServer{Enabled: true, Kind: "memory"}
	if got := researchPriorityStatus(cfg); got.Text != "degraded / internal source missing" {
		t.Fatalf("single-source research status=%+v", got)
	}
	cfg.MCP.Servers["ivoai-context"] = config.MCPServer{Enabled: true, Kind: "context"}
	if got := researchPriorityStatus(cfg); got.Text != "memory -> context -> web" {
		t.Fatalf("ready research status=%+v", got)
	}
}

func TestHeadroomIsBypassedOnlyForActiveSharedKnowledge(t *testing.T) {
	cfg := config.Default()
	if !primaryHeadroomEnabled(cfg) || headroomBypassedForSharedKnowledge(cfg) {
		t.Fatal("Headroom was bypassed without an active shared-knowledge MCP")
	}
	cfg.MCP.Servers["ivoai-memory"] = config.MCPServer{Enabled: true, Kind: "memory"}
	if primaryHeadroomEnabled(cfg) || !headroomBypassedForSharedKnowledge(cfg) {
		t.Fatal("active memory MCP did not bypass lossy Headroom compression")
	}
	cfg.Memory.Enabled = false
	if !primaryHeadroomEnabled(cfg) {
		t.Fatal("disabled memory integration still bypassed Headroom")
	}
	cfg.MCP.Servers["ivoai-context"] = config.MCPServer{Enabled: true, Kind: "context"}
	if primaryHeadroomEnabled(cfg) {
		t.Fatal("active Context MCP did not bypass lossy Headroom compression")
	}
	cfg.Headroom.Enabled = false
	if primaryHeadroomEnabled(cfg) || headroomBypassedForSharedKnowledge(cfg) {
		t.Fatal("disabled Headroom was reported as a shared-knowledge bypass")
	}
}

func TestAuthoritativeKnowledgeBypassMatrixIsProviderNeutral(t *testing.T) {
	providers := []string{"direct", "headroom", "caveman"}
	for _, provider := range providers {
		for _, headroomEnabled := range []bool{false, true} {
			for _, memoryEnabled := range []bool{false, true} {
				for _, contextEnabled := range []bool{false, true} {
					name := fmt.Sprintf("provider=%s/headroom=%t/memory=%t/context=%t", provider, headroomEnabled, memoryEnabled, contextEnabled)
					t.Run(name, func(t *testing.T) {
						cfg := config.Default()
						cfg.Compression.Provider = provider
						cfg.Headroom.Enabled = headroomEnabled
						cfg.Memory.Enabled = memoryEnabled
						cfg.MCP.Servers = map[string]config.MCPServer{
							"ivoai-memory":  {Enabled: memoryEnabled, Kind: "memory"},
							"ivoai-context": {Enabled: contextEnabled, Kind: "context"},
						}
						policy := sharedKnowledgeCompressionPolicyFor(cfg, 1)
						authoritative := memoryEnabled || contextEnabled
						wantBypass := provider != "direct" && authoritative
						wantEffective := provider
						if wantBypass {
							wantEffective = "direct"
						}
						if policy.AuthoritativeActive != authoritative || policy.Bypassed != wantBypass || policy.EffectiveProvider != wantEffective || policy.RequestedProvider != provider || policy.SelectedSourceCount != 1 {
							t.Fatalf("policy=%+v authoritative=%t bypass=%t effective=%s", policy, authoritative, wantBypass, wantEffective)
						}
						if compressionBypassedForSharedKnowledge(cfg) != wantBypass {
							t.Fatal("compatibility helper disagrees with provider-neutral policy")
						}
					})
				}
			}
		}
	}
}

func TestCompressionBypassObservationIsBoundedAndProviderNeutral(t *testing.T) {
	cfg := config.Default()
	cfg.Compression.Provider = "caveman"
	cfg.Headroom.Enabled = false
	cfg.MCP.Servers = map[string]config.MCPServer{"ivoai-context": {Enabled: true, Kind: "context"}}
	policy := sharedKnowledgeCompressionPolicyFor(cfg, 2)
	event := compressionObservation("codex", core.SessionObservation{}, policy)
	if event.Provider != "direct" || event.RequestedProvider != "caveman" || !event.CompressionBypassed || !event.AuthoritativeKnowledge || event.SelectedSourceCount != 2 || event.RoutingReason != observability.ReasonAuthoritativeSharedKnowledge {
		t.Fatalf("event=%+v", event)
	}
	if _, err := observability.Normalize(event); err != nil {
		t.Fatalf("normalize bypass event: %v", err)
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
