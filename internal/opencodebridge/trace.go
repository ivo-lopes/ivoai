package opencodebridge

import "strings"

// ExecutionTrace contains only bounded operational metadata. Prompts, tool
// arguments/results, provider tokens and arbitrary stderr never enter it.
type ExecutionTrace struct {
	ConfiguredMCP       []string    `json:"configured_mcp,omitempty"`
	Executor            string      `json:"executor"`
	Executable          string      `json:"resolved_executable"`
	Version             string      `json:"executor_version"`
	Directory           string      `json:"working_directory"`
	PID                 int         `json:"child_pid"`
	ExitCode            int         `json:"exit_code"`
	Signal              string      `json:"signal,omitempty"`
	Events              int         `json:"stdout_events"`
	LastEvent           string      `json:"last_valid_event"`
	Completion          bool        `json:"completion_event"`
	FinalResponse       bool        `json:"final_response_present"`
	OutputBytes         int         `json:"partial_output_bytes"`
	StderrBytes         int         `json:"stderr_bytes"`
	Failure             string      `json:"failure_class,omitempty"`
	RequestedModel      string      `json:"requested_model,omitempty"`
	RequestedEffort     string      `json:"requested_reasoning,omitempty"`
	EffectiveModel      string      `json:"effective_model,omitempty"`
	EffectiveEffort     string      `json:"effective_reasoning,omitempty"`
	ConfigurationSource string      `json:"configuration_source,omitempty"`
	Tools               []ToolTrace `json:"mcp_operations,omitempty"`
}
type ToolTrace struct {
	Server       string   `json:"server"`
	Name         string   `json:"name"`
	Status       string   `json:"status"`
	Failure      string   `json:"failure_class,omitempty"`
	ContentTypes []string `json:"content_types,omitempty"`
}

func classifyFailure(message string) string {
	value := strings.ToLower(message)
	if indicatesAuthenticationFailure(value) {
		return "executor_auth_failure"
	}
	if strings.Contains(value, "unexpected response type") || strings.Contains(value, "failed to deserialize") {
		return "mcp_result_decode_failure"
	}
	if strings.Contains(value, "requires a newer version of codex") {
		return "executor_version_incompatible"
	}
	if strings.Contains(value, "model") && (strings.Contains(value, "not available") || strings.Contains(value, "not supported") || strings.Contains(value, "does not exist")) {
		return "EXPLICIT_MODEL_UNAVAILABLE"
	}
	return "executor_failure"
}
