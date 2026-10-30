//go:build linux

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/ivo-lopes/ivoai/internal/session"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPrimaryPrefetchCommonAdmissionControlledProcess(t *testing.T) {
	binary := subscriptionStdioFixtureBinary(t)
	for _, mode := range []string{"success", "no_auth", "nonzero_exit", "inventory_invalid", "inventory_nonzero"} {
		t.Run(mode, func(t *testing.T) {
			p, pin, home := subscriptionStdioFixture(t, binary, mode)
			base := p.scope
			base.globals = append([]string{}, base.globals[:len(base.globals)-6]...)
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			probe := func(ctx context.Context, p prefetchSubscriptionPlan) (*prefetchSubscriptionAdmission, prefetchCommonLifecycle, error) {
				var owned *prefetchSubscriptionStdioSession
				open := prefetchSubscriptionPinnedStdio(p, pin)
				a, e := prefetchSubscriptionProbe(ctx, p, func(ctx context.Context, c *exec.Cmd) (prefetchSubscriptionSession, error) {
					s, e := open(ctx, c)
					if s != nil {
						owned = s.(*prefetchSubscriptionStdioSession)
					}
					return s, e
				})
				life := prefetchCommonLifecycle{}
				if owned != nil {
					life.reaped = owned.reaped
					life.nativeWaitZero = owned.reaped && owned.command.ProcessState != nil && owned.command.ProcessState.Success()
				}
				return a, life, e
			}
			permit, obs := prefetchCommonPrepare(ctx, base, prefetchSubscriptionHostPolicy{true}, probe, prefetchCommonInventoryOwned, func(prefetchDirectScope) bool { return true })
			if mode == "success" {
				if permit == nil || !obs.NativeWaitZero || !obs.OwnedReaped {
					t.Fatal("controlled composed lifecycle did not pass")
				}
				command, e := permit.takeCommand(ctx, filepath.Join(home, "schema"), "--literal-question")
				if e != nil || command.Run() != nil {
					t.Fatal("controlled fake exec did not complete")
				}
				if _, e = os.Stat(filepath.Join(home, "model.started")); e != nil {
					t.Fatal("positive fake model-start oracle absent")
				}
			} else {
				if permit != nil || obs.State != "NOT_RUN" || obs.ModelAttempts != 0 {
					t.Fatal("failed process admitted exec")
				}
				if _, e := os.Stat(filepath.Join(home, "model.started")); !os.IsNotExist(e) {
					t.Fatal("failed qualification reached fake model")
				}
				if mode == "no_auth" || mode == "nonzero_exit" {
					if _, e := os.Stat(filepath.Join(home, "inventory.ready")); !os.IsNotExist(e) {
						t.Fatal("route refusal reached inventory")
					}
				}
			}
			body, _ := json.Marshal(obs)
			if string(body) == "" {
				t.Fatal("observation unavailable")
			}
		})
	}
	// A fake executable is rejected by the live literal official-pin path before
	// startup. The injected positive above is never an official/auth certificate.
	p, _, home := subscriptionStdioFixture(t, binary, "success")
	base := p.scope
	base.globals = append([]string{}, base.globals[:len(base.globals)-6]...)
	permit, obs := prefetchCommonLiveAdmission(context.Background(), base, prefetchSubscriptionHostPolicy{true})
	if permit != nil || obs.Reason != "PIN_DENIED" || obs.ProbeAttempts != 0 {
		t.Fatal("live branch accepted synthetic binary")
	}
	if _, e := os.Stat(filepath.Join(home, "inventory.ready")); !os.IsNotExist(e) {
		t.Fatal("literal pin refusal started process")
	}
}

func TestPrimaryPrefetchCommonInventoryCancellationReapsOwnedGroup(t *testing.T) {
	binary := subscriptionStdioFixtureBinary(t)
	p, _, home := subscriptionStdioFixture(t, binary, "inventory_held")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, e := prefetchDirectInventory(ctx, p.inventoryScope(), func(c *exec.Cmd) error { return prefetchCommonInventoryOwned(ctx, c) })
		done <- e
	}()
	subscriptionWaitFixture(t, filepath.Join(home, "ready"))
	child, e := os.ReadFile(filepath.Join(home, "child.pid"))
	if e != nil {
		t.Fatal("owned descendant oracle missing")
	}
	pid, e := strconv.Atoi(string(child))
	if e != nil {
		t.Fatal("invalid controlled child PID")
	}
	start := session.ProcessStart(pid)
	if start == "" {
		t.Fatal("controlled descendant identity missing")
	}
	cancel()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancelled held pipe accepted")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("owned inventory did not clean up within existing lifecycle bounds")
	}
	if session.ProcessMatches(pid, start) {
		t.Fatal("owned held-pipe descendant survived cleanup")
	}
}

