package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/knowledgepolicy"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

const sharedKnowledgeInstructions = knowledgepolicy.ResearchFirstInstructions + `
When the user explicitly asks to remember information across LLMs or sessions, write exactly one canonical page with memory_write_page in the current project, or scope="global" only for a standing user/team fact that must apply to every project. Never duplicate the same fact across scopes. Verify once with memory_read_page before claiming success. Context/RAG is read-only from agent sessions; never claim conversational data was written to Context.
Concurrent sessions may be active, so do not assume the newest session owns shared state and do not overwrite an existing page unless the user explicitly requested an update. If the MCP tools are unavailable, say shared-knowledge retrieval is unavailable instead of silently substituting another source.`

const sharedKnowledgeHeadroomBypass = "Headroom bypassed: its lossy compression can truncate exact shared-memory or Context tool results; launching the official client directly"

func sharedKnowledgeCompressionBypass(provider string) string {
	if provider == "headroom" {
		return sharedKnowledgeHeadroomBypass
	}
	return "Compression bypassed: authoritative shared-memory or Context tool results require exact fidelity; launching the official client directly"
}

func compressionBypassedForSharedKnowledge(cfg config.Config) bool {
	return cfg.Compression.Provider != "direct" && headroomBypassedForSharedKnowledge(cfg)
}

// ai-memory 1.29.0 does not annotate its MCP tools as read-only. Recent Codex
// releases conservatively require approval for unannotated tools, which makes a
// headless `codex exec --ask-for-approval never` unable to perform even a memory
// lookup. These process-local overrides approve only IvoAI's bounded read tools;
// memory writes and every unrelated MCP operation retain the user's policy.
var codexSharedKnowledgeReadApprovals = map[string][]string{
	"ivoai-memory":  {"memory_query", "memory_read_page"},
	"ivoai-context": {"context_search", "context_get_document", "context_recent", "context_health"},
}

func sharedKnowledgeAgentArgs(executor string, existing []string, cfg config.Config) []string {
	return managedAgentArgs(executor, existing, cfg, "")
}

func managedAgentArgs(executor string, existing []string, cfg config.Config, skillInstructions string) []string {
	instructions := managedInstructions(skillInstructions)
	if executor == "codex" {
		args := []string{"-c", "developer_instructions=" + strconv.Quote(instructions)}
		return codexSharedKnowledgeReadApprovalArgs(append(args, existing...), cfg)
	}
	if executor == "opencode" {
		return append([]string(nil), existing...)
	}
	return append([]string{"--append-system-prompt", instructions}, existing...)
}

func managedInstructions(skillInstructions string) string {
	instructions := sharedKnowledgeInstructions
	if strings.TrimSpace(skillInstructions) != "" {
		instructions += "\n\n" + skillInstructions
	}
	return instructions
}

// openCodeInstructionEnvironment uses OpenCode's official process-local
// OPENCODE_CONFIG_CONTENT instructions setting. It preserves any caller-owned
// inline configuration and points it at a private, ephemeral IVOAI instruction
// file; no OpenCode global or project configuration is modified.
func openCodeInstructionEnvironment(environment []string, stateDir, instructions string) ([]string, func(), error) {
	runtimeRoot := filepath.Join(stateDir, "opencode-runtime")
	if err := platform.EnsurePrivateDir(runtimeRoot); err != nil {
		return nil, nil, err
	}
	directory, err := os.MkdirTemp(runtimeRoot, "session-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		_ = os.Remove(filepath.Join(directory, "instructions.md"))
		_ = os.Remove(directory)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		cleanup()
		return nil, nil, err
	}
	path := filepath.Join(directory, "instructions.md")
	if err := platform.AtomicWritePrivate([]byte(instructions), path); err != nil {
		cleanup()
		return nil, nil, err
	}

	content := map[string]json.RawMessage{}
	if existing := environmentValue(environment, "OPENCODE_CONFIG_CONTENT"); existing != "" {
		if err := json.Unmarshal([]byte(existing), &content); err != nil {
			cleanup()
			return nil, nil, errors.New("existing OPENCODE_CONFIG_CONTENT is invalid JSON")
		}
	}
	var paths []string
	if raw, ok := content["instructions"]; ok {
		if err := json.Unmarshal(raw, &paths); err != nil {
			cleanup()
			return nil, nil, errors.New("existing OpenCode instructions configuration is invalid")
		}
	}
	paths = append(paths, path)
	rawPaths, _ := json.Marshal(paths)
	content["instructions"] = rawPaths
	encoded, err := json.Marshal(content)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return setAppEnvironment(environment, "OPENCODE_CONFIG_CONTENT", string(encoded)), cleanup, nil
}

func environmentValue(environment []string, key string) string {
	prefix := key + "="
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func setAppEnvironment(environment []string, key, value string) []string {
	prefix := key + "="
	result := append([]string(nil), environment...)
	for index, entry := range result {
		if strings.HasPrefix(entry, prefix) {
			result[index] = prefix + value
			return result
		}
	}
	return append(result, prefix+value)
}

func codexSharedKnowledgeReadApprovalArgs(existing []string, cfg config.Config) []string {
	args := make([]string, 0, 12+len(existing))
	// Fixed order keeps argv deterministic and avoids constructing an incomplete
	// Codex MCP table when the optional remote server has not been registered.
	for _, serverName := range []string{"ivoai-memory", "ivoai-context"} {
		server, registered := cfg.MCP.Servers[serverName]
		if !registered || !server.Enabled {
			continue
		}
		for _, tool := range codexSharedKnowledgeReadApprovals[serverName] {
			approval := "mcp_servers." + serverName + ".tools." + tool + `.approval_mode="approve"`
			args = append(args, "-c", approval)
		}
	}
	return append(args, existing...)
}

// Headroom 0.36.0 cannot reliably associate Codex Code Mode's
// custom_tool_call_output items with their MCP tool names. Its generic compressor
// can therefore shorten an exact memory page even when the tool-result protection
// list is configured. Shared knowledge is authoritative, so prefer an unmodified
// official-client stream whenever either managed knowledge MCP is active.
func primaryHeadroomEnabled(cfg config.Config) bool {
	if !cfg.Headroom.Enabled {
		return false
	}
	for _, name := range []string{"ivoai-memory", "ivoai-context"} {
		server, ok := cfg.MCP.Servers[name]
		if !ok || !server.Enabled {
			continue
		}
		if name != "ivoai-memory" || cfg.Memory.Enabled {
			return false
		}
	}
	return true
}

func headroomBypassedForSharedKnowledge(cfg config.Config) bool {
	return cfg.Headroom.Enabled && !primaryHeadroomEnabled(cfg)
}

// Codex's stable Code Mode router fails closed without its separately released
// host companion, removing every tool including MCP. Managed installs therefore
// require the exact-version, checksum-verified companion beside the Codex binary.
func validateManagedAgentRuntime(executor string, state config.State) error {
	component := state.Components[executor]
	if executor != "codex" || !component.Managed {
		return nil
	}
	host := filepath.Join(filepath.Dir(component.Path), "codex-code-mode-host")
	hostState := state.Components["codex-code-mode-host"]
	info, err := os.Stat(host)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 || !hostState.Installed || !hostState.Managed || hostState.Path != host || hostState.Version != component.Version {
		return errors.New("managed Codex tool host is missing or incompatible; run ivoai setup")
	}
	return nil
}
