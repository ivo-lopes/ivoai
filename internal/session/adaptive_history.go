package session

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"golang.org/x/sys/unix"
)

type historyAttempt struct {
	ID         string               `json:"attempt_id"`
	SessionID  string               `json:"ivoai_session_id"`
	Frontend   string               `json:"frontend"`
	FrontendID string               `json:"frontend_session_id"`
	Requested  string               `json:"requested_executor"`
	Effective  string               `json:"effective_executor"`
	State      string               `json:"turn_state"`
	Failure    string               `json:"failure_class"`
	Final      bool                 `json:"final_response_present"`
	NativeID   string               `json:"native_session_id"`
	Ended      *time.Time           `json:"ended_at"`
	Trace      *core.ExecutionTrace `json:"trace"`
}

func deniedHistoryAttempt(v Session, a historyAttempt) bool {
	if a.SessionID != "" && a.SessionID != v.SessionID || a.Frontend != "" && a.Frontend != v.Frontend || a.FrontendID != "" && v.FrontendSessionID != "" && a.FrontendID != v.FrontendSessionID || a.Requested != "" && a.Effective != "" && a.Requested != a.Effective {
		return true
	}
	if a.Failure != "" || a.Trace != nil && (a.Trace.Failure != "" || a.Trace.Signal != "") {
		return true
	}
	switch a.State {
	case "running":
		return a.Ended != nil
	case "completed":
		return !a.Final || a.NativeID == "" || a.Trace == nil || !a.Trace.Completion || !a.Trace.FinalResponse || a.Trace.ExitCode != 0
	default:
		return true
	}
}

func deniedHistoryProjection(v Session) bool {
	for _, raw := range v.TurnAttempts {
		var a historyAttempt
		if json.Unmarshal(raw, &a) != nil || deniedHistoryAttempt(v, a) {
			return true
		}
		if a.State == "running" && v.ObservationAttemptID != "" && a.ID != v.ObservationAttemptID {
			return true
		}
	}
	return false
}

// AdaptiveHistoryEligible is a provider-neutral admission condition, never a
// capability grant. A clean bounded projection cannot clear earlier taint or
// prove an already-truncated legacy history.
func AdaptiveHistoryEligible(v Session) bool {
	return !v.AdaptiveHistoryDenied && !v.LegacyAttemptAuditPartial && !(v.RequestJournalVersion == 0 && len(v.TurnAttempts) >= 32)
}

func (s Store) historyMarkerDenied(id string) (bool, error) {
	fd, err := s.privateJournalDirectory("turn-attempts", id, false)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer unix.Close(fd)
	return privateRecordExists(fd, "adaptive-history-denied.json")
}

func (s Store) markHistoryDenied(ctx context.Context, id string) error {
	fd, err := s.privateJournalDirectory("turn-attempts", id, true)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if exists, err := privateRecordExists(fd, "adaptive-history-denied.json"); err != nil || exists {
		return err
	}
	body, _ := json.Marshal(struct {
		Schema int    `json:"schema_version"`
		Owner  string `json:"owner_session_id"`
		Denied bool   `json:"adaptive_history_denied"`
	}{1, id, true})
	if err := writePrivateJournalRecord(ctx, fd, "adaptive-history-denied.json", body); err != nil && !errors.Is(err, unix.EEXIST) {
		return err
	}
	return nil
}
