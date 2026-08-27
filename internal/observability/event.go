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
	CategoryExecutor      Category = "executor"
	CategoryCapability    Category = "capability"
	CategoryFallback      Category = "fallback"
	CategoryMemory        Category = "memory"
	CategoryContext       Category = "context"
	CategoryCompression   Category = "compression"
	CategoryOrchestration Category = "orchestration"
	CategoryDAG           Category = "dag"
	CategoryWorker        Category = "worker"
	CategoryApproval      Category = "approval"
	CategoryQuota         Category = "quota"
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
)

type State string

const (
	StatePending     State = "pending"
	StateSelected    State = "selected"
	StateRunning     State = "running"
	StateCompleted   State = "completed"
	StateDegraded    State = "degraded"
	StateUnavailable State = "unavailable"
	StateFailed      State = "failed"
	StateBlocked     State = "blocked"
	StateAllowed     State = "allowed"
	StateDenied      State = "denied"
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
	ReasonKnowledgeReady         Reason = "knowledge_ready"
	ReasonKnowledgeDegraded      Reason = "knowledge_degraded"
	ReasonPolicyAllowed          Reason = "policy_allowed"
	ReasonRedacted               Reason = "redacted"
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
	case StatePending, StateSelected, StateRunning, StateCompleted, StateDegraded, StateUnavailable, StateFailed, StateBlocked, StateAllowed, StateDenied:
	default:
		return errors.New("invalid observability state")
	}
	if e.SessionID != "" && (len(e.SessionID) != 37 || !strings.HasPrefix(e.SessionID, "sess_")) {
		return errors.New("invalid observability session ID")
	}
	if !safeLabel(e.TaskID, 64) || !safeWorker(e.WorkerID) || !oneOf(e.Provider, "", "codex", "claude") || !oneOf(e.Executor, "", "codex", "claude") {
		return errors.New("invalid observability correlation metadata")
	}
	if e.Component != "" && !oneOf(string(e.Component), "codex", "claude", "memory", "context", "compression", "orchestration", "skills", "tools") {
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

func validReason(value Reason) bool {
	switch value {
	case "", ReasonDirect, ReasonPrimaryAvailable, ReasonCapabilityMatch, ReasonQuotaAvailable, ReasonQuotaExhausted, ReasonQuotaStale, ReasonTelemetryNotExposed, ReasonProbeFailed, ReasonAuthTransition, ReasonAlternateSelected, ReasonProviderUnavailable, ReasonProviderQuotaExhausted, ReasonModelQuotaExhausted, ReasonHeadroomEnabled, ReasonHeadroomBypassed, ReasonKnowledgeReady, ReasonKnowledgeDegraded, ReasonPolicyAllowed, ReasonRedacted:
		return true
	default:
		return false
	}
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
		return fmt.Sprintf("%s %s", e.Operation, e.State)
	}
	return fmt.Sprintf("%s %s %s", e.Operation, correlation, e.State)
}
