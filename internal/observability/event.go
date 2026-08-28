// Package observability defines the bounded, secret-free operational events
// that the ivoai control plane may persist with session metadata.
package observability

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

type Category string

const (
	CategoryExecutor       Category = "executor"
	CategoryCapability     Category = "capability"
	CategoryFallback       Category = "fallback"
	CategoryMemory         Category = "memory"
	CategoryContext        Category = "context"
	CategoryCompression    Category = "compression"
	CategoryOrchestration  Category = "orchestration"
	CategoryDAG            Category = "dag"
	CategoryWorker         Category = "worker"
	CategoryApproval       Category = "approval"
	CategoryQuota          Category = "quota"
	CategorySkillRegistry  Category = "skill_registry"
	CategorySkillIndex     Category = "skill_index"
	CategorySkillPolicy    Category = "skill_policy"
	CategorySupplyChain    Category = "supply_chain"
	CategoryWorkingContext Category = "working_context"
)

type Operation string

const (
	OperationExecutorSelect          Operation = "executor.select"
	OperationCapabilityResolve       Operation = "capability.resolve"
	OperationFallbackRoute           Operation = "fallback.route"
	OperationMemoryBootstrap         Operation = "memory.bootstrap"
	OperationContextBootstrap        Operation = "context.bootstrap"
	OperationCompressionSelect       Operation = "compression.select"
	OperationOrchestrationInitialize Operation = "orchestration.initialize"
	OperationDAGPlan                 Operation = "dag.plan"
	OperationWorkerLifecycle         Operation = "worker.lifecycle"
	OperationApprovalPolicy          Operation = "approval.policy"
	OperationQuotaProbe              Operation = "quota.probe"
	OperationQuotaRoute              Operation = "quota.route"
	OperationRegistryDiscovery       Operation = "skill_registry.discovery"
	OperationSkillCandidate          Operation = "skill.candidate"
	OperationSkillQuarantine         Operation = "skill.quarantine"
	OperationSkillConflict           Operation = "skill.conflict"
	OperationSkillGate               Operation = "skill.gate"
	OperationSkillContentLoad        Operation = "skill.content_load"
	OperationPolicyDecision          Operation = "skill_policy.decision"
	OperationSupplyResolve           Operation = "supply_chain.resolve"
	OperationSupplyStage             Operation = "supply_chain.stage"
	OperationSupplyPromote           Operation = "supply_chain.promote"
	OperationSupplyRollback          Operation = "supply_chain.rollback"
	OperationArtifactStoreWrite      Operation = "artifact_store.write"
	OperationArtifactStoreRead       Operation = "artifact_store.read"
	OperationArtifactStoreRangeRead  Operation = "artifact_store.range_read"
	OperationArtifactStoreGC         Operation = "artifact_store.gc"
	OperationArtifactStoreDenied     Operation = "artifact_store.denied"
	OperationWorkerResultProjected   Operation = "worker_result.projected"
	OperationWorkingContextBudget    Operation = "working_context.budget"
	OperationWorkingContextDegraded  Operation = "working_context.degraded"
)

type State string

const (
	StatePending          State = "pending"
	StateSelected         State = "selected"
	StateRunning          State = "running"
	StateCompleted        State = "completed"
	StateDegraded         State = "degraded"
	StateUnavailable      State = "unavailable"
	StateFailed           State = "failed"
	StateBlocked          State = "blocked"
	StateAllowed          State = "allowed"
	StateDenied           State = "denied"
	StateQuarantined      State = "quarantined"
	StateStaged           State = "staged"
	StatePromoted         State = "promoted"
	StateRolledBack       State = "rolled_back"
	StateApprovalRequired State = "approval_required"
)

type Reason string

