//go:build linux

// Package ownedprocess retires private process-group signals before reaping the
// group's leader. It grants no process ownership and never accepts a model PID.
package ownedprocess

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

type Group struct {
	command         *exec.Cmd
	mu              sync.Mutex
	retired, forced bool
}

func New(command *exec.Cmd) (*Group, error) {
	if command == nil || command.Process == nil || command.Process.Pid <= 0 || command.SysProcAttr == nil || !command.SysProcAttr.Setpgid || command.SysProcAttr.Pgid != 0 {
		return nil, errors.New("private process group required")
	}
	return &Group{command: command}, nil
}

func (g *Group) Signal(sig syscall.Signal) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.retired {
		return os.ErrProcessDone
	}
	g.forced = true
	return unix.Kill(-g.command.Process.Pid, unix.Signal(sig))
}

func (g *Group) Forced() bool { g.mu.Lock(); defer g.mu.Unlock(); return g.forced }

// Wait must be called after the caller drains the required stdout protocol.
// WNOWAIT reserves the leader while all signal paths retire and remaining
// descendants are cleaned up, so cancellation cannot target a recycled PGID.
func (g *Group) Wait() error {
	var info unix.Siginfo
	var observed error
	for {
		observed = unix.Waitid(unix.P_PID, g.command.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if !errors.Is(observed, unix.EINTR) {
			break
		}
	}
	g.mu.Lock()
	var cleanup error
	if observed == nil {
		cleanup = unix.Kill(-g.command.Process.Pid, unix.SIGKILL)
		if errors.Is(cleanup, unix.ESRCH) {
			cleanup = nil
		}
	}
	g.retired = true
	g.mu.Unlock()
	if err := g.command.Wait(); err != nil {
		return err
	}
	if observed != nil || cleanup != nil {
		return errors.New("owned process cleanup failed")
	}
	return nil
}
