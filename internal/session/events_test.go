package session

import (
	"context"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"testing"
	"time"
)

func TestObservationReplaySequenceAndGap(t *testing.T) {
	s := Session{SessionID: "sess_0123456789abcdef0123456789abcdef"}
	for i := 0; i < 140; i++ {
		if err := AppendObservation(&s, observability.Event{Category: observability.CategoryWorker, Operation: observability.OperationWorkerLifecycle, State: observability.StateRunning}); err != nil {
			t.Fatal(err)
		}
	}
	if !ReplayObservations(s, 0).Gap {
		t.Fatal("initial truncated history not disclosed")
	}
	page := ReplayObservations(s, 1)
	if !page.Gap || page.LatestSequence != 140 || page.OldestSequence != 13 || len(page.Events) != 128 {
		t.Fatalf("bad replay: %+v", page)
	}
	if page.Events[0].SchemaVersion != 1 || page.Events[0].Sequence != 13 {
		t.Fatal("missing identity")
	}
	page = ReplayObservations(s, 139)
	if page.Gap || len(page.Events) != 1 || page.Events[0].Sequence != 140 {
		t.Fatal("cursor not respected")
	}
	before := s.ObservationSequence
	if AppendObservation(&s, observability.Event{}) == nil || s.ObservationSequence != before {
		t.Fatal("invalid event advanced sequence")
	}
}

func TestObservationLegacyIdentityStable(t *testing.T) {
	s := Session{SessionID: "sess_0123456789abcdef0123456789abcdef", Observability: []observability.Event{{ObservedAt: time.Now(), Category: observability.CategoryWorker, Operation: observability.OperationWorkerLifecycle, State: observability.StateRunning}}}
	before := ReplayObservations(s, 0)
	if err := AppendObservation(&s, observability.Event{Category: observability.CategoryWorker, Operation: observability.OperationWorkerLifecycle, State: observability.StateCompleted}); err != nil {
		t.Fatal(err)
	}
	after := ReplayObservations(s, 0)
	if before.Events[0].Sequence != after.Events[0].Sequence || after.LatestSequence != 2 {
		t.Fatal("unstable legacy identities")
	}
}

func TestObservationWatchCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := Store{Root: t.TempDir()}
	changes, err := store.Watch(ctx, "sess_0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case _, ok := <-changes:
		if ok {
			t.Fatal("unexpected notification")
		}
	case <-time.After(time.Second):
		t.Fatal("watcher did not stop")
	}
}

func TestObservationSequenceSurvivesStoreRestartAndWatch(t *testing.T) {
	store, value := fixtureSession(t, t.TempDir())
	if err := store.Create(value); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes, err := store.Watch(ctx, value.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	appendEvent := func(s *Session) error {
		return AppendObservation(s, observability.Event{Category: observability.CategoryWorker, Operation: observability.OperationWorkerLifecycle, State: observability.StateRunning})
	}
	if _, err = store.Update(value.SessionID, appendEvent); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("atomic write not observed")
	}
	restarted := Store{Root: store.Root}
	next, err := restarted.Update(value.SessionID, appendEvent)
	if err != nil {
		t.Fatal(err)
	}
	if next.ObservationSequence != 2 || next.Observability[1].Sequence != 2 {
		t.Fatal("restart lost cursor")
	}
}

func TestObservationPlanCorrelation(t *testing.T) {
	s := Session{SessionID: "sess_0123456789abcdef0123456789abcdef", PlanID: "plan_0123456789abcdef0123456789abcdef"}
	if err := AppendObservation(&s, observability.Event{Category: observability.CategoryWorker, Operation: observability.OperationWorkerLifecycle, State: observability.StatePending}); err != nil {
		t.Fatal(err)
	}
	if s.Observability[0].PlanID != s.PlanID {
		t.Fatal("missing plan correlation")
	}
}

func TestDecisionObservationOnlyMetadata(t *testing.T) {
	store, value := fixtureSession(t, t.TempDir())
	if err := store.Create(value); err != nil {
		t.Fatal(err)
	}
	const id = "plan_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := store.RequestDecision(value.SessionID, id, "plan"); err != nil {
		t.Fatal(err)
	}
	if err := store.ResolveDecision(value.SessionID, id, true); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.Get(value.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Observability) != 2 || reloaded.Observability[0].State != observability.StateApprovalRequired || reloaded.Observability[1].State != observability.StateAllowed {
		t.Fatal("approval transitions not recorded")
	}
}
