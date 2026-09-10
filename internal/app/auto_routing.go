package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/session"
)

// confirmPrimaryRoute is an operator decision, never a model-callable tool.
// It runs before selecting the new child; permission_mode=full cannot waive it.
func confirmPrimaryRoute(ctx context.Context, store session.Store, sessionID, from, to string) error {
	if from == to {
		return nil
	}
	if !quota.Supported(quota.Provider(from)) || !quota.Supported(quota.Provider(to)) {
		return errors.New("PROVIDER_UNAVAILABLE: unsupported primary route")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	id := "routing_" + hex.EncodeToString(nonce[:])
	summary := fmt.Sprintf("Primary route unavailable. Proposed executor change: %s → %s. The primary retains a strong runtime-verified model. Approve this change? Reject to stop without substituting the executor.", from, to)
	if err := store.RequestDecisionSummary(sessionID, id, "routing", summary); err != nil {
		return err
	}
	if _, err := store.Update(sessionID, func(v *session.Session) error {
		v.CurrentPhase = "waiting_for_routing_approval"
		return nil
	}); err != nil {
		return err
	}
	err := store.WaitDecision(ctx, sessionID, id)
	_, updateErr := store.Update(sessionID, func(v *session.Session) error {
		v.CurrentPhase = "conversation"
		if err != nil {
			v.CurrentPhase = "degraded"
		}
		return nil
	})
	if err != nil {
		return err
	}
	return updateErr
}
