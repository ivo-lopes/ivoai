//go:build linux

package codexfrontend

import (
	"errors"
	"os/exec"

	"golang.org/x/sys/unix"
)

// Keep the leader unreaped through the last process-group signal: its PID and
// process-group identity cannot be reused while WNOWAIT retains its exit state.
// The ordinary leader exits on stdin EOF; only remaining descendants are killed.
func waitOwnedFrontend(command *exec.Cmd, retireSignals func()) error {
	var info unix.Siginfo
	var observeErr error
	for {
		observeErr = unix.Waitid(unix.P_PID, command.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if !errors.Is(observeErr, unix.EINTR) {
			break
		}
	}
	var cleanupErr error
	if observeErr == nil {
		cleanupErr = unix.Kill(-command.Process.Pid, unix.SIGKILL)
		if errors.Is(cleanupErr, unix.ESRCH) {
			cleanupErr = nil
		}
	}
	// Cmd.Wait reaps before joining its context watcher. Retire the raw
	// process-group signal first, waiting for any in-flight Cancel to finish.
	// Never hold that guard while Wait joins the watcher: Cancel needs it too.
	retireSignals()
	// Wait joins exec's context watcher even when Waitid failed. Never signal
	// a possibly reused group after ECHILD or another failed ownership check.
	if err := command.Wait(); err != nil {
		return err
	}
	if observeErr != nil || cleanupErr != nil {
		return errors.New("CODEX_APP_SERVER_CLEANUP_FAILED")
	}
	return nil
}
