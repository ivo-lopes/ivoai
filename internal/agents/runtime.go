package agents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ivo-lopes/ivoai/internal/headroom"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"golang.org/x/term"
)

type Runtime struct {
	Runner       platform.Runner
	In           io.Reader
	Out, Err     io.Writer
	AgentPath    string
	HeadroomPath string
	Environment  []string
}

type Observation struct {
	PID          int
	HeadroomUsed bool
}

func (r Runtime) Launch(ctx context.Context, agent string, args []string, headroomEnabled bool) error {
	return r.LaunchObserved(ctx, agent, args, headroomEnabled, nil)
}

func (r Runtime) LaunchObserved(ctx context.Context, agent string, args []string, headroomEnabled bool, observe func(Observation)) error {
	if agent != "codex" && agent != "claude" {
		return fmt.Errorf("unsupported agent %q", agent)
	}
	direct := r.AgentPath
	var err error
	if direct == "" {
		direct, err = r.Runner.LookPath(agent)
	}
	if err != nil || direct == "" {
		return fmt.Errorf("%s is not installed; run ivoai setup", agent)
	}
	command, commandArgs := direct, args
	environment := r.Environment
	if environment == nil {
		environment = os.Environ()
	}
	if agent == "claude" {
		environment = setEnvironment(environment, "DISABLE_AUTOUPDATER", "1")
	}
	wrappedUsed := false
	if headroomEnabled {
		status := (headroom.Manager{Runner: r.Runner, Binary: r.HeadroomPath}).Inspect(ctx, true)
		compatible := status.CodexCompatible
		if agent == "claude" {
			compatible = status.ClaudeCompatible
		}
		if status.Healthy && compatible {
			wrapped := r.HeadroomPath
			if wrapped == "" {
				wrapped, _ = r.Runner.LookPath("headroom")
			}
			if wrapped != "" {
				command = wrapped
				wrappedUsed = true
				commandArgs = append([]string{"wrap", agent, "--"}, args...)
				// Managed agents intentionally live outside the user's PATH so ivoai
				// cannot shadow or overwrite pre-existing third-party installations.
				// Headroom resolves the selected agent by name, so expose only the
				// managed agent directory to this child process.
				environment = prependPathTo(environment, filepath.Dir(direct))
			}
		}
	}
	err = runInteractive(ctx, command, commandArgs, environment, r.In, r.Out, r.Err, func(pid int) {
		if observe != nil {
			observe(Observation{PID: pid, HeadroomUsed: wrappedUsed})
		}
	})
	var startErr *StartError
	if wrappedUsed && errors.As(err, &startErr) {
		if r.Err != nil {
			fmt.Fprintf(r.Err, "warning: Headroom could not start; launching %s directly: %v\n", agent, startErr)
		}
		// Preserve agent-specific protections (notably Claude's managed-update
		// guard) when the selected Headroom executable disappears between the
		// successful preflight and process start.
		return runInteractive(ctx, direct, args, environment, r.In, r.Out, r.Err, func(pid int) {
			if observe != nil {
				observe(Observation{PID: pid, HeadroomUsed: false})
			}
		})
	}
	return err
}

func runInteractive(ctx context.Context, command string, args, environment []string, in io.Reader, out, errOut io.Writer, observe func(int)) error {
	if terminal, ok := in.(*os.File); ok && term.IsTerminal(int(terminal.Fd())) {
		if state, stateErr := term.GetState(int(terminal.Fd())); stateErr == nil {
			defer func() { _ = term.Restore(int(terminal.Fd()), state) }()
		}
	}
	// Interactive clients inherit ivoai's foreground process group. A child in
	// a separate group cannot read the controlling terminal until terminal
	// ownership is explicitly transferred, and is suspended with SIGTTIN. The
	// shared foreground group also lets terminal and shell job-control signals
	// such as SIGINT, SIGTSTP, SIGCONT and SIGWINCH reach the complete stack.
	// We intentionally use exec.Command instead of CommandContext so cancellation
	// gets a bounded SIGTERM grace period before SIGKILL.
	cmd := exec.Command(command, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, errOut
	if environment != nil {
		cmd.Env = environment
	}
	if err := cmd.Start(); err != nil {
		return &StartError{Err: err}
	}
	if observe != nil {
		observe(cmd.Process.Pid)
	}
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	for {
		select {
		case sig := <-signals:
			if s, ok := sig.(syscall.Signal); ok {
				// Ctrl+C has already reached the child through the foreground group;
				// do not turn one keypress into two interrupts. TERM and HUP may be
				// addressed only to the supervisor, so forward those explicitly.
				if s == syscall.SIGINT {
					continue
				}
				_ = cmd.Process.Signal(s)
			}
		case waitErr := <-done:
			if waitErr == nil {
				return nil
			}
			var exitErr *exec.ExitError
			if errors.As(waitErr, &exitErr) {
				return &ExitError{Code: exitErr.ExitCode(), Err: waitErr}
			}
			return waitErr
		case <-ctx.Done():
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-done:
				return ctx.Err()
			case <-time.After(2 * time.Second):
				_ = cmd.Process.Kill()
				<-done
				return ctx.Err()
			}
		}
	}
}

func prependPath(directory string) []string {
	return prependPathTo(os.Environ(), directory)
}

func prependPathTo(environment []string, directory string) []string {
	if environment == nil {
		environment = os.Environ()
	}
	for idx, entry := range environment {
		if strings.HasPrefix(entry, "PATH=") {
			environment[idx] = "PATH=" + directory + string(os.PathListSeparator) + strings.TrimPrefix(entry, "PATH=")
			return environment
		}
	}
	return append(environment, "PATH="+directory)
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

type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return fmt.Sprintf("agent exited with status %d", e.Code) }
func (e *ExitError) Unwrap() error { return e.Err }

type StartError struct{ Err error }

func (e *StartError) Error() string { return fmt.Sprintf("start agent: %v", e.Err) }
func (e *StartError) Unwrap() error { return e.Err }
