package opencodebridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ivo-lopes/ivoai/internal/observability"
)

// operationalEvents exposes only the validated bounded session journal, never
// executor output or upstream SSE payloads. Slow readers have a write deadline;
// changes coalesce upstream and a reconnect retrieves retained history.
func (b *Bridge) operationalEvents(w http.ResponseWriter, r *http.Request) {
	if b.eventSnapshot == nil || b.eventSubscribe == nil {
		http.Error(w, "events unavailable", http.StatusServiceUnavailable)
		return
	}
	cursor := r.Header.Get("Last-Event-ID")
	if cursor == "" {
		cursor = r.URL.Query().Get("after")
	}
	var after uint64
	if cursor != "" {
		var err error
		after, err = strconv.ParseUint(cursor, 10, 64)
		if err != nil {
			http.Error(w, "invalid event cursor", 400)
			return
		}
	}
	select {
	case b.eventSlots <- struct{}{}:
		defer func() { <-b.eventSlots }()
	default:
		http.Error(w, "event subscriber limit", 429)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	changes, err := b.eventSubscribe(ctx)
	if err != nil {
		http.Error(w, "events unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	lastSession := r.URL.Query().Get("session_id")
	started := false
	send := func() bool {
		page, err := b.eventSnapshot(after)
		if err != nil {
			return false
		}
		if lastSession != "" && page.SessionID != lastSession {
			page, err = b.eventSnapshot(0)
			if err != nil {
				return false
			}
			page.Gap = true
		}
		if len(page.Events) > 128 {
			return false
		}
		for _, event := range page.Events {
			if event.Validate() != nil || event.SessionID != page.SessionID {
				return false
			}
		}
		view := b.statusFn()
		view.UpdatedAt = time.Now().UTC()
		// A conversation may change between reading its journal and status. Never
		// attach another session's events to the visible snapshot.
		if page.SessionID != "" && view.SessionID != page.SessionID {
			return true
		}
		b.mu.Lock()
		if b.requirePromptGate {
			view.PromptReadiness = b.promptReadiness.State
			view.PromptMissing = append([]string(nil), b.promptReadiness.Missing...)
		}
		b.mu.Unlock()
		body, err := json.Marshal(struct {
			Replay observability.Page `json:"replay"`
			Status Status             `json:"status"`
		}{page, view})
		if err != nil || len(body) > 512<<10 {
			return false
		}
		_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err = fmt.Fprintf(w, "id: %d\nevent: operational\ndata: %s\n\n", page.LatestSequence, body); err != nil {
			return false
		}
		if controller.Flush() != nil {
			return false
		}
		started = true
		after = page.LatestSequence
		lastSession = page.SessionID
		return true
	}
	if !send() {
		if !started {
			http.Error(w, "operational snapshot unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.eventStop:
			return
		case _, ok := <-changes:
			// Coalesce bursts: at most ten status/replay projections per second.
			pause := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				pause.Stop()
				return
			case <-b.eventStop:
				pause.Stop()
				return
			case <-pause.C:
			}
			if !ok || !send() {
				return
			}
		case <-heartbeat.C:
			if !send() {
				return
			}
		}
	}
}
