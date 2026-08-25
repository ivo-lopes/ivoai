// Package session owns non-sensitive operational metadata for ivoai sessions.
package session

import (
	"time"

	"github.com/ivo-lopes/ivoai/internal/quota"
)

type Mode string

const (
	StatePlanned     State = "planned"
	StatePrimary     State = "primary"
	StateQueued      State = "queued"
	ModeDirect       Mode  = "direct"
	ModeOrchestrated Mode  = "orchestrated"
	ModeAuto         Mode  = "auto"
)

type EffortSource string

type BootstrapMetadata struct {
	Performed      bool       `json:"performed"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
	MemoryStatus   string     `json:"memory_status"`
	ContextStatus  string     `json:"context_status"`
	ReferenceCount int        `json:"reference_count"`
	BriefHash      string     `json:"brief_hash,omitempty"`
}

type TaskMetadata struct {
	ID                    string    `json:"id"`
	Role                  string    `json:"role"`
	Dependencies          []string  `json:"dependencies,omitempty"`
	ParallelGroup         string    `json:"parallel_group,omitempty"`
	CapabilityScore       int       `json:"capability_score"`
	Tier                  string    `json:"tier"`
	Executor              string    `json:"executor,omitempty"`
	Model                 ModelInfo `json:"model"`
	Effort                string    `json:"effort,omitempty"`
	EffortSource          string    `json:"effort_source"`
	State                 State     `json:"state"`
	DurationMilliseconds  int64     `json:"duration_ms,omitempty"`
	HeadroomUsed          bool      `json:"headroom_used"`
	IntentionalRedundancy bool      `json:"intentional_redundancy,omitempty"`
	Escalations           int       `json:"escalations,omitempty"`
	EscalationReason      string    `json:"escalation_reason,omitempty"`
	ExecutionMode         string    `json:"execution_mode"`
	DelegationBenefit     int       `json:"delegation_benefit"`
	DelegationOverhead    int       `json:"delegation_overhead"`
	DelegationReason      string    `json:"delegation_reason"`
}

type State string

const (
	StateStarting  State = "starting"
	StateRunning   State = "running"
	StateDegraded  State = "degraded"
	StateStopping  State = "stopping"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
	StateBlocked   State = "blocked"
	StateWaiting   State = "waiting_for_quota"
)

type ModelSource string

const (
	ModelRuntimeVerified    ModelSource = "runtime_verified"
	ModelCapabilityRegistry ModelSource = "capability_registry"
	ModelArgument           ModelSource = "argument"
	ModelConfigured         ModelSource = "configured"
	ModelDefault            ModelSource = "default"
	ModelUnsupported        ModelSource = "unsupported"
	ModelUnknown            ModelSource = "unknown"
)

type ModelInfo struct {
	Name   string      `json:"name"`
	Source ModelSource `json:"source"`
}

type Worker struct {
	ID                string     `json:"id"`
	Role              string     `json:"role"`
	Executor          string     `json:"executor"`
	Model             ModelInfo  `json:"model"`
	PID               int        `json:"pid,omitempty"`
	ProcessStart      string     `json:"process_start,omitempty"`
	State             State      `json:"state"`
	StartedAt         time.Time  `json:"started_at"`
	EndedAt           *time.Time `json:"ended_at,omitempty"`
	ExitCode          *int       `json:"exit_code,omitempty"`
	RufloTaskID       string     `json:"ruflo_task_id,omitempty"`
	HeadroomUsed      bool       `json:"headroom_used"`
	RequestedExecutor string     `json:"requested_executor,omitempty"`
	FallbackReason    string     `json:"fallback_reason,omitempty"`
	TaskID            string     `json:"task_id,omitempty"`
	Tier              string     `json:"tier,omitempty"`
	CapabilityScore   int        `json:"capability_score,omitempty"`
	Effort            string     `json:"effort,omitempty"`
	EffortSource      string     `json:"effort_source,omitempty"`
}

type Session struct {
	SessionID            string                                 `json:"session_id"`
	StartedAt            time.Time                              `json:"started_at"`
	UpdatedAt            time.Time                              `json:"updated_at"`
	EndedAt              *time.Time                             `json:"ended_at,omitempty"`
	Mode                 Mode                                   `json:"mode"`
	PrimaryExecutor      string                                 `json:"primary_executor"`
	WorkingDirectory     string                                 `json:"working_directory"`
	PrimaryPID           int                                    `json:"primary_pid,omitempty"`
	PrimaryProcessStart  string                                 `json:"primary_process_start,omitempty"`
	PrimaryModel         ModelInfo                              `json:"primary_model"`
	HeadroomRequested    bool                                   `json:"headroom_requested"`
	HeadroomUsed         bool                                   `json:"headroom_used"`
	RufloEnabled         bool                                   `json:"ruflo_enabled"`
	RufloHealthy         bool                                   `json:"ruflo_healthy"`
	RufloSafeMode        bool                                   `json:"ruflo_safe_mode"`
	ProviderExecution    bool                                   `json:"provider_execution"`
	SwarmID              string                                 `json:"swarm_id,omitempty"`
	SwarmState           string                                 `json:"swarm_state,omitempty"`
	PrimaryRufloTaskID   string                                 `json:"primary_ruflo_task_id,omitempty"`
	Workers              []Worker                               `json:"workers"`
	MaxWorkers           int                                    `json:"max_workers"`
	ContextStatus        string                                 `json:"context_status"`
	MemoryStatus         string                                 `json:"memory_status"`
	ServerStatus         string                                 `json:"server_status"`
	ExitCode             *int                                   `json:"exit_code,omitempty"`
	State                State                                  `json:"state"`
	Auto                 bool                                   `json:"auto"`
	InitialPlanner       string                                 `json:"initial_planner,omitempty"`
	CurrentPrimary       string                                 `json:"current_primary,omitempty"`
	FailoverCount        int                                    `json:"failover_count,omitempty"`
	ConsecutiveFailovers int                                    `json:"consecutive_failovers,omitempty"`
	LastFailoverAt       *time.Time                             `json:"last_failover_at,omitempty"`
	LastFailoverReason   string                                 `json:"last_failover_reason,omitempty"`
	CurrentPhase         string                                 `json:"current_phase,omitempty"`
	CheckpointAvailable  bool                                   `json:"checkpoint_available"`
	CheckpointUpdatedAt  *time.Time                             `json:"checkpoint_updated_at,omitempty"`
	Quota                map[quota.Provider]quota.ProviderQuota `json:"quota,omitempty"`
	OptimizationStrategy string                                 `json:"optimization_strategy,omitempty"`
	KnowledgeBootstrap   BootstrapMetadata                      `json:"knowledge_bootstrap"`
	PlanID               string                                 `json:"plan_id,omitempty"`
	Tasks                []TaskMetadata                         `json:"tasks,omitempty"`
	EscalationCount      int                                    `json:"escalation_count,omitempty"`
}

func UnknownModel() ModelInfo { return ModelInfo{Name: "unknown", Source: ModelUnknown} }

func (s Session) Active() bool {
	return s.State == StateStarting || s.State == StateRunning || s.State == StateDegraded || s.State == StateStopping || s.State == StateWaiting
}
