// Package workers executes bounded, non-interactive work through the official
// Codex and Claude Code clients. Ruflo never performs inference in this path.
package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ivo-lopes/ivoai/internal/headroom"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/session"
)

const (
	MaxTaskBytes   = 32 << 10
	MaxResultBytes = 1 << 20
)

var providerEnvironment = map[string]struct{}{
	"ANTHROPIC_API_KEY": {}, "OPENAI_API_KEY": {}, "OPENROUTER_API_KEY": {},
	"ANTHROPIC_BASE_URL": {}, "OPENAI_BASE_URL": {},
	"GOOGLE_API_KEY": {}, "GOOGLE_GEMINI_API_KEY": {}, "GEMINI_API_KEY": {},
	"AZURE_OPENAI_API_KEY": {}, "GROQ_API_KEY": {}, "OLLAMA_API_KEY": {},
	"AWS_ACCESS_KEY_ID": {}, "AWS_SECRET_ACCESS_KEY": {}, "AWS_SESSION_TOKEN": {},
	"GOOGLE_APPLICATION_CREDENTIALS": {}, "CLAUDE_CODE_USE_BEDROCK": {},
	"CLAUDE_CODE_USE_VERTEX": {}, "CLAUDE_CODE_USE_FOUNDRY": {},
}

type Request struct {
	Executor  string
	Task      string
	Model     string
	Directory string
	Runtime   string
}

type Observation struct {
	PID          int
	ProcessStart string
	HeadroomUsed bool
}

type Result struct {
	Text         string
	ExitCode     int
	Model        session.ModelInfo
	HeadroomUsed bool
}

type Adapter struct {
	Runner          platform.Runner
	CodexPath       string
	ClaudePath      string
	HeadroomPath    string
	HeadroomEnabled bool
}

func (a Adapter) Capability(ctx context.Context, executor string) error {
	path, err := a.binary(executor)
	if err != nil {
		return err
	}
	args := []string{"exec", "--help"}
	if executor == "claude" {
		args = []string{"--help"}
	}
	_, err = a.Runner.Run(ctx, path, args, platform.RunOptions{Timeout: 15 * time.Second})
	if err != nil {
		return fmt.Errorf("%s worker mode is unavailable: %w", executor, err)
	}
	return nil
}

func (a Adapter) Run(ctx context.Context, request Request, observe func(Observation)) (Result, error) {
	if len(request.Task) == 0 || len(request.Task) > MaxTaskBytes || strings.ContainsRune(request.Task, '\x00') {
		return Result{}, errors.New("worker task must contain between 1 byte and 32 KiB")
	}
	if request.Directory == "" || !filepath.IsAbs(request.Directory) {
		return Result{}, errors.New("worker directory must be a trusted absolute path")
	}
	if info, err := os.Stat(request.Directory); err != nil || !info.IsDir() {
		return Result{}, errors.New("worker directory is unavailable")
	}
	if request.Runtime == "" || !filepath.IsAbs(request.Runtime) {
		return Result{}, errors.New("worker runtime directory must be absolute")
	}
	if request.Model != "" && session.ResolveModel("", request.Model, request.Executor, "").Source != session.ModelArgument {
		return Result{}, errors.New("invalid worker model")
	}
	if err := platform.EnsurePrivateDir(request.Runtime); err != nil {
		return Result{}, err
	}
	direct, err := a.binary(request.Executor)
	if err != nil {
		return Result{}, err
	}
	args, resultFile, err := workerArgs(request)
	if err != nil {
		return Result{}, err
	}
	if resultFile != "" {
		defer os.Remove(resultFile)
	}
	command, commandArgs := direct, args
	useHeadroom := false
	if a.HeadroomEnabled && a.HeadroomPath != "" {
		status := (headroom.Manager{Runner: a.Runner, Binary: a.HeadroomPath}).Inspect(ctx, true)
		compatible := status.CodexCompatible
		if request.Executor == "claude" {
			compatible = status.ClaudeCompatible
		}
		if status.Healthy && compatible {
			command = a.HeadroomPath
			commandArgs = append([]string{"wrap", request.Executor, "--"}, args...)
			useHeadroom = true
		}
	}
	result, startErr := run(ctx, command, commandArgs, request, direct, useHeadroom, observe)
	if startErr != nil && useHeadroom && isStartError(startErr) {
		return run(ctx, direct, args, request, direct, false, observe)
	}
	if startErr != nil {
		return result, startErr
	}
	if request.Executor == "codex" {
		body, readErr := readBounded(resultFile)
		if readErr != nil {
			return result, fmt.Errorf("read Codex worker result: %w", readErr)
		}
		result.Text = body
	} else {
		result.Text = claudeResult(result.Text)
	}
	result.Model = session.ResolveModel("", request.Model, request.Executor, "")
	return result, nil
}

