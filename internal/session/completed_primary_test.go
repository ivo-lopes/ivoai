package session

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/core"
)

func completedHistoryFixture(t *testing.T) (Store, CompletedPrimaryPlanSnapshot) {
	t.Helper()
	s, v := fixtureSession(t, t.TempDir())
	v.Mode, v.Auto, v.Coordinator, v.Frontend = ModeAuto, true, "native", "codex"
	v.State, v.InitialPlanner, v.CurrentPrimary, v.SwarmID = StateRunning, "codex", "codex", "native_"+v.SessionID
	v.PlanID = "plan_0123456789abcdef0123456789abcdef"
	v.Tasks = []TaskMetadata{{ID: "inspect", Role: "research", Tier: "LIGHT", State: StateCompleted, Model: UnknownModel()}}
	d := strings.Repeat("a", 64)
	v.PrimaryTurn = &core.PrimaryTurnReceipt{ID: "receipt_a", TurnID: "turn_a", AttemptID: "attempt_a", FrontendSessionID: "frontend_a", Executor: "codex", Mode: core.PrimaryClosed, PlanID: v.PlanID, ConfigDigest: d, StateDigest: d, ScopeDigest: d}
	if err := s.Create(v); err != nil {
		t.Fatal(err)
	}
	return s, CompletedPrimaryPlanSnapshot{SchemaVersion: 1, Session: v, Checkpoint: []byte("{\n \"updated_at\": \"2026-11-04T00:00:00Z\"\n}\n")}
}

func TestCompletedPrimaryHistoryIsDurableExactIdempotentAndOwnerScoped(t *testing.T) {
	s, snapshot := completedHistoryFixture(t)
	ctx := context.Background()
	ref, err := s.SaveCompletedPrimaryPlan(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.SaveCompletedPrimaryPlan(ctx, snapshot)
	if err != nil || again != ref {
		t.Fatal("idempotent archive changed", err)
	}
	// No live object or transient ArtifactStore survives this reconstruction.
	read, err := (Store{Root: s.Root}).ReadCompletedPrimaryPlan(ctx, snapshot.Session.SessionID, ref)
	if err != nil || !bytes.Equal(read.Checkpoint, snapshot.Checkpoint) || read.Session.Tasks[0].ID != snapshot.Session.Tasks[0].ID {
		t.Fatal("durable exact history unavailable", err)
	}
	other, _ := NewID()
	if _, err := s.ReadCompletedPrimaryPlan(ctx, other, ref); err == nil {
		t.Fatal("another session recovered owner history")
	}
	path := filepath.Join(s.Root, "completed-primary", snapshot.Session.SessionID, ref.SHA256+".json")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("history not private")
	}
	before, _ := os.ReadFile(s.path(snapshot.Session.SessionID))
	if _, err := s.ReadCompletedPrimaryPlan(ctx, snapshot.Session.SessionID, ref); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(s.path(snapshot.Session.SessionID))
	if !bytes.Equal(before, after) {
		t.Fatal("history read mutated the live journal")
	}
}

func TestCompletedPrimaryHistoryPagesWithoutDeletingOlderPlans(t *testing.T) {
	s, snapshot := completedHistoryFixture(t)
	ctx := context.Background()
	const count = 40
	for i := range count {
		r := *snapshot.Session.PrimaryTurn
		r.ID, r.TurnID, r.AttemptID = fmt.Sprintf("receipt_%d", i), fmt.Sprintf("turn_%d", i), fmt.Sprintf("attempt_%d", i)
		snapshot.Session.PrimaryTurn = &r
		ref, err := s.SaveCompletedPrimaryPlan(ctx, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		snapshot.Session.CompletedPrimaryPlan = &ref
	}
	ref := snapshot.Session.CompletedPrimaryPlan
	for i := count - 1; i >= 0; i-- {
		if ref == nil {
			t.Fatal("history was truncated")
		}
		page, err := s.ReadCompletedPrimaryPlan(ctx, snapshot.Session.SessionID, *ref)
		if err != nil || page.Session.PrimaryTurn.ID != fmt.Sprintf("receipt_%d", i) {
			t.Fatal("history page changed", err)
		}
		ref = page.Session.CompletedPrimaryPlan
	}
	if ref != nil {
		t.Fatal("history chain did not terminate")
	}
}

func TestCompletedPrimaryHistoryRefusesTamperingAndSymlinks(t *testing.T) {
	for _, kind := range []string{"contents", "leaf_link", "parent_link", "invalid_ref", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			s, snapshot := completedHistoryFixture(t)
			ctx := context.Background()
			ref, err := s.SaveCompletedPrimaryPlan(ctx, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			directory := filepath.Join(s.Root, "completed-primary", snapshot.Session.SessionID)
			path := filepath.Join(directory, ref.SHA256+".json")
			switch kind {
			case "contents":
				if err := os.WriteFile(path, bytes.Repeat([]byte("x"), int(ref.Bytes)), 0600); err != nil {
					t.Fatal(err)
				}
			case "leaf_link":
				saved := path + ".original"
				if os.Rename(path, saved) != nil || os.Symlink(saved, path) != nil {
					t.Fatal("link fixture failed")
				}
			case "parent_link":
				saved := directory + ".original"
				if os.Rename(directory, saved) != nil || os.Symlink(saved, directory) != nil {
					t.Fatal("parent fixture failed")
				}
			case "invalid_ref":
				ref.SHA256 = "../../outside"
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			if _, err := s.ReadCompletedPrimaryPlan(ctx, snapshot.Session.SessionID, ref); err == nil {
				t.Fatal("unsafe history recovered")
			}
			if kind != "invalid_ref" {
				if _, err := s.SaveCompletedPrimaryPlan(ctx, snapshot); err == nil {
					t.Fatal("unsafe history overwritten")
				}
			}
		})
	}
}
