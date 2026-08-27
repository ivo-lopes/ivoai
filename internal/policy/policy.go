// Package policy defines the deny-by-default control-plane policy applied
// above skills, tools, hooks, and executors. External content is never an
// authority source for these decisions.
package policy

import (
	"errors"
	"sort"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/skills"
)

type Decision string

const (
	Allow           Decision = "ALLOW"
	Deny            Decision = "DENY"
	RequireApproval Decision = "REQUIRE_APPROVAL"
)

type SubjectKind string

const (
	SubjectSkill    SubjectKind = "skill"
	SubjectTool     SubjectKind = "tool"
	SubjectHook     SubjectKind = "hook"
	SubjectExecutor SubjectKind = "executor"
)

type CapabilityRule struct {
	Available        bool
	MaximumRisk      skills.RiskTier
	ApprovalRequired bool
	Destructive      bool
}

type Engine struct {
	Capabilities map[string]CapabilityRule
}

type Request struct {
	SubjectID             string
	SubjectKind           SubjectKind
	DeclaredCapabilities  []string
	RequestedCapabilities []string
	Risk                  skills.RiskTier
	Scope                 string
	MetadataValid         bool
	ConflictResolved      bool
}

type Approval struct {
	Required     bool     `json:"required"`
	Capabilities []string `json:"capabilities,omitempty"`
	Scope        string   `json:"scope,omitempty"`
}

type Result struct {
	Decision Decision `json:"decision"`
	Reason   string   `json:"reason"`
	Approval Approval `json:"approval"`
	Scope    string   `json:"scope"`
}

func DefaultEngine() Engine {
	return Engine{Capabilities: map[string]CapabilityRule{
		"filesystem.read":         {Available: true, MaximumRisk: skills.RiskModerate},
		"context.read":            {Available: true, MaximumRisk: skills.RiskModerate},
		"memory.read":             {Available: true, MaximumRisk: skills.RiskModerate},
		"network.read":            {Available: true, MaximumRisk: skills.RiskModerate},
		"filesystem.write":        {Available: true, MaximumRisk: skills.RiskHigh, ApprovalRequired: true},
		"memory.write":            {Available: true, MaximumRisk: skills.RiskHigh, ApprovalRequired: true},
		"tool.invoke":             {Available: true, MaximumRisk: skills.RiskHigh, ApprovalRequired: true},
		"filesystem.delete":       {Available: false, MaximumRisk: skills.RiskCritical, Destructive: true},
		"shell.execute":           {Available: false, MaximumRisk: skills.RiskCritical},
		"privilege.elevate":       {Available: false, MaximumRisk: skills.RiskCritical},
		"sandbox.disable":         {Available: false, MaximumRisk: skills.RiskCritical},
		"orchestration.authority": {Available: false, MaximumRisk: skills.RiskCritical},
	}}
}

func (e Engine) Evaluate(request Request) Result {
	result := Result{Decision: Deny, Scope: request.Scope}
	if !safeID(request.SubjectID) || !validSubject(request.SubjectKind) || !safeScope(request.Scope) {
		result.Reason = "invalid_request_metadata"
		return result
	}
	if !request.MetadataValid {
		result.Reason = "invalid_or_quarantined_metadata"
		return result
	}
	if !request.ConflictResolved {
		result.Reason = "unresolved_conflict"
		return result
	}
	if !validRisk(request.Risk) {
		result.Reason = "unknown_risk"
		return result
	}
	declared := set(request.DeclaredCapabilities)
	requested := sortedUnique(request.RequestedCapabilities)
	if len(requested) == 0 {
		result.Reason = "no_capability_requested"
		return result
	}
	var approvals []string
	for _, capability := range requested {
		if !declared[capability] {
			result.Reason = "undeclared_capability"
			return result
		}
		rule, known := e.Capabilities[capability]
		if !known {
			result.Reason = "unknown_capability"
			return result
		}
		if !rule.Available {
			if rule.Destructive {
				result.Reason = "destructive_capability_denied"
			} else {
				result.Reason = "capability_unavailable"
			}
			return result
		}
		if riskWeight(request.Risk) > riskWeight(rule.MaximumRisk) {
			result.Reason = "risk_exceeds_capability_policy"
			return result
		}
		if rule.ApprovalRequired {
			approvals = append(approvals, capability)
		}
	}
	if request.Risk == skills.RiskCritical {
		result.Reason = "critical_risk_denied"
		return result
	}
	if request.Risk == skills.RiskHigh || len(approvals) > 0 {
		result.Decision = RequireApproval
		result.Reason = "human_approval_required"
		result.Approval = Approval{Required: true, Capabilities: approvals, Scope: request.Scope}
		return result
	}
	result.Decision = Allow
	result.Reason = "declared_capability_allowed"
	return result
}

func (e Engine) Validate() error {
	if len(e.Capabilities) == 0 || len(e.Capabilities) > 256 {
		return errors.New("policy engine requires a bounded capability registry")
	}
	for capability, rule := range e.Capabilities {
		if !safeCapability(capability) || !validRisk(rule.MaximumRisk) {
			return errors.New("invalid capability policy")
		}
	}
	return nil
}

func sortedUnique(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func set(values []string) map[string]bool {
	result := map[string]bool{}
	for _, value := range sortedUnique(values) {
		result[value] = true
	}
	return result
}

func safeID(value string) bool {
	return value != "" && len(value) <= 128 && safeCapability(value) && !strings.Contains(value, "..")
}

func safeCapability(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || strings.ContainsRune("._:-", char)) {
			return false
		}
	}
	return true
}

func safeScope(value string) bool {
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "\x00\x1b\r\n/\\") {
		return false
	}
	return true
}

func validSubject(value SubjectKind) bool {
	return value == SubjectSkill || value == SubjectTool || value == SubjectHook || value == SubjectExecutor
}

func validRisk(value skills.RiskTier) bool {
	return value == skills.RiskLow || value == skills.RiskModerate || value == skills.RiskHigh || value == skills.RiskCritical
}

func riskWeight(value skills.RiskTier) int {
	switch value {
	case skills.RiskLow:
		return 1
	case skills.RiskModerate:
		return 2
	case skills.RiskHigh:
		return 3
	case skills.RiskCritical:
		return 4
	default:
		return 99
	}
}
