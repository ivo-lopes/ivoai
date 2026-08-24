package agents

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"golang.org/x/sys/unix"
)

func TestInteractiveChildCanReadFromForegroundPTY(t *testing.T) {
	if os.Getenv("IVOAI_PTY_HELPER") == "1" {
		fake := os.Getenv("IVOAI_PTY_FAKE")
		err := runInteractive(context.Background(), fake, nil, nil, os.Stdin, os.Stdout, os.Stderr, nil)
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(125)
		}
		os.Exit(0)
	}

	root := t.TempDir()
	fake := writeExecutable(t, root, "fake-agent", `#!/bin/sh
printf 'groups child=%s foreground=%s\n' "$(ps -o pgid= -p $$ | tr -d ' ')" "$(ps -o tpgid= -p $$ | tr -d ' ')"
IFS= read -r line
printf 'received:%s\n' "$line"
exit 23
`)
	assertPTYConversation(t, "TestInteractiveChildCanReadFromForegroundPTY", "IVOAI_PTY_HELPER=1", "IVOAI_PTY_FAKE="+fake)
}

func TestHeadroomWrappedClaudeCanReadFromForegroundPTY(t *testing.T) {
	if os.Getenv("IVOAI_PTY_HEADROOM_HELPER") == "1" {
		runtime := Runtime{Runner: platform.ExecRunner{}, In: os.Stdin, Out: os.Stdout, Err: os.Stderr, AgentPath: os.Getenv("IVOAI_PTY_FAKE"), HeadroomPath: os.Getenv("IVOAI_PTY_HEADROOM")}
		err := runtime.Launch(context.Background(), "claude", nil, true)
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(125)
		}
		os.Exit(0)
	}
	root := t.TempDir()
	fake := writeExecutable(t, root, "claude", `#!/bin/sh
printf 'groups child=%s foreground=%s\n' "$(ps -o pgid= -p $$ | tr -d ' ')" "$(ps -o tpgid= -p $$ | tr -d ' ')"
IFS= read -r line
printf 'received:%s\n' "$line"
exit 23
`)
	headroom := writeExecutable(t, root, "headroom", `#!/bin/sh
if [ "$1" = "--version" ]; then echo "headroom 0.36.0"; exit 0; fi
if [ "$1" = "wrap" ] && [ "$3" = "--help" ]; then exit 0; fi
if [ "$1" = "wrap" ]; then agent="$2"; shift 3; exec "$agent" "$@"; fi
exit 2
`)
	assertPTYConversation(t, "TestHeadroomWrappedClaudeCanReadFromForegroundPTY", "IVOAI_PTY_HEADROOM_HELPER=1", "IVOAI_PTY_FAKE="+fake, "IVOAI_PTY_HEADROOM="+headroom)
}

func TestInteractiveRuntimeRestoresCanonicalEchoModes(t *testing.T) {
	if os.Getenv("IVOAI_PTY_RESTORE_HELPER") == "1" {
		err := runInteractive(context.Background(), os.Getenv("IVOAI_PTY_FAKE"), nil, nil, os.Stdin, os.Stdout, os.Stderr, nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(125)
		}
		state, stateErr := unix.IoctlGetTermios(int(os.Stdin.Fd()), unix.TCGETS)
		if stateErr != nil || state.Lflag&unix.ECHO == 0 || state.Lflag&unix.ICANON == 0 {
			fmt.Fprintln(os.Stdout, "terminal-restored:false")
			os.Exit(1)
		}
		fmt.Fprintln(os.Stdout, "terminal-restored:true")
		os.Exit(0)
	}
	root := t.TempDir()
	fake := writeExecutable(t, root, "fake-agent", "#!/bin/sh\nstty -echo -icanon\nexit 0\n")
	master, slave := openPTY(t)
	defer master.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestInteractiveRuntimeRestoresCanonicalEchoModes$")
	cmd.Env = append(os.Environ(), "IVOAI_PTY_RESTORE_HELPER=1", "IVOAI_PTY_FAKE="+fake)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = slave.Close()
	_ = master.SetReadDeadline(time.Now().Add(2 * time.Second))
	body, err := io.ReadAll(master)
	if err != nil && !errors.Is(err, syscall.EIO) {
		t.Fatal(err)
	}
	if waitErr := cmd.Wait(); waitErr != nil || !strings.Contains(strings.ReplaceAll(string(body), "\r", ""), "terminal-restored:true") {
		t.Fatalf("terminal restore output=%q wait=%v", body, waitErr)
	}
}

