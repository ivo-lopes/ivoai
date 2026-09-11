package opencodebridge

// The terminal adapter deliberately owns no orchestration policy. Both this
// Codex-oriented UI and the managed OpenCode UI submit to Bridge.chat, which
// performs admission before invoking the official structured executor.
import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
)

type TerminalOptions struct {
	SessionID string
	Model     string
	Effort    string
	In        io.Reader
	Out       io.Writer
}

type terminalResult struct {
	content string
	err     error
}

// terminalText excludes terminal control sequences, including embedded ESC.
// It is for rendering only; the original prompt never goes to status or logs.
func terminalText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value)
}

func (b *Bridge) terminalRequest(ctx context.Context, method, path string, body any, result any, sessionID, messageID string) error {
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.URL()+path, input)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-IVOAI-Session", sessionID)
	req.Header.Set("X-IVOAI-Message", messageID)
	// Never forward the process-local token through a redirect or proxy.
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("TURN_TRANSPORT_FAILED: controlled turn interrupted or unavailable")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return errors.New("TURN_RESPONSE_INVALID: response exceeded limit")
	}
	if resp.StatusCode >= 300 {
		var failure struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &failure) == nil && failure.Error.Message != "" {
			return fmt.Errorf("%s: %s", terminalText(failure.Error.Code), terminalText(failure.Error.Message))
		}
		return fmt.Errorf("TURN_REQUEST_FAILED: HTTP %d", resp.StatusCode)
	}
	if result != nil && json.Unmarshal(data, result) != nil {
		return errors.New("TURN_RESPONSE_INVALID: invalid JSON")
	}
	return nil
}

