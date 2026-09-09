package opencodebridge

import "time"

// TurnAttempt is bounded metadata, independent of the frontend process exit
// and of whether the executor managed to create a native thread.
type TurnAttempt struct {
	ID                string          `json:"attempt_id"`
	SessionID         string          `json:"ivoai_session_id,omitempty"`
	Frontend          string          `json:"frontend"`
	FrontendSessionID string          `json:"frontend_session_id"`
	RequestedExecutor string          `json:"requested_executor"`
	EffectiveExecutor string          `json:"effective_executor"`
	RequestedModel    string          `json:"requested_model,omitempty"`
	EffectiveModel    string          `json:"effective_model,omitempty"`
	RequestedEffort   string          `json:"requested_reasoning,omitempty"`
	EffectiveEffort   string          `json:"effective_reasoning,omitempty"`
	StartedAt         time.Time       `json:"started_at"`
	EndedAt           *time.Time      `json:"ended_at,omitempty"`
	State             string          `json:"turn_state"`
	ExitCode          *int            `json:"executor_exit_code,omitempty"`
	Failure           string          `json:"failure_class,omitempty"`
	FinalResponse     bool            `json:"final_response_present"`
	NativeSessionID   string          `json:"native_session_id,omitempty"`
	Trace             *ExecutionTrace `json:"trace,omitempty"`
}

func executorErrorMessage(class string) string {
	if class == "non_git_repository_gate" {
		return "Codex could not start because the current directory is outside a Git repository."
	}
	return "IVOAI executor failed (" + class + "); partial output was not accepted"
}
