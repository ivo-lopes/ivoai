//go:build linux

package codexfrontend

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestNativeCancellationAfterRetirementBeforeWaitJoinDoesNotSignal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/sh", "-c", "exit 0")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	signals := &ownedFrontendSignals{}
	var rawSignals atomic.Int32
	cancelCalled := make(chan struct{})
	command.Cancel = func() error {
		defer close(cancelCalled)
		return signals.signal(func() error {
			rawSignals.Add(1)
			return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		})
	}
	if err := command.Start(); err != nil {
		t.Fatal("native cancellation fixture did not start")
	}
	err := waitOwnedFrontend(command, func() {
		// WNOWAIT has proven leader exit. Force the context watcher into
		// custom Cancel before Cmd.Wait gets to reap and join that watcher.
		signals.retire()
		cancel()
		select {
		case <-cancelCalled:
		case <-time.After(time.Second):
			t.Error("context watcher did not reach retired cancellation")
		}
	})
	if err != nil || rawSignals.Load() != 0 || command.ProcessState == nil || !command.ProcessState.Success() {
		t.Fatal("late cancellation signalled a retired group or corrupted normal exit")
	}
}

func TestNativeSignalRetirementWaitsForLastConcurrentSignal(t *testing.T) {
	signals := &ownedFrontendSignals{}
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	signalled := make(chan error, 1)
	go func() {
		signalled <- signals.signal(func() error {
			calls.Add(1)
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("signal did not reach causal barrier")
	}
	retiring, retired := make(chan struct{}), make(chan struct{})
	go func() { close(retiring); signals.retire(); close(retired) }()
	<-retiring
	select {
	case <-retired:
		close(release)
		t.Fatal("retirement returned while raw signal was in flight")
	default:
	}
	close(release)
	select {
	case <-retired:
	case <-time.After(time.Second):
		t.Fatal("retirement did not join the last raw signal")
	}
	if err := <-signalled; err != nil {
		t.Fatal("admitted signal unexpectedly failed")
	}
	if err := signals.signal(func() error { calls.Add(1); return nil }); !errors.Is(err, os.ErrProcessDone) || calls.Load() != 1 {
		t.Fatal("retired cancellation invoked a raw signal")
	}
}