func assertPTYConversation(t *testing.T, testName string, environment ...string) {
	t.Helper()
	master, slave := openPTY(t)
	defer master.Close()
	defer slave.Close()

	cmd := exec.Command(os.Args[0], "-test.run=^"+testName+"$")
	cmd.Env = append(os.Environ(), environment...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = slave.Close()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	lines := make(chan string, 8)
	readErr := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(master)
		for scanner.Scan() {
			lines <- strings.TrimSuffix(scanner.Text(), "\r")
		}
		readErr <- scanner.Err()
	}()

	groupLine := waitPTYLine(t, lines, readErr, 2*time.Second)
	if !strings.HasPrefix(groupLine, "groups child=") {
		t.Fatalf("unexpected PTY output: %q", groupLine)
	}
	parts := strings.Fields(groupLine)
	if len(parts) != 3 || strings.TrimPrefix(parts[1], "child=") != strings.TrimPrefix(parts[2], "foreground=") {
		t.Fatalf("interactive child is not the foreground terminal group: %q", groupLine)
	}
	if _, err := io.WriteString(master, "hello-pty\n"); err != nil {
		t.Fatal(err)
	}
	if line := waitPTYLine(t, lines, readErr, 2*time.Second); line != "hello-pty" && line != "received:hello-pty" {
		t.Fatalf("unexpected PTY echo/response: %q", line)
	} else if line == "hello-pty" {
		if response := waitPTYLine(t, lines, readErr, 2*time.Second); response != "received:hello-pty" {
			t.Fatalf("fake agent did not read stdin: %q", response)
		}
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 23 {
			t.Fatalf("helper exit=%v; expected preserved fake exit 23", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("interactive wrapper did not return after the child exited")
	}
}

func openPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		master.Close()
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		master.Close()
		t.Fatal(err)
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		t.Fatal(err)
	}
	return master, slave
}

func waitPTYLine(t *testing.T, lines <-chan string, readErr <-chan error, timeout time.Duration) string {
	t.Helper()
	select {
	case line := <-lines:
		return line
	case err := <-readErr:
		// A PTY master reports EIO when the last slave closes. The scanner can
		// therefore enqueue the child's final line and the closing error before
		// this select runs; always prefer that already-observed output.
		select {
		case line := <-lines:
			return line
		default:
		}
		t.Fatalf("PTY closed before expected output: %v", err)
	case <-time.After(timeout):
		t.Fatal("timed out waiting for PTY output; child may be suspended by job control")
	}
	return ""
}

func TestLaunchFallsBackDirectWhenHeadroomUnavailable(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "direct-ran")
	agent := writeExecutable(t, root, "codex", "#!/bin/sh\n: > \"$1\"\n")
	runtime := Runtime{Runner: platform.ExecRunner{}, AgentPath: agent, HeadroomPath: filepath.Join(root, "missing-headroom")}
	if err := runtime.Launch(context.Background(), "codex", []string{marker}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("direct agent did not run after Headroom preflight failure")
	}
}

func TestDirectLaunchPreservesCWDArgumentsAndObservesPID(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "observation")
	agent := writeExecutable(t, root, "codex", "#!/bin/sh\nprintf '%s\\n%s\\n' \"$PWD\" \"$*\" > \"$1\"\n")
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	var observed Observation
	runtime := Runtime{Runner: platform.ExecRunner{}, AgentPath: agent}
	if err := runtime.LaunchObserved(context.Background(), "codex", []string{marker, "--model", "fixture-model"}, false, func(value Observation) { observed = value }); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), root+"\n"+marker+" --model fixture-model") {
		t.Fatalf("cwd/arguments not preserved: %q", body)
	}
	if observed.PID <= 0 || observed.HeadroomUsed {
		t.Fatalf("observation = %+v (pid %s)", observed, strconv.Itoa(observed.PID))
	}
}

func TestHeadroomCanResolveManagedAgentOutsideUserPath(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "wrapped-ran")
	agent := writeExecutable(t, root, "codex", "#!/bin/sh\n: > \"$1\"\n")
	headroom := writeExecutable(t, root, "headroom", `#!/bin/sh
if [ "$1" = "--version" ]; then echo "headroom 0.36.0"; exit 0; fi
if [ "$1" = "wrap" ] && [ "$3" = "--help" ]; then exit 0; fi
if [ "$1" = "wrap" ]; then agent="$2"; shift 3; exec "$agent" "$@"; fi
exit 2
`)
	t.Setenv("PATH", "/usr/bin:/bin")
	runtime := Runtime{Runner: platform.ExecRunner{}, AgentPath: agent, HeadroomPath: headroom}
	if err := runtime.Launch(context.Background(), "codex", []string{marker}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("Headroom did not resolve the managed agent through the scoped PATH")
	}
}

