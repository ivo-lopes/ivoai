//go:build linux

package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/session"
)

func subscriptionStdioFixtureBinary(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	source, err := os.ReadFile("testdata/subscription_stdio_fixture.go.txt")
	if err != nil {
		t.Fatal("fixture source unavailable")
	}
	code := filepath.Join(root, "main.go")
	if os.WriteFile(code, source, 0600) != nil {
		t.Fatal("fixture source write failed")
	}
	binary := filepath.Join(root, "codex")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-trimpath", "-o", binary, code)
	cmd.Env = append(prefetchSubscriptionEnvironment(), "GOTOOLCHAIN=local", "GOENV=off", "GOPROXY=off", "GOSUMDB=off", "CGO_ENABLED=0")
	cmd.Stderr = nil
	cmd.Stdout = nil
	if cmd.Run() != nil {
		t.Fatal("controlled stdio fixture build failed")
	}
	data, err := os.ReadFile(binary)
	if err != nil || os.WriteFile(filepath.Join(root, "codex-code-mode-host"), data, 0700) != nil {
		t.Fatal("controlled companion setup failed")
	}
	return binary
}
func subscriptionStdioFixture(t *testing.T, binary, mode string) (prefetchSubscriptionPlan, prefetchSubscriptionExecutablePin, string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if os.Mkdir(home, 0700) != nil {
		t.Fatal("fixture HOME creation failed")
	}
	scope, err := prefetchNewDirectScope(binary, root, mode, "read-only", []string{"HOME=" + home, "PATH=/usr/bin:/bin"})
	if err != nil {
		t.Fatal("fixture scope failed")
	}
	plan, err := prefetchPrepareSubscription(scope, prefetchSubscriptionHostPolicy{PermitFilteredEnvironment: true})
	if err != nil {
		t.Fatal("fixture plan failed")
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal("fixture hash unavailable")
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	return plan, prefetchSubscriptionExecutablePin{main: binary, companion: filepath.Join(filepath.Dir(binary), "codex-code-mode-host"), mainSHA: digest, companionSHA: digest}, home
}
func subscriptionWaitFixture(t *testing.T, path string) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if b, e := os.ReadFile(path); e == nil && len(b) > 0 {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("controlled child readiness not published")
		case <-tick.C:
		}
	}
}

func TestPrimaryPrefetchSubscriptionStdioControlledProtocols(t *testing.T) {
	binary := subscriptionStdioFixtureBinary(t)
	cases := []struct {
		mode string
		want error
	}{
		{"success", nil}, {"notifications", nil}, {"no_auth", prefetchSubscriptionAuthDenied}, {"wrong_id", prefetchSubscriptionRPCError}, {"remote_error", prefetchSubscriptionRPCError}, {"partial", prefetchSubscriptionRPCError}, {"oversize", prefetchSubscriptionRPCError}, {"deep", prefetchSubscriptionRPCError}, {"nodes", prefetchSubscriptionRPCError}, {"large_notification", prefetchSubscriptionRPCError}, {"duplicate", prefetchSubscriptionRPCError}, {"server_request", prefetchSubscriptionRPCError}, {"notification_flood", prefetchSubscriptionRPCError}, {"early_eof", prefetchSubscriptionRPCError}, {"nonzero_exit", prefetchSubscriptionCleanupFailed},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			plan, pin, _ := subscriptionStdioFixture(t, binary, tc.mode)
			open := prefetchSubscriptionPinnedStdio(plan, pin)
			var owned *prefetchSubscriptionStdioSession
			wrapped := func(ctx context.Context, cmd *exec.Cmd) (prefetchSubscriptionSession, error) {
				s, e := open(ctx, cmd)
				if s != nil {
					owned = s.(*prefetchSubscriptionStdioSession)
				}
				return s, e
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			a, err := prefetchSubscriptionProbe(ctx, plan, wrapped)
			if err != tc.want {
				t.Fatal("controlled protocol result differs from closed expectation")
			}
			if tc.want == nil && (a == nil || !a.summary.Accepted || a.summary.Connected) {
				t.Fatal("controlled projection missing or overclaimed connection")
			}
			if tc.want != nil && a != nil {
				t.Fatal("failed protocol retained admission")
			}
			if owned == nil || !owned.reaped || owned.command.ProcessState == nil {
				t.Fatal("owned fixture was not reaped")
			}
			if owned.total > prefetchSubscriptionMaxBytes+1 {
				t.Fatal("buffered transport overread beyond the global budget plus one byte")
			}
			if tc.want == nil && (!owned.command.ProcessState.Success() || owned.termRequested) {
				t.Fatal("controlled success did not exit through stdin EOF with Wait0")
			}
			if owned.Close() != owned.closeErr {
				t.Fatal("cleanup is not idempotent")
			}
			if err != nil && strings.Contains(err.Error(), "fixture-private-secret-canary") {
				t.Fatal("stderr or RPC secret leaked")
			}
			for _, v := range []any{owned, pin} {
				if strings.Contains(fmt.Sprintf("%#v", v), "fixture-private-secret-canary") {
					t.Fatal("transport diagnostic retained private fixture data")
				}
			}
			if a != nil {
				b, _ := json.Marshal(a)
				if strings.Contains(string(b), "fixture-private-secret-canary") {
					t.Fatal("transport projection retained private fixture data")
				}
			}
		})
	}
}

