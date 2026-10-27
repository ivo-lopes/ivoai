package core

import (
	"context"
	"errors"
	"fmt"
)

// ExecutorRequest is transient input to an official executor. It carries no
// transport, scheduler or authentication implementation; Prompt is untrusted
// task data and must not be persisted as operational metadata.
type ExecutorRequest struct {
	Executor          string
	Model             string
	Effort            string
	SelectionMode     string
	CatalogRevision   string
	Prompt            string
	FrontendSessionID string
	ExecutorSessionID string
}

type ExecutorResult struct {
	AuthReference       string
	Trace               *ExecutionTrace
	ExecutorSessionID   string
	CompressionUsed     bool
	CompressionProvider string
	SelectionMode       string
	RequestedModel      string
	Model               string
	Effort              string
	CatalogRevision     string
	ConfigurationSource string
}

type ExecutorRunner interface {
	Run(context.Context, ExecutorRequest, func(string) error) (ExecutorResult, error)
}

// ExecutorFailure is deliberately metadata-only. It gives the bridge and
// diagnostics a stable reason without exposing stderr, prompts, credentials,
// or tool results.
type ExecutorFailure struct {
	Class    string
	ExitCode int
}

func (e *ExecutorFailure) Error() string {
	if e.ExitCode >= 0 {
		return fmt.Sprintf("%s (exit %d)", e.Class, e.ExitCode)
	}
	return e.Class
}

func FailureClass(err error) string {
	var value *ExecutorFailure
	if errors.As(err, &value) && value.Class != "" {
		return value.Class
	}
	return "executor_failure"
}

// ExecutionTrace contains only bounded operational metadata. Prompts, tool
// arguments/results, authentication tokens and arbitrary stderr never enter it.
type ExecutionTrace struct {
	Usage               *ReportedTokenUsage `json:"usage,omitempty"`
	UsageStatus         string              `json:"usage_status,omitempty"`
	ConfiguredMCP       []string            `json:"configured_mcp,omitempty"`
	Executor            string              `json:"executor"`
	Executable          string              `json:"resolved_executable"`
	Version             string              `json:"executor_version"`
	Directory           string              `json:"working_directory"`
	PID                 int                 `json:"child_pid"`
	ExitCode            int                 `json:"exit_code"`
	Signal              string              `json:"signal,omitempty"`
	Events              int                 `json:"stdout_events"`
	LastEvent           string              `json:"last_valid_event"`
	Completion          bool                `json:"completion_event"`
	FinalResponse       bool                `json:"final_response_present"`
	OutputBytes         int                 `json:"partial_output_bytes"`
	StderrBytes         int                 `json:"stderr_bytes"`
	Failure             string              `json:"failure_class,omitempty"`
	RequestedModel      string              `json:"requested_model,omitempty"`
	RequestedEffort     string              `json:"requested_reasoning,omitempty"`
	EffectiveModel      string              `json:"effective_model,omitempty"`
	EffectiveEffort     string              `json:"effective_reasoning,omitempty"`
	ConfigurationSource string              `json:"configuration_source,omitempty"`
	Tools               []ToolTrace         `json:"mcp_operations,omitempty"`
}

// ReportedTokenUsage retains a bounded official-client report, not an estimate,
// price, quota measurement or independent attestation of a provider/backend.
// CachedInputTokens is a subset of InputTokens; nil means it was not reported.
// A CLI invocation may contain multiple model calls. Never sum this report with
// partial/cumulative events or treat it as usage for the whole IVOAI session.
type ReportedTokenUsage struct {
	InputTokens       int64  `json:"input_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
	CachedInputTokens *int64 `json:"cached_input_tokens,omitempty"`
	Basis             string `json:"basis"`
	Source            string `json:"source"`
	Scope             string `json:"scope"`
}
type ToolTrace struct {
	Server       string   `json:"server"`
	Name         string   `json:"name"`
	Status       string   `json:"status"`
	Failure      string   `json:"failure_class,omitempty"`
	ContentTypes []string `json:"content_types,omitempty"`
}