// This deliberately hostile fixture is a negative admission control. The
// non-child can be killed but cannot be Wait'ed by this process outside a PID
// envelope. Never turn a zombie/non-running observation into "reaped all".
func TestPrimaryPrefetchCommonResistantDescendantDeniesAdmission(t *testing.T) {
	binary := subscriptionStdioFixtureBinary(t)
	p, pin, home := subscriptionStdioFixture(t, binary, "inventory_resistant")
	base := p.scope
	base.globals = append([]string{}, base.globals[:len(base.globals)-6]...)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probe := func(ctx context.Context, p prefetchSubscriptionPlan) (*prefetchSubscriptionAdmission, prefetchCommonLifecycle, error) {
		var owned *prefetchSubscriptionStdioSession
		open := prefetchSubscriptionPinnedStdio(p, pin)
		a, e := prefetchSubscriptionProbe(ctx, p, func(ctx context.Context, c *exec.Cmd) (prefetchSubscriptionSession, error) {
			s, e := open(ctx, c)
			if s != nil {
				owned = s.(*prefetchSubscriptionStdioSession)
			}
			return s, e
		})
		life := prefetchCommonLifecycle{}
		if owned != nil {
			life.reaped = owned.reaped
			life.nativeWaitZero = owned.reaped && owned.command.ProcessState != nil && owned.command.ProcessState.Success()
		}
		return a, life, e
	}
	type result struct {
		p *prefetchCommonAdmission
		o prefetchCommonObservation
	}
	done := make(chan result, 1)
	go func() {
		permit, o := prefetchCommonPrepare(ctx, base, prefetchSubscriptionHostPolicy{true}, probe, prefetchCommonInventoryOwned, func(prefetchDirectScope) bool { return true })
		done <- result{permit, o}
	}()
	subscriptionWaitFixture(t, filepath.Join(home, "ready"))
	readPID := func(name string) (int, string) {
		raw, e := os.ReadFile(filepath.Join(home, name))
		if e != nil {
			t.Fatal("owned fixture identity missing")
		}
		pid, e := strconv.Atoi(string(raw))
		if e != nil {
			t.Fatal("owned fixture identity malformed")
		}
		start := session.ProcessStart(pid)
		if start == "" {
			t.Fatal("owned fixture process identity unavailable")
		}
		return pid, start
	}
	parent, parentStart := readPID("inventory.pid")
	child, childStart := readPID("child.pid")
	cancel()
	select {
	case r := <-done:
		if r.p != nil || r.o.State != "NOT_RUN" || r.o.ModelAttempts != 0 || r.o.InventoryAttempts != 1 || r.o.ProbeAttempts != 1 {
			t.Fatal("resistant descendant admitted a model")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("resistant inventory exceeded existing cleanup bound")
	}
	if session.ProcessMatches(parent, parentStart) {
		t.Fatal("owned inventory leader was not reaped")
	}
	raw, e := os.ReadFile("/proc/" + strconv.Itoa(child) + "/stat")
	if !prefetchFixtureDescendantStopped(raw, e, childStart) {
		t.Fatal("descendant still executing or identity/state unknown")
	}
	t.Log("negative admission preserved; descendant_nonexecuting=Z_or_absent; descendant_reap=NOT_OBSERVED")
}

// A process can disappear between opening procfs and reading stat: Linux then
// returns ESRCH rather than ENOENT. Only those known-absence errors qualify;
// permission/I/O failures and malformed or mismatched identities stay unknown.
func prefetchFixtureDescendantStopped(raw []byte, err error, start string) bool {
	if os.IsNotExist(err) || errors.Is(err, syscall.ESRCH) {
		return true
	}
	if err != nil || len(raw) > 16384 {
		return false
	}
	i := bytes.LastIndexByte(raw, ')')
	if i < 0 {
		return false
	}
	fields := strings.Fields(string(raw[i+1:]))
	return len(fields) >= 20 && fields[19] == start && fields[0] == "Z"
}

func TestPrimaryPrefetchCommonDescendantObservationConservative(t *testing.T) {
	fields := append([]string{"Z"}, make([]string, 18)...)
	fields = append(fields, "123")
	raw := []byte("42 (fixture) " + strings.Join(fields, " "))
	for _, err := range []error{syscall.ENOENT, syscall.ESRCH} {
		if !prefetchFixtureDescendantStopped(nil, err, "123") {
			t.Fatal("known process absence was not recognized")
		}
	}
	// Use real-shaped nonempty stat fields; empty fields are not positional.
	for i := range fields {
		if fields[i] == "" {
			fields[i] = "0"
		}
	}
	raw = []byte("42 (fixture) " + strings.Join(fields, " "))
	if !prefetchFixtureDescendantStopped(raw, nil, "123") {
		t.Fatal("matching zombie was not recognized as nonexecuting")
	}
	for _, err := range []error{syscall.EPERM, syscall.EIO} {
		if prefetchFixtureDescendantStopped(raw, err, "123") {
			t.Fatal("unknown procfs failure was accepted")
		}
	}
	if prefetchFixtureDescendantStopped(raw, nil, "999") || prefetchFixtureDescendantStopped([]byte("malformed"), nil, "123") {
		t.Fatal("unknown identity or malformed snapshot was accepted")
	}
	fields[0] = "S"
	if prefetchFixtureDescendantStopped([]byte("42 (fixture) "+strings.Join(fields, " ")), nil, "123") {
		t.Fatal("executing descendant was accepted")
	}
}
