package session

import (
	"errors"

	"github.com/ivo-lopes/ivoai/internal/core"
)

// PrimaryTurnActive is shared by the host and the existing scheduler. It grants
// nothing by itself; callers still perform their normal policy checks.
func PrimaryTurnActive(v Session) bool {
	r := v.PrimaryTurn
	return r != nil && r.Validate() == nil && r.Executor == "codex" && r.Mode != core.PrimaryClosed &&
		v.Active() && v.State == StateRunning && v.Mode == ModeAuto && v.Coordinator == "native" &&
		!v.ProviderExecution && !v.RecoveryRequested && v.TurnState == "running" &&
		v.ObservationTurnID == r.TurnID && v.ObservationAttemptID == r.AttemptID &&
		v.Frontend == "codex" && v.FrontendSessionID == r.FrontendSessionID &&
		v.CurrentPrimary == r.Executor && v.PrimaryExecutor == r.Executor
}

// EnterPrimaryDAG is monotonic for an admitted turn. Even an invalid/rejected
// plan request consumes the simple completion path; it must never silently
// return to primary-only completion after starting the plan protocol.
// Legacy sessions retain their existing DAG admission behavior.
func (s Store) EnterPrimaryDAG(id string, expected ...core.PrimaryTurnIdentity) error {
	initial, err := s.Get(id)
	if err != nil {
		return err
	}
	if initial.PrimaryTurn == nil {
		if len(expected) != 0 {
			return errors.New("PRIMARY_TURN_DENIED: stale turn capability")
		}
		return err
	}
	_, err = s.Update(id, func(v *Session) error {
		if v.PrimaryTurn == nil {
			if len(expected) != 0 {
				return errors.New("PRIMARY_TURN_DENIED: stale turn capability")
			}
			return nil
		}
		if len(expected) != 1 || !PrimaryTurnActive(*v) || v.PrimaryTurn.Identity() != expected[0] {
			return errors.New("PRIMARY_TURN_DENIED: admitted turn is no longer active")
		}
		v.PrimaryTurn.Mode = core.PrimaryDAG
		v.CurrentPhase = "planning"
		return nil
	})
	return err
}

// RetirePrimaryTurn clears only a closed operational pointer from a different
// recorded turn. Attempts/checkpoints/plans/history remain untouched. A retry
// within the same TurnID cannot revive a consumed receipt.
func (s Store) RetirePrimaryTurn(id string) error {
	v, err := s.Get(id)
	if err != nil || v.PrimaryTurn == nil {
		return err
	}
	_, err = s.Update(id, func(v *Session) error {
		if v.PrimaryTurn == nil {
			return nil
		}
		if v.PrimaryTurn.Mode != core.PrimaryClosed || v.ObservationTurnID == "" || v.ObservationTurnID == v.PrimaryTurn.TurnID || v.ObservationAttemptID == "" || v.TurnState != "running" {
			return errors.New("PRIMARY_TURN_DENIED: prior turn capability is not retireable")
		}
		v.PrimaryTurn = nil
		return nil
	})
	return err
}
