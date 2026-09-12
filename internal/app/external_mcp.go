package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/connections"
	"github.com/ivo-lopes/ivoai/internal/externalmcp"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/serverpool"
)

const externalMCPTokenEnvironment = "IVOAI_EXTERNAL_MCP_SESSION_TOKEN"

func (a *App) prepareSessionKnowledge(ctx context.Context, cfg config.Config, selectors []string, executor, runtimeDir string, environment []string, observe func(observability.Event)) (sessionKnowledge, error) {
	return a.prepareSessionKnowledgeWithApprovals(ctx, cfg, selectors, executor, runtimeDir, environment, observe, false)
}

func (a *App) prepareSessionKnowledgeWithApprovals(ctx context.Context, cfg config.Config, selectors []string, executor, runtimeDir string, environment []string, observe func(observability.Event), managed bool) (sessionKnowledge, error) {
	return a.prepareSessionKnowledgeSelection(ctx, cfg, selectors, nil, executor, runtimeDir, environment, observe, managed)
}

func (a *App) prepareSessionKnowledgeSelection(ctx context.Context, cfg config.Config, selectors []string, selection *serverpool.Selection, executor, runtimeDir string, environment []string, observe func(observability.Event), managed bool) (sessionKnowledge, error) {
	var result sessionKnowledge
	var err error
	if selection == nil {
		result, err = a.prepareKnowledgeRouter(ctx, cfg, selectors, executor, runtimeDir, environment, observe)
	} else {
		result, err = a.prepareKnowledgeSelection(ctx, cfg, *selection, executor, runtimeDir, environment, observe)
	}
	if err != nil {
		return result, err
	}
	names := externalMCPNames(cfg)
	if len(names) == 0 {
		return result, nil
	}
	registry := connections.Registry{Store: a.Store}
	targets := []externalmcp.Target{}
	for _, name := range names {
		if connections.IsManagedMCPName(name) {
			result.close()
			return sessionKnowledge{}, errors.New("external MCP uses a reserved IVOAI control-plane name; rename it")
		}
		entry := cfg.MCP.Servers[name]
		headers, err := registry.Headers(entry)
		if err != nil {
			result.close()
			return sessionKnowledge{}, fmt.Errorf("external MCP %q: %w", name, err)
		}
		target := externalmcp.Target{Name: name, URL: entry.URL, Headers: headers, Restricted: true}
		// Discovery is read-only; no cached annotation can silently authorize a
		// changed upstream tool. A unavailable MCP projects zero tools, not all.
		if managed || entry.ResolvedDirectPolicy() == "read_only" {
			inventory, probeErr := registry.DiscoverTools(ctx, entry)
			if probeErr == nil {
				for _, tool := range inventory {
					if tool.ReadOnly {
						target.AllowedTools = append(target.AllowedTools, tool.Name)
					} else if managed && entry.ResolvedMCPPolicy() == "read_auto_ask_mutating" {
						target.AllowedTools = append(target.AllowedTools, tool.Name)
						target.ApprovalTools = append(target.ApprovalTools, tool.Name)
					}
				}
			}
		}
		targets = append(targets, target)
	}
	mode := cfg.OpenCode.ResolvedPermissionMode()
	if !managed {
		mode = "full"
	} // Explicit native TUIs retain their own approval UI.
	gateway, err := externalmcp.Start(targets, mode)
	if err != nil {
		result.close()
		return sessionKnowledge{}, err
	}
	result.external = gateway
	if managed {
		// No grace window before the per-turn task admission callback is installed.
		gateway.SetToolAdmission(func(string, string) (bool, bool) { return false, false })
	}
	result.environment = setProcessEnvironment(result.environment, externalMCPTokenEnvironment, gateway.Token())
	servers := map[string]config.MCPServer{}
	for name, entry := range result.config.MCP.Servers {
		servers[name] = entry
	}
	result.config.MCP.Servers = servers
	for i, name := range names {
		entry := cfg.MCP.Servers[name]
		entry.URL = gateway.URL(i)
		entry.HeaderEnv = map[string]string{"Authorization": externalMCPTokenEnvironment}
		entry.SessionApproved = managed || cfg.OpenCode.ResolvedPermissionMode() == "full"
		result.config.MCP.Servers[name] = entry
	}
	args, err := processLocalExternalMCPArgs(executor, runtimeDir, result.config)
	if err != nil {
		result.close()
		return sessionKnowledge{}, err
	}
	result.args = append(result.args, args...)
	return result, nil
}

