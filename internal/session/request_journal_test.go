package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func requestJournalFixture(t *testing.T) (Store, Session) {
	t.Helper()
	s, v := fixtureSession(t, t.TempDir())
	v.Mode, v.Auto, v.Coordinator, v.Frontend = ModeAuto, true, "native", "codex"
	v.State, v.InitialPlanner, v.CurrentPrimary, v.SwarmID = StateRunning, "codex", "codex", "native_"+v.SessionID
	v.FrontendSessionID, v.KnowledgeScopeID = "frontend_fixture", "knowledge_fixture"
	v.FrontendSessions = map[string]string{"codex": v.FrontendSessionID}
	if err := s.Create(v); err != nil {
		t.Fatal(err)
	}
	return s, v
}

func TestRequestJournalRetainsOldestClaimAcrossCacheLimitAndReopen(t *testing.T) {
	s, v := requestJournalFixture(t)
	ctx := context.Background()
	const count = 300
	for i := range count {
		ok, err := s.ClaimFrontendRequest(ctx, v.SessionID, v.FrontendSessionID, fmt.Sprintf("message_%03d", i))
		if err != nil || !ok {
			t.Fatalf("claim%d failed: %v", i, err)
		}
	}
	stored, err := s.Get(v.SessionID)
	if err != nil || len(stored.FrontendRequests) != 256 || stored.RequestJournalVersion != 1 {
		t.Fatal("bounded metadata projection invalid", err)
	}
	if _, found := stored.FrontendRequests[v.FrontendSessionID+":message_000"]; found {
		t.Fatal("fixture did not exceed the old cache")
	}
	s = Store{Root: s.Root}
	if ok, err := s.ClaimFrontendRequest(ctx, v.SessionID, v.FrontendSessionID, "message_000"); err != nil || ok {
		t.Fatal("oldest completed claim was replayable after restart", err)
	}
	if filepath.Dir(s.path(v.SessionID)) != s.journalDir() {
		t.Fatal("journal session remained visible to old writers")
	}
	for _, path := range []string{filepath.Join(s.Root, v.SessionID+".json"), filepath.Join(s.frontendDir(), v.SessionID+".json")} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("legacy rollback path still exposes journal session")
		}
	}
	if _, err := s.Update(v.SessionID, func(v *Session) error { v.RequestJournalVersion = 0; return nil }); err == nil {
		t.Fatal("request journal format was downgraded")
	}
	if got, err := s.FindFrontend("codex", v.FrontendSessionID, v.WorkingDirectory); err != nil || got.SessionID != v.SessionID {
		t.Fatal("namespace migration lost frontend binding", err)
	}
	page, err := s.ListBounded(ctx, 10, 1<<20)
	if err != nil || len(page.Sessions) != 1 {
		t.Fatal("bounded administration lost journal namespace", err)
	}
}

