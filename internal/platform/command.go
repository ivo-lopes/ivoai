package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Runner executes programs using an argv vector. It never invokes a shell.
type Runner interface {
	LookPath(name string) (string, error)
	Run(ctx context.Context, command string, args []string, options RunOptions) (Result, error)
}

type RunOptions struct {
	Dir      string
	Env      []string
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	Timeout  time.Duration
	TTY      bool
	CleanEnv bool
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type ExecRunner struct{}

func (ExecRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (ExecRunner) Run(ctx context.Context, command string, args []string, o RunOptions) (Result, error) {
	if command == "" || strings.ContainsRune(command, '\x00') {
		return Result{}, errors.New("invalid empty command")
	}
	if o.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = o.Dir
	if o.CleanEnv {
		cmd.Env = append([]string(nil), o.Env...)
	} else if o.Env != nil {
		cmd.Env = mergedEnvironment(os.Environ(), o.Env)
	}
	cmd.Stdin = o.Stdin
	var stdout, stderr strings.Builder
	if o.Stdout != nil {
		cmd.Stdout = o.Stdout
	} else {
		cmd.Stdout = &stdout
	}
	if o.Stderr != nil {
		cmd.Stderr = o.Stderr
	} else {
		cmd.Stderr = &stderr
	}
	// Interactive children must remain in ivoai's foreground process group.
	// Moving them to a new group without also transferring terminal ownership
	// causes reads from stdin to be suspended by SIGTTIN. CommandContext still
	// terminates the direct child when the caller cancels the operation.
	err := cmd.Run()
	r := Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 0}
	if err == nil {
		return r, nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return r, fmt.Errorf("%s timed out: %w", command, ctx.Err())
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		r.ExitCode = exitErr.ExitCode()
		return r, fmt.Errorf("%s exited with status %d", command, r.ExitCode)
	}
	r.ExitCode = -1
	return r, fmt.Errorf("start %s: %w", command, err)
}

func mergedEnvironment(base, overrides []string) []string {
	keys := make(map[string]struct{}, len(overrides))
	for _, entry := range overrides {
		if idx := strings.IndexByte(entry, '='); idx > 0 {
			keys[entry[:idx]] = struct{}{}
		}
	}
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		idx := strings.IndexByte(entry, '=')
		if idx > 0 {
			if _, replace := keys[entry[:idx]]; replace {
				continue
			}
		}
		result = append(result, entry)
	}
	return append(result, overrides...)
}
