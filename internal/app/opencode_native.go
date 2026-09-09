package app

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/agents"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/knowledgepolicy"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
)

func (a *App) nativeOpenCode(cfg config.Config, state config.State, directory, runtime string, environment []string, worker bool) *agents.NativeOpenCode {
	component := state.Components["opencode"]
	if !component.Installed || !component.Managed || component.Path == "" || component.Version == "" {
		return nil
	}
	if info, err := os.Stat(component.Path); err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return nil
	}
	if validateManagedAgentRuntime("opencode", state) != nil {
		return nil
	}
	if environment == nil {
		environment = os.Environ()
	}
	permissions := opencodebridge.NativePermissionPolicy(cfg.OpenCode.PermissionMode, worker)
	if worker {
		filtered := []string{}
		for _, entry := range environment {
			if !strings.HasPrefix(entry, externalMCPTokenEnvironment+"=") {
				filtered = append(filtered, entry)
			}
		}
		environment = filtered
	}
	// These endpoints belong to the session-local knowledge router. Never
	// project global MCPs or upstream credentials into controlled workers.
	servers := map[string]any{}
	for _, item := range []struct{ name, key string }{{"ivoai-memory", "IVOAI_MEMORY_MCP_URL"}, {"ivoai-context", "IVOAI_CONTEXT_MCP_URL"}} {
		endpoint := ""
		for _, entry := range environment {
			if len(entry) > len(item.key)+1 && entry[:len(item.key)+1] == item.key+"=" {
				endpoint = entry[len(item.key)+1:]
			}
		}
		if endpoint != "" {
			parsed, err := url.Parse(endpoint)
			if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
				return nil
			}
			servers[item.name] = map[string]any{"type": "remote", "url": endpoint, "oauth": false, "headers": map[string]string{"Authorization": "Bearer {env:IVOAI_KNOWLEDGE_SESSION_TOKEN}"}}
		}
	}
	if !worker {
		for _, name := range externalMCPNames(cfg) {
			entry := cfg.MCP.Servers[name]
			if entry.HeaderEnv["Authorization"] != externalMCPTokenEnvironment {
				continue
			}
			servers[name] = map[string]any{"type": "remote", "url": entry.URL, "oauth": false, "headers": map[string]string{"Authorization": "Bearer {env:" + externalMCPTokenEnvironment + "}"}}
			if entry.SessionApproved {
				permissions[name+"_*"] = "allow"
			}
		}
	}
	return &agents.NativeOpenCode{Options: opencodebridge.ManagedOptions{NativeExecutor: true, OpenCodePath: component.Path, Version: component.Version, Directory: directory, RuntimeDir: filepath.Join(runtime, "native-assets"), StateDir: filepath.Join(runtime, "native-state"), Environment: environment, PermissionMode: cfg.OpenCode.PermissionMode, NativePermissions: permissions, NativeMCP: servers, Instructions: knowledgepolicy.ResearchFirstInstructions}}
}

func nativeCapabilityAvailable(ctx context.Context, native *agents.NativeOpenCode) bool {
	if native == nil {
		return false
	}
	value, err := native.Probe(ctx)
	return err == nil && value.Eligible
}
