package opencodebridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/ivo-lopes/ivoai/internal/codexresolver"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/routing"
)

type ExecutorSpec struct {
	ObserveProgress      func(ExecutionTrace)
	Version              string
	ObserveConfiguration bool
	SHA256               string
	Path                 string
	Args                 []string
	Env                  []string
	Dir                  string
	Disabled             bool
	Compression          core.CompressionProvider
	CompressionEnabled   bool
	RuntimeDir           string
}

type CLIRunner struct {
	Codex  ExecutorSpec
	Claude ExecutorSpec
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

func failure(class string) error { return &ExecutorFailure{Class: class, ExitCode: -1} }

func FailureClass(err error) string {
	var value *ExecutorFailure
	if errors.As(err, &value) && value.Class != "" {
		return value.Class
	}
	return "executor_failure"
}

func (r CLIRunner) Run(ctx context.Context, request ExecutorRequest, emit func(string) error) (outcome ExecutorResult, runError error) {
	spec := r.Codex
	if request.Executor == "claude" {
		spec = r.Claude
	}
	if spec.Disabled || spec.Path == "" {
		return ExecutorResult{}, failure("executor_unavailable")
	}
	if spec.SHA256 != "" {
		hash, err := codexresolver.Fingerprint(spec.Path)
		if err != nil || hash != spec.SHA256 {
			return ExecutorResult{}, failure("CODEX_SESSION_EXECUTABLE_CHANGED")
		}
	}
	trace := ExecutionTrace{Executor: request.Executor, Executable: spec.Path, Version: spec.Version, Directory: spec.Dir, ExitCode: -1, RequestedModel: request.Model, RequestedEffort: request.Effort}
	for _, arg := range spec.Args {
		if strings.HasPrefix(arg, "mcp_servers.") {
			key, _, _ := strings.Cut(arg, "=")
			if strings.HasSuffix(key, ".url") || strings.HasSuffix(key, ".command") {
				trace.ConfiguredMCP = append(trace.ConfiguredMCP, key)
			}
		}
	}
	defer func() {
		trace.EffectiveModel = outcome.Model
		trace.EffectiveEffort = outcome.Effort
		trace.ConfigurationSource = outcome.ConfigurationSource
		if runError != nil {
			trace.Failure = FailureClass(runError)
		}
		outcome.Trace = &trace
	}()
	originalEmit := emit
	emit = func(value string) error { trace.OutputBytes += len(value); return originalEmit(value) }
	args := append([]string(nil), spec.Args...)
	if request.Executor == "codex" {
		invocation := codexresolver.SplitExecArguments(spec.Args)
		args = invocation.Arguments(request.ExecutorSessionID, spec.Dir, appendSelectionArgs(nil, request))
	} else {
		args = append(args, "--print", "--verbose", "--output-format", "stream-json", "--include-partial-messages")
		args = appendSelectionArgs(args, request)
		if request.ExecutorSessionID != "" {
			args = append(args, "--resume", request.ExecutorSessionID)
		}
		args = append(args, "-")
	}
	if request.Executor == "codex" {
		args = codexresolver.ConfigurationArgs(args)
	}
	command := spec.Path
	environment := spec.Env
	compressionProvider := "direct"
	compressionUsed := false
	var lease core.CompressionLease
	if spec.CompressionEnabled && spec.Compression != nil {
		component := core.ComponentCodex
		if request.Executor == "claude" {
			component = core.ComponentClaude
		}
		prepared, prepareErr := spec.Compression.Prepare(ctx, core.CompressionRequest{Executor: component, DirectPath: spec.Path, Args: args, Environment: environment, RuntimeDir: spec.RuntimeDir, Fidelity: core.CompressionCompressible})
		if prepareErr == nil && prepared != nil {
			decision := prepared.Decision()
			if decision.Used {
				command, args, environment = decision.Command, decision.Args, decision.Environment
				lease = prepared
				compressionUsed = true
				compressionProvider = string(decision.Provider)
			} else {
				_ = prepared.Close(context.Background())
			}
		}
	}
	if lease != nil {
		defer lease.Close(context.Background())
	}
	cmd := exec.Command(command, args...)
	cmd.Dir = spec.Dir
	cmd.Env = environment
	cmd.SysProcAttr = executorProcessAttributes()
	cmd.Stdin = strings.NewReader(request.Prompt)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return ExecutorResult{}, failure("bridge_protocol_failure")
	}
	var stderr strings.Builder
	cmd.Stderr = &boundedWriter{writer: &stderr, remaining: 64 << 10}
	if err := cmd.Start(); err != nil {
		return ExecutorResult{}, failure("executor_start_failure")
	}
	trace.PID = cmd.Process.Pid
	processDone := make(chan struct{})
	var closeDone sync.Once
	go func() {
		select {
		case <-ctx.Done():
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			timer := time.NewTimer(time.Second)
			defer timer.Stop()
			select {
			case <-processDone:
			case <-timer.C:
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		case <-processDone:
		}
	}()
	result := ExecutorResult{
		ExecutorSessionID: request.ExecutorSessionID, CompressionUsed: compressionUsed, CompressionProvider: compressionProvider,
		SelectionMode: request.SelectionMode, RequestedModel: request.Model, CatalogRevision: request.CatalogRevision,
	}
	finalResponsePresent := false
	structuredFailureClass := ""
	claudeTools := make(map[string]int)
	parseErr := ScanJSONLines(stdout, func(value map[string]any) error {
		if spec.ObserveProgress != nil {
			defer func() {
				progress := trace
				progress.Tools = append([]ToolTrace(nil), trace.Tools...)
				spec.ObserveProgress(progress)
			}()
		}
		trace.Events++
		trace.LastEvent = safeExecutorText(stringValue(value["type"]), 64)
		if sessionID, ok := value["session_id"].(string); ok && safeID(sessionID) {
			result.ExecutorSessionID = sessionID
		}
		if threadID, ok := value["thread_id"].(string); ok && safeID(threadID) {
			result.ExecutorSessionID = threadID
		}
		if request.Executor == "claude" && value["type"] == "system" && value["subtype"] == "init" {
			if model, ok := value["model"].(string); ok && safeDisplayText(model) {
				result.Model = model
			}
			if effort, ok := value["effort"].(string); ok {
				result.Effort = effort
			}
		}
		if request.Executor == "codex" {
			if value["type"] == "turn.completed" {
				trace.Completion = true
			}
			if value["type"] == "turn.failed" {
				raw, _ := json.Marshal(value["error"])
				structuredFailureClass = classifyFailure(string(raw))
			}
			if value["type"] != "item.completed" {
				return nil
			}
			item, _ := value["item"].(map[string]any)
			if item["type"] == "mcp_tool_call" && len(trace.Tools) < 128 {
				tool := ToolTrace{Server: safeExecutorText(stringValue(item["server"]), 80), Name: safeExecutorText(stringValue(item["tool"]), 128), Status: safeExecutorText(stringValue(item["status"]), 32)}
				if item["error"] != nil {
					raw, _ := json.Marshal(item["error"])
					tool.Failure = classifyFailure(string(raw))
					if tool.Failure == "executor_failure" {
						tool.Failure = "mcp_tool_failure"
					}
				}
				if result, ok := item["result"].(map[string]any); ok {
					if isError, _ := result["isError"].(bool); isError {
						tool.Failure = "mcp_tool_failure"
					}
					if blocks, ok := result["content"].([]any); ok {
						for _, raw := range blocks {
							if b, ok := raw.(map[string]any); ok && len(tool.ContentTypes) < 16 {
								tool.ContentTypes = append(tool.ContentTypes, safeExecutorText(stringValue(b["type"]), 32))
							}
						}
					}
				}
				trace.Tools = append(trace.Tools, tool)
			}
			if item["type"] == "agent_message" {
				if text, ok := item["text"].(string); ok {
					finalResponsePresent = finalResponsePresent || strings.TrimSpace(text) != ""
					return emit(safeExecutorText(text, 1<<20))
				}
			}
			if marker := codexActivityMarker(item); marker != "" {
				return emit(marker)
			}
			return nil
		}
		if value["type"] == "stream_event" {
			event, _ := value["event"].(map[string]any)
			if event["type"] == "content_block_start" {
				block, _ := event["content_block"].(map[string]any)
				if block["type"] == "tool_use" {
					name := stringValue(block["name"])
					parts := strings.SplitN(name, "__", 3)
					if len(parts) == 3 && parts[0] == "mcp" && len(trace.Tools) < 128 {
						claudeTools[stringValue(block["id"])] = len(trace.Tools)
						trace.Tools = append(trace.Tools, ToolTrace{Server: safeExecutorText(parts[1], 80), Name: safeExecutorText(parts[2], 128), Status: "started"})
					}
					return emit(activityMarker("tool", stringValue(block["name"]), "started"))
				}
			}
			if event["type"] == "content_block_delta" {
				delta, _ := event["delta"].(map[string]any)
				if delta["type"] == "text_delta" {
					if text, ok := delta["text"].(string); ok {
						finalResponsePresent = finalResponsePresent || strings.TrimSpace(text) != ""
						return emit(safeExecutorText(text, 1<<20))
					}
				}
			}
		}
		if value["type"] == "user" {
			message, _ := value["message"].(map[string]any)
			if blocks, ok := message["content"].([]any); ok {
				for _, raw := range blocks {
					block, _ := raw.(map[string]any)
					if block["type"] == "tool_result" {
						if index, found := claudeTools[stringValue(block["tool_use_id"])]; found {
							tool := &trace.Tools[index]
							tool.Status = "completed"
							if isError, _ := block["is_error"].(bool); isError {
								raw, _ := json.Marshal(block["content"])
								tool.Failure = classifyFailure(string(raw))
								if tool.Failure == "executor_failure" {
									tool.Failure = "mcp_tool_failure"
								}
							}
						}
						if err := emit(activityMarker("tool", "", "completed")); err != nil {
							return err
						}
					}
				}
			}
		}
		if value["type"] == "result" {
			trace.Completion = true
			isError, _ := value["is_error"].(bool)
			if isError {
				structuredFailureClass = "executor_failure"
				if message, ok := value["result"].(string); ok {
					structuredFailureClass = classifyFailure(message)
				}
			}
		}
		return nil
	})
	// A malformed stream or disconnected consumer can stop scanning while the
	// child is still writing. Terminate the process group before Wait so a full
	// stdout pipe cannot deadlock the bridge.
	if parseErr != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	waitErr := cmd.Wait()
	trace.StderrBytes = stderr.Len()
	trace.FinalResponse = finalResponsePresent
	if cmd.ProcessState != nil {
		trace.ExitCode = cmd.ProcessState.ExitCode()
		if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			trace.Signal = status.Signal().String()
		}
	}
	closeDone.Do(func() { close(processDone) })
	if parseErr != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return result, failure("executor_cancelled")
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return result, failure("executor_timeout")
		}
		return result, failure("executor_stream_incomplete")
	}
	if waitErr != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return result, failure("executor_cancelled")
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return result, failure("executor_timeout")
		}
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			class := "executor_exit_nonzero"
			if structuredFailureClass != "" {
				class = structuredFailureClass
			} else if known := classifyFailure(stderr.String()); known != "executor_failure" {
				class = known
			}
			return result, &ExecutorFailure{Class: class, ExitCode: exitErr.ExitCode()}
		}
		return result, failure("executor_failure")
	}
	if result.ExecutorSessionID == "" {
		return result, failure("executor_stream_incomplete")
	}
	if structuredFailureClass != "" {
		return result, failure(structuredFailureClass)
	}
	if !finalResponsePresent || !trace.Completion {
		return result, failure("executor_stream_incomplete")
	}
	if spec.ObserveConfiguration && request.Executor == "codex" {
		model, effort, err := routing.CodexThreadConfiguration(ctx, spec.Path, result.ExecutorSessionID, spec.Env)
		if err == nil {
			result.Model = model
			result.Effort = effort
			result.ConfigurationSource = "official_thread_configuration"
		}
	}
	if request.Executor == "claude" && result.Model != "" {
		result.ConfigurationSource = "official_init_model/validated_cli_effort"
		// Claude's SDK init omits effort for local SDK consumers. A successful
		// invocation with an explicit, catalog-validated --effort is configuration
		// evidence, not server-side reasoning telemetry.
		if result.Effort == "" {
			result.Effort = request.Effort
		}
	}
	if request.SelectionMode == "explicit" && result.Model != "" && request.Model != "" && result.Model != request.Model {
		return result, failure("EXPLICIT_MODEL_MISMATCH")
	}
	if request.SelectionMode == "explicit" && result.Effort != "" && request.Effort != "" && result.Effort != request.Effort {
		return result, failure("EXPLICIT_REASONING_MISMATCH")
	}
	return result, nil
}

