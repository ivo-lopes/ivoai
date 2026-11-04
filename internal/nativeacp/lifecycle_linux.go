//go:build linux

package nativeacp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// The leader is reserved with WNOWAIT until all raw group signals retire.
// This preserves the same no-PID-reuse invariant as the managed Codex frontend.
type ownedSignals struct {
	mu      sync.Mutex
	retired bool
	forced  bool
}

func (s *ownedSignals) signal(pid int, sig unix.Signal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retired {
		return os.ErrProcessDone
	}
	s.forced = true
	return unix.Kill(-pid, sig)
}

func waitOwned(command *exec.Cmd, signals *ownedSignals) error {
	var info unix.Siginfo
	var err error
	for {
		err = unix.Waitid(unix.P_PID, command.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	signals.mu.Lock()
	var cleanupErr error
	if err == nil {
		cleanupErr = unix.Kill(-command.Process.Pid, unix.SIGKILL)
		if errors.Is(cleanupErr, unix.ESRCH) {
			cleanupErr = nil
		}
	}
	signals.retired = true
	signals.mu.Unlock()
	waitErr := command.Wait()
	if waitErr != nil {
		return waitErr
	}
	if err != nil || cleanupErr != nil {
		return failed("ACP_PROCESS_CLEANUP_FAILED")
	}
	return nil
}

func supervise(ctx context.Context, command *exec.Cmd, signals *ownedSignals, done <-chan struct{}) {
	select {
	case <-done:
		return
	case <-ctx.Done():
	}
	_ = signals.signal(command.Process.Pid, unix.SIGTERM)
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-done:
		return
	case <-timer.C:
		_ = signals.signal(command.Process.Pid, unix.SIGKILL)
	}
}
