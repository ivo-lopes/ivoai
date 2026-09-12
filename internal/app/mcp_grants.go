package app

import (
	"context"
	"errors"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/connections"
	"github.com/ivo-lopes/ivoai/internal/externalmcp"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
)

// The planner requests names only. IVOAI checks live inventory and policy before
// presenting the plan. Immediate start never skips approval of non-read tools.
func (a *App) checkMCPPlan(ctx context.Context, cfg config.Config, tasks []routing.TaskInput) (bool, error) {
	latest, err := a.Store.Load()
	if err != nil {
		return false, err
	}
	cfg.MCP = latest.MCP
	required := false
	inventories := map[string]map[string]connections.MCPToolCapability{}
	for _, task := range tasks {
		if err := externalmcp.ValidateScope(task.AllowedMCPTools); err != nil {
			return false, err
		}
		for _, name := range task.AllowedMCPs {
			if !connections.IsManagedMCPName(name) && len(task.AllowedMCPTools[name]) == 0 {
				return false, errors.New("MCP_DENIED: external task requires exact allowed_mcp_tools")
			}
		}
		for name, tools := range task.AllowedMCPTools {
			if connections.IsManagedMCPName(name) {
				if name == "ivoai-orchestrator" {
					return false, errors.New("MCP_DENIED: workers cannot receive orchestration authority")
				}
				continue
			}
			entry, ok := cfg.MCP.Servers[name]
			if !ok || !entry.Enabled || entry.Kind != "external" {
				return false, errors.New("MCP_DENIED: unavailable external server")
			}
			if inventories[name] == nil {
				inventory, err := (connections.Registry{Store: a.Store}).DiscoverTools(ctx, entry)
				if err != nil {
					return false, err
				}
				inventories[name] = map[string]connections.MCPToolCapability{}
				for _, tool := range inventory {
					inventories[name][tool.Name] = tool
				}
			}
			for _, toolName := range tools {
				tool, ok := inventories[name][toolName]
				if !ok {
					return false, errors.New("MCP_DENIED: tool absent from live inventory")
				}
				if !tool.ReadOnly {
					if entry.ResolvedMCPPolicy() != "read_auto_ask_mutating" {
						return false, errors.New("MCP_DENIED: server policy permits only read tools")
					}
					required = true
				}
			}
		}
	}
	return required, nil
}

func approvedPlan(value session.Session) bool {
	if value.PlanID == "" {
		return false
	}
	for _, d := range value.Decisions {
		if d.Kind == "plan" && d.ID == value.PlanID && d.State == "approved" {
			return true
		}
	}
	return false
}

// A primary is not a super-worker: it receives only currently runnable,
// primary-owned task tools, never grants belonging to delegated workers.
func primaryMCPGrant(value session.Session, cfg config.Config, server, tool string) (bool, bool) {
	approved := approvedPlan(value)
	for _, decision := range value.Decisions {
		if decision.Kind == "plan" && decision.ID == value.PlanID && decision.State != "approved" {
			return false, false
		}
	}
	if !value.Active() || (!approved && cfg.Orchestration.Auto.ResolvedPlanExecution() != "immediate") {
		return false, false
	}
	completed := map[string]bool{}
	for _, task := range value.Tasks {
		completed[task.ID] = task.State == session.StateCompleted
	}
	for _, task := range value.Tasks {
		if task.ExecutionMode != "primary" || task.State == session.StateCompleted || task.State == session.StateFailed || task.State == session.StateBlocked {
			continue
		}
		ready := true
		for _, dependency := range task.Dependencies {
			if !completed[dependency] {
				ready = false
			}
		}
		if !ready {
			continue
		}
		for _, allowed := range task.AllowedMCPTools[server] {
			if allowed == tool {
				return true, approved
			}
		}
	}
	return false, false
}