func indicatesAuthenticationFailure(value string) bool {
	value = strings.ToLower(value)
	for _, marker := range []string{"oauth session expired", "failed to authenticate", "authentication required", "not authenticated", "please log in", "please login", "unauthorized", "subscription access", "use an anthropic api key"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func appendSelectionArgs(args []string, request ExecutorRequest) []string {
	if request.Model != "" {
		args = append(args, "--model", request.Model)
	}
	if request.Effort == "" {
		return args
	}
	if request.Executor == "codex" {
		return append(args, "-c", "model_reasoning_effort="+strconv.Quote(request.Effort))
	}
	return append(args, "--effort", request.Effort)
}

func executorProcessAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}

func codexActivityMarker(item map[string]any) string {
	kind := stringValue(item["type"])
	status := stringValue(item["status"])
	if status == "" {
		status = "completed"
	}
	switch kind {
	case "command_execution":
		return activityMarker("command", "", status)
	case "file_change":
		return activityMarker("file change", "", status)
	case "mcp_tool_call":
		name := stringValue(item["tool"])
		if name == "" {
			name = stringValue(item["name"])
		}
		return activityMarker("MCP tool", name, status)
	case "web_search":
		return activityMarker("web search", "", status)
	}
	return ""
}

func activityMarker(kind, name, status string) string {
	kind = safeExecutorText(kind, 64)
	name = safeExecutorText(name, 80)
	status = safeExecutorText(status, 32)
	if name != "" {
		return fmt.Sprintf("\n[%s: %s · %s]\n", kind, name, status)
	}
	return fmt.Sprintf("\n[%s · %s]\n", kind, status)
}

func safeExecutorText(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 0x20 && r != 0x7f && !(r >= 0x80 && r <= 0x9f) && !(r >= 0x202a && r <= 0x202e) && !(r >= 0x2066 && r <= 0x2069) {
			return r
		}
		return -1
	}, value)
	if len(value) > limit {
		value = value[:limit]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	return value
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

type boundedWriter struct {
	writer    io.Writer
	remaining int
}

func (w *boundedWriter) Write(body []byte) (int, error) {
	original := len(body)
	if w.remaining > 0 {
		part := body
		if len(part) > w.remaining {
			part = part[:w.remaining]
		}
		_, _ = w.writer.Write(part)
		w.remaining -= len(part)
	}
	return original, nil
}

func nestedString(value map[string]any, keys ...string) string {
	current := any(value)
	for _, key := range keys {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = object[key]
	}
	text, _ := current.(string)
	return text
}

func decodeJSONLine(line []byte) map[string]any {
	var value map[string]any
	_ = json.Unmarshal(line, &value)
	return value
}
