package app

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/connections"
	"github.com/ivo-lopes/ivoai/internal/secrets"
)

func TestDirectSessionReceivesOnlyLoopbackCapability(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	var output, stderr bytes.Buffer
	a, err := New("test", strings.NewReader(""), &output, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	profile := config.ServerProfile{
		ID: "srv_voicecorp_test", Alias: "voicecorp", URL: "https://voicecorp.invalid", Status: "connected", Enabled: true, Purpose: "voicecorp", Protocol: connections.ProtocolVersion,
		ContextMCPURL: "https://voicecorp.invalid/mcp/context", MemoryMCPURL: "https://voicecorp.invalid/mcp/memory", MemoryHooksURL: "https://voicecorp.invalid/memory", Features: map[string]bool{"context": true, "memory": true},
	}
	cfg := config.Default()
	cfg.Headroom.Enabled = false
	cfg.Connections.Servers["voicecorp"] = profile
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if err := (secrets.Store{Path: a.Store.Paths.Secrets}).Set(profile.ID, secrets.ClientCredential{Token: "upstream-secret", ClientID: "client"}); err != nil {
		t.Fatal(err)
	}
	agent := filepath.Join(root, "codex-fixture")
	script := "#!/bin/sh\nprintf '%s\\n%s\\n%s\\n' \"$IVOAI_SERVER_TOKEN\" \"$IVOAI_KNOWLEDGE_SESSION_TOKEN\" \"$AI_MEMORY_SERVER_URL\"\nprintf '%s\\n' \"$*\"\n"
	if err := os.WriteFile(agent, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	state, _ := a.Store.LoadState()
	state.Components["codex"] = config.ComponentState{Installed: true, Path: agent}
	if err := a.Store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	if err := a.LaunchWithKnowledge(context.Background(), "codex", nil, []string{"voicecorp"}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) < 4 || lines[0] == "" || lines[0] != lines[1] || lines[0] == "upstream-secret" {
		t.Fatalf("session capability isolation failed: %q", output.String())
	}
	if !strings.HasPrefix(lines[2], "http://127.0.0.1:") || !strings.HasSuffix(lines[2], "/memory") {
		t.Fatalf("memory hook does not use loopback router: %q", lines[2])
	}
	if !strings.Contains(lines[3], "mcp_servers.ivoai-context.url=\"http://127.0.0.1:") || strings.Contains(output.String(), "voicecorp.invalid") || strings.Contains(output.String(), "upstream-secret") {
		t.Fatalf("upstream metadata leaked to child: %q", output.String())
	}
	if !strings.Contains(strings.ToLower(stderr.String()), "bypassed") || strings.Contains(stderr.String(), "upstream-secret") {
		t.Fatalf("provider-neutral bypass warning=%q", stderr.String())
	}
}

func TestRuntimeWorkerKnowledgeUsesSessionRouter(t *testing.T) {
	t.Setenv("IVOAI_CONTEXT_MCP_URL", "http://127.0.0.1:1234/mcp/context")
	t.Setenv("IVOAI_MEMORY_MCP_URL", "http://127.0.0.1:1234/mcp/memory")
	servers := runtimeKnowledgeServers(map[string]config.MCPServer{"ivoai-context": {URL: "https://upstream.invalid", Enabled: true, Kind: "context"}})
	if servers["ivoai-context"].URL != "http://127.0.0.1:1234/mcp/context" || servers["ivoai-memory"].URL != "http://127.0.0.1:1234/mcp/memory" {
		t.Fatalf("workers did not inherit session routing: %#v", servers)
	}
}

func TestClaudeDirectUsesPrivateSessionMCPConfiguration(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	var output bytes.Buffer
	a, err := New("test", strings.NewReader(""), &output, &output)
	if err != nil {
		t.Fatal(err)
	}
	profile := config.ServerProfile{ID: "srv_mindsite_test", Alias: "mindsite", URL: "https://mindsite.invalid", Status: "connected", Enabled: true, Purpose: "mindsite", Protocol: 1, ContextMCPURL: "https://mindsite.invalid/context", MemoryMCPURL: "https://mindsite.invalid/memory", Features: map[string]bool{"context": true, "memory": true}}
	cfg := config.Default()
	cfg.Headroom.Enabled = false
	cfg.Connections.Servers["mindsite"] = profile
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if err := (secrets.Store{Path: a.Store.Paths.Secrets}).Set(profile.ID, secrets.ClientCredential{Token: "claude-upstream-secret"}); err != nil {
		t.Fatal(err)
	}
	agent := filepath.Join(root, "claude-fixture")
	if err := os.WriteFile(agent, []byte("#!/bin/sh\nprintf '%s\\n' \"$IVOAI_SERVER_TOKEN\"\ncat \"$2\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	state, _ := a.Store.LoadState()
	state.Components["claude-code"] = config.ComponentState{Installed: true, Path: agent}
	if err := a.Store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	if err := a.LaunchWithKnowledge(context.Background(), "claude", nil, []string{"mindsite"}); err != nil {
		t.Fatal(err)
	}
	value := output.String()
	if strings.Contains(value, "claude-upstream-secret") || strings.Contains(value, "mindsite.invalid") || !strings.Contains(value, "http://127.0.0.1:") || !strings.Contains(value, "${IVOAI_KNOWLEDGE_SESSION_TOKEN}") {
		t.Fatalf("Claude MCP isolation failed: %q", value)
	}
}

func TestDisabledMemoryIsNotReenabledInSessionEnvironment(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	a, err := New("test", strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	profile := config.ServerProfile{ID: "srv_context_only", Alias: "context-only", URL: "https://context.invalid", Status: "connected", Enabled: true, Purpose: "context", ContextMCPURL: "https://context.invalid/context", MemoryMCPURL: "https://context.invalid/memory", MemoryHooksURL: "https://context.invalid/hooks"}
	cfg := config.Default()
	cfg.Memory.Enabled = false
	cfg.Connections.Servers[profile.Alias] = profile
	if err := (secrets.Store{Path: a.Store.Paths.Secrets}).Set(profile.ID, secrets.ClientCredential{Token: "never-forwarded"}); err != nil {
		t.Fatal(err)
	}
	knowledge, err := a.prepareSessionKnowledge(context.Background(), cfg, []string{profile.Alias}, "codex", t.TempDir(), []string{
		"AI_MEMORY_SERVER_URL=https://stale.invalid",
		"AI_MEMORY_AUTH_TOKEN=stale-token",
		"IVOAI_MEMORY_MCP_URL=https://stale.invalid/mcp",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer knowledge.close()
	joined := strings.Join(knowledge.environment, "\n")
	for _, key := range []string{"AI_MEMORY_SERVER_URL=", "AI_MEMORY_AUTH_TOKEN=", "IVOAI_MEMORY_MCP_URL="} {
		if strings.Contains(joined, key) {
			t.Fatalf("disabled memory leaked into session environment: %s", joined)
		}
	}
	if !strings.Contains(joined, "IVOAI_CONTEXT_MCP_URL=http://127.0.0.1:") || knowledge.config.MCP.Servers["ivoai-memory"].Enabled {
		t.Fatalf("context-only routing was not preserved: env=%s config=%+v", joined, knowledge.config.MCP.Servers)
	}
}

func TestCompressionPolicyUsesOnlySessionSelectedSources(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	a, err := New("test", strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	voice := config.ServerProfile{ID: "srv_voice_exact", Alias: "voicecorp", URL: "https://voice.invalid", Status: "connected", Enabled: true, Purpose: "voicecorp", ContextMCPURL: "https://voice.invalid/context", Features: map[string]bool{"context": true}}
	mind := config.ServerProfile{ID: "srv_mind_inactive", Alias: "mindsite", URL: "https://mind.invalid", Status: "connected", Enabled: true, Purpose: "mindsite"}
	replica := config.ServerProfile{ID: "srv_voice_replica", Alias: "voicecorp-2", URL: "https://voice-2.invalid", Status: "connected", Enabled: true, Purpose: "voice-redundancy", RedundancyGroup: "voice-prod", Priority: 10, ContextMCPURL: "https://voice-2.invalid/context", Features: map[string]bool{"context": true}}
	standby := config.ServerProfile{ID: "srv_voice_standby", Alias: "voicecorp-3", URL: "https://voice-3.invalid", Status: "connected", Enabled: true, Purpose: "voice-redundancy", RedundancyGroup: "voice-prod", Priority: 20, ContextMCPURL: "https://voice-3.invalid/context", Features: map[string]bool{"context": true}}
	cfg := config.Default()
	cfg.Compression.Provider = "caveman"
	cfg.Headroom.Enabled = false
	cfg.Connections.Servers = map[string]config.ServerProfile{voice.Alias: voice, mind.Alias: mind, replica.Alias: replica, standby.Alias: standby}
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	credentialStore := secrets.Store{Path: a.Store.Paths.Secrets}
	for _, profile := range []config.ServerProfile{voice, mind, replica, standby} {
		if err := credentialStore.Set(profile.ID, secrets.ClientCredential{Token: "isolated-" + profile.ID}); err != nil {
			t.Fatal(err)
		}
	}
	check := func(selectors []string, wantActive, wantBypass bool, wantCount int) {
		t.Helper()
		knowledge, err := a.prepareSessionKnowledge(context.Background(), cfg, selectors, "codex", t.TempDir(), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer knowledge.close()
		policy := sharedKnowledgeCompressionPolicyFor(knowledge.config, len(knowledge.aliases()))
		if policy.AuthoritativeActive != wantActive || policy.Bypassed != wantBypass || policy.SelectedSourceCount != wantCount {
			t.Fatalf("selectors=%v policy=%+v", selectors, policy)
		}
	}
	check([]string{"mindsite"}, false, false, 1)
	check([]string{"voicecorp"}, true, true, 1)
	check([]string{"voicecorp", "mindsite"}, true, true, 2)
	check([]string{"voice-redundancy"}, true, true, 2)
}