func TestRequestJournalImportsLegacyClaimsAndRefusesUnknownTruncatedHistory(t *testing.T) {
	for _, count := range []int{1, 256} {
		t.Run(fmt.Sprintf("legacy_%d", count), func(t *testing.T) {
			s, v := requestJournalFixture(t)
			_, err := s.Update(v.SessionID, func(v *Session) error {
				v.FrontendRequests = map[string]time.Time{}
				for i := range count {
					v.FrontendRequests[v.FrontendSessionID+":"+fmt.Sprint(i)] = time.Now().UTC()
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(s.path(v.SessionID))
			ok, err := s.ClaimFrontendRequest(context.Background(), v.SessionID, v.FrontendSessionID, "new")
			if count == 256 {
				if ok || !errors.Is(err, ErrClaimHistoryIncomplete) {
					t.Fatal("unknown old history was fabricated", err)
				}
				after, _ := os.ReadFile(s.path(v.SessionID))
				if !bytes.Equal(before, after) {
					t.Fatal("refused import changed legacy history")
				}
			} else {
				if !ok || err != nil {
					t.Fatal(err)
				}
				if ok, err := s.ClaimFrontendRequest(context.Background(), v.SessionID, v.FrontendSessionID, "0"); ok || err != nil {
					t.Fatal("legacy duplicate executed", err)
				}
			}
		})
	}
}

func TestRequestJournalDeduplicatesAcrossLogicalOwnersButNotOtherScopes(t *testing.T) {
	s, first := requestJournalFixture(t)
	second := first
	second.SessionID, _ = NewID()
	second.SwarmID = "native_" + second.SessionID
	if err := s.Create(second); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ClaimFrontendRequest(context.Background(), first.SessionID, first.FrontendSessionID, "same"); !ok || err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ClaimFrontendRequest(context.Background(), second.SessionID, second.FrontendSessionID, "same"); ok || err != nil {
		t.Fatal("another logical owner replayed same scoped frontend request", err)
	}
	other := first
	other.SessionID, _ = NewID()
	other.SwarmID, other.KnowledgeScopeID = "native_"+other.SessionID, "different_organization_and_repo_scope"
	if err := s.Create(other); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ClaimFrontendRequest(context.Background(), other.SessionID, other.FrontendSessionID, "same"); !ok || err != nil {
		t.Fatal("unrelated owner scope was conflated", err)
	}
}

func TestRequestJournalSymlinksCancellationAndStorageFailuresNeverAdmit(t *testing.T) {
	for _, kind := range []string{"parent", "claim_entry", "marker", "namespace", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			s, v := requestJournalFixture(t)
			ctx := context.Background()
			scope := requestScope(v, v.FrontendSessionID)
			outside := t.TempDir()
			before, _ := os.ReadDir(outside)
			switch kind {
			case "parent":
				if os.Symlink(outside, filepath.Join(s.Root, "request-claims")) != nil {
					t.Fatal("parent link fixture")
				}
			case "namespace":
				if os.Symlink(outside, s.journalDir()) != nil {
					t.Fatal("namespace link fixture")
				}
			case "claim_entry", "marker":
				fd, err := s.privateJournalDirectory("request-claims", scope, true)
				if err != nil {
					t.Fatal(err)
				}
				_ = os.NewFile(uintptr(fd), "fixture-close").Close()
				name := journalDigest("ivoai-request-v1", v.FrontendSessionID, "request") + ".json"
				if kind == "marker" {
					name = "legacy-imported.json"
				}
				if os.Symlink(filepath.Join(outside, "never-followed"), filepath.Join(s.Root, "request-claims", scope, name)) != nil {
					t.Fatal("leaf link fixture")
				}
			case "cancelled":
				next, cancel := context.WithCancel(ctx)
				cancel()
				ctx = next
			}
			ok, err := s.ClaimFrontendRequest(ctx, v.SessionID, v.FrontendSessionID, "request")
			if ok {
				t.Fatal("unsafe storage admitted executor")
			}
			if kind != "claim_entry" && err == nil {
				t.Fatal("storage failure was hidden")
			}
			after, _ := os.ReadDir(outside)
			if len(before) != len(after) {
				t.Fatal("journal escaped the private root")
			}
		})
	}
}

func TestRequestJournalProcessHelper(t *testing.T) {
	if os.Getenv("IVOAI_CLAIM_JOURNAL_HELPER") != "1" {
		t.Skip("private child fixture")
	}
	s := Store{Root: os.Getenv("IVOAI_CLAIM_JOURNAL_ROOT")}
	ok, err := s.ClaimFrontendRequest(context.Background(), os.Getenv("IVOAI_CLAIM_JOURNAL_OWNER"), "frontend_fixture", "raced")
	if err != nil {
		t.Fatal("child claim failed")
	}
	if ok {
		fmt.Println("CLAIMED")
	} else {
		fmt.Println("DUPLICATE")
	}
}

func TestRequestJournalTwoProcessesClaimExactlyOnce(t *testing.T) {
	s, v := requestJournalFixture(t)
	// Finish namespace/migration setup before racing two independent owners.
	if ok, err := s.ClaimFrontendRequest(context.Background(), v.SessionID, v.FrontendSessionID, "warmup"); !ok || err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out := make(chan []byte, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRequestJournalProcessHelper$", "-test.count=1")
			cmd.Env = []string{"PATH=/usr/bin:/bin", "IVOAI_CLAIM_JOURNAL_HELPER=1", "IVOAI_CLAIM_JOURNAL_ROOT=" + s.Root, "IVOAI_CLAIM_JOURNAL_OWNER=" + v.SessionID}
			data, err := cmd.CombinedOutput()
			if err != nil {
				out <- []byte("ERROR")
				return
			}
			out <- data
		}()
	}
	wait.Wait()
	close(out)
	claimed, duplicate := 0, 0
	for data := range out {
		if bytes.Contains(data, []byte("CLAIMED")) {
			claimed++
		}
		if bytes.Contains(data, []byte("DUPLICATE")) {
			duplicate++
		}
	}
	if claimed != 1 || duplicate != 1 {
		t.Fatalf("claim race: claimed=%d duplicate=%d", claimed, duplicate)
	}
}

