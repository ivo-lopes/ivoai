//go:build linux

package app

import (
	"context"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// No callback/boolean comes from a model, discovery JSON or caller config.
func prefetchCommonLiveAdmission(ctx context.Context, scope prefetchDirectScope, policy prefetchSubscriptionHostPolicy) (*prefetchCommonAdmission, prefetchCommonObservation) {
	return prefetchCommonPrepare(ctx, scope, policy, prefetchCommonOfficialProbe, prefetchCommonInventoryOwned, prefetchCommonOfficialMatches)
}
func prefetchCommonOfficialMatches(scope prefetchDirectScope) bool {
	return prefetchSubscriptionFileMatches(scope.executable, prefetchSubscriptionApprovedMain) && prefetchSubscriptionFileMatches(filepath.Join(filepath.Dir(scope.executable), "codex-code-mode-host"), prefetchSubscriptionApprovedCompanion)
}
func prefetchCommonOfficialProbe(ctx context.Context, plan prefetchSubscriptionPlan) (*prefetchSubscriptionAdmission, prefetchCommonLifecycle, error) {
	open := prefetchSubscriptionOfficialStdio(plan)
	var owned *prefetchSubscriptionStdioSession
	route, err := prefetchSubscriptionProbe(ctx, plan, func(ctx context.Context, cmd *exec.Cmd) (prefetchSubscriptionSession, error) {
		child, e := open(ctx, cmd)
		if child != nil {
			owned, _ = child.(*prefetchSubscriptionStdioSession)
		}
		return child, e
	})
	life := prefetchCommonLifecycle{}
	if owned != nil {
		life.reaped = owned.reaped
		life.nativeWaitZero = owned.reaped && owned.command.ProcessState != nil && owned.command.ProcessState.Success()
	}
	return route, life, err
}

// Reuse the same owned group/EOF/TERM/KILL/reap lifecycle for the bounded MCP
// inventory. The context is the same explicit15s inventory context; no
// CommandContext leader-only cancellation races group cleanup.
func prefetchCommonInventoryOwned(ctx context.Context, incoming *exec.Cmd) error {
	if ctx == nil || ctx.Err() != nil || incoming == nil || incoming.Stdout == nil {
		return prefetchSubscriptionCanceled
	}
	cmd := exec.Command(incoming.Path, incoming.Args[1:]...)
	cmd.Args = append([]string{}, incoming.Args...)
	cmd.Dir = incoming.Dir
	cmd.Env = append([]string{}, incoming.Env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stderr = io.Discard
	cmd.WaitDelay = prefetchSubscriptionReapBound
	inR, inW, e := os.Pipe()
	if e != nil {
		return prefetchSubscriptionTransportUnavailable
	}
	outR, outW, e := os.Pipe()
	if e != nil {
		inR.Close()
		inW.Close()
		return prefetchSubscriptionTransportUnavailable
	}
	cmd.Stdin, cmd.Stdout = inR, outW
	if cmd.Start() != nil {
		inR.Close()
		inW.Close()
		outR.Close()
		outW.Close()
		return prefetchSubscriptionTransportUnavailable
	}
	inR.Close()
	outW.Close()
	inW.Close()
	owned := &prefetchSubscriptionStdioSession{command: cmd, input: inW, output: outR, closed: make(chan struct{}), pidfd: -1}
	if fd, e := unix.PidfdOpen(cmd.Process.Pid, 0); e == nil {
		owned.pidfd = fd
	}
	done := make(chan error, 1)
	go func() { _, e := io.Copy(incoming.Stdout, io.LimitReader(outR, (256<<10)+1)); done <- e }()
	var readErr error
	readDone := false
	select {
	case readErr = <-done:
		readDone = true
	case <-ctx.Done():
		readErr = prefetchSubscriptionCanceled
	}
	closeErr := owned.Close()
	// Close cancels a held pipe; collect its owned copier before returning.
	if !readDone {
		select {
		case <-done:
		case <-time.After(prefetchSubscriptionReapBound):
			return prefetchSubscriptionCleanupFailed
		}
	}
	if readErr != nil || closeErr != nil || ctx.Err() != nil || !owned.reaped || cmd.ProcessState == nil || !cmd.ProcessState.Success() {
		return prefetchSubscriptionCleanupFailed
	}
	return nil
}
