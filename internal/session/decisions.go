package session

import (
	"context"
	"errors"
	"regexp"
	"time"
)

const MaxDecisions = 64

// Decision records only the identity and disposition of a specific plan or
// routing proposal. It cannot contain user prompts, model output or secrets.
// Plan approval and quota routing approval are different, non-transferable
// capabilities. Full OpenCode tool permissions do not imply either approval.
type Decision struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Summary    string     `json:"summary,omitempty"`
	State      string     `json:"state"`
	CreatedAt  time.Time  `json:"created_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

var decisionID = regexp.MustCompile(`^(plan|routing)_[a-f0-9]{32}$`)

func validateDecisions(values []Decision) error {
	if len(values) > MaxDecisions {
		return errors.New("too many session decisions")
	}
	seen := map[string]bool{}
	for _, d := range values {
		match := decisionID.FindStringSubmatch(d.ID)
		if len(match) != 2 || match[1] != d.Kind || seen[d.ID] || d.CreatedAt.IsZero() {
			return errors.New("invalid session decision identity")
		}
		seen[d.ID] = true
		if d.Summary != "" && !safeText(d.Summary, 1024) {
			return errors.New("invalid decision metadata")
		}
		switch d.State {
		case "pending":
			if d.ResolvedAt != nil {
				return errors.New("pending decision has resolution")
			}
		case "approved", "rejected", "cancelled":
			if d.ResolvedAt == nil || d.ResolvedAt.Before(d.CreatedAt) {
				return errors.New("invalid decision resolution")
			}
		default:
			return errors.New("invalid decision state")
		}
	}
	return nil
}

func (s Store) RequestDecision(sessionID, id, kind string) error {
	return s.RequestDecisionSummary(sessionID, id, kind, "")
}

// RequestDecisionSummary is a host-only UI boundary. summary must be generated
// from public routing metadata, never a prompt, tool output or model rationale.
func (s Store) RequestDecisionSummary(sessionID, id, kind, summary string) error {
	_, err := s.Update(sessionID, func(v *Session) error {
		if !v.Active() {
			return errors.New("session is not active")
		}
		for _, d := range v.Decisions {
			if d.ID == id {
				return errors.New("decision identity already used")
			}
		}
		if len(v.Decisions) >= MaxDecisions {
			return errors.New("session decision limit reached")
		}
		v.Decisions = append(v.Decisions, Decision{ID: id, Kind: kind, Summary: summary, State: "pending", CreatedAt: time.Now().UTC()})
		return nil
	})
	return err
}

// ResolveDecision is called only by the authenticated user-interface boundary,
// never registered as a model/worker MCP tool.
func (s Store) ResolveDecision(sessionID, id string, allow bool) error {
	state := "rejected"
	if allow {
		state = "approved"
	}
	return s.finishDecision(sessionID, id, state)
}

func (s Store) finishDecision(sessionID, id, state string) error {
	_, err := s.Update(sessionID, func(v *Session) error {
		if !v.Active() {
			return errors.New("session is not active")
		}
		for i := range v.Decisions {
			d := &v.Decisions[i]
			if d.ID == id {
				if d.State != "pending" {
					return errors.New("decision already resolved")
				}
				now := time.Now().UTC()
				d.ResolvedAt = &now
				d.State = state
				return nil
			}
		}
		return errors.New("decision does not belong to this session")
	})
	return err
}

// WaitDecision supports the existing separate stdio orchestration process.
// Polling is bounded at one local metadata read per second, not upstream health
// polling. Cancellation revokes pending authorization; no worker can proceed
// on a stale approval from a different plan/session.
func (s Store) WaitDecision(ctx context.Context, sessionID, id string) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			_ = s.finishDecision(sessionID, id, "cancelled")
			return ctx.Err()
		}
		v, err := s.Get(sessionID)
		if err != nil {
			return err
		}
		if !v.Active() {
			return errors.New("session is not active")
		}
		found := false
		for _, d := range v.Decisions {
			if d.ID != id {
				continue
			}
			found = true
			switch d.State {
			case "approved":
				return nil
			case "rejected":
				return errors.New("PLAN_OR_ROUTING_REJECTED")
			case "cancelled":
				return context.Canceled
			}
		}
		if !found {
			return errors.New("decision does not belong to this session")
		}
		select {
		case <-ctx.Done():
			_ = s.finishDecision(sessionID, id, "cancelled")
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
