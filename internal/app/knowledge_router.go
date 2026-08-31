package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/connections"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/knowledgerouter"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/secrets"
	"github.com/ivo-lopes/ivoai/internal/serverpool"
)

const knowledgeSessionTokenEnvironment = "IVOAI_KNOWLEDGE_SESSION_TOKEN"

type sessionKnowledge struct {
	router      *knowledgerouter.Router
	selection   serverpool.Selection
	environment []string
	config      config.Config
	args        []string
}

func (a *App) prepareSessionKnowledge(ctx context.Context, cfg config.Config, selectors []string, executor, runtimeDir string, existingEnvironment []string, observe func(observability.Event)) (sessionKnowledge, error) {
	pool, err := serverpool.New(cfg.Connections.Servers)
	if err != nil {
		return sessionKnowledge{}, fmt.Errorf("load server pool: %w", err)
	}
	selection, err := pool.Resolve(selectors)
	if err != nil {
		return sessionKnowledge{}, err
	}
	result := sessionKnowledge{selection: selection, environment: cleanKnowledgeEnvironment(existingEnvironment), config: cfg}
	if len(selection.Groups) == 0 {
		return result, nil
	}
	secretData, err := (secrets.Store{Path: a.Store.Paths.Secrets}).Load()
	if err != nil {
		return sessionKnowledge{}, err
	}
	credentials := make(map[string]secrets.ClientCredential)
	for _, group := range selection.Groups {
		for _, profile := range group.Profiles {
			credential, exists := secretData.Servers[profile.ID]
			if !exists || credential.Token == "" {
				return sessionKnowledge{}, fmt.Errorf("knowledge source %q has no credential; reconnect that server", profile.Alias)
			}
			credentials[profile.ID] = credential
		}
	}
	router, err := knowledgerouter.Start(knowledgerouter.Options{Selection: selection, Credentials: credentials, Client: a.statusHTTPClient(), Observe: func(event knowledgerouter.Event) {
		if observe == nil {
			return
		}
		state := observability.StateCompleted
		reason := observability.ReasonPrimaryAvailable
		if event.State == "failed" {
			state, reason = observability.StateDegraded, observability.ReasonProviderUnavailable
		}
		fallback := observability.Reason("")
		if event.Failover {
			fallback = observability.ReasonAlternateSelected
		}
		observe(observability.Event{Category: observability.CategoryConnection, Operation: observability.OperationKnowledgeRoute, State: state, Component: coreComponentForKnowledge(event.Operation), DurationMilliseconds: event.Duration.Milliseconds(), RoutingReason: reason, FallbackReason: fallback, SourceID: event.SourceID, SourceAlias: event.SourceAlias, Purpose: event.Purpose, SelectedSourceCount: event.SelectedCount, Failover: event.Failover, PartialFailure: event.Partial})
	}})
	if err != nil {
		return sessionKnowledge{}, err
	}
	result.router = router
	result.environment = setProcessEnvironment(result.environment, knowledgeSessionTokenEnvironment, router.Token())
	// Compatibility for the existing worker adapter: this value is the
	// short-lived loopback capability, never an upstream server credential.
	result.environment = setProcessEnvironment(result.environment, connections.ServerTokenEnvironment, router.Token())
	contextEnabled := hasSelectedFeature(selection, "context")
	memoryEnabled := cfg.Memory.Enabled && hasSelectedFeature(selection, "memory")
	if contextEnabled {
		result.environment = setProcessEnvironment(result.environment, "IVOAI_CONTEXT_MCP_URL", router.BaseURL()+"/mcp/context")
	}
	if memoryEnabled {
		result.environment = setProcessEnvironment(result.environment, "AI_MEMORY_SERVER_URL", router.BaseURL()+"/memory")
		result.environment = setProcessEnvironment(result.environment, "AI_MEMORY_AUTH_TOKEN", router.Token())
		result.environment = setProcessEnvironment(result.environment, "IVOAI_MEMORY_MCP_URL", router.BaseURL()+"/mcp/memory")
	}
	result.config.MCP.Servers = map[string]config.MCPServer{
		"ivoai-context": {URL: router.BaseURL() + "/mcp/context", Enabled: contextEnabled, Kind: "context"},
		"ivoai-memory":  {URL: router.BaseURL() + "/mcp/memory", HooksURL: router.BaseURL() + "/memory", Enabled: memoryEnabled, Kind: "memory"},
	}
	result.args, err = processLocalKnowledgeArgs(executor, runtimeDir, result.config)
	if err != nil {
		_ = router.Close(ctx)
		return sessionKnowledge{}, err
	}
	return result, nil
}

