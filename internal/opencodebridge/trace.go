package opencodebridge

import (
	"strings"

	"github.com/ivo-lopes/ivoai/internal/core"
)

// Source-compatible aliases keep existing JSON and trace consumers intact.
type ExecutionTrace = core.ExecutionTrace
type ToolTrace = core.ToolTrace

func classifyFailure(message string) string {
	value := strings.ToLower(message)
	if strings.Contains(value, "not inside a trusted directory") && strings.Contains(value, "--skip-git-repo-check was not specified") {
		return "non_git_repository_gate"
	}
	if strings.Contains(value, "sandbox") && (strings.Contains(value, "failed to") || strings.Contains(value, "operation not permitted")) {
		return "sandbox_failure"
	}
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
