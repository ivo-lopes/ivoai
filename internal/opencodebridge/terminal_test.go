package opencodebridge

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type decisionRunner struct {
	decision chan bool
	started  atomic.Bool
}

func (r *decisionRunner) Run(ctx context.Context, _ ExecutorRequest, emit func(string) error) (ExecutorResult, error) {
	r.started.Store(true)
	select {
	case allow := <-r.decision:
		if !allow {
			return ExecutorResult{}, errors.New("plan rejected")
		}
		_ = emit("approved fixture result")
		return ExecutorResult{}, nil
	case <-ctx.Done():
		return ExecutorResult{}, ctx.Err()
	}
}

type terminalEvents chan string

func (w terminalEvents) Write(p []byte) (int, error) {
	select {
	case w <- string(p):
	default:
	}
	return len(p), nil
}

func TestTerminalDecisionsRemainExplicit(t *testing.T) {
	for _, choice := range []string{"/approve", "/reject", "EOF"} {
		t.Run(choice, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			runner := &decisionRunner{decision: make(chan bool, 1)}
			var decided atomic.Bool
			bridge, err := Start(Options{Runner: runner, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{} },
				NativePermissions: func() []PermissionView {
					if runner.started.Load() && !decided.Load() {
						return []PermissionView{{ID: "plan_fixture", Description: "Plan ready"}}
					}
					return nil
				},
				ReplyNativePermission: func(_ context.Context, id string, allow bool) error {
					if id != "plan_fixture" {
						return errors.New("wrong decision")
					}
					decided.Store(true)
					runner.decision <- allow
					return nil
				}})
			if err != nil {
				t.Fatal(err)
			}
			defer bridge.Close(context.Background())
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			events := make(terminalEvents, 32)
			done := make(chan error, 1)
			go func() {
				done <- bridge.RunTerminal(ctx, TerminalOptions{SessionID: "decision_fixture", In: reader, Out: events})
			}()
			_, _ = io.WriteString(writer, "fixture\n/submit\n")
			waiting := false
			for !waiting {
				select {
				case value := <-events:
					waiting = strings.Contains(value, "/approve or /reject")
				case <-ctx.Done():
					t.Fatal("decision not presented")
				}
			}
			if decided.Load() {
				t.Fatal("decision implicitly approved")
			}
			if choice != "EOF" {
				_, _ = io.WriteString(writer, choice+"\n")
			}
			_ = writer.Close()
			if err := <-done; (err == nil) != (choice == "/approve") {
				t.Fatalf("choice=%s err=%v", choice, err)
			}
		})
	}
}

func TestTerminalGateBeforeExecutorAndSharedTurn(t *testing.T) {
	for _, frontend := range []string{"codex", "opencode"} {
		for _, input := range []string{"corrija isso", "Read VERSION and report the value. Acceptance: return only the version; do not modify files."} {
			t.Run(frontend+input[:4], func(t *testing.T) {
				runner := &fakeRunner{}
				var attempt TurnAttempt
				bridge, err := Start(Options{Frontend: frontend, RequirePromptGate: true, Runner: runner, PreferredExecutor: "codex", Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{Frontend: frontend} }, Attempt: func(value TurnAttempt) error { attempt = value; return nil }})
				if err != nil {
					t.Fatal(err)
				}
				defer bridge.Close(context.Background())
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				var output bytes.Buffer
				if err := bridge.RunTerminal(ctx, TerminalOptions{SessionID: "frontend_fixture", In: strings.NewReader(input), Out: &output}); err != nil {
					t.Fatal(err)
				}
				runner.mu.Lock()
				calls := len(runner.requests)
				runner.mu.Unlock()
				if input == "corrija isso" {
					if calls != 0 || !strings.Contains(output.String(), "insufficient") {
						t.Fatal("gate bypass or missing rejection")
					}
				} else {
					if calls != 1 || !strings.Contains(output.String(), "bridge ok") || attempt.Frontend != frontend {
						t.Fatalf("shared turn failed: calls=%d frontend=%s", calls, attempt.Frontend)
					}
				}
			})
		}
	}
}

func TestTerminalFailureDoesNotBecomeGracefulSuccess(t *testing.T) {
	bridge, err := Start(Options{Runner: partialFailureRunner{}, PreferredExecutor: "codex", Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{} }})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	var output bytes.Buffer
	err = bridge.RunTerminal(context.Background(), TerminalOptions{SessionID: "failure_fixture", In: strings.NewReader("fixture"), Out: &output})
	if err == nil || strings.Contains(output.String(), "untrusted partial output") {
		t.Fatal("failed turn became success or leaked partial output")
	}
}

func TestTerminalTextStripsControlCharacters(t *testing.T) {
	if strings.ContainsAny(terminalText("hello\x1b[31m\rworld\x00"), "\x1b\r\x00") {
		t.Fatal("terminal escape retained")
	}
}