func TestStartedHeadroomExitIsNotMistakenForPreflightFailure(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "direct-must-not-run")
	agent := writeExecutable(t, root, "codex", "#!/bin/sh\n: > \"$1\"\n")
	headroom := writeExecutable(t, root, "headroom", `#!/bin/sh
if [ "$1" = "--version" ]; then echo "headroom 0.36.0"; exit 0; fi
if [ "$1" = "wrap" ] && [ "$3" = "--help" ]; then exit 0; fi
exit 42
`)
	runtime := Runtime{Runner: platform.ExecRunner{}, AgentPath: agent, HeadroomPath: headroom}
	err := runtime.Launch(context.Background(), "codex", []string{marker}, true)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 42 {
		t.Fatalf("wrapper exit = %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("direct agent was started after a wrapper process had already run")
	}
}

func TestClaudeStartFallbackPreservesManagedUpdateGuard(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "claude-environment")
	agent := writeExecutable(t, root, "claude", "#!/bin/sh\nprintf '%s' \"${DISABLE_AUTOUPDATER:-}\" > \"$1\"\n")
	count := filepath.Join(root, "headroom-count")
	headroom := writeExecutable(t, root, "headroom", `#!/bin/sh
count=0
[ -f "`+count+`" ] && count="$(cat "`+count+`")"
count=$((count + 1))
printf '%s' "$count" > "`+count+`"
if [ "$1" = "--version" ]; then echo "headroom 0.36.0"; exit 0; fi
if [ "$1" = "wrap" ] && [ "$3" = "--help" ]; then
  [ "$count" -eq 3 ] && rm -- "$0"
  exit 0
fi
exit 2
`)
	runtime := Runtime{Runner: platform.ExecRunner{}, AgentPath: agent, HeadroomPath: headroom}
	if err := runtime.Launch(context.Background(), "claude", []string{marker}, true); err != nil {
		t.Fatal(err)
	}
	value, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "1" {
		t.Fatalf("Claude fallback did not preserve DISABLE_AUTOUPDATER: %q", value)
	}
}

func TestInteractiveCancellationAllowsGracefulCleanupAndReapsChild(t *testing.T) {
	root := t.TempDir()
	started := filepath.Join(root, "started")
	cleaned := filepath.Join(root, "cleaned")
	agent := writeExecutable(t, root, "codex", `#!/bin/sh
trap 'printf cleaned > "$2"; exit 0' TERM HUP INT
printf started > "$1"
while :; do sleep 0.02; done
`)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	pid := make(chan int, 1)
	go func() {
		done <- runInteractive(ctx, agent, []string{started, cleaned}, nil, nil, io.Discard, io.Discard, func(value int) { pid <- value })
	}()
	childPID := <-pid
	waitForFile(t, started, time.Second)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled interactive child was not reaped")
	}
	waitForFile(t, cleaned, time.Second)
	if err := syscall.Kill(childPID, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("interactive child %d still exists: %v", childPID, err)
	}
}

func TestInteractivePTYForwardsSignalsAndResize(t *testing.T) {
	if os.Getenv("IVOAI_PTY_SIGNAL_HELPER") == "1" {
		err := runInteractive(context.Background(), os.Getenv("IVOAI_PTY_FAKE"), []string{os.Getenv("IVOAI_PTY_MARKER")}, nil, os.Stdin, os.Stdout, os.Stderr, nil)
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(125)
		}
		os.Exit(0)
	}

	root := t.TempDir()
	marker := filepath.Join(root, "signals")
	fake := writeExecutable(t, root, "fake-agent", `#!/bin/sh
marker=$1
trap 'printf winch > "$marker"' WINCH
trap 'printf term >> "$marker"; exit 42' TERM
printf 'signal-ready\n'
while :; do sleep 0.02; done
`)
	master, slave := openPTY(t)
	defer master.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestInteractivePTYForwardsSignalsAndResize$")
	cmd.Env = append(os.Environ(), "IVOAI_PTY_SIGNAL_HELPER=1", "IVOAI_PTY_FAKE="+fake, "IVOAI_PTY_MARKER="+marker)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = slave.Close()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	reader := bufio.NewReader(master)
	_ = master.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := reader.ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "signal-ready" {
		t.Fatalf("signal helper did not become ready: %q err=%v", line, err)
	}
	// SIGWINCH is delivered by the kernel to the complete foreground group.
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	waitForFileContent(t, marker, "winch", time.Second)
	// A supervisor or session stop may address only ivoai; it must forward TERM.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("expected preserved signal-handler exit code")
	} else if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 42 {
		t.Fatalf("signal helper exit=%v; expected 42", err)
	}
	waitForFileContent(t, marker, "winchterm", time.Second)
}