func externalMCPNames(cfg config.Config) []string {
	names := []string{}
	for name, entry := range cfg.MCP.Servers {
		if entry.Enabled && entry.Kind == "external" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func processLocalExternalMCPArgs(executor, runtimeDir string, cfg config.Config) ([]string, error) {
	args := []string{}
	claude := map[string]any{}
	codexNames := map[string]bool{}
	for _, name := range externalMCPNames(cfg) {
		entry := cfg.MCP.Servers[name]
		if entry.HeaderEnv["Authorization"] != externalMCPTokenEnvironment {
			continue
		}
		switch executor {
		case "codex":
			// Codex -c splits keys on dots; TOML quotes become literal name
			// characters rather than escaping a path segment. Its server names
			// are restricted to letters, digits, underscores and hyphens.
			codexName := strings.ReplaceAll(name, ".", "_")
			if codexNames[codexName] {
				return nil, errors.New("external MCP names collide in Codex; rename dotted aliases")
			}
			codexNames[codexName] = true
			prefix := "mcp_servers." + codexName
			args = append(args, "-c", prefix+".url="+strconv.Quote(entry.URL), "-c", prefix+".bearer_token_env_var="+strconv.Quote(externalMCPTokenEnvironment), "-c", prefix+".http_headers={}", "-c", prefix+".env_http_headers={}", "-c", prefix+".enabled=true")
			if entry.SessionApproved {
				args = append(args, "-c", prefix+`.default_tools_approval_mode="approve"`)
			}
		case "claude":
			claude[name] = map[string]any{"type": "http", "url": entry.URL, "headers": map[string]string{"Authorization": "Bearer ${" + externalMCPTokenEnvironment + "}"}}
			if entry.SessionApproved {
				args = append(args, "--allowedTools", "mcp__"+name+"__*")
			}
		}
	}
	if len(claude) > 0 {
		body, err := json.Marshal(map[string]any{"mcpServers": claude})
		if err != nil {
			return nil, err
		}
		path := filepath.Join(runtimeDir, "external-mcp.json")
		if err := platform.AtomicWritePrivate(body, path); err != nil {
			return nil, err
		}
		args = append([]string{"--mcp-config", path}, args...)
	}
	return args, nil
}

func (a *App) MCPSetBearer(name, token string) error {
	return (connections.Registry{Store: a.Store}).SetBearer(name, token)
}
func (a *App) MCPSetHeader(name, header, value string) error {
	return (connections.Registry{Store: a.Store}).SetHeader(name, header, value)
}
func (a *App) MCPClearAuth(name string) error {
	return (connections.Registry{Store: a.Store}).ClearAuth(name)
}
func (a *App) MCPTest(ctx context.Context, name string) error {
	registry := connections.Registry{Store: a.Store}
	entries, err := registry.List()
	if err != nil {
		return err
	}
	entry, ok := entries[name]
	if !ok || entry.Kind != "external" {
		return errors.New("external MCP not found")
	}
	count, err := registry.Refresh(ctx, name)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "MCP initialize=PASS tools/list=PASS tools=%d auth=%s\n", count, resolvedMCPAuth(entry))
	return nil
}
func resolvedMCPAuth(entry config.MCPServer) string {
	if entry.AuthMode == "" {
		return "none"
	}
	return entry.AuthMode
}

func safeMCPDisplayURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[invalid endpoint]"
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	return strconv.Quote(platform.Redact(u.String()))
}