const (
	ReasonDirect                 Reason = "direct"
	ReasonPrimaryAvailable       Reason = "primary_available"
	ReasonCapabilityMatch        Reason = "capability_match"
	ReasonQuotaAvailable         Reason = "quota_available"
	ReasonQuotaExhausted         Reason = "quota_exhausted"
	ReasonQuotaStale             Reason = "quota_stale"
	ReasonTelemetryNotExposed    Reason = "telemetry_not_exposed"
	ReasonProbeFailed            Reason = "probe_failed"
	ReasonAuthTransition         Reason = "auth_transition"
	ReasonAlternateSelected      Reason = "alternate_selected"
	ReasonProviderUnavailable    Reason = "provider_unavailable"
	ReasonProviderQuotaExhausted Reason = "provider_quota_exhausted"
	ReasonModelQuotaExhausted    Reason = "model_quota_exhausted"
	ReasonHeadroomEnabled        Reason = "headroom_enabled"
	ReasonHeadroomBypassed       Reason = "headroom_bypassed"
	ReasonCavemanEnabled         Reason = "caveman_enabled"
	ReasonCavemanFallback        Reason = "caveman_fallback"
	ReasonKnowledgeReady         Reason = "knowledge_ready"
	ReasonKnowledgeDegraded      Reason = "knowledge_degraded"
	ReasonPolicyAllowed          Reason = "policy_allowed"
	ReasonPolicyDenied           Reason = "policy_denied"
	ReasonApprovalRequired       Reason = "approval_required"
	ReasonInvalidMetadata        Reason = "invalid_metadata"
	ReasonUnresolvedConflict     Reason = "unresolved_conflict"
	ReasonIntegrityVerified      Reason = "integrity_verified"
	ReasonIntegrityMismatch      Reason = "integrity_mismatch"
	ReasonImmutableRevision      Reason = "immutable_revision"
	ReasonValidationFailed       Reason = "validation_failed"
	ReasonRollbackComplete       Reason = "rollback_complete"
	ReasonRedacted               Reason = "redacted"
	ReasonArtifactStored         Reason = "artifact_stored"
	ReasonArtifactRecovered      Reason = "artifact_recovered"
	ReasonContextBudgetApplied   Reason = "context_budget_applied"
	ReasonStoreUnavailable       Reason = "store_unavailable"
	ReasonAccessDenied           Reason = "access_denied"
	ReasonArtifactExpired        Reason = "artifact_expired"
	ReasonResultProjected        Reason = "result_projected"
)

// Event is deliberately an allowlist rather than a generic attributes map.
// It cannot represent prompts, responses, artifacts, headers, environments,
// credentials, cookies, provider auth records, or private knowledge content.
type Event struct {
	ObservedAt            time.Time        `json:"observed_at"`
	Category              Category         `json:"category"`
	Operation             Operation        `json:"operation"`
	State                 State            `json:"state"`
	SessionID             string           `json:"session_id,omitempty"`
	TaskID                string           `json:"task_id,omitempty"`
	WorkerID              string           `json:"worker_id,omitempty"`
	Provider              string           `json:"provider,omitempty"`
	Executor              string           `json:"executor,omitempty"`
	Component             core.ComponentID `json:"component,omitempty"`
	Capability            core.Capability  `json:"capability,omitempty"`
	DurationMilliseconds  int64            `json:"duration_ms,omitempty"`
	RoutingReason         Reason           `json:"routing_reason,omitempty"`
	FallbackReason        Reason           `json:"fallback_reason,omitempty"`
	WindowKind            string           `json:"window_kind,omitempty"`
	WindowDurationMinutes int64            `json:"window_duration_minutes,omitempty"`
	TelemetryState        string           `json:"telemetry_state,omitempty"`
	RemainingPercent      *float64         `json:"remaining_percent,omitempty"`
	ResetsAt              *time.Time       `json:"resets_at,omitempty"`
	SkillID               string           `json:"skill_id,omitempty"`
	ArtifactID            string           `json:"artifact_id,omitempty"`
	Revision              string           `json:"revision,omitempty"`
	RiskTier              string           `json:"risk_tier,omitempty"`
	PolicyDecision        string           `json:"policy_decision,omitempty"`
	SkillLifecycle        string           `json:"skill_lifecycle,omitempty"`
	TrustLevel            string           `json:"trust_level,omitempty"`
	SubjectID             string           `json:"subject_id,omitempty"`
	SubjectKind           string           `json:"subject_kind,omitempty"`
	ArtifactBytes         int64            `json:"artifact_bytes,omitempty"`
	FindingCount          int              `json:"finding_count,omitempty"`
	ReferenceCount        int              `json:"reference_count,omitempty"`
	Truncated             bool             `json:"truncated,omitempty"`
}

