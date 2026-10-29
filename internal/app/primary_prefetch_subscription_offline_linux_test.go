//go:build linux

package app

// This opt-in is PREPARED ONLY. It is excluded from the source-only test regex.
// Default skip is not successful execution of an official-client qualification.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type subscriptionOfflineObserved struct {
	prefetchSubscriptionSession
	configObserved, userDeclared, systemDeclared bool
}

func (s *subscriptionOfflineObserved) Call(ctx context.Context, r prefetchSubscriptionRequest) (io.ReadCloser, error) {
	reader, err := s.prefetchSubscriptionSession.Call(ctx, r)
	if err != nil || r.ID != 2 {
		return reader, err
	}
	body, e := io.ReadAll(io.LimitReader(reader, prefetchSubscriptionMaxBytes+1))
	closeErr := reader.Close()
	if e != nil || closeErr != nil || len(body) > prefetchSubscriptionMaxBytes {
		clear(body)
		return nil, prefetchSubscriptionRPCInvalid
	}
	result, e := prefetchSubscriptionRPC(ctx, bytes.NewReader(body), 2)
	if e == nil {
		if config, ok := result["config"].(map[string]any); ok {
			if servers, ok := config["mcp_servers"].(map[string]any); ok {
				s.configObserved = true
				_, s.userDeclared = servers["synthetic_user"]
				_, s.systemDeclared = servers["synthetic_system"]
			}
		}
	}
	return &prefetchSubscriptionMemoryFrame{body: body, Reader: bytes.NewReader(body)}, nil
}

