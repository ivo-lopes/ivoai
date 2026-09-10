package cli

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/terminalui"
)

// These settings use the same validated, persistent ConfigSet domain as the
// CLI. No parallel menu configuration or credential storage is introduced.
func (s *menuSession) orchestrationPolicies() (bool, error) {
	for {
		v, err := s.app.MenuSnapshot()
		if err != nil {
			return false, err
		}
		actions := s.orchestrationPolicyActions(v)
		id, err := s.choose("Orchestration Policies (effective next AUTO session)", actions, nil)
		if err != nil || id == "" {
			return false, err
		}
		exit, err := s.execute(findAction(actions, id))
		if err != nil {
			fmt.Fprintln(s.app.Err, "Error:", UserError(err))
		}
		if exit {
			return true, err
		}
		terminalui.Pause(s.app.In, s.app.Out)
	}
}

func (s *menuSession) orchestrationPolicyActions(v app.MenuSnapshot) []menuAction {
	return []menuAction{
		{id: "policy.sources", label: "Knowledge routing: " + v.KnowledgeRouting, run: s.policyChoice("Knowledge routing", "knowledge_routing", []string{"purpose-auto", "all-enabled", "explicit-only"})},
		{id: "policy.concurrency", label: "Concurrency: " + v.Concurrency, run: s.policyChoice("Concurrency", "concurrency", []string{"auto", "sequential"})},
		{id: "policy.worker-cap", label: fmt.Sprintf("Worker cap: %d (0 = auto)", v.WorkerCap), run: s.policyInteger("Worker cap (0 = automatic, 1-12 = maximum)", "worker_cap", v.WorkerCap, 0, 12)},
		{id: "policy.writes", label: toggleLabel("Parallel writes when isolated", v.ParallelWrites), run: s.policyChoice("Parallel writes (requires isolated worktrees)", "parallel_writes", []string{"true", "false"})},
		{id: "policy.provider", label: "Provider preference: " + v.ProviderPreference, run: s.policyChoice("Provider preference (explicit session selection remains authoritative)", "provider_preference", []string{"auto", "codex", "claude"})},
		{id: "policy.low-quota", label: fmt.Sprintf("Low quota threshold: %d%%", v.LowQuotaThreshold), run: s.policyInteger("Low quota remaining threshold (%)", "low_quota_threshold", v.LowQuotaThreshold, 1, 100)},
		{id: "policy.primary", label: "Primary: strong / workers: minimum sufficient", disabled: "Resolved from the verified runtime model catalog; explicit model selection remains authoritative"},
		{id: "policy.mcp", label: "Worker MCP policy: deny by default", disabled: "Only task-specific policy grants are projected"},
		{id: "policy.confirmation", label: "Material quota routing changes: confirmation required", disabled: "Independent from plan approval and OpenCode tool permissions"},
	}
}

func (s *menuSession) policyChoice(title, key string, choices []string) func() (bool, error) {
	return func() (bool, error) {
		actions := make([]menuAction, 0, len(choices))
		for _, choice := range choices {
			actions = append(actions, menuAction{id: "policy.choice." + choice, label: choice, run: s.simple(func() error {
				return s.app.ConfigSet("orchestration.auto."+key, choice)
			})})
		}
		id, err := s.choose(title+" (effective next AUTO session)", actions, nil)
		if err != nil || id == "" {
			return false, err
		}
		return s.execute(findAction(actions, id))
	}
}

func (s *menuSession) policyInteger(label, key string, current, minimum, maximum int) func() (bool, error) {
	return func() (bool, error) {
		value, err := s.promptValidated(label, false, strconv.Itoa(current), func(value string) error {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < minimum || parsed > maximum {
				return errors.New("value is outside the supported range")
			}
			return nil
		})
		if err != nil {
			return false, err
		}
		return false, s.app.ConfigSet("orchestration.auto."+key, value)
	}
}
