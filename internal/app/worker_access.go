package app

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"sync"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/connections"
	"github.com/ivo-lopes/ivoai/internal/externalmcp"
	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/policy"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/serverpool"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/skillcatalog"
	"github.com/ivo-lopes/ivoai/internal/skillgate"
	"github.com/ivo-lopes/ivoai/internal/skills"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
	"github.com/ivo-lopes/ivoai/internal/workers"
)

// prepareWorkerAccess is an authority boundary: task text can request names,
// never endpoints, headers or credentials. Every worker receives an independent
// revocable loopback capability. Institutional credentials remain in IVOAI.
func (a *App) prepareWorkerAccess(ctx context.Context, cfg config.Config, store session.Store, id string, task routing.Task, request workers.Request) (workers.Request, error) {
	profile, err := orchestration.CapabilityProfile(task.Role)
	if err != nil {
		return request, err
	}
	current, err := store.Get(id)
	if err != nil {
		return request, err
	}
	selected := map[string]bool{}
	for _, alias := range current.KnowledgeSources {
		selected[alias] = true
	}
	profiles := map[string]config.ServerProfile{}
	for _, alias := range task.KnowledgeSources {
		p, ok := cfg.Connections.Servers[alias]
		if !ok || !selected[alias] {
			return request, errors.New("MCP_DENIED: worker source is outside the approved session scope")
		}
		profiles[alias] = p
	}
	pool, err := serverpool.New(profiles)
	if err != nil {
		return request, err
	}
	selection, err := pool.Resolve(task.KnowledgeSources)
	if err != nil {
		return request, err
	}
	var knowledge sessionKnowledge
	var gateway *externalmcp.Gateway
	var once sync.Once
	release := func() {
		once.Do(func() {
			if gateway != nil {
				gateway.Close()
			}
			knowledge.close()
		})
	}
	success := false
	defer func() {
		if !success {
			release()
		}
	}()
	targets := []externalmcp.Target{}
	capabilities := map[string]bool{"filesystem.read": true, "filesystem.write": profile.Write && len(task.WritePaths) > 0 && !request.PatchOnly}
	seen := map[string]bool{}
	for _, name := range task.AllowedMCPs {
		if seen[name] {
			return request, errors.New("MCP_DENIED: duplicate worker MCP")
		}
		seen[name] = true
		target := externalmcp.Target{Name: name, Restricted: true}
		if name == "ivoai-memory" || name == "ivoai-context" {
			if len(selection.Groups) == 0 {
				return request, errors.New("MCP_DENIED: knowledge MCP requires an explicit worker source")
			}
			if knowledge.router == nil {
				knowledge, err = a.prepareKnowledgeSelection(ctx, cfg, selection, "", "", nil, nil)
				if err != nil {
					return request, err
				}
			}
			entry := knowledge.config.MCP.Servers[name]
			if !entry.Enabled {
				return request, errors.New("MCP_DENIED: requested knowledge capability unavailable")
			}
			target.URL, target.Headers = entry.URL, http.Header{"Authorization": {"Bearer " + knowledge.router.Token()}}
			if name == "ivoai-memory" {
				target.AllowedTools = []string{"memory_query", "memory_read_page", "memory_recent", "memory_status"}
				capabilities["memory.read"] = true
			} else {
				target.AllowedTools = []string{"context_search", "context_get_document", "context_recent", "context_health"}
				capabilities["context.read"] = true
			}
		} else {
			entry, ok := cfg.MCP.Servers[name]
			if !ok || !entry.Enabled || entry.Kind != "external" || connections.IsManagedMCPName(name) {
				return request, errors.New("MCP_DENIED: external worker MCP unavailable")
			}
			registry := connections.Registry{Store: a.Store}
			inventory, err := registry.DiscoverTools(ctx, entry)
			if err != nil {
				return request, err
			}
			for _, tool := range inventory {
				if tool.ReadOnly {
					target.AllowedTools = append(target.AllowedTools, tool.Name)
				}
			}
			// Writes are not inferred from a server name or full frontend mode.
			if len(target.AllowedTools) == 0 {
				return request, errors.New("MCP_DENIED: no verified read-only worker tools")
			}
			target.URL = entry.URL
			target.Headers, err = registry.Headers(entry)
			if err != nil {
				return request, err
			}
			capabilities["network.read"] = true
		}
		targets = append(targets, target)
	}
	gate := skillgate.Gate{Registry: skills.Store{Path: skills.RegistryPath(a.Store.Paths.StateDir)}, Supply: supplychain.Manager{Root: filepath.Join(a.Store.Paths.DataDir, "supply-chain")}, Policy: policy.DefaultEngine()}
	registry, err := gate.Registry.Load()
	if err != nil {
		return request, err
	}
	excluded := []string{}
	for id, preference := range cfg.Skills.Sources {
		if preference.Disabled {
			excluded = append(excluded, id)
		}
	}
	if cfg.Skills.ResolvedPonytail() == "off" {
		excluded = append(excluded, "ponytail")
	}
	candidates := skillcatalog.WorkerCandidates(registry, task.Role, task.Task, request.Executor, cfg.Skills.ResolvedPonytail())
	if cfg.Skills.ResolvedPonytail() == "auto" && (task.Role != "implementation" || task.Scores.Risk >= 70) {
		excluded = append(excluded, "ponytail")
	}
	skillResult, err := gate.Evaluate(ctx, skillgate.Input{ExplicitOnly: true, Required: task.Skills, Candidates: candidates, ExcludedArtifacts: excluded, Executor: request.Executor, AvailableCapabilities: capabilities})
	if len(skillResult.Events) > 0 {
		_, saveErr := store.Update(id, func(value *session.Session) error {
			for _, event := range skillResult.Events {
				event.SessionID, event.TaskID, event.WorkerID = id, task.ID, request.WorkerID
				if err := session.AppendObservation(value, event); err != nil {
					return err
				}
			}
			return nil
		})
		if saveErr != nil {
			return request, saveErr
		}
	}
	if err != nil {
		return request, err
	}
	request.SkillInstructions = skillResult.Instructions
	request.SelectedSkills = append([]string(nil), skillResult.Selected...)
	grants := []workers.MCPGrant{}
	if len(targets) > 0 {
		// Plan approval grants these read-only capabilities, not arbitrary MCP
		// writes. The proxy enforces the allowlist even for a malicious child.
		gateway, err = externalmcp.Start(targets, "full")
		if err != nil {
			return request, err
		}
		for i, target := range targets {
			grants = append(grants, workers.MCPGrant{Name: target.Name, URL: gateway.URL(i), Tools: target.AllowedTools, Token: gateway.Token()})
		}
	}
	request.Access, err = workers.NewAccess(request.Directory, capabilities["filesystem.write"], grants)
	if err != nil {
		return request, err
	}
	request.Release = release
	success = true
	return request, nil
}
