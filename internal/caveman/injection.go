package caveman

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/core"
)

func prepareExecutor(request core.CompressionRequest, endpoint string) (core.CompressionDecision, error) {
	environment := unsetEnvironment(cloneEnvironment(request.Environment), "CAVE_CAPTURE_DIR", "CAVEMAN_CAPTURE_DIR")
	decision := core.CompressionDecision{Command: request.DirectPath, Args: append([]string(nil), request.Args...), Environment: environment}
	switch request.Executor {
	case core.ComponentCodex:
		decision.Args = append(decision.Args,
			"-c", `model_provider="ivoai-caveman"`,
			"-c", `model_providers.ivoai-caveman.name="IvoAI Caveman"`,
			"-c", "model_providers.ivoai-caveman.base_url="+strconv.Quote(endpoint+"/chatgpt"),
			"-c", `model_providers.ivoai-caveman.wire_api="responses"`,
			"-c", "model_providers.ivoai-caveman.requires_openai_auth=true",
		)
	case core.ComponentClaude:
		// Do not set ANTHROPIC_AUTH_TOKEN. Claude Code keeps ownership of its
		// subscription OAuth/API-key mechanism and sends it through the process-
		// local base URL just as it would to the first-party endpoint.
		decision.Environment = setEnvironment(decision.Environment, "ANTHROPIC_BASE_URL", endpoint+"/w/claude")
	default:
		return core.CompressionDecision{}, fmt.Errorf("unsupported executor %q", request.Executor)
	}
	return decision, nil
}

func cloneEnvironment(environment []string) []string {
	if environment == nil {
		return append([]string(nil), os.Environ()...)
	}
	return append([]string(nil), environment...)
}

func environmentValue(environment []string, key string) string {
	prefix := key + "="
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func setEnvironment(environment []string, key, value string) []string {
	prefix := key + "="
	result := append([]string(nil), environment...)
	for index, entry := range result {
		if strings.HasPrefix(entry, prefix) {
			result[index] = prefix + value
			return result
		}
	}
	return append(result, prefix+value)
}

func unsetEnvironment(environment []string, keys ...string) []string {
	blocked := map[string]bool{}
	for _, key := range keys {
		blocked[key] = true
	}
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		if !blocked[key] {
			result = append(result, entry)
		}
	}
	return result
}
