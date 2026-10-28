package core

import (
	"encoding/hex"
	"errors"
	"strings"
)

type PrimaryTurnMode string

const (
	PrimaryReadOnly PrimaryTurnMode = "primary_read_only"
	PrimaryDAG      PrimaryTurnMode = "dag"
	PrimaryClosed   PrimaryTurnMode = "closed"
)

// PrimaryTurnReceipt is bounded operational metadata in the existing session
// store, not a credential or an authorization token. Host callbacks must check
// the live session, configuration, executor and source boundary on every use.
type PrimaryTurnReceipt struct {
	ID                string          `json:"id"`
	TurnID            string          `json:"turn_id"`
	AttemptID         string          `json:"attempt_id"`
	FrontendSessionID string          `json:"frontend_session_id"`
	Executor          string          `json:"executor"`
	Mode              PrimaryTurnMode `json:"mode"`
	PlanID            string          `json:"plan_id,omitempty"`
	ConfigDigest      string          `json:"config_digest"`
	StateDigest       string          `json:"state_digest"`
	ScopeDigest       string          `json:"scope_digest"`
}

type PrimaryTurnIdentity struct {
	ID, TurnID, AttemptID, FrontendSessionID string
}

func (r PrimaryTurnReceipt) Identity() PrimaryTurnIdentity {
	return PrimaryTurnIdentity{r.ID, r.TurnID, r.AttemptID, r.FrontendSessionID}
}

func (r PrimaryTurnReceipt) Validate() error {
	for _, value := range []string{r.ID, r.TurnID, r.AttemptID, r.FrontendSessionID} {
		if value == "" || len(value) > 128 || strings.ContainsAny(value, "\x00\x1b\r\n") {
			return errors.New("invalid primary turn identity")
		}
	}
	if r.Executor == "" || len(r.Executor) > 64 || strings.ContainsAny(r.Executor, "\x00\x1b\r\n") || r.Mode != PrimaryReadOnly && r.Mode != PrimaryDAG && r.Mode != PrimaryClosed {
		return errors.New("invalid primary turn policy")
	}
	if len(r.PlanID) > 80 || strings.ContainsAny(r.PlanID, "\x00\x1b\r\n") || r.Mode == PrimaryReadOnly && r.PlanID != "" {
		return errors.New("invalid primary turn plan binding")
	}
	for _, digest := range []string{r.ConfigDigest, r.StateDigest, r.ScopeDigest} {
		if len(digest) != 64 {
			return errors.New("invalid primary turn binding")
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return errors.New("invalid primary turn binding")
		}
	}
	return nil
}