var operationCategories = map[Operation]Category{
	OperationExecutorSelect:          CategoryExecutor,
	OperationCapabilityResolve:       CategoryCapability,
	OperationFallbackRoute:           CategoryFallback,
	OperationMemoryBootstrap:         CategoryMemory,
	OperationContextBootstrap:        CategoryContext,
	OperationCompressionSelect:       CategoryCompression,
	OperationOrchestrationInitialize: CategoryOrchestration,
	OperationDAGPlan:                 CategoryDAG,
	OperationWorkerLifecycle:         CategoryWorker,
	OperationApprovalPolicy:          CategoryApproval,
	OperationQuotaProbe:              CategoryQuota,
	OperationQuotaRoute:              CategoryQuota,
	OperationRegistryDiscovery:       CategorySkillRegistry,
	OperationSkillCandidate:          CategorySkillIndex,
	OperationSkillQuarantine:         CategorySkillIndex,
	OperationSkillConflict:           CategorySkillIndex,
	OperationSkillGate:               CategorySkillRegistry,
	OperationSkillContentLoad:        CategorySkillRegistry,
	OperationPolicyDecision:          CategorySkillPolicy,
	OperationSupplyResolve:           CategorySupplyChain,
	OperationSupplyStage:             CategorySupplyChain,
	OperationSupplyPromote:           CategorySupplyChain,
	OperationSupplyRollback:          CategorySupplyChain,
	OperationArtifactStoreWrite:      CategoryWorkingContext,
	OperationArtifactStoreRead:       CategoryWorkingContext,
	OperationArtifactStoreRangeRead:  CategoryWorkingContext,
	OperationArtifactStoreGC:         CategoryWorkingContext,
	OperationArtifactStoreDenied:     CategoryWorkingContext,
	OperationWorkerResultProjected:   CategoryWorkingContext,
	OperationWorkingContextBudget:    CategoryWorkingContext,
	OperationWorkingContextDegraded:  CategoryWorkingContext,
}

// Normalize redacts the only free-text fields and validates every persisted
// dimension against a bounded allowlist.
func Normalize(value Event) (Event, error) {
	if value.ObservedAt.IsZero() {
		value.ObservedAt = time.Now().UTC()
	} else {
		value.ObservedAt = value.ObservedAt.UTC()
	}
	value.RoutingReason = normalizeReason(value.RoutingReason)
	value.FallbackReason = normalizeReason(value.FallbackReason)
	if value.ResetsAt != nil {
		reset := value.ResetsAt.UTC()
		value.ResetsAt = &reset
	}
	return value, value.Validate()
}

func (e Event) Validate() error {
	if e.ObservedAt.IsZero() {
		return errors.New("observability event requires a timestamp")
	}
	category, ok := operationCategories[e.Operation]
	if !ok || category != e.Category {
		return errors.New("invalid observability category or operation")
	}
	switch e.State {
	case StatePending, StateSelected, StateRunning, StateCompleted, StateDegraded, StateUnavailable, StateFailed, StateBlocked, StateAllowed, StateDenied, StateQuarantined, StateStaged, StatePromoted, StateRolledBack, StateApprovalRequired:
	default:
		return errors.New("invalid observability state")
	}
	if e.SessionID != "" && (len(e.SessionID) != 37 || !strings.HasPrefix(e.SessionID, "sess_")) {
		return errors.New("invalid observability session ID")
	}
	if !safeLabel(e.TaskID, 64) || !safeWorker(e.WorkerID) || !oneOf(e.Provider, "", "codex", "claude", "opencode", "caveman", "headroom", "direct") || !oneOf(e.Executor, "", "codex", "claude", "opencode") {
		return errors.New("invalid observability correlation metadata")
	}
	if e.Component != "" && !oneOf(string(e.Component), "codex", "claude", "memory", "context", "compression", "orchestration", "working_context", "skills", "tools") {
		return errors.New("invalid observability component")
	}
	if e.Capability != "" && !safeLabel(string(e.Capability), 96) {
		return errors.New("invalid observability capability")
	}
	if e.DurationMilliseconds < 0 || e.DurationMilliseconds > int64((24*time.Hour)/time.Millisecond) {
		return errors.New("invalid observability duration")
	}
	if e.WindowDurationMinutes < 0 || e.WindowDurationMinutes > 525600 || !safeLabel(e.WindowKind, 64) || !safeLabel(e.TelemetryState, 64) {
		return errors.New("invalid observability quota metadata")
	}
	if e.RemainingPercent != nil && (*e.RemainingPercent < 0 || *e.RemainingPercent > 100) {
		return errors.New("invalid observability quota percentage")
	}
	if !validReason(e.RoutingReason) || !validReason(e.FallbackReason) {
		return errors.New("unsafe observability reason")
	}
	if !safeCanonicalID(e.SkillID, 128) || !safeCanonicalID(e.ArtifactID, 128) || !safeCanonicalID(e.SubjectID, 128) || !oneOf(e.SubjectKind, "", "skill", "tool", "hook", "executor") || !validRevision(e.Revision) || !oneOf(e.RiskTier, "", "low", "moderate", "high", "critical") || !oneOf(e.PolicyDecision, "", "ALLOW", "DENY", "REQUIRE_APPROVAL") || !oneOf(e.SkillLifecycle, "", "staged", "active", "quarantined", "previous") || !safeCanonicalID(e.TrustLevel, 64) {
		return errors.New("invalid skill control-plane observability metadata")
	}
	if e.ArtifactBytes < 0 || e.ArtifactBytes > 1<<40 || e.FindingCount < 0 || e.FindingCount > 1024 || e.ReferenceCount < 0 || e.ReferenceCount > 1024 {
		return errors.New("invalid working-context observability metadata")
	}
	return nil
}

