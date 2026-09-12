package codexfrontend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
)

// The official App Server remains responsible for native turn/history events.
// Its process-local Responses provider only returns the shared core's final
// answer, never native tool calls. The provider cannot run without a matching
// single-use admission made at the App Server transport boundary.
func (f *Facade) responses(w http.ResponseWriter, r *http.Request) {
	if !authorized(r, f.providerToken) {
		http.Error(w, "unauthorized", 401)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxFrame+1))
	if err != nil || len(body) > maxFrame || !json.Valid(body) {
		http.Error(w, "invalid response request", 400)
		return
	}
	f.mu.Lock()
	turn := f.active
	if turn == nil || turn.consumed {
		f.mu.Unlock()
		http.Error(w, "TURN_NOT_ADMITTED", 403)
		return
	}
	turn.consumed = true
	ctx, cancel := context.WithCancel(r.Context())
	turn.cancel = cancel
	f.mu.Unlock()
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	send := func(event any) {
		data, _ := json.Marshal(event)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		if flush, ok := w.(http.Flusher); ok {
			flush.Flush()
		}
	}
	send(map[string]any{"type": "response.created", "response": map[string]any{"id": turn.message}})
	type result struct {
		text string
		err  error
	}
	finished := make(chan result, 1)
	go func() {
		text, err := f.options.Bridge.SubmitTurn(ctx, f.options.SessionID, turn.message, turn.prompt, turn.model, turn.effort)
		finished <- result{text, err}
	}()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	lastProgress := ""
	nextProgress := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case outcome := <-finished:
			if outcome.err != nil {
				f.mu.Lock()
				if f.active == turn {
					f.lastTurnError = outcome.err
				}
				f.mu.Unlock()
				// The shared bridge already bounds and sanitizes these errors.
				send(map[string]any{"type": "response.failed", "response": map[string]any{"id": turn.message, "status": "failed", "error": map[string]any{"code": "IVOAI_TURN_FAILED", "message": outcome.err.Error()}}})
				return
			}
			send(map[string]any{"type": "response.output_item.done", "item": map[string]any{"id": turn.message + "_answer", "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": outcome.text}}}})
			send(map[string]any{"type": "response.completed", "response": map[string]any{"id": turn.message, "status": "completed"}})
			return
		case <-ticker.C:
			f.presentDecisions(ctx, turn)
			if time.Now().After(nextProgress) {
				lastProgress = f.presentProgress(ctx, turn, lastProgress)
				nextProgress = time.Now().Add(time.Second)
			}
		}
	}
}

func safeMetadata(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
	if len(value) > 128 {
		return value[:128]
	}
	return value
}

func (f *Facade) presentProgress(ctx context.Context, turn *admittedTurn, previous string) string {
	probe, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	view, err := f.options.Bridge.OperationalStatus(probe)
	if err != nil {
		return previous
	}
	plan := []any{}
	for index, worker := range view.Workers {
		if index >= 32 {
			break
		}
		state := "pending"
		switch worker.State {
		case "running", "starting":
			state = "inProgress"
		case "completed":
			state = "completed"
		}
		// This is metadata, not task prompts, results or worker transcripts.
		step := fmt.Sprintf("%s %s [%s] %s / %s / %s; skills=%s; MCPs=%s", safeMetadata(worker.ID), safeMetadata(worker.Role), safeMetadata(worker.State), safeMetadata(worker.Executor), safeMetadata(worker.Model), safeMetadata(worker.Effort), safeMetadata(strings.Join(worker.Skills, ",")), safeMetadata(strings.Join(worker.MCPs, ",")))
		plan = append(plan, map[string]any{"step": step, "status": state})
	}
	explanation := fmt.Sprintf("IVOAI · %s · %d tasks · %d active / %d queued / %d done · quota=%s", safeMetadata(view.PlanState), view.TaskCount, view.WorkersActive, view.WorkersQueued, view.WorkersDone, safeMetadata(view.QuotaMode))
	encoded, _ := json.Marshal(map[string]any{"explanation": explanation, "plan": plan})
	if string(encoded) == previous {
		return previous
	}
	f.mu.Lock()
	nativeID := turn.nativeID
	active := f.active == turn
	f.mu.Unlock()
	if !active || nativeID == "" {
		return previous
	}
	f.send(map[string]any{"method": "turn/plan/updated", "params": map[string]any{"threadId": turn.thread, "turnId": nativeID, "explanation": explanation, "plan": plan}})
	return string(encoded)
}

func (f *Facade) presentDecisions(ctx context.Context, turn *admittedTurn) {
	probe, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	decisions, err := f.options.Bridge.PendingDecisions(probe)
	if err != nil {
		return
	}
	for _, decision := range decisions {
		f.mu.Lock()
		key := "ivoai_decision_" + decision.ID
		_, seen := f.decisions[key]
		if f.active != turn || turn.nativeID == "" || seen {
			f.mu.Unlock()
			continue
		}
		f.decisions[key] = decision.ID
		nativeID := turn.nativeID
		f.mu.Unlock()
		f.send(map[string]any{"id": key, "method": "item/tool/requestUserInput", "params": map[string]any{
			"threadId": turn.thread, "turnId": nativeID, "itemId": key, "isBlocking": true,
			"questions": []any{map[string]any{"id": "decision", "header": "IVOAI", "question": decision.Description, "isOther": false, "isSecret": false, "options": []any{
				map[string]any{"label": "Approve", "description": "Approve only this IVOAI plan or routing decision."},
				map[string]any{"label": "Cancel", "description": "Do not authorize this decision."},
			}}},
		}})
	}
}
