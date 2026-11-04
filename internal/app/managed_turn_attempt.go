package app

import (
	"context"
	"encoding/json"

	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/session"
)

// The public host and its integration fixtures use this identical audit and
// projection transition. The journal preserves originals; only the UI metadata
// drops older detailed traces and retains its bounded terminal summaries.
func persistManagedTurnAttempt(ctx context.Context, store session.Store, id string, attempt opencodebridge.TurnAttempt) error {
	attempt.SessionID = id
	body, err := json.Marshal(attempt)
	if err != nil {
		return err
	}
	if err := store.AppendTurnAttempt(ctx, id, body); err != nil {
		return err
	}
	_, err = store.Update(id, func(v *session.Session) error {
		replaced, previousState := false, ""
		for i, raw := range v.TurnAttempts {
			var previous opencodebridge.TurnAttempt
			if json.Unmarshal(raw, &previous) != nil {
				continue
			}
			if previous.ID == attempt.ID {
				previousState = previous.State
				v.TurnAttempts[i] = body
				replaced = true
			} else if previous.Trace != nil {
				previous.Trace = nil
				v.TurnAttempts[i], _ = json.Marshal(previous)
			}
		}
		if !replaced {
			v.TurnAttempts = append(v.TurnAttempts, body)
		}
		if len(v.TurnAttempts) > 32 {
			v.TurnAttempts = v.TurnAttempts[len(v.TurnAttempts)-32:]
		}
		v.TurnState, v.ExecutorExitCode = attempt.State, attempt.ExitCode
		if attempt.Trace != nil {
			v.ExecutorTrace, _ = json.Marshal(attempt.Trace)
		}
		if previousState != attempt.State {
			return appendTurnObservation(v, attempt)
		}
		return nil
	})
	return err
}