func TestLiveSubscriptionStdioNoAuthQualification(t *testing.T) {
	if os.Getenv("IVOAI_SUBSCRIPTION_OFFLINE_NOAUTH_20261029") != "REVIEWED_ONE_NEGATIVE" {
		t.Skip("explicit reviewed one-run offline qualification required")
	}
	// Fail before opening the official transport unless the frozen namespace
	// envelope proves fresh synthetic HOME/etc and no external network route.
	if os.Getenv("HOME") != "/case/home" || os.Getenv("CODEX_HOME") != "/case/codehome" {
		t.Fatal("offline fixture envelope unavailable")
	}
	hostNS, e := os.ReadFile("/fixture/host-netns")
	currentNS, nse := os.Readlink("/proc/self/ns/net")
	if e != nil || nse != nil || strings.TrimSpace(string(hostNS)) == currentNS {
		t.Fatal("private network namespace not established")
	}
	interfaces, e := net.Interfaces()
	if e != nil || len(interfaces) != 1 || interfaces[0].Name != "lo" || interfaces[0].Flags&net.FlagUp == 0 {
		t.Fatal("namespace has unexpected network interfaces")
	}
	routes, e := os.ReadFile("/proc/net/route")
	if e != nil || len(strings.Fields(string(routes))) != 11 {
		t.Fatal("namespace has an unexpected IPv4 route")
	}
	v6, e := os.ReadFile("/proc/net/ipv6_route")
	if e != nil {
		t.Fatal("namespace IPv6 policy unavailable")
	}
	for _, line := range strings.Split(strings.TrimSpace(string(v6)), "\n") {
		if line != "" && !strings.HasSuffix(line, " lo") {
			t.Fatal("namespace has an unexpected IPv6 route")
		}
	}
	entries, e := os.ReadDir("/case/home")
	if e != nil || len(entries) != 0 {
		t.Fatal("synthetic HOME was not fresh")
	}
	entries, e = os.ReadDir("/case/codehome")
	if e != nil || len(entries) != 1 || entries[0].Name() != "config.toml" {
		t.Fatal("synthetic CODEX_HOME was not fresh")
	}
	for _, path := range []string{"/case/effects/user.started", "/case/effects/system.started", "/case/qualification.json"} {
		if _, e = os.Lstat(path); !os.IsNotExist(e) {
			t.Fatal("offline output/tripwire already existed")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	listener, e := net.Listen("tcp", "127.0.0.1:34567")
	if e != nil {
		t.Fatal("synthetic API-route tripwire unavailable")
	}
	defer listener.Close()
	var apiHit atomic.Bool
	listenerDone := make(chan struct{})
	defer func() { _ = listener.Close(); <-listenerDone }()
	go func() {
		defer close(listenerDone)
		for {
			connection, e := listener.Accept()
			if e != nil {
				return
			}
			apiHit.Store(true)
			_ = connection.Close()
			cancel()
		}
	}()
	scope, e := prefetchNewDirectScope("/reviewed/codex", "/case/repo", "gpt-6.1-sol", "read-only", []string{"HOME=/case/home", "CODEX_HOME=/case/codehome", "PATH=/usr/bin:/bin", "LANG=C.UTF-8", "TMPDIR=/tmp"})
	if e != nil {
		t.Fatal("reviewed source constructor refused fixture")
	}
	plan, e := prefetchPrepareSubscription(scope, prefetchSubscriptionHostPolicy{PermitFilteredEnvironment: true})
	if e != nil {
		t.Fatal("reviewed source profile refused fixture")
	}
	open := prefetchSubscriptionOfficialStdio(plan)
	var owned *prefetchSubscriptionStdioSession
	var observed *subscriptionOfflineObserved
	a, probeErr := prefetchSubscriptionProbe(ctx, plan, func(ctx context.Context, cmd *exec.Cmd) (prefetchSubscriptionSession, error) {
		s, e := open(ctx, cmd)
		if e != nil {
			return nil, e
		}
		owned = s.(*prefetchSubscriptionStdioSession)
		observed = &subscriptionOfflineObserved{prefetchSubscriptionSession: s}
		return observed, nil
	})
	noMCP := true
	for _, path := range []string{"/case/effects/user.started", "/case/effects/system.started"} {
		if _, e = os.Lstat(path); !os.IsNotExist(e) {
			noMCP = false
		}
	}
	noAuthFile := true
	for _, path := range []string{"/case/codehome/auth.json", "/case/home/.codex/auth.json"} {
		if _, e = os.Lstat(path); !os.IsNotExist(e) {
			noAuthFile = false
		}
	}
	configOK := observed != nil && observed.configObserved && observed.userDeclared && observed.systemDeclared
	reaped := owned != nil && owned.reaped
	wait0 := reaped && owned.command.ProcessState != nil && owned.command.ProcessState.Success()
	passed := a == nil && probeErr == prefetchSubscriptionAuthDenied && configOK && noMCP && !apiHit.Load() && noAuthFile && wait0
	outcome := "FAIL_NOAUTH_QUALIFICATION"
	if passed {
		outcome = "PASS_NOAUTH_NEGATIVE_NOT_AUTHENTICATED"
	}
	reason := "UNKNOWN"
	if value, ok := probeErr.(prefetchSubscriptionReason); ok {
		reason = value.Error()
	}
	receipt := struct {
		Outcome, Reason                                                                                                                                                                 string
		SourceConstructorConfigObserved, UserMCPDeclared, SystemMCPDeclared, NoMCPTripwire, NoAPIRouteTripwire, NoAuthFile, OwnedReaped, NativeWaitZero, AuthenticatedPositiveCertified bool
	}{Outcome: outcome, Reason: reason, SourceConstructorConfigObserved: configOK, UserMCPDeclared: observed != nil && observed.userDeclared, SystemMCPDeclared: observed != nil && observed.systemDeclared, NoMCPTripwire: noMCP, NoAPIRouteTripwire: !apiHit.Load(), NoAuthFile: noAuthFile, OwnedReaped: reaped, NativeWaitZero: wait0, AuthenticatedPositiveCertified: false}
	file, e := os.OpenFile("/case/qualification.json", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal("offline receipt unavailable")
	}
	writeErr := json.NewEncoder(file).Encode(receipt)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("offline receipt write failed")
	}
	if !passed {
		t.Fatal("offline negative qualification did not meet its fixed contract")
	}
}