func coreComponentForKnowledge(operation string) core.ComponentID {
	if strings.HasPrefix(operation, "context") {
		return core.ComponentContext
	}
	return core.ComponentMemory
}

func (k sessionKnowledge) close() {
	if k.router != nil {
		_ = k.router.Close(context.Background())
	}
}

func (k sessionKnowledge) aliases() []string {
	if k.router == nil {
		return nil
	}
	return k.router.SortedSourceAliases()
}

func processLocalKnowledgeArgs(executor, runtimeDir string, cfg config.Config) ([]string, error) {
	contextServer, contextEnabled := cfg.MCP.Servers["ivoai-context"]
	memoryServer, memoryEnabled := cfg.MCP.Servers["ivoai-memory"]
	contextEnabled = contextEnabled && contextServer.Enabled
	memoryEnabled = memoryEnabled && memoryServer.Enabled
	if !contextEnabled && !memoryEnabled {
		return nil, nil
	}
	if executor == "codex" {
		args := []string{}
		for _, item := range []struct {
			name    string
			server  config.MCPServer
			enabled bool
		}{{"ivoai-memory", memoryServer, memoryEnabled}, {"ivoai-context", contextServer, contextEnabled}} {
			if !item.enabled {
				continue
			}
			prefix := "mcp_servers." + item.name
			args = append(args,
				"-c", prefix+".url="+strconv.Quote(item.server.URL),
				"-c", prefix+".bearer_token_env_var="+strconv.Quote(knowledgeSessionTokenEnvironment),
			)
		}
		return args, nil
	}
	if executor == "claude" {
		servers := map[string]any{}
		for _, item := range []struct {
			name    string
			server  config.MCPServer
			enabled bool
		}{{"ivoai-memory", memoryServer, memoryEnabled}, {"ivoai-context", contextServer, contextEnabled}} {
			if item.enabled {
				servers[item.name] = map[string]any{"type": "http", "url": item.server.URL, "headers": map[string]string{"Authorization": "Bearer ${" + knowledgeSessionTokenEnvironment + "}"}}
			}
		}
		body, err := json.Marshal(map[string]any{"mcpServers": servers})
		if err != nil {
			return nil, err
		}
		path := filepath.Join(runtimeDir, "knowledge-mcp.json")
		if err := platform.AtomicWritePrivate(body, path); err != nil {
			return nil, err
		}
		return []string{"--mcp-config", path}, nil
	}
	// OpenCode's direct executor remains provider-neutral at the selection
	// boundary. Its MCP transport integration is intentionally deferred with
	// the existing HTTP/OpenAPI work rather than mutating global user config.
	return nil, nil
}

func hasSelectedFeature(selection serverpool.Selection, feature string) bool {
	for _, group := range selection.Groups {
		for _, profile := range group.Profiles {
			if feature == "context" && profile.ContextMCPURL != "" || feature == "memory" && profile.MemoryMCPURL != "" {
				return true
			}
		}
	}
	return false
}

func cleanKnowledgeEnvironment(environment []string) []string {
	blocked := map[string]bool{
		connections.ServerTokenEnvironment: true,
		"AI_MEMORY_SERVER_URL":             true,
		"AI_MEMORY_AUTH_TOKEN":             true,
		"IVOAI_CONTEXT_MCP_URL":            true,
		"IVOAI_MEMORY_MCP_URL":             true,
		knowledgeSessionTokenEnvironment:   true,
	}
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		key, _, found := strings.Cut(entry, "=")
		if !found || blocked[key] {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func runtimeKnowledgeServers(fallback map[string]config.MCPServer) map[string]config.MCPServer {
	contextURL := strings.TrimSpace(os.Getenv("IVOAI_CONTEXT_MCP_URL"))
	memoryURL := strings.TrimSpace(os.Getenv("IVOAI_MEMORY_MCP_URL"))
	if contextURL == "" && memoryURL == "" {
		return fallback
	}
	result := map[string]config.MCPServer{}
	if contextURL != "" {
		result["ivoai-context"] = config.MCPServer{URL: contextURL, Enabled: true, Kind: "context"}
	}
	if memoryURL != "" {
		result["ivoai-memory"] = config.MCPServer{URL: memoryURL, Enabled: true, Kind: "memory"}
	}
	return result
}

func parseKnowledgeSelectors(values []string) ([]string, error) {
	result := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		for _, selector := range strings.Split(value, ",") {
			selector = strings.TrimSpace(selector)
			if err := serverpool.ValidateLabel("knowledge source", selector); err != nil {
				return nil, err
			}
			if !seen[selector] {
				result = append(result, selector)
				seen[selector] = true
			}
		}
	}
	if len(result) > serverpool.MaxSelectedSources {
		return nil, errors.New("too many knowledge sources selected")
	}
	return result, nil
}
