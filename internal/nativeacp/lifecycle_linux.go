//go:build linux

package nativeacp

import (
	"context"
	"github.com/ivo-lopes/ivoai/internal/ownedprocess"
	"os/exec"
	"syscall"
	"time"
)

type ownedSignals struct{ *ownedprocess.Group }

func waitOwned(_ *exec.Cmd, signals *ownedSignals) error { return signals.Wait() }

func supervise(ctx context.Context, command *exec.Cmd, signals *ownedSignals, done <-chan struct{}) {
	select {
	case <-done:
		return
	case <-ctx.Done():
	}
	_ = signals.Signal(syscall.SIGTERM)
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-done:
		return
	case <-timer.C:
		_ = signals.Signal(syscall.SIGKILL)
	}
}
