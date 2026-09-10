package session

import (
	"context"
	"testing"
	"time"
)

func TestDecisionsPersistAndDoNotCrossSessionsOrKinds(t *testing.T) {
	s, v := fixtureSession(t, t.TempDir())
	if err := s.Create(v); err != nil {
		t.Fatal(err)
	}
	const id = "plan_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := s.RequestDecision(v.SessionID, id, "routing"); err == nil {
		t.Fatal("plan/routing capability confusion")
	}
	if err := s.RequestDecision(v.SessionID, id, "plan"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := s.Get(v.SessionID)
	if err != nil || len(reloaded.Decisions) != 1 || reloaded.Decisions[0].State != "pending" {
		t.Fatal("lost pending approval")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := s.WaitDecision(ctx, v.SessionID, id); err == nil {
		t.Fatal("started without approval")
	}
	if err := s.ResolveDecision(v.SessionID, id, true); err == nil {
		t.Fatal("cancelled decision reactivated")
	}
	if err := s.RequestDecision(v.SessionID, id, "plan"); err == nil {
		t.Fatal("reused decision identity")
	}
}

func TestDecisionApproveAndReject(t *testing.T) {
	for _, allow := range []bool{true, false} {
		s, v := fixtureSession(t, t.TempDir())
		if err := s.Create(v); err != nil {
			t.Fatal(err)
		}
		const id = "routing_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		if err := s.RequestDecision(v.SessionID, id, "routing"); err != nil {
			t.Fatal(err)
		}
		if err := s.ResolveDecision(v.SessionID, id, allow); err != nil {
			t.Fatal(err)
		}
		if err := s.WaitDecision(context.Background(), v.SessionID, id); (err == nil) != allow {
			t.Fatalf("allow=%v err=%v", allow, err)
		}
		if err := s.ResolveDecision(v.SessionID, id, !allow); err == nil {
			t.Fatal("approval changed after resolution")
		}
	}
}
