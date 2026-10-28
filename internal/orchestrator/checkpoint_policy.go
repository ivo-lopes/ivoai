package orchestrator

import (
	"errors"
	"os"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func (s *Server) checkPrimaryDAGIdentity(v session.Session) error {
	if err := s.checkPrimaryIdentity(v); err != nil {
		return err
	}
	if v.PrimaryTurn != nil && (v.PrimaryTurn.Mode != core.PrimaryDAG || v.PlanID == "" || v.PrimaryTurn.PlanID != v.PlanID) {
		return errors.New("PRIMARY_TURN_DENIED: mutation requires the admitted DAG")
	}
	return nil
}

// Checkpoint bytes and metadata share the session lock with receipt consumption.
// Load/SaveCheckpoint do not take this lock; UpdateCheckpoint would recurse.
// Legacy sessions retain their existing checkpoint/recovery data contract.
func (s *Server) updateCheckpoint(mutate func(*session.Checkpoint) error) error {
	_, err := s.Store.Update(s.SessionID, func(v *session.Session) error {
		if err := s.checkPrimaryDAGIdentity(*v); err != nil {
			return err
		}
		checkpoint, err := s.Store.LoadCheckpoint(s.SessionID)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := mutate(&checkpoint); err != nil {
			return err
		}
		if err := s.Store.SaveCheckpoint(s.SessionID, checkpoint); err != nil {
			return err
		}
		saved, err := s.Store.LoadCheckpoint(s.SessionID)
		if err != nil {
			return err
		}
		v.CheckpointAvailable = true
		v.CheckpointUpdatedAt = &saved.UpdatedAt
		return nil
	})
	return err
}