// RunTerminal accepts multiline prompts terminated by /submit. EOF submits a
// final buffered prompt but never grants approval. All decisions stay explicit.
func (b *Bridge) RunTerminal(ctx context.Context, options TerminalOptions) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	model, effort := options.Model, options.Effort
	if model == "" {
		model = "auto"
	}
	if _, ok := b.catalog.Resolve(model, effort); !ok {
		return errors.New("MODEL_UNAVAILABLE: selection not in runtime catalog")
	}
	fmt.Fprintln(options.Out, "IVOAI · Codex Orchestrated\nMultiline prompt: finish with /submit. Acceptance criteria are required.\n/models · /model <id> · /reasoning <effort> · /status · /approve · /reject · /cancel · /exit")
	lines := make(chan string)
	inputError := make(chan error, 1)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(options.In)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
		inputError <- scanner.Err()
	}()
	var prompt strings.Builder
	var active context.CancelFunc
	var pending []PermissionView
	finished := make(chan terminalResult, 1)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	eof, sequence := false, 0
	lastStatus, lastDecision := "", ""
	var lastFailure error
	defer func() {
		if active != nil {
			active()
		}
	}()
	start := func() {
		text := strings.TrimSpace(prompt.String())
		prompt.Reset()
		if text == "" {
			return
		}
		sequence++
		turnCtx, cancel := context.WithCancel(ctx)
		active = cancel
		pending, lastDecision = nil, ""
		messageID := fmt.Sprintf("turn_%d", sequence)
		selectedModel, selectedEffort := model, effort
		go func() {
			var response struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
			}
			body := map[string]any{"model": selectedModel, "reasoning_effort": selectedEffort, "messages": []map[string]string{{"role": "user", "content": text}}}
			err := b.terminalRequest(turnCtx, "POST", "/turn", body, &response, options.SessionID, messageID)
			content := ""
			if len(response.Choices) > 0 {
				content = response.Choices[0].Message.Content
			}
			finished <- terminalResult{content, err}
		}()
	}
	status := func() {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		var view Status
		if b.terminalRequest(probeCtx, "GET", "/status", nil, &view, "", "") == nil {
			line := fmt.Sprintf("Prompt %s | Plan %s (%d tasks) | Workers %d active / %d queued / %d done | Primary %s %s %s | Quota %s", view.PromptReadiness, view.PlanState, view.TaskCount, view.WorkersActive, view.WorkersQueued, view.WorkersDone, view.Primary, view.EffectiveModel, view.EffectiveEffort, view.QuotaMode)
			for _, w := range view.Workers {
				line += fmt.Sprintf("\n  %s %s: %s / %s / %s / %s; skills=%s; MCPs=%s; purposes=%s", w.ID, w.Role, w.State, w.Executor, w.Model, w.Effort, strings.Join(w.Skills, ","), strings.Join(w.MCPs, ","), strings.Join(w.Purposes, ","))
			}
			if line != lastStatus {
				fmt.Fprintln(options.Out, terminalText(line))
				lastStatus = line
			}
		}
		if b.terminalRequest(probeCtx, "GET", "/native-permissions", nil, &pending, "", "") == nil && len(pending) > 0 {
			if pending[0].ID != lastDecision {
				fmt.Fprintf(options.Out, "%s\n/approve or /reject (%s)\n", terminalText(pending[0].Description), terminalText(pending[0].ID))
				lastDecision = pending[0].ID
			}
			if eof {
				active()
				lastFailure = errors.New("APPROVAL_REQUIRED: input closed without an explicit decision")
			}
		}
	}
	for {
		if eof && active == nil {
			return lastFailure
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result := <-finished:
			active()
			active = nil
			pending = nil
			if result.err != nil {
				lastFailure = result.err
				fmt.Fprintln(options.Out, terminalText(result.err.Error()))
			} else {
				lastFailure = nil
				fmt.Fprintln(options.Out, terminalText(result.content))
			}
		case <-ticker.C:
			if active != nil {
				status()
			}
		case line, ok := <-lines:
			if !ok {
				lines = nil
				eof = true
				if err := <-inputError; err != nil {
					return errors.New("PROMPT_TOO_LARGE_OR_INPUT_FAILED")
				}
				if active == nil {
					start()
				}
				continue
			}
			command := strings.TrimSpace(line)
			if command == "/exit" {
				if active != nil {
					return errors.New("TURN_CANCELLED")
				}
				return lastFailure
			}
			if command == "/cancel" {
				prompt.Reset()
				if active != nil {
					active()
				}
				continue
			}
			if command == "/status" {
				status()
				continue
			}
			if command == "/approve" || command == "/reject" {
				if len(pending) == 0 {
					fmt.Fprintln(options.Out, "No pending decision.")
					continue
				}
				err := b.terminalRequest(ctx, "POST", "/native-permissions/reply", map[string]any{"id": pending[0].ID, "allow": command == "/approve"}, nil, "", "")
				if err != nil {
					fmt.Fprintln(options.Out, err)
				} else {
					fmt.Fprintln(options.Out, "Decision recorded.")
					pending = nil
					lastDecision = ""
				}
				continue
			}
			if active != nil {
				fmt.Fprintln(options.Out, "Turn running. Use /approve, /reject, /status or /cancel.")
				continue
			}
			if command == "/models" {
				for _, entry := range b.catalog.Entries() {
					fmt.Fprintf(options.Out, "%s — %s; reasoning=%s\n", terminalText(entry.ID), terminalText(entry.Name), strings.Join(entry.SupportedEfforts, ","))
				}
				continue
			}
			if strings.HasPrefix(command, "/model ") {
				candidate := strings.TrimSpace(strings.TrimPrefix(command, "/model "))
				if _, ok := b.catalog.Resolve(candidate, ""); !ok {
					fmt.Fprintln(options.Out, "MODEL_UNAVAILABLE")
					continue
				}
				model, effort = candidate, ""
				fmt.Fprintln(options.Out, "Model selection recorded; material primary changes require routing approval.")
				continue
			}
			if strings.HasPrefix(command, "/reasoning ") {
				candidate := strings.TrimSpace(strings.TrimPrefix(command, "/reasoning "))
				if _, ok := b.catalog.Resolve(model, candidate); !ok {
					fmt.Fprintln(options.Out, "REASONING_UNAVAILABLE: select a runtime model first.")
					continue
				}
				effort = candidate
				continue
			}
			if command == "/submit" {
				start()
				continue
			}
			if prompt.Len()+len(line)+1 > 1<<20 {
				return errors.New("PROMPT_TOO_LARGE")
			}
			prompt.WriteString(line)
			prompt.WriteByte('\n')
		}
	}
}
