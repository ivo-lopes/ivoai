// Package session owns non-sensitive operational metadata for ivoai sessions.
package session

import "time"

type Mode string

const (
	ModeDirect       Mode = "direct"
	ModeOrchestrated Mode = "orchestrated"
)

type State string

const (
	StateStarting  State = "starting"
	StateRunning   State = "running"
	StateDegraded  State = "degraded"
	StateStopping  State = "stopping"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
)

type ModelSource string

const (
	ModelRuntimeVerified ModelSource = "runtime_verified"
	ModelArgument        ModelSource = "argument"
	ModelConfigured      ModelSource = "configured"
	ModelUnknown         ModelSource = "unknown"
)

type ModelInfo struct {
	Name   string      `json:"name"`
	Source ModelSource `json:"source"`
}

type Worker struct {
	ID           string     `json:"id"`
	Role         string     `json:"role"`
	Executor     string     `json:"executor"`
	Model        ModelInfo  `json:"model"`
	PID          int        `json:"pid,omitempty"`
	ProcessStart string     `json:"process_start,omitempty"`
	State        State      `json:"state"`
	StartedAt    time.Time  `json:"started_at"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
	ExitCode     *int       `json:"exit_code,omitempty"`
	RufloTaskID  string     `json:"ruflo_task_id,omitempty"`
	HeadroomUsed bool       `json:"headroom_used"`
}

type Session struct {
	SessionID           string     `json:"session_id"`
	StartedAt           time.Time  `json:"started_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	EndedAt             *time.Time `json:"ended_at,omitempty"`
	Mode                Mode       `json:"mode"`
	PrimaryExecutor     string     `json:"primary_executor"`
	WorkingDirectory    string     `json:"working_directory"`
	PrimaryPID          int        `json:"primary_pid,omitempty"`
	PrimaryProcessStart string     `json:"primary_process_start,omitempty"`
	PrimaryModel        ModelInfo  `json:"primary_model"`
	HeadroomRequested   bool       `json:"headroom_requested"`
	HeadroomUsed        bool       `json:"headroom_used"`
	RufloEnabled        bool       `json:"ruflo_enabled"`
	RufloHealthy        bool       `json:"ruflo_healthy"`
	RufloSafeMode       bool       `json:"ruflo_safe_mode"`
	ProviderExecution   bool       `json:"provider_execution"`
	SwarmID             string     `json:"swarm_id,omitempty"`
	SwarmState          string     `json:"swarm_state,omitempty"`
	PrimaryRufloTaskID  string     `json:"primary_ruflo_task_id,omitempty"`
	Workers             []Worker   `json:"workers"`
	MaxWorkers          int        `json:"max_workers"`
	ContextStatus       string     `json:"context_status"`
	MemoryStatus        string     `json:"memory_status"`
	ServerStatus        string     `json:"server_status"`
	ExitCode            *int       `json:"exit_code,omitempty"`
	State               State      `json:"state"`
}

func UnknownModel() ModelInfo { return ModelInfo{Name: "unknown", Source: ModelUnknown} }

func (s Session) Active() bool {
	return s.State == StateStarting || s.State == StateRunning || s.State == StateDegraded || s.State == StateStopping
}