func TestTurnAttemptAuditRetainsExactEventsPastProjectionLimit(t *testing.T) {
	s, v := requestJournalFixture(t)
	ctx := context.Background()
	var first []byte
	for i := range 40 {
		raw := []byte(fmt.Sprintf("{\n \"attempt_id\":\"attempt_%d\", \"ivoai_session_id\":\"%s\", \"turn_state\":\"completed\"\n}", i, v.SessionID))
		if i == 0 {
			first = raw
		}
		if err := s.AppendTurnAttempt(ctx, v.SessionID, raw); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Update(v.SessionID, func(v *Session) error {
			v.TurnAttempts = append(v.TurnAttempts, json.RawMessage(raw))
			if len(v.TurnAttempts) > 32 {
				v.TurnAttempts = v.TurnAttempts[len(v.TurnAttempts)-32:]
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	read, err := (Store{Root: s.Root}).ReadTurnAttemptEvents(ctx, v.SessionID, "attempt_0")
	if err != nil || len(read) != 1 || !bytes.Equal(first, read[0]) {
		t.Fatal("old exact audit event lost", err)
	}
	if err := s.AppendTurnAttempt(ctx, v.SessionID, first); err != nil {
		t.Fatal(err)
	}
	read, err = s.ReadTurnAttemptEvents(ctx, v.SessionID, "attempt_0")
	if err != nil || len(read) != 1 {
		t.Fatal("duplicate audit event appended", err)
	}
	other, _ := NewID()
	if _, err := s.ReadTurnAttemptEvents(ctx, other, "attempt_0"); err == nil {
		t.Fatal("audit crossed session owner")
	}
	if err := s.AppendTurnAttempt(ctx, v.SessionID, []byte(`{"attempt_id":"foreign","ivoai_session_id":"`+other+`"}`)); err == nil {
		t.Fatal("foreign audit owner accepted")
	}
}

func TestTurnAttemptAuditRejectsCorruptionAndUnsafeNames(t *testing.T) {
	s, v := requestJournalFixture(t)
	raw := []byte(`{"attempt_id":"audit","ivoai_session_id":"` + v.SessionID + `"}`)
	if err := s.AppendTurnAttempt(context.Background(), v.SessionID, raw); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(s.Root, "turn-attempts", v.SessionID, journalDigest("ivoai-turn-attempt-v1", "audit"))
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, entries[0].Name()), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadTurnAttemptEvents(context.Background(), v.SessionID, "audit"); err == nil {
		t.Fatal("corrupted audit accepted")
	}
	if _, err := s.ReadTurnAttemptEvents(context.Background(), v.SessionID, strings.Repeat("x", 129)); err == nil {
		t.Fatal("unbounded attempt accepted")
	}
}

func TestRequestJournalNamespaceWatchAndReopenRemainOperational(t *testing.T) {
	s, v := requestJournalFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	changed, err := s.Watch(ctx, v.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ClaimFrontendRequest(ctx, v.SessionID, v.FrontendSessionID, "watch"); !ok || err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	case <-ctx.Done():
		t.Fatal("namespace migration lost live events")
	}
	if _, err := s.Update(v.SessionID, func(v *Session) error { v.State = StateCompleted; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reopen(v.SessionID, v.WorkingDirectory, ModeAuto); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ClaimFrontendRequest(ctx, v.SessionID, v.FrontendSessionID, "watch"); ok || err != nil {
		t.Fatal("reopen replayed claimed request", err)
	}
}

func TestLivePublishedCoreCannotReadOrRewriteJournalSessions(t *testing.T) {
	previous := os.Getenv("IVOAI_PRECLAIM_CORE_PATH")
	if previous == "" {
		t.Skip("explicit verified published v0.16.0 reader required")
	}
	data, err := os.ReadFile(previous)
	digest := sha256.Sum256(data)
	if err != nil || hex.EncodeToString(digest[:]) != "a933d4d313930066409c77682a8cc26b574b5c259174e1cb68fac6b890c9d2ef" {
		t.Fatal("published rollback Core digest mismatch")
	}
	root := t.TempDir()
	environment := []string{"PATH=/usr/bin:/bin", "HOME=" + root, "NO_COLOR=1"}
	for _, name := range []string{"CONFIG", "DATA", "STATE", "CACHE"} {
		environment = append(environment, "XDG_"+name+"_HOME="+filepath.Join(root, name))
	}
	s, v := requestJournalFixture(t)
	target := Store{Root: filepath.Join(root, "STATE", "ivoai", "sessions")}
	if err := target.Create(v); err != nil {
		t.Fatal(err)
	}
	s = target
	if ok, err := s.ClaimFrontendRequest(context.Background(), v.SessionID, v.FrontendSessionID, "oldest"); !ok || err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.path(v.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(previous, "session", "list", "--json")
	command.Env = environment
	out, err := command.Output()
	if err != nil {
		t.Fatal("prior stable reader failed")
	}
	var values []Session
	if json.Unmarshal(out, &values) != nil || len(values) != 0 {
		t.Fatal("old Core discovered journal-bound sessions")
	}
	command = exec.Command(previous, "session", "show", "--json", v.SessionID)
	command.Env = environment
	if command.Run() == nil {
		t.Fatal("old Core could open replay-protected session")
	}
	after, _ := os.ReadFile(s.path(v.SessionID))
	if !bytes.Equal(before, after) {
		t.Fatal("rollback reader rewrote protected state")
	}
	if ok, err := (Store{Root: s.Root}).ClaimFrontendRequest(context.Background(), v.SessionID, v.FrontendSessionID, "oldest"); ok || err != nil {
		t.Fatal("reapply lost permanent claim", err)
	}
}
