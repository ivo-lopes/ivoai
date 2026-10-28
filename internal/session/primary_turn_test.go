package session

import (
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/core"
)

func TestPrimaryTurnCannotReviveOrCrossRecordedTurn(t *testing.T) {
	store, v := fixtureSession(t, t.TempDir())
	d := strings.Repeat("a", 64)
	r := core.PrimaryTurnReceipt{ID: "receipt_a", TurnID: "turn_a", AttemptID: "attempt_a", FrontendSessionID: "front_a", Executor: "codex", Mode: core.PrimaryReadOnly, ConfigDigest: d, StateDigest: d, ScopeDigest: d}
	v.Mode = ModeAuto
	v.Auto = true
	v.State = StateRunning
	v.Coordinator = "native"
	v.SwarmID = "native_" + v.SessionID
	v.CurrentPrimary = "codex"
	v.InitialPlanner = "codex"
	v.Frontend = "codex"
	v.FrontendSessionID = "front_a"
	v.FrontendSessions = map[string]string{"codex": "front_a"}
	v.TurnState = "running"
	v.ObservationTurnID = "turn_a"
	v.ObservationAttemptID = "attempt_a"
	v.PrimaryTurn = &r
	if err := store.Create(v); err != nil {
		t.Fatal(err)
	}
	if err := store.EnterPrimaryDAG(v.SessionID); err == nil {
		t.Fatal("uncaptured identity entered DAG")
	}
	wrong := r.Identity()
	wrong.AttemptID = "other"
	if err := store.EnterPrimaryDAG(v.SessionID, wrong); err == nil {
		t.Fatal("foreign identity entered DAG")
	}
	if err := store.EnterPrimaryDAG(v.SessionID, r.Identity()); err != nil {
		t.Fatal(err)
	}
	_, err := store.Update(v.SessionID, func(v *Session) error { v.PrimaryTurn.Mode = core.PrimaryClosed; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RetirePrimaryTurn(v.SessionID); err == nil {
		t.Fatal("same turn retired its consumed receipt")
	}
	if err := store.EnterPrimaryDAG(v.SessionID, r.Identity()); err == nil {
		t.Fatal("closed receipt revived")
	}
	_, err = store.Update(v.SessionID, func(v *Session) error {
		v.ObservationTurnID = "turn_b"
		v.ObservationAttemptID = "attempt_b"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RetirePrimaryTurn(v.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := store.EnterPrimaryDAG(v.SessionID, r.Identity()); err == nil {
		t.Fatal("stale identity adopted next legacy turn")
	}
	saved, _ := store.Get(v.SessionID)
	if saved.PrimaryTurn != nil || saved.ObservationTurnID != "turn_b" {
		t.Fatal("retirement changed current turn metadata")
	}
}
