//go:build linux

package app

// Default-skipped metadata qualification only. No production/cohort caller.
// The official opt-in is not executed during implementation/compilation.
import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLiveSubscriptionClientReportedRoute(t *testing.T) {
	if os.Getenv("IVOAI_SUBSCRIPTION_AUTH_METADATA_20261029") != "REVIEWED_ONE_METADATA_PROBE" {
		t.Skip("one separately reviewed authenticated metadata probe required")
	}
	if os.Getenv("IVOAI_SUBSCRIPTION_HOST_POLICY") != "FILTERED_ENV_PERMITTED" {
		t.Fatal("reviewed environment policy unavailable")
	}
	expectedUID, uidErr := strconv.Atoi(os.Getenv("IVOAI_SUBSCRIPTION_OUTER_UID"))
	expectedGID, gidErr := strconv.Atoi(os.Getenv("IVOAI_SUBSCRIPTION_OUTER_GID"))
	if uidErr != nil || gidErr != nil || expectedUID != os.Getuid() || expectedGID != os.Getgid() {
		t.Fatal("primary identity envelope failed before client startup")
	}
	outerNet := os.Getenv("IVOAI_SUBSCRIPTION_OUTER_NETNS")
	outerPID := os.Getenv("IVOAI_SUBSCRIPTION_OUTER_PIDNS")
	currentNet, netErr := os.Readlink("/proc/self/ns/net")
	currentPID, pidErr := os.Readlink("/proc/self/ns/pid")
	if netErr != nil || pidErr != nil || outerNet == "" || outerPID == "" || currentNet != outerNet || currentPID == outerPID {
		t.Fatal("reviewed PID/network envelope failed before client startup")
	}

	output := os.Getenv("IVOAI_SUBSCRIPTION_PRIVATE_OUTPUT")
	binary := os.Getenv("IVOAI_SUBSCRIPTION_REVIEWED_CODEX")
	if !filepath.IsAbs(output) || !strings.HasPrefix(output, "/var/tmp/ivoai-subscription-auth-metadata-") || !filepath.IsAbs(binary) {
		t.Fatal("reviewed private envelope unavailable")
	}
	actual, e := filepath.EvalSymlinks(output)
	if e != nil || actual != output {
		t.Fatal("private output path refused")
	}
	metadata, e := os.Lstat(output)
	if e != nil || !metadata.IsDir() || metadata.Mode().Perm() != 0700 {
		t.Fatal("private output permissions refused")
	}
	owner, ok := metadata.Sys().(*syscall.Stat_t)
	if !ok || int(owner.Uid) != os.Getuid() {
		t.Fatal("private output owner refused")
	}
	root := filepath.Join(output, "workspace")
	if os.Mkdir(root, 0700) != nil {
		t.Fatal("fixture was not fresh")
	}
	fixture := []byte("Read-only metadata probe; no question, model turn or real project.\n")
	if os.WriteFile(filepath.Join(root, "README"), fixture, 0600) != nil {
		t.Fatal("owned fixture unavailable")
	}
	fixtureDirectory, e := os.OpenFile(root, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if e != nil {
		t.Fatal("owned fixture directory unavailable")
	}
	defer fixtureDirectory.Close()
	fixtureDirectoryIdentity, e := fixtureDirectory.Stat()
	if e != nil {
		t.Fatal("owned fixture identity unavailable")
	}

	environment := prefetchSubscriptionEnvironment()
	homePresent := false
	for _, entry := range environment {
		if strings.HasPrefix(entry, "HOME=") {
			homePresent = filepath.IsAbs(strings.TrimPrefix(entry, "HOME="))
		}
	}
	if !homePresent {
		t.Fatal("client-owned HOME unavailable")
	}
	// HOME, CODEX_HOME and XDG/DBus entries stay in place. This test does not
	// open, copy, hash, change, synthesize or inspect any personal auth/config.
	scope, e := prefetchNewDirectScope(binary, root, "gpt-6.1-sol", "read-only", environment)
	if e != nil {
		t.Fatal("reviewed source constructor refused scope")
	}
	plan, e := prefetchPrepareSubscription(scope, prefetchSubscriptionHostPolicy{PermitFilteredEnvironment: true})
	if e != nil {
		t.Fatal("reviewed source profile refused scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	open := prefetchSubscriptionOfficialStdio(plan)
	var owned *prefetchSubscriptionStdioSession
	admission, probeErr := prefetchSubscriptionProbe(ctx, plan, func(ctx context.Context, cmd *exec.Cmd) (prefetchSubscriptionSession, error) {
		session, e := open(ctx, cmd)
		if session != nil {
			owned = session.(*prefetchSubscriptionStdioSession)
		}
		return session, e
	})
	// Read only the original owned directory, never follow a replaced fixture
	// path into any personal configuration or auth location.
	fd, readErr := syscall.Openat(int(fixtureDirectory.Fd()), "README", syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	var fixtureAfter []byte
	if readErr == nil {
		file := os.NewFile(uintptr(fd), "owned-fixture")
		info, statErr := file.Stat()
		if statErr == nil && info.Mode().IsRegular() && info.Size() <= 1024 {
			fixtureAfter, readErr = io.ReadAll(io.LimitReader(file, 1025))
		}
		_ = file.Close()
	}
	entries, dirErr := fixtureDirectory.ReadDir(-1)
	currentDirectory, pathErr := os.Lstat(root)
	fixtureUnchanged := readErr == nil && dirErr == nil && pathErr == nil && os.SameFile(fixtureDirectoryIdentity, currentDirectory) && bytes.Equal(fixtureAfter, fixture) && len(entries) == 1 && entries[0].Name() == "README"
	reaped := owned != nil && owned.reaped
	wait0 := reaped && owned.command.ProcessState != nil && owned.command.ProcessState.Success()
	reported := admission != nil && admission.summary.Accepted && admission.summary.Provider == "OPENAI" && admission.summary.Auth == "CHATGPT_COMPATIBLE" && admission.summary.Route == "CHATGPT_CANONICAL"
	// officialWitness is host evidence from the literal pinned constructor,
	// never a field decoded from an account/config response or a model input.
	receipt := subscriptionAuthenticatedOutcome(subscriptionAuthenticatedObservation{
		officialWitness: owned != nil, reported: reported, probeErr: probeErr,
		reaped: reaped, waitZero: wait0, contextActive: ctx.Err() == nil,
		fixtureUnchanged: fixtureUnchanged, identityPreserved: true, networkPreserved: true, pidContained: true,
	})
	file, e := os.OpenFile(filepath.Join(output, "qualification.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal("private receipt unavailable")
	}
	writeErr := json.NewEncoder(file).Encode(receipt)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("private receipt write failed")
	}
	if !receipt.AuthenticatedPositiveCertified {
		t.Fatal("authenticated metadata qualification did not meet its fixed contract")
	}
}

// Private host observations: raw account/config/paths do not belong here.
type subscriptionAuthenticatedObservation struct {
	officialWitness, reported, reaped, waitZero, contextActive, fixtureUnchanged, identityPreserved, networkPreserved, pidContained bool
	probeErr                                                                                                                        error
}

func (subscriptionAuthenticatedObservation) String() string {
	return "subscription-authenticated-observation"
}
func (subscriptionAuthenticatedObservation) GoString() string {
	return "subscription-authenticated-observation"
}

type subscriptionAuthenticatedReceipt struct {
	Outcome, Reason, Tokens                                                                                                                                      string
	ClientReported, OwnedReaped, NativeWaitZero, ContextActive, FixtureUnchanged, PrimaryIdentityPreserved, NetworkNamespacePreserved, PIDContained              bool
	AuthenticatedPositiveCertified, DriverModelPromptIssued, ExclusiveInteractiveCredentialCertified, BillingCertified, CohortConnected, AtomicSnapshotCertified bool
}

func subscriptionAuthenticatedOutcome(value subscriptionAuthenticatedObservation) subscriptionAuthenticatedReceipt {
	reason := "UNKNOWN"
	if value.probeErr == nil {
		reason = "ACCEPTED_UNQUALIFIED"
	} else if closed, ok := value.probeErr.(prefetchSubscriptionReason); ok {
		reason = closed.Error()
	}
	eligible := value.probeErr == nil && value.reported && value.reaped && value.waitZero && value.contextActive && value.fixtureUnchanged && value.identityPreserved && value.networkPreserved && value.pidContained
	outcome := "STOP_METADATA_QUALIFICATION_NOT_MET"
	certified := false
	if eligible {
		if value.officialWitness {
			outcome = "PASS_CLIENT_REPORTED_CHATGPT_CANONICAL_ROUTE_DURING_PROBE"
			certified = true
		} else {
			outcome = "SYNTHETIC_DATA_ONLY_NOT_CERTIFIED"
		}
	}
	return subscriptionAuthenticatedReceipt{Outcome: outcome, Reason: reason, Tokens: "NOT_APPLICABLE_NO_MODEL_PROMPT_ISSUED", ClientReported: value.officialWitness && value.reported, OwnedReaped: value.reaped, NativeWaitZero: value.waitZero, ContextActive: value.contextActive, FixtureUnchanged: value.fixtureUnchanged, PrimaryIdentityPreserved: value.identityPreserved, NetworkNamespacePreserved: value.networkPreserved, PIDContained: value.pidContained, AuthenticatedPositiveCertified: certified}
}