func TestInteractivePTYForwardsInterruptAndHangup(t *testing.T) {
	if os.Getenv("IVOAI_PTY_FORWARD_HELPER") == "1" {
		err := runInteractive(context.Background(), os.Getenv("IVOAI_PTY_FAKE"), nil, nil, os.Stdin, os.Stdout, os.Stderr, nil)
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		if err != nil {
			os.Exit(125)
		}
		os.Exit(0)
	}
	for _, test := range []struct {
		name string
		sig  syscall.Signal
		code int
	}{
		{"interrupt", syscall.SIGINT, 44},
		{"hangup", syscall.SIGHUP, 43},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			fake := writeExecutable(t, root, "fake-agent", `#!/bin/sh
trap 'exit 44' INT
trap 'exit 43' HUP
printf 'forward-ready\n'
while :; do sleep 0.02; done
`)
			master, slave := openPTY(t)
			defer master.Close()
			cmd := exec.Command(os.Args[0], "-test.run=^TestInteractivePTYForwardsInterruptAndHangup$")
			cmd.Env = append(os.Environ(), "IVOAI_PTY_FORWARD_HELPER=1", "IVOAI_PTY_FAKE="+fake)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			_ = slave.Close()
			t.Cleanup(func() {
				_ = cmd.Process.Kill()
				_, _ = cmd.Process.Wait()
			})
			_ = master.SetReadDeadline(time.Now().Add(2 * time.Second))
			line, err := bufio.NewReader(master).ReadString('\n')
			if err != nil || strings.TrimSpace(line) != "forward-ready" {
				t.Fatalf("signal helper did not become ready: %q err=%v", line, err)
			}
			var signalErr error
			if test.sig == syscall.SIGINT {
				signalErr = syscall.Kill(-cmd.Process.Pid, test.sig)
			} else {
				signalErr = cmd.Process.Signal(test.sig)
			}
			if signalErr != nil {
				t.Fatal(signalErr)
			}
			if err := cmd.Wait(); err == nil {
				t.Fatalf("expected exit %d", test.code)
			} else if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != test.code {
				t.Fatalf("signal helper exit=%v; expected %d", err, test.code)
			}
		})
	}
}

func TestHeadroomCancellationLeavesNoOwnedFakeProcesses(t *testing.T) {
	root := t.TempDir()
	proxyPIDPath := filepath.Join(root, "proxy.pid")
	agentPIDPath := filepath.Join(root, "agent.pid")
	ready := filepath.Join(root, "ready")
	cleaned := filepath.Join(root, "cleaned")
	agent := writeExecutable(t, root, "claude", `#!/bin/sh
printf '%s' "$$" > "$IVOAI_FAKE_AGENT_PID"
trap 'exit 0' TERM HUP INT
while :; do sleep 0.02; done
`)
	headroom := writeExecutable(t, root, "headroom", `#!/bin/sh
if [ "$1" = "--version" ]; then echo "headroom 0.36.0"; exit 0; fi
if [ "$1" = "wrap" ] && [ "$3" = "--help" ]; then exit 0; fi
if [ "$1" != "wrap" ]; then exit 2; fi
agent=$2
shift 3
sleep 30 & proxy=$!
printf '%s' "$proxy" > "$IVOAI_FAKE_PROXY_PID"
"$agent" "$@" & child=$!
cleanup() {
  kill "$child" "$proxy" 2>/dev/null || true
  wait "$child" 2>/dev/null || true
  wait "$proxy" 2>/dev/null || true
  printf cleaned > "$IVOAI_FAKE_CLEANED"
  exit 0
}
trap cleanup TERM HUP INT
printf ready > "$IVOAI_FAKE_READY"
wait "$child"
cleanup
`)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	runtime := Runtime{Runner: platform.ExecRunner{}, AgentPath: agent, HeadroomPath: headroom}
	t.Setenv("IVOAI_FAKE_PROXY_PID", proxyPIDPath)
	t.Setenv("IVOAI_FAKE_AGENT_PID", agentPIDPath)
	t.Setenv("IVOAI_FAKE_READY", ready)
	t.Setenv("IVOAI_FAKE_CLEANED", cleaned)
	go func() { done <- runtime.Launch(ctx, "claude", nil, true) }()
	waitForFile(t, proxyPIDPath, time.Second)
	waitForFile(t, agentPIDPath, time.Second)
	waitForFile(t, ready, time.Second)
	proxyPID := readPID(t, proxyPIDPath)
	agentPID := readPID(t, agentPIDPath)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Headroom cancellation error=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Headroom wrapper did not finish cleanup")
	}
	waitForFile(t, cleaned, time.Second)
	for name, pid := range map[string]int{"proxy": proxyPID, "agent": agentPID} {
		if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Fatalf("owned fake %s process %d still exists: %v", name, pid, err)
		}
	}
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func waitForFileContent(t *testing.T, path, expected string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(path)
		if err == nil && string(body) == expected {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	body, _ := os.ReadFile(path)
	t.Fatalf("timed out waiting for %s to contain %q; got %q", path, expected, body)
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	value, err := strconv.Atoi(string(body))
	if err != nil || value <= 0 {
		t.Fatalf("invalid PID marker %q: %v", body, err)
	}
	return value
}

func writeExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
