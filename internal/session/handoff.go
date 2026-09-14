package session

import (
	"encoding/json"
	"errors"
	"time"
)

type HandoffLineage struct {
	SourceSession       string    `json:"source_session"`
	SourceProvider      string    `json:"source_provider"`
	DestinationProvider string    `json:"destination_provider"`
	ConfirmedAt         time.Time `json:"confirmed_at"`
	Reason              string    `json:"reason"`
}

// HandoffBrief deliberately excludes runtime plans, provider mappings, prompts,
// transcripts, tool grants and worker bodies. It is data, not policy authority.
type HandoffBrief struct {
	Objective   string   `json:"objective"`
	Decisions   []string `json:"decisions,omitempty"`
	Completed   []string `json:"completed,omitempty"`
	Outstanding []string `json:"outstanding,omitempty"`
	Constraints []string `json:"constraints,omitempty"`
	Acceptance  []string `json:"acceptance,omitempty"`
	Blockers    []string `json:"blockers,omitempty"`
	NextStep    string   `json:"next_step,omitempty"`
}

func (s Store) HandoffBrief(id string) (HandoffBrief, error) {
	c, err := s.LoadCheckpoint(id)
	if err != nil {
		return HandoffBrief{}, errors.New("HANDOFF_CHECKPOINT_UNAVAILABLE")
	}
	b := HandoffBrief{Objective: c.Objective, Decisions: c.Decisions, Completed: c.Completed, Outstanding: c.Outstanding, Blockers: c.Blockers, NextStep: c.NextStep, Constraints: c.Constraints, Acceptance: c.Acceptance}
	seenConstraints, seenAcceptance := map[string]bool{}, map[string]bool{}
	if c.Recovery != nil {
		for _, task := range c.Recovery.Plan.Tasks {
			for _, item := range task.Constraints {
				if !seenConstraints[item] {
					b.Constraints = append(b.Constraints, item)
					seenConstraints[item] = true
				}
			}
			for _, item := range task.Acceptance {
				if !seenAcceptance[item] {
					b.Acceptance = append(b.Acceptance, item)
					seenAcceptance[item] = true
				}
			}
		}
	}
	body, err := json.Marshal(b)
	if err != nil || len(body) > 8192 || b.Objective == "" {
		return HandoffBrief{}, errors.New("HANDOFF_BRIEF_UNAVAILABLE_OR_OVERSIZED")
	}
	// The checkpoint loader already applies the shared secret and field checks.
	return b, nil
}

func validLineage(l *HandoffLineage) bool {
	if l == nil {
		return true
	}
	return ValidateID(l.SourceSession) == nil && (l.SourceProvider == "codex" || l.SourceProvider == "claude") && (l.DestinationProvider == "codex" || l.DestinationProvider == "claude") && l.SourceProvider != l.DestinationProvider && !l.ConfirmedAt.IsZero() && safeText(l.Reason, 128)
}
