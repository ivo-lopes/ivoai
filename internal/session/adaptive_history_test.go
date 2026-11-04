package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
)

func cleanHistoryAttempt(v Session, id string) historyAttempt {
	return historyAttempt{ID: id, SessionID: v.SessionID, Frontend: v.Frontend, FrontendID: v.FrontendSessionID, Requested: "codex", Effective: "codex", State: "completed", Final: true, NativeID: "native_fixture", Trace: &core.ExecutionTrace{Completion: true, FinalResponse: true}}
}

func TestAdaptiveHistoryTaintSurvivesEvictionAndRestart(t *testing.T) {
	for _, kind := range []string{"failed", "cancelled", "foreign", "fallback"} {
		t.Run(kind, func(t *testing.T) {
			s, v := requestJournalFixture(t)
			if ok, err := s.ClaimFrontendRequest(context.Background(), v.SessionID, v.FrontendSessionID, "start"); err != nil || !ok {
				t.Fatal(err)
			}
			bad := cleanHistoryAttempt(v, "bad")
			switch kind {
			case "failed":
				bad.State = "failed"
			case "cancelled":
				bad.State = "cancelled"
			case "foreign":
				bad.SessionID = "foreign_owner"
			case "fallback":
				bad.Effective = "claude"
			}
			raw, _ := json.Marshal(bad)
			// Legacy host-owned metadata can contain a foreign delivery. Reading is
			// conservative, and the next authorized write commits its denial.
			if kind == "foreign" {
				_, err := s.Update(v.SessionID, func(v *Session) error { v.TurnAttempts = []json.RawMessage{raw}; return nil })
				if err != nil {
					t.Fatal(err)
				}
			} else if err := s.AppendTurnAttempt(context.Background(), v.SessionID, raw); err != nil {
				t.Fatal(err)
			}
			for i := range 40 {
				good, _ := json.Marshal(cleanHistoryAttempt(v, fmt.Sprint("clean_", i)))
				if err := s.AppendTurnAttempt(context.Background(), v.SessionID, good); err != nil {
					t.Fatal(err)
				}
				_, err := s.Update(v.SessionID, func(v *Session) error {
					v.TurnAttempts = append(v.TurnAttempts, good)
					if len(v.TurnAttempts) > 32 {
						v.TurnAttempts = v.TurnAttempts[len(v.TurnAttempts)-32:]
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			got, err := (Store{Root: s.Root}).Get(v.SessionID)
			if err != nil || !got.AdaptiveHistoryDenied || AdaptiveHistoryEligible(got) || len(got.TurnAttempts) != 32 {
				t.Fatal("eviction restored adaptive authority", err)
			}
			if _, err := s.Update(v.SessionID, func(v *Session) error { v.AdaptiveHistoryDenied = false; v.TurnAttempts = nil; return nil }); err == nil {
				t.Fatal("taint was cleared")
			}
		})
	}
}

func TestAdaptiveHistoryForeignCallbackDoesNotMutateOwner(t *testing.T) {
	s, v := requestJournalFixture(t)
	before, err := os.ReadFile(s.path(v.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	foreign := cleanHistoryAttempt(v, "foreign")
	foreign.FrontendID = "another_frontend"
	raw, _ := json.Marshal(foreign)
	if err := s.AppendTurnAttempt(context.Background(), v.SessionID, raw); err == nil {
		t.Fatal("foreign event accepted")
	}
	after, _ := os.ReadFile(s.path(v.SessionID))
	if !bytes.Equal(before, after) {
		t.Fatal("foreign callback changed session")
	}
	if _, err := os.Lstat(filepath.Join(s.Root, "turn-attempts")); !os.IsNotExist(err) {
		t.Fatal("foreign callback created owner state", err)
	}
}

func historyTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entry := info.Mode().String()
		if !d.IsDir() {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			entry += string(b)
		}
		out[path] = entry
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAdaptiveHistoryReadOnlyLegacyInspectionDoesNotPersist(t *testing.T) {
	s, v := requestJournalFixture(t)
	// Simulate a record created before the monotonic marker existed.
	raw, _ := json.Marshal(cleanHistoryAttempt(v, "legacy"))
	var attempt map[string]any
	_ = json.Unmarshal(raw, &attempt)
	attempt["turn_state"] = "failed"
	raw, _ = json.Marshal(attempt)
	stored, _ := os.ReadFile(s.path(v.SessionID))
	var disk map[string]json.RawMessage
	if err := json.Unmarshal(stored, &disk); err != nil {
		t.Fatal(err)
	}
	disk["turn_attempts"], _ = json.Marshal([]json.RawMessage{raw})
	stored, _ = json.Marshal(disk)
	if err := os.WriteFile(s.path(v.SessionID), stored, 0600); err != nil {
		t.Fatal(err)
	}
	before := historyTree(t, s.Root)
	got, err := s.Get(v.SessionID)
	if err != nil || AdaptiveHistoryEligible(got) {
		t.Fatal("legacy bad history was trusted", err)
	}
	if _, err := s.List(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListBounded(context.Background(), 10, 1<<20); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, historyTree(t, s.Root)) {
		t.Fatal("read-only inspection persisted history/migration")
	}
	if _, err := s.Update(v.SessionID, func(v *Session) error { v.CurrentPhase = "authorized_update"; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "turn-attempts", v.SessionID, "adaptive-history-denied.json")); err != nil {
		t.Fatal("authorized mutation did not retain taint", err)
	}
}

func TestAdaptiveHistoryLegacyFullProjectionIsUnproven(t *testing.T) {
	v := Session{TurnAttempts: make([]json.RawMessage, 32)}
	if AdaptiveHistoryEligible(v) {
		t.Fatal("truncated legacy audit treated as complete")
	}
	v.RequestJournalVersion = 1
	v.LegacyAttemptAuditPartial = true
	if AdaptiveHistoryEligible(v) {
		t.Fatal("legacy partial audit regained authority")
	}
}

func TestAdaptiveHistoryCompletedProjectionMayOmitOlderTrace(t *testing.T) {
	s, v := requestJournalFixture(t)
	if ok, err := s.ClaimFrontendRequest(context.Background(), v.SessionID, v.FrontendSessionID, "start"); err != nil || !ok {
		t.Fatal(err)
	}
	var first []byte
	for i := range 40 {
		a := cleanHistoryAttempt(v, fmt.Sprint("good_", i))
		ended := time.Now().UTC()
		code := 0
		a.Ended = &ended
		a.ExitCode = &code
		full, _ := json.Marshal(a)
		if i == 0 {
			first = full
		}
		if err := s.AppendTurnAttempt(context.Background(), v.SessionID, full); err != nil {
			t.Fatal(err)
		}
		_, err := s.Update(v.SessionID, func(v *Session) error {
			for index, raw := range v.TurnAttempts {
				var prior historyAttempt
				if err := json.Unmarshal(raw, &prior); err != nil {
					return err
				}
				prior.Trace = nil
				v.TurnAttempts[index], _ = json.Marshal(prior)
			}
			v.TurnAttempts = append(v.TurnAttempts, full)
			if len(v.TurnAttempts) > 32 {
				v.TurnAttempts = v.TurnAttempts[len(v.TurnAttempts)-32:]
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.Get(v.SessionID)
		if err != nil || !AdaptiveHistoryEligible(got) {
			t.Fatal("valid trimmed projection lost adaptive eligibility", err)
		}
	}
	events, err := s.ReadTurnAttemptEvents(context.Background(), v.SessionID, "good_0")
	if err != nil || len(events) != 1 || !bytes.Equal(events[0], first) {
		t.Fatal("projection trim lost exact historic trace", err)
	}
}

func TestAdaptiveHistoryNewTerminalEventRequiresOriginalTrace(t *testing.T) {
	for _, kind := range []string{"missing_trace", "no_completion", "no_final_event"} {
		t.Run(kind, func(t *testing.T) {
			s, v := requestJournalFixture(t)
			a := cleanHistoryAttempt(v, "terminal")
			ended := time.Now().UTC()
			code := 0
			a.Ended = &ended
			a.ExitCode = &code
			switch kind {
			case "missing_trace":
				a.Trace = nil
			case "no_completion":
				a.Trace.Completion = false
			case "no_final_event":
				a.Trace.FinalResponse = false
			}
			raw, _ := json.Marshal(a)
			if err := s.AppendTurnAttempt(context.Background(), v.SessionID, raw); err != nil {
				t.Fatal(err)
			}
			got, err := s.Get(v.SessionID)
			if err != nil || AdaptiveHistoryEligible(got) {
				t.Fatal("inconclusive original terminal event granted eligibility", err)
			}
		})
	}
}

func TestAdaptiveHistoryPartialLegacyAuditCannotBeReclassifiedComplete(t *testing.T) {
	s, v := requestJournalFixture(t)
	if _, err := s.Update(v.SessionID, func(v *Session) error {
		for i := range 32 {
			raw, _ := json.Marshal(cleanHistoryAttempt(*v, fmt.Sprint("legacy_", i)))
			v.TurnAttempts = append(v.TurnAttempts, raw)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ClaimFrontendRequest(context.Background(), v.SessionID, v.FrontendSessionID, "new"); err != nil || !ok {
		t.Fatal(err)
	}
	got, err := s.Get(v.SessionID)
	if err != nil || !got.LegacyAttemptAuditPartial || !got.AdaptiveHistoryDenied || AdaptiveHistoryEligible(got) {
		t.Fatal("migration invented complete legacy history", err)
	}
	if _, err := s.Update(v.SessionID, func(v *Session) error { v.LegacyAttemptAuditPartial = false; return nil }); err == nil {
		t.Fatal("metadata reclassified partial history complete")
	}
	if _, err := os.Stat(filepath.Join(s.Root, "turn-attempts", v.SessionID, "adaptive-history-denied.json")); err != nil {
		t.Fatal("partial-history denial is not durable", err)
	}
}