func normalizeReason(value Reason) Reason {
	if value != "" && platform.Redact(string(value)) != string(value) {
		return ReasonRedacted
	}
	return value
}

func safeLabel(value string, limit int) bool {
	if value == "" {
		return true
	}
	if len(value) > limit || strings.ContainsAny(value, "\x00\x1b\r\n\t /\\") {
		return false
	}
	return platform.Redact(value) == value
}

func safeWorker(value string) bool {
	return value == "" || len(value) == 39 && strings.HasPrefix(value, "worker_") && safeLabel(value, 39)
}

func safeCanonicalID(value string, limit int) bool {
	if value == "" {
		return true
	}
	if len(value) > limit || platform.Redact(value) != value {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("._:-", char)) {
			return false
		}
	}
	return true
}

func validReason(value Reason) bool {
	switch value {
	case "", ReasonDirect, ReasonPrimaryAvailable, ReasonCapabilityMatch, ReasonQuotaAvailable, ReasonQuotaExhausted, ReasonQuotaStale, ReasonTelemetryNotExposed, ReasonProbeFailed, ReasonAuthTransition, ReasonAlternateSelected, ReasonProviderUnavailable, ReasonProviderQuotaExhausted, ReasonModelQuotaExhausted, ReasonHeadroomEnabled, ReasonHeadroomBypassed, ReasonCavemanEnabled, ReasonCavemanFallback, ReasonKnowledgeReady, ReasonKnowledgeDegraded, ReasonPolicyAllowed, ReasonPolicyDenied, ReasonApprovalRequired, ReasonInvalidMetadata, ReasonUnresolvedConflict, ReasonIntegrityVerified, ReasonIntegrityMismatch, ReasonImmutableRevision, ReasonValidationFailed, ReasonRollbackComplete, ReasonRedacted, ReasonArtifactStored, ReasonArtifactRecovered, ReasonContextBudgetApplied, ReasonStoreUnavailable, ReasonAccessDenied, ReasonArtifactExpired, ReasonResultProjected:
		return true
	default:
		return false
	}
}

func validRevision(value string) bool {
	if value == "" {
		return true
	}
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func (e Event) Summary() string {
	correlation := e.Executor
	if correlation == "" {
		correlation = e.Provider
	}
	if e.TaskID != "" {
		correlation = e.TaskID
	}
	if correlation == "" {
		correlation = string(e.Component)
	}
	if correlation == "" {
		correlation = e.SkillID
	}
	if correlation == "" {
		correlation = e.ArtifactID
	}
	if correlation == "" {
		correlation = e.SubjectID
	}
	if correlation == "" {
		return fmt.Sprintf("%s %s", e.Operation, e.State)
	}
	return fmt.Sprintf("%s %s %s", e.Operation, correlation, e.State)
}
