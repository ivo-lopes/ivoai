package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/codeintel"
	"github.com/ivo-lopes/ivoai/internal/components"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/policy"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/skills"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
	"github.com/ivo-lopes/ivoai/internal/workers"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

const nativeCodeMCP = "ivoai-code"

// CodeIntelligenceProvider binds a local provider to a host-selected worktree and
// organization scope. It performs no installation, indexing or configuration write.
func (a *App) CodeIntelligenceProvider(ctx context.Context, root, scope string) (*codeintel.Provider, error) {
	for _, spec := range components.DefaultCatalog() {
		if spec.Name == "graft" {
			return a.codeIntelligenceProvider(ctx, root, scope, spec)
		}
	}
	return nil, errors.New("code intelligence unavailable: managed Graft metadata absent")
}

func (a *App) codeIntelligenceProvider(ctx context.Context, root, scope string, spec components.Spec) (*codeintel.Provider, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := codeintel.Identity(root, scope, "filesystem.read"); err != nil {
		return nil, err
	}
	inventory, err := codeintel.Inventory(ctx, root)
	if err != nil {
		return nil, err
	}
	if len(inventory) == 0 {
		return nil, codeintel.ErrNoSupportedSources
	}
	decision := policy.DefaultEngine().Evaluate(policy.Request{SubjectID: nativeCodeMCP, SubjectKind: policy.SubjectTool, Scope: scope, DeclaredCapabilities: []string{"filesystem.read"}, RequestedCapabilities: []string{"filesystem.read"}, Risk: skills.RiskLow, MetadataValid: true, ConflictResolved: true})
	if decision.Decision != policy.Allow {
		return nil, errors.New("code intelligence denied by filesystem policy")
	}
	expected, err := components.ResolvedSource(spec)
	if err != nil {
		return nil, err
	}
	state, err := a.Store.LoadState()
	if err != nil {
		return nil, err
	}
	component := state.Components["graft"]
	if !component.Managed || !component.Installed || component.Version != codeintel.Version {
		return nil, errors.New("managed code runtime unavailable")
	}
	active, distribution, err := (supplychain.Manager{Root: filepath.Join(a.Store.Paths.DataDir, "supply-chain")}).Active("graft")
	if err != nil || !reflect.DeepEqual(active, expected) || component.Path != filepath.Join(distribution, "graft-runtime") {
		return nil, errors.New("managed code runtime integrity or identity mismatch")
	}
	node := components.ManagedNodePath(a.Store.Paths.DataDir)
	if info, err := os.Lstat(node); err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return nil, errors.New("managed code Node runtime unavailable")
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return &codeintel.Provider{Root: root, Scope: scope, Permissions: "filesystem.read", CacheRoot: filepath.Join(a.Store.Paths.CacheDir, "code-intelligence"), Node: node, Distribution: distribution, Executable: executable, Allowed: true}, nil
}

func nativeCodeTool(name string) bool { return slices.Contains(codeintel.ToolNames(), name) }

// Only host-projected native endpoints join external endpoints at the process
// boundary. A persistent server entry cannot acquire native authority.
func processLocalMCPNames(cfg config.Config) []string {
	names := externalMCPNames(cfg)
	if entry, ok := cfg.MCP.Servers[nativeCodeMCP]; ok && entry.Enabled && entry.Kind == "native" && entry.HeaderEnv["Authorization"] == externalMCPTokenEnvironment {
		names = append(names, nativeCodeMCP)
	}
	sort.Strings(names)
	return names
}

func nativeCodeEnvironment(environment []string, cfg config.Config) ([]string, error) {
	entry, ok := cfg.MCP.Servers[nativeCodeMCP]
	if !ok || !entry.Enabled || entry.Kind != "native" || entry.HeaderEnv["Authorization"] != externalMCPTokenEnvironment {
		return environment, nil
	}
	content := map[string]json.RawMessage{}
	if existing := environmentValue(environment, "OPENCODE_CONFIG_CONTENT"); existing != "" {
		if json.Unmarshal([]byte(existing), &content) != nil || content == nil {
			return nil, errors.New("invalid OpenCode process configuration")
		}
	}
	servers := map[string]json.RawMessage{}
	if raw, ok := content["mcp"]; ok {
		if json.Unmarshal(raw, &servers) != nil || servers == nil {
			return nil, errors.New("invalid OpenCode MCP configuration")
		}
	}
	servers[nativeCodeMCP], _ = json.Marshal(map[string]any{"type": "remote", "url": entry.URL, "oauth": false, "headers": map[string]string{"Authorization": "Bearer {env:" + externalMCPTokenEnvironment + "}"}})
	content["mcp"], _ = json.Marshal(servers)
	body, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	return setProcessEnvironment(environment, "OPENCODE_CONFIG_CONTENT", string(body)), nil
}

func (a *App) prefetchWorkerCode(ctx context.Context, provider *codeintel.Provider, id string, task routing.Task, request workers.Request) (workers.Request, error) {
	query, ok := codeintel.ForTask(task.Task)
	if !ok {
		return request, nil
	}
	// Preserve the host's existing scoped envelope. Source evidence never enters
	// SkillInstructions or changes its capability grants.
	envelope := map[string]json.RawMessage{}
	if request.SharedContextBrief != "" {
		if err := json.Unmarshal([]byte(request.SharedContextBrief), &envelope); err != nil || envelope == nil {
			return request, errors.New("invalid worker context envelope")
		}
	}
	evidence, err := a.prefetchCodeEvidence(ctx, provider, id, task.ID, request.WorkerID, query)
	if err != nil {
		return request, err
	}
	envelope["code_evidence"] = evidence
	body, err := json.Marshal(envelope)
	if err != nil || len(body) > 32<<10 {
		return request, errors.New("code prefetch exceeds worker context budget")
	}
	request.SharedContextBrief = string(body)
	return request, nil
}

// prefetchCodeEvidence is shared by primary and workers, preserving exact
// restricted artifacts without creating executor grants or loading skills.
func (a *App) prefetchCodeEvidence(ctx context.Context, provider *codeintel.Provider, id, taskID, workerID string, query core.CodeQuery) (json.RawMessage, error) {
	result, err := provider.Inspect(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(result.Text) > 4096 {
		return nil, errors.New("code prefetch exceeded evidence budget")
	}
	store, err := workingcontext.NewLocalStore(filepath.Join(a.Store.Paths.CacheDir, "working-context"), workingcontext.LocalOptions{})
	if err != nil {
		return nil, err
	}
	ref, err := store.Put(ctx, workingcontext.PutRequest{Kind: workingcontext.ArtifactStdout, MediaType: "text/plain; charset=utf-8", Owner: workingcontext.Ownership{SessionID: id, TaskID: taskID, WorkerID: workerID}, Sensitivity: workingcontext.SensitivityRestricted, Truncated: result.Truncated}, strings.NewReader(result.Text))
	if err != nil {
		return nil, err
	}
	evidence := struct {
		Text      string                     `json:"text"`
		Artifact  workingcontext.ArtifactRef `json:"artifact"`
		Fidelity  core.CompressionFidelity   `json:"fidelity"`
		Untrusted bool                       `json:"untrusted"`
	}{result.Text, ref, core.CompressionExactRequired, true}
	return json.Marshal(evidence)
}