func workerArgs(request Request) ([]string, string, error) {
	if request.Executor == "codex" {
		file := filepath.Join(request.Runtime, "codex-result-"+requestID()+".txt")
		args := []string{"exec", "--json", "--output-last-message", file}
		if request.Model != "" {
			args = append(args, "--model", request.Model)
		}
		return append(args, "-"), file, nil
	}
	if request.Executor == "claude" {
		args := []string{"--print", "--output-format", "json"}
		if request.Model != "" {
			args = append(args, "--model", request.Model)
		}
		return args, "", nil
	}
	return nil, "", errors.New("worker executor must be codex or claude")
}

func (a Adapter) binary(executor string) (string, error) {
	path, base := a.CodexPath, "codex"
	if executor == "claude" {
		path, base = a.ClaudePath, "claude"
	} else if executor != "codex" {
		return "", errors.New("worker executor must be codex or claude")
	}
	if path == "" || !filepath.IsAbs(path) || filepath.Base(path) != base {
		return "", fmt.Errorf("%s worker executable is not a trusted component path", executor)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("%s worker executable is unavailable", executor)
	}
	return path, nil
}

type limitedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	remaining := MaxResultBytes - b.Len()
	if remaining <= 0 {
		b.overflow = true
		return original, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
		b.overflow = true
	}
	_, _ = b.Buffer.Write(value)
	return original, nil
}

func run(ctx context.Context, command string, args []string, request Request, direct string, headroomUsed bool, observe func(Observation)) (Result, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = request.Directory
	cmd.Stdin = strings.NewReader(request.Task)
	cmd.Env = workerEnvironment(direct, request.Executor)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout, stderr limitedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("start worker: %w", err)
	}
	if observe != nil {
		observe(Observation{PID: cmd.Process.Pid, ProcessStart: session.ProcessStart(cmd.Process.Pid), HeadroomUsed: headroomUsed})
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		err = <-done
		if err == nil {
			err = ctx.Err()
		}
	}
	result := Result{Text: stdout.String(), HeadroomUsed: headroomUsed}
	if stdout.overflow || stderr.overflow {
		return result, errors.New("worker output exceeded the 1 MiB safety limit")
	}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, fmt.Errorf("worker exited with status %d: %s", result.ExitCode, safeDiagnostic(stderr.String()))
	}
	return result, err
}

func workerEnvironment(agentPath, executor string) []string {
	result := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		key, _, found := strings.Cut(entry, "=")
		if found && (key == "PATH" || key == "DISABLE_AUTOUPDATER") {
			continue
		}
		if _, prohibited := providerEnvironment[key]; found && prohibited {
			continue
		}
		result = append(result, entry)
	}
	result = append(result, "PATH="+filepath.Dir(agentPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	if executor == "claude" {
		result = append(result, "DISABLE_AUTOUPDATER=1")
	}
	return result
}

func readBounded(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, MaxResultBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > MaxResultBytes {
		return "", errors.New("worker result exceeded the 1 MiB safety limit")
	}
	return string(body), nil
}

func claudeResult(value string) string {
	var parsed struct {
		Result string `json:"result"`
	}
	if json.Unmarshal([]byte(value), &parsed) == nil && parsed.Result != "" {
		return parsed.Result
	}
	return value
}

func safeDiagnostic(value string) string {
	value = platform.Redact(strings.TrimSpace(value))
	if len(value) > 2048 {
		value = value[:2048] + "..."
	}
	return strings.ReplaceAll(value, "\x1b", "")
}

func isStartError(err error) bool { return strings.Contains(err.Error(), "start worker:") }

func requestID() string {
	id, err := session.NewID()
	if err != nil {
		return fmt.Sprintf("%d", os.Getpid())
	}
	return strings.TrimPrefix(id, "sess_")
}
