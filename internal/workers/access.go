package workers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

// MCPGrant is host-created input, never deserialized from model output. Token
// must be a short-lived, task-scoped loopback proxy capability, not upstream
// authentication. The proxy independently enforces Tools and source scope.
type MCPGrant struct {
	Name  string   `json:"name"`
	URL   string   `json:"-"`
	Tools []string `json:"tools"`
	Token string   `json:"-"`
}

func (MCPGrant) String() string   { return "[task-scoped MCP grant]" }
func (MCPGrant) GoString() string { return "[task-scoped MCP grant]" }

// Access is an immutable process-local projection. Nil retains the explicit
// legacy advisory adapter contract. AUTO always supplies a projection, even
// when it contains zero MCPs. Its private fields cannot be populated by JSON.
type Access struct {
	directory string
	write     bool
	grants    []MCPGrant
	prefix    string
}

func (*Access) String() string   { return "[worker capability projection]" }
func (*Access) GoString() string { return "[worker capability projection]" }

// Metadata is safe for operational UI and tests; it never returns transport
// addresses or credentials, and callers cannot mutate the effective grant.
func (a *Access) Metadata() (bool, map[string][]string) {
	result := map[string][]string{}
	if a == nil {
		return false, result
	}
	for _, grant := range a.grants {
		result[grant.Name] = append([]string(nil), grant.Tools...)
	}
	return a.write, result
}

// ConfigureNative replaces, rather than merges, the native worker MCP policy.
// It reuses the same controlled OpenCode HTTP lifecycle as other native work.
func (a *Access) ConfigureNative(options opencodebridge.ManagedOptions) opencodebridge.ManagedOptions {
	options.NativeExecutor, options.Bridge, options.Directory = true, nil, a.directory
	options.Environment = a.environment(options.Environment)
	options.NativeMCP = map[string]any{}
	options.NativePermissions = opencodebridge.NativePermissionPolicy("full", true)
	options.NativePermissions["external_directory"] = "deny"
	if a.write {
		options.NativePermissions["edit"] = "allow"
	}
	for i, grant := range a.grants {
		name := a.prefix + grant.Name
		options.NativeMCP[name] = map[string]any{"type": "remote", "url": grant.URL, "oauth": false, "headers": map[string]string{"Authorization": "Bearer {env:" + a.tokenVariable(i) + "}"}}
		for _, tool := range grant.Tools {
			options.NativePermissions[name+"_"+tool] = "allow"
		}
	}
	return options
}

var toolIdentifier = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,95}$`)
var codexServerIdentifier = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

func NewAccess(directory string, write bool, grants []MCPGrant) (*Access, error) {
	if !filepath.IsAbs(directory) || strings.ContainsAny(directory, "\x00\r\n\x1b") || len(grants) > 16 {
		return nil, errors.New("invalid worker access projection")
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	a := &Access{directory: filepath.Clean(directory), write: write, prefix: "ivoai-worker-" + hex.EncodeToString(nonce[:]) + "-"}
	seen := map[string]bool{}
	for _, grant := range grants {
		u, err := url.Parse(grant.URL)
		if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !toolIdentifier.MatchString(grant.Name) || seen[grant.Name] || len(grant.Tools) == 0 || len(grant.Tools) > 128 || len(grant.Token) < 16 || len(grant.Token) > 256 || strings.ContainsAny(grant.Token, "\r\n\x00") {
			return nil, errors.New("MCP_DENIED: invalid task-scoped loopback grant")
		}
		seen[grant.Name] = true
		tools := append([]string(nil), grant.Tools...)
		for _, name := range tools {
			if !toolIdentifier.MatchString(name) {
				return nil, errors.New("MCP_DENIED: invalid tool grant")
			}
		}
		sort.Strings(tools)
		grant.Tools = tools
		a.grants = append(a.grants, grant)
	}
	return a, nil
}

func (a *Access) environment(base []string) []string {
	result := make([]string, 0, len(base)+len(a.grants))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if _, blocked := providerEnvironment[key]; blocked {
			continue
		}
		if strings.HasPrefix(key, "IVOAI_") || strings.HasPrefix(key, "AI_MEMORY_") || strings.HasPrefix(key, "GIT_") {
			continue
		}
		result = append(result, entry)
	}
	for i, grant := range a.grants {
		result = append(result, a.tokenVariable(i)+"="+grant.Token)
	}
	return result
}

func (a *Access) tokenVariable(i int) string {
	// Limited to 16 grants; deterministic references contain no credential.
	return "IVOAI_WORKER_MCP_TOKEN_" + string(rune('A'+i))
}

func (a Adapter) isolateScopedMCPs(ctx context.Context, executable string, request Request, args []string) ([]string, error) {
	access := request.Access
	if request.Executor == "codex" {
		// Reuse the official inventory/disable boundary, then add uniquely named
		// task-local projections. No personal server can supply transport fields
		// to a managed alias through TOML merging.
		a.KnowledgeServers = nil
		isolated, err := a.isolateMCPs(ctx, executable, request.Executor, args)
		if err != nil {
			return nil, err
		}
		projection := []string{}
		for i, grant := range access.grants {
			key := "mcp_servers." + access.prefix + grant.Name
			projection = append(projection,
				"-c", key+".url="+strconv.Quote(grant.URL),
				"-c", key+".bearer_token_env_var="+strconv.Quote(access.tokenVariable(i)),
				"-c", key+".enabled=true",
				"-c", key+".enabled_tools="+tomlStringArray(grant.Tools),
				"-c", key+`.default_tools_approval_mode="approve"`,
			)
		}
		return append(projection, isolated...), nil
	}
	if request.Executor != "claude" {
		return nil, errors.New("unsupported scoped worker executor")
	}
	if access.write {
		if a.Runner == nil {
			return nil, errors.New("Claude restricted worktree capability unavailable")
		}
		probe, err := a.Runner.Run(ctx, executable, []string{"--help"}, platform.RunOptions{Timeout: 15 * time.Second})
		if err != nil || len(probe.Stdout) > 256<<10 || !strings.Contains(probe.Stdout, "--restricted") || !strings.Contains(probe.Stdout, "acceptEdits") {
			return nil, errors.New("Claude restricted worktree capability unavailable")
		}
	}
	servers := map[string]any{}
	allowed := []string{}
	for i, grant := range access.grants {
		name := access.prefix + grant.Name
		servers[name] = map[string]any{"type": "http", "url": grant.URL, "headers": map[string]string{"Authorization": "Bearer ${" + access.tokenVariable(i) + "}"}}
		for _, tool := range grant.Tools {
			allowed = append(allowed, "mcp__"+name+"__"+tool)
		}
	}
	body, err := json.Marshal(map[string]any{"mcpServers": servers})
	if err != nil {
		return nil, err
	}
	projection := []string{"--strict-mcp-config", "--mcp-config", string(body)}
	if len(allowed) > 0 {
		projection = append(projection, "--allowedTools", strings.Join(allowed, ","))
	}
	return append(projection, args...), nil
}
