// Package policy defines the deny-by-default control-plane policy applied
// above skills, tools, hooks, and executors. External content is never an
// authority source for these decisions.
package policy

import (
	"errors"
	"sort"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/observability"
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
	Capabilities          map[string]CapabilityRule
	MinimumAutomaticTrust string
	Observe               func(observability.Event)
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
	return Engine{MinimumAutomaticTrust: "commit_pinned_local_digest", Capabilities: map[string]CapabilityRule{
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

type TrustRequest struct {
	SubjectID         string
	TrustLevel        string
	SignatureStatus   string
	AttestationStatus string
	Automatic         bool
}

// EvaluateTrust distinguishes integrity from authenticity. A locally recorded
// digest over a commit-pinned artifact is sufficient for the default local
// managed store, but is never represented as an independent signature or
// attestation. Deployments may raise MinimumAutomaticTrust.
func (e Engine) EvaluateTrust(request TrustRequest) Result {
	result := Result{Decision: Deny, Scope: "supply_chain", Reason: "unverifiable_provenance"}
	defer func() {
		if e.Observe == nil {
			return
		}
		state, reason := observability.StateDenied, observability.ReasonPolicyDenied
		if result.Decision == Allow {
			state, reason = observability.StateAllowed, observability.ReasonPolicyAllowed
		}
		event, err := observability.Normalize(observability.Event{Category: observability.CategorySkillPolicy, Operation: observability.OperationPolicyDecision, State: state, SubjectID: request.SubjectID, SubjectKind: string(SubjectSkill), PolicyDecision: string(result.Decision), TrustLevel: request.TrustLevel, RoutingReason: reason})
		if err == nil {
			e.Observe(event)
		}
	}()
	if !safeID(request.SubjectID) || e.Validate() != nil {
		result.Reason = "invalid_policy_engine"
		return result
	}
	actual, ok := trustWeight(request.TrustLevel)
	if !ok {
		return result
	}
	minimum := e.MinimumAutomaticTrust
	if minimum == "" {
		minimum = "commit_pinned_local_digest"
	}
	required, ok := trustWeight(minimum)
	if !ok {
		result.Reason = "invalid_trust_policy"
		return result
	}
	if request.Automatic && actual < required {
		result.Reason = "trust_below_automatic_policy"
		return result
	}
	if request.SignatureStatus == "verified" && actual < 4 || request.AttestationStatus == "verified" && actual < 3 {
		result.Reason = "inconsistent_trust_metadata"
		return result
	}
	result.Decision = Allow
	result.Reason = "provenance_trust_allowed"
	return result
}

func trustWeight(value string) (int, bool) {
	switch value {
	case "unverifiable":
		return 0, true
	case "commit_pinned_local_digest":
		return 1, true
	case "upstream_checksum":
		return 2, true
	case "independent_attestation":
		return 3, true
	case "independent_signature":
		return 4, true
	default:
		return 0, false
	}
}

func (e Engine) Evaluate(request Request) (result Result) {
	result = Result{Decision: Deny, Scope: request.Scope}
	defer func() { e.emit(request, result) }()
	if err := e.Validate(); err != nil {
		result.Reason = "invalid_policy_engine"
		return result
	}
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
	if len(request.DeclaredCapabilities) > 256 || len(request.RequestedCapabilities) > 256 {
		result.Reason = "capability_request_too_large"
		return result
	}
	declared := set(request.DeclaredCapabilities)
	requested := sortedUnique(request.RequestedCapabilities)
	if len(requested) == 0 {
		if request.Risk == skills.RiskCritical {
			result.Reason = "critical_risk_denied"
			return result
		}
		if request.Risk == skills.RiskHigh {
			result.Decision = RequireApproval
			result.Reason = "human_approval_required"
			result.Approval = Approval{Required: true, Scope: request.Scope}
			return result
		}
		result.Decision = Allow
		result.Reason = "no_runtime_capability_required"
		return result
	}
	var approvals []string
	for _, capability := range requested {
		if !safeCapability(capability) {
			result.Reason = "invalid_capability"
			return result
		}
		if !declared[capability] {
			result.Reason = "undeclared_capability"
			return result
		}
		rule, known := e.Capabilities[capability]
		if !known {
			result.Reason = "unknown_capability"
			return result
		}
		if rule.Destructive {
			result.Reason = "destructive_capability_denied"
			return result
		}
		if !rule.Available {
			result.Reason = "capability_unavailable"
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

func (e Engine) emit(request Request, result Result) {
	if e.Observe == nil {
		return
	}
	state, reason := observability.StateDenied, observability.ReasonPolicyDenied
	if result.Decision == Allow {
		state, reason = observability.StateAllowed, observability.ReasonPolicyAllowed
	} else if result.Decision == RequireApproval {
		state, reason = observability.StateApprovalRequired, observability.ReasonApprovalRequired
	}
	event, err := observability.Normalize(observability.Event{
		Category: observability.CategorySkillPolicy, Operation: observability.OperationPolicyDecision, State: state,
		SubjectID: request.SubjectID, SubjectKind: string(request.SubjectKind), RiskTier: string(request.Risk), PolicyDecision: string(result.Decision), RoutingReason: reason,
	})
	if err == nil {
		e.Observe(event)
	}
}

func (e Engine) Validate() error {
	if len(e.Capabilities) == 0 || len(e.Capabilities) > 256 {
		return errors.New("policy engine requires a bounded capability registry")
	}
	if e.MinimumAutomaticTrust != "" {
		if _, ok := trustWeight(e.MinimumAutomaticTrust); !ok {
			return errors.New("policy engine has an invalid automatic trust threshold")
		}
	}
	for capability, rule := range e.Capabilities {
		if !safeCapability(capability) || !validRisk(rule.MaximumRisk) || rule.Destructive && rule.Available {
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
