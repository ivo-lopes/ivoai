package orchestration

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestNativeLifecycleNeedsNoExternalCoordinatorAndRejectsOtherOwner(t *testing.T) {
	root := t.TempDir()
	id, _ := session.NewID()
	now := time.Now().UTC()
	store := session.Store{Root: filepath.Join(root, "sessions")}
	v := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true, Coordinator: "native", InitialPlanner: "codex", CurrentPrimary: "codex", PrimaryExecutor: "codex", WorkingDirectory: root, PrimaryModel: session.UnknownModel(), Workers: []session.Worker{}, MaxWorkers: 8, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected", State: session.StateStarting}
	if err := store.Create(v); err != nil {
		t.Fatal(err)
	}
	n := NativeOrchestrator{Store: store, SessionID: id}
	swarm, err := n.Initialize(context.Background(), 8)
	if err != nil || !swarm.Healthy || swarm.ID != "native_"+id {
		t.Fatal(swarm, err)
	}
	primary, err := n.RegisterLifecycle(context.Background(), "primary", id)
	if err != nil {
		t.Fatal(err)
	}
	if err := n.CancelLifecycle(context.Background(), primary); err != nil {
		t.Fatal(err)
	}
	if _, err := n.RegisterLifecycle(context.Background(), "worker", "not-owned"); err == nil {
		t.Fatal("foreign lifecycle registered")
	}
	if err := n.CancelLifecycle(context.Background(), "native_other_primary"); err == nil {
		t.Fatal("foreign lifecycle cancelled")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := n.Initialize(ctx, 8); err == nil {
		t.Fatal("cancelled initialization succeeded")
	}
}

func TestHostResourceObservationAndUnknownFallback(t *testing.T) {
	h := parseHostResources(16, []byte("MemAvailable: 8388608 kB\n"), []byte("2.0 1.0 0.5 1/100 1234\n"), []byte("some avg10=60.00 avg60=50 total=1\n"))
	if h.AvailableMemoryBytes != 8<<30 || h.Load != 2 || h.IOPressure != .6 {
		t.Fatalf("invalid parsed metadata: %+v", h)
	}
	if n := Concurrency(parseHostResources(16, nil, nil, nil), ConcurrencyInputs{Runnable: 10, ProviderSlots: 10, WorkerMemoryBytes: 1 << 30}); n != 1 {
		t.Fatal("unknown resources did not degrade", n)
	}
}
