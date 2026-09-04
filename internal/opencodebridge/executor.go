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

	"github.com/ivo-lopes/ivoai/internal/core"
)

type ExecutorSpec struct {
	Path               string
	Args               []string
	Env                []string
	Dir                string
	Disabled           bool
	Compression        core.CompressionProvider
	CompressionEnabled bool
	RuntimeDir         string
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

func (r CLIRunner) Run(ctx context.Context, request ExecutorRequest, emit func(string) error) (ExecutorResult, error) {
	spec := r.Codex
	if request.Executor == "claude" {
		spec = r.Claude
	}
	if spec.Disabled || spec.Path == "" {
		return ExecutorResult{}, failure("executor_unavailable")
	}
	args := append([]string(nil), spec.Args...)
	if request.Executor == "codex" {
		if request.ExecutorSessionID == "" {
			args = append(args, "exec", "--json", "--color", "never")
			args = appendSelectionArgs(args, request)
			args = append(args, "-C", spec.Dir, "-")
		} else {
			args = append(args, "exec", "resume", "--json")
			args = appendSelectionArgs(args, request)
			args = append(args, request.ExecutorSessionID, "-")
		}
	} else {
		args = append(args, "--print", "--verbose", "--output-format", "stream-json", "--include-partial-messages")
		args = appendSelectionArgs(args, request)
		if request.ExecutorSessionID != "" {
			args = append(args, "--resume", request.ExecutorSessionID)
		}
		args = append(args, "-")
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
		SelectionMode: request.SelectionMode, RequestedModel: request.Model, Model: request.Model, Effort: request.Effort, CatalogRevision: request.CatalogRevision,
	}
	finalResponsePresent := false
	structuredFailureClass := ""
	parseErr := ScanJSONLines(stdout, func(value map[string]any) error {
		if sessionID, ok := value["session_id"].(string); ok && safeID(sessionID) {
			result.ExecutorSessionID = sessionID
		}
		if threadID, ok := value["thread_id"].(string); ok && safeID(threadID) {
			result.ExecutorSessionID = threadID
		}
		if request.Executor == "codex" {
			if value["type"] != "item.completed" {
				return nil
			}
			item, _ := value["item"].(map[string]any)
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
						if err := emit(activityMarker("tool", "", "completed")); err != nil {
							return err
						}
					}
				}
			}
		}
		if value["type"] == "result" {
			isError, _ := value["is_error"].(bool)
			if isError {
				structuredFailureClass = "executor_failure"
				if message, ok := value["result"].(string); ok && indicatesAuthenticationFailure(message) {
					structuredFailureClass = "executor_auth_failure"
				}
			}
		}
		return nil
	})
	waitErr := cmd.Wait()
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
			} else if indicatesAuthenticationFailure(stderr.String()) {
				class = "executor_auth_failure"
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
	if !finalResponsePresent {
		return result, failure("executor_stream_incomplete")
	}
	return result, nil
}

func indicatesAuthenticationFailure(value string) bool {
	value = strings.ToLower(value)
	for _, marker := range []string{"authentication required", "not authenticated", "please log in", "please login", "unauthorized", "subscription access", "use an anthropic api key"} {
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