func TestPrimaryPrefetchSubscriptionStdioPinnedScopeAndWhitelist(t *testing.T) {
	binary := subscriptionStdioFixtureBinary(t)
	plan, pin, _ := subscriptionStdioFixture(t, binary, "success")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	for _, kind := range []string{"main_pin", "companion_pin", "environment", "directory", "arguments"} {
		t.Run(kind, func(t *testing.T) {
			p := pin
			command := plan.probeCommand(ctx)
			switch kind {
			case "main_pin":
				p.mainSHA = strings.Repeat("0", 64)
			case "companion_pin":
				p.companionSHA = strings.Repeat("0", 64)
			case "environment":
				command.Env = append(command.Env, "OPENAI_API_KEY=fixture-private-secret-canary")
			case "directory":
				command.Dir = filepath.Dir(command.Dir)
			case "arguments":
				command.Args = append(command.Args, "--forged")
			}
			s, e := prefetchSubscriptionPinnedStdio(plan, p)(ctx, command)
			if s != nil || e == nil || command.Process != nil {
				t.Fatal("unverified scope or pin launched a child")
			}
		})
	}
	open := prefetchSubscriptionPinnedStdio(plan, pin)
	s, err := open(ctx, plan.probeCommand(ctx))
	if err != nil {
		t.Fatal("controlled session open failed")
	}
	owned := s.(*prefetchSubscriptionStdioSession)
	defer owned.Close()
	if _, err = s.Call(ctx, prefetchSubscriptionRequest{ID: 1, Method: "account/login/start", Params: map[string]any{}}); err != prefetchSubscriptionRPCInvalid {
		t.Fatal("outbound method whitelist failed")
	}
	if _, err = s.Call(ctx, prefetchSubscriptionRequest{ID: 1, Method: "initialize", Params: map[string]any{}}); err == nil {
		t.Fatal("terminal failure permitted a retry")
	}
	if !owned.reaped {
		t.Fatal("rejected method did not clean up owned child")
	}
}

func TestPrimaryPrefetchSubscriptionStdioCancelHeldPipes(t *testing.T) {
	binary := subscriptionStdioFixtureBinary(t)
	for _, mode := range []string{"held_pipe", "ignore_term"} {
		t.Run(mode, func(t *testing.T) {
			plan, pin, home := subscriptionStdioFixture(t, binary, mode)
			open := prefetchSubscriptionPinnedStdio(plan, pin)
			var owned *prefetchSubscriptionStdioSession
			root, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ctx, stop := context.WithCancel(root)
			defer stop()
			done := make(chan error, 1)
			go func() {
				_, err := prefetchSubscriptionProbe(ctx, plan, func(ctx context.Context, c *exec.Cmd) (prefetchSubscriptionSession, error) {
					s, e := open(ctx, c)
					if s != nil {
						owned = s.(*prefetchSubscriptionStdioSession)
					}
					return s, e
				})
				done <- err
				close(done)
			}()
			defer func() {
				stop()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Error("controlled test cleanup did not finish")
				}
			}()
			subscriptionWaitFixture(t, filepath.Join(home, "ready"))
			childPID := 0
			childStart := ""
			if mode == "held_pipe" {
				raw, e := os.ReadFile(filepath.Join(home, "child.pid"))
				if e != nil {
					t.Fatal("controlled child identity missing")
				}
				childPID, _ = strconv.Atoi(string(raw))
				childStart = session.ProcessStart(childPID)
				if childStart == "" {
					t.Fatal("controlled descendant identity unavailable")
				}
			}
			before := time.Now()
			stop()
			select {
			case err := <-done:
				if err != prefetchSubscriptionCanceled {
					t.Fatal("canceled protocol did not fail closed")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("blocked pipe cancellation did not finish within cleanup bound")
			}
			if time.Since(before) > 2*time.Second || owned == nil || !owned.reaped {
				t.Fatal("owned cancellation cleanup not observed")
			}
			if mode == "ignore_term" {
				status, ok := owned.command.ProcessState.Sys().(syscall.WaitStatus)
				if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL || !owned.termRequested {
					t.Fatal("TERM-resistant controlled child did not exercise KILL escalation")
				}
			}
			if mode == "held_pipe" && session.ProcessMatches(childPID, childStart) {
				t.Fatal("owned descendant survived or remained unreaped")
			}
		})
	}
}

func TestPrimaryPrefetchSubscriptionStdioRejectsEffectsAndWrongOrder(t *testing.T) {
	binary := subscriptionStdioFixtureBinary(t)
	for _, method := range []string{"thread/start", "turn/start", "config/value/write", "account/login/start", "account/logout", "mcpServerStatus/list"} {
		t.Run(method, func(t *testing.T) {
			plan, pin, _ := subscriptionStdioFixture(t, binary, "success")
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			s, e := prefetchSubscriptionPinnedStdio(plan, pin)(ctx, plan.probeCommand(ctx))
			if e != nil {
				t.Fatal("controlled open failed")
			}
			defer s.Close()
			if _, e = s.Call(ctx, prefetchSubscriptionRequest{ID: 1, Method: method, Params: map[string]any{}}); e != prefetchSubscriptionRPCInvalid {
				t.Fatal("effectful/unapproved method accepted")
			}
			if !s.(*prefetchSubscriptionStdioSession).reaped {
				t.Fatal("unapproved method did not terminate owned session")
			}
		})
	}
	plan, pin, _ := subscriptionStdioFixture(t, binary, "success")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	s, e := prefetchSubscriptionPinnedStdio(plan, pin)(ctx, plan.probeCommand(ctx))
	if e != nil {
		t.Fatal("controlled open failed")
	}
	defer s.Close()
	if e = s.Notify(ctx, "initialized"); e != prefetchSubscriptionRPCInvalid {
		t.Fatal("notification before initialize accepted")
	}
	if e = s.Notify(ctx, "initialized"); e == nil {
		t.Fatal("failed notification allowed replay")
	}
}
