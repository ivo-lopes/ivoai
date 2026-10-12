package opencodebridge

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestOperationalEventsAuthenticatedReplayAndCleanup(t *testing.T) {
	changes := make(chan struct{}, 1)
	cancelled := make(chan struct{})
	bridge, err := Start(Options{Runner: &fakeRunner{}, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{SessionID: "sess_0123456789abcdef0123456789abcdef"} }, EventSnapshot: func(after uint64) (observability.Page, error) {
		return observability.Page{SchemaVersion: 1, LatestSequence: 8, Gap: after == 1, Events: []observability.Event{}}, nil
	}, EventSubscribe: func(ctx context.Context) (<-chan struct{}, error) {
		go func() { <-ctx.Done(); close(cancelled) }()
		return changes, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	response, err := http.Get(bridge.URL() + "/events")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("unauthenticated stream admitted")
	}
	request, _ := http.NewRequest("GET", bridge.URL()+"/events", nil)
	request.Header.Set("Authorization", "Bearer "+bridge.Token())
	request.Header.Set("Last-Event-ID", "1")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data: ") {
			var frame struct {
				Replay observability.Page `json:"replay"`
			}
			if err = json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), &frame); err != nil {
				t.Fatal(err)
			}
			if !frame.Replay.Gap || frame.Replay.LatestSequence != 8 {
				t.Fatal("cursor ignored")
			}
			break
		}
	}
	response.Body.Close()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("subscriber leaked")
	}
}

func TestOperationalEventsReconnectAcrossSessionScopes(t *testing.T) {
	const current = "sess_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const previous = "sess_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	bridge, err := Start(Options{Runner: &fakeRunner{}, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{SessionID: current} }, EventSnapshot: func(after uint64) (observability.Page, error) {
		events := []observability.Event{}
		if after == 0 {
			events = append(events, observability.Event{ObservedAt: time.Now(), SessionID: current, SchemaVersion: 1, Sequence: 1, Category: observability.CategoryWorker, Operation: observability.OperationWorkerLifecycle, State: observability.StateRunning})
		}
		return observability.Page{SchemaVersion: 1, SessionID: current, LatestSequence: 10, Events: events}, nil
	}, EventSubscribe: func(context.Context) (<-chan struct{}, error) { return make(chan struct{}), nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	request, _ := http.NewRequest("GET", bridge.URL()+"/events?session_id="+previous, nil)
	request.Header.Set("Authorization", "Bearer "+bridge.Token())
	request.Header.Set("Last-Event-ID", "5")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data: ") {
			var frame struct {
				Replay observability.Page `json:"replay"`
			}
			if err = json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), &frame); err != nil {
				t.Fatal(err)
			}
			if !frame.Replay.Gap || len(frame.Replay.Events) != 1 {
				t.Fatal("old session cursor suppressed new session events")
			}
			return
		}
	}
	t.Fatal("no replay frame")
}
