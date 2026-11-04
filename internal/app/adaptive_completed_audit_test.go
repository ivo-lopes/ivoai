package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func completedSummaryFixture(t *testing.T) (*localMetadataFixture, session.Session, []byte) {
	t.Helper()
	f := newAdaptiveFixture(t)
	v, err := f.store.Get(f.id)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	zero := 0
	digest := strings.Repeat("a", 64)
	v.PlanID = "plan_0123456789abcdef0123456789abcdef"
	v.PrimaryTurn = &core.PrimaryTurnReceipt{ID: "receipt_review", TurnID: "turn_review", AttemptID: "attempt_review", FrontendSessionID: v.FrontendSessionID, Executor: "codex", Mode: core.PrimaryClosed, PlanID: v.PlanID, ConfigDigest: digest, StateDigest: digest, ScopeDigest: digest}
	v.Tasks = []session.TaskMetadata{{ID: "inspect", Role: "research", Tier: "LIGHT", State: session.StateCompleted, Model: session.UnknownModel()}}
	v.ExecutorSessionID = "owned_native_thread"
	a := opencodebridge.TurnAttempt{ID: "attempt_review", TurnID: "turn_review", SessionID: v.SessionID, Frontend: v.Frontend, FrontendSessionID: v.FrontendSessionID, RequestedExecutor: "codex", EffectiveExecutor: "codex", State: "completed", EndedAt: &now, ExitCode: &zero, FinalResponse: true, NativeSessionID: v.ExecutorSessionID, Trace: &core.ExecutionTrace{Completion: true, FinalResponse: true, ExitCode: 0}}
	raw, _ := json.Marshal(a)
	if err := f.store.AppendTurnAttempt(context.Background(), v.SessionID, raw); err != nil {
		t.Fatal(err)
	}
	a.Trace = nil
	summary, _ := json.Marshal(a)
	v.TurnAttempts = []json.RawMessage{summary}
	return f, v, raw
}

func TestAdaptiveCompletedDAGRecoversExactJournalTraceAfterProductionCompaction(t *testing.T) {
	f, v, raw := completedSummaryFixture(t)
	out, err := prepareAdaptiveCompletedPlan(context.Background(), f.store, v)
	if err != nil || out.PlanID != "" || out.CompletedPrimaryPlan == nil {
		t.Fatal("production compacted DAG did not become owned history", err)
	}
	archived, err := f.store.ReadCompletedPrimaryPlan(context.Background(), v.SessionID, *out.CompletedPrimaryPlan)
	if err != nil || len(archived.Session.TurnAttempts) != 1 || !bytes.Equal(archived.Session.TurnAttempts[0], raw) {
		t.Fatal("archive did not preserve exact audited terminal event", err)
	}
	if !bytes.Equal(out.TurnAttempts[0], v.TurnAttempts[0]) {
		t.Fatal("active projection expanded old traces again")
	}
}

func TestAdaptiveCompletedDAGRejectsChangedSummaryOrAudit(t *testing.T) {
	for _, kind := range []string{"summary", "missing", "corrupt", "foreign", "duplicate_terminal"} {
		t.Run(kind, func(t *testing.T) {
			f, v, raw := completedSummaryFixture(t)
			v.RequestJournalVersion = 1
			switch kind {
			case "summary":
				var a opencodebridge.TurnAttempt
				_ = json.Unmarshal(v.TurnAttempts[0], &a)
				a.NativeSessionID = "different"
				v.TurnAttempts[0], _ = json.Marshal(a)
			case "missing":
				if err := os.RemoveAll(filepath.Join(f.store.Root, "turn-attempts", v.SessionID)); err != nil {
					t.Fatal(err)
				}
			default:
				var envelopePath string
				if err := filepath.WalkDir(filepath.Join(f.store.Root, "turn-attempts", v.SessionID), func(path string, d os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if !d.IsDir() && strings.HasSuffix(path, ".json") {
						envelopePath = path
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				if kind == "corrupt" {
					if err := os.WriteFile(envelopePath, []byte("corrupt"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if kind == "foreign" {
					body, _ := os.ReadFile(envelopePath)
					body = bytes.Replace(body, []byte(v.SessionID), []byte("foreign_owner"), 1)
					if err := os.WriteFile(envelopePath, body, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if kind == "duplicate_terminal" {
					var a opencodebridge.TurnAttempt
					_ = json.Unmarshal(raw, &a)
					a.Trace.Events = 123
					other, _ := json.Marshal(a)
					if err := f.store.AppendTurnAttempt(context.Background(), v.SessionID, other); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := prepareAdaptiveCompletedPlan(context.Background(), f.store, v); err == nil {
				t.Fatal("unproven audit promoted a completed DAG")
			}
		})
	}
}
