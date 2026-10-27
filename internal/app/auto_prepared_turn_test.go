package app

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestPreparedAutoTurnReleasesResourcesOnEveryExit(t *testing.T) {
	sentinel := errors.New("synthetic lifecycle failure")
	for _, phase := range []string{"resolve", "prepare", "executor", "validate", "empty final", "oversized final", "emit", "success"} {
		t.Run(phase, func(t *testing.T) {
			prepared, run, validated, emitted, released := 0, 0, 0, 0, 0
			resolved := false
			runner := autoTurnRunner{
				resolve: func(request core.ExecutorRequest) (core.ExecutorRequest, error) {
					if phase == "resolve" {
						return request, sentinel
					}
					request.Model, resolved = "resolved-fixture", true
					return request, nil
				},
				prepare: func(_ context.Context, request core.ExecutorRequest) (preparedAutoTurn, error) {
					prepared++
					if !resolved || request.Model != "resolved-fixture" {
						t.Fatal("preparation did not receive the resolved request")
					}
					value := preparedAutoTurn{
						Release: func() { released++ },
						Validate: func() error {
							validated++
							if emitted != 0 || released != 0 {
								t.Fatal("final response escaped or capability closed before validation")
							}
							if phase == "validate" {
								return sentinel
							}
							return nil
						},
						Runner: turnRunnerFunc(func(_ context.Context, _ core.ExecutorRequest, output func(string) error) (core.ExecutorResult, error) {
							run++
							result := core.ExecutorResult{ExecutorSessionID: "thread_fixture"}
							if phase == "empty final" {
								return result, nil
							}
							text := "bounded fixture final"
							if phase == "oversized final" {
								text = strings.Repeat("x", (1<<20)+1)
							}
							if err := output(text); err != nil {
								return result, err
							}
							if phase == "executor" {
								return result, sentinel
							}
							return result, nil
						}),
					}
					if phase == "prepare" {
						// Preparation may acquire a lease before a later step fails.
						return value, sentinel
					}
					return value, nil
				},
			}
			result, err := runner.Run(context.Background(), core.ExecutorRequest{Prompt: "fixture request"}, func(string) error {
				emitted++
				if validated != 1 || released != 0 {
					t.Fatal("final emitter ran before validation or after release")
				}
				if phase == "emit" {
					return sentinel
				}
				return nil
			})
			if (err == nil) != (phase == "success") {
				t.Fatalf("unexpected lifecycle result: %v", err)
			}
			if phase == "resolve" {
				if prepared != 0 || released != 0 || run != 0 {
					t.Fatal("resolution failure acquired execution resources")
				}
				return
			}
			if prepared != 1 || released != 1 || run != map[bool]int{true: 0, false: 1}[phase == "prepare"] {
				t.Fatal("prepared execution resource was leaked, duplicated or used after preparation failure")
			}
			shouldValidate := phase == "validate" || phase == "empty final" || phase == "emit" || phase == "success"
			if validated != map[bool]int{true: 1, false: 0}[shouldValidate] {
				t.Fatal("completion policy was skipped or ran after executor failure")
			}
			shouldEmit := phase == "emit" || phase == "success"
			if emitted != map[bool]int{true: 1, false: 0}[shouldEmit] {
				t.Fatal("partial or unvalidated response was emitted")
			}
			if phase != "prepare" && result.ExecutorSessionID != "thread_fixture" {
				t.Fatal("execution outcome was lost during final validation")
			}
			if (phase == "prepare" || phase == "executor" || phase == "validate" || phase == "emit") && !errors.Is(err, sentinel) {
				t.Fatal("lifecycle error identity changed")
			}
			if phase == "empty final" && core.FailureClass(err) != "FINAL_RESPONSE_MISSING" {
				t.Fatal("missing final response changed its failure class")
			}
		})
	}
}

func TestPreparedAutoTurnKeepsExactBudgetAndExistingPassthrough(t *testing.T) {
	for _, buffered := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing injected passthrough", true: "validated final"}[buffered], func(t *testing.T) {
			var validated bool
			emissions := []string{}
			runner := autoTurnRunner{prepare: func(context.Context, core.ExecutorRequest) (preparedAutoTurn, error) {
				prepared := preparedAutoTurn{Runner: turnRunnerFunc(func(_ context.Context, _ core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
					if err := emit(strings.Repeat("x", (1<<20)-1)); err != nil {
						return core.ExecutorResult{}, err
					}
					return core.ExecutorResult{}, emit("y")
				})}
				if buffered {
					prepared.Validate = func() error { validated = true; return nil }
				}
				return prepared, nil
			}}
			_, err := runner.Run(context.Background(), core.ExecutorRequest{Prompt: "fixture request"}, func(text string) error {
				if buffered && !validated {
					t.Fatal("final response emitted before completion policy")
				}
				emissions = append(emissions, text)
				return nil
			})
			if err != nil || len(strings.Join(emissions, "")) != 1<<20 || len(emissions) != map[bool]int{true: 1, false: 2}[buffered] {
				t.Fatal("1 MiB boundary, buffered final or existing passthrough semantics changed")
			}
		})
	}
}

func TestPreparedAutoTurnConcurrentPoliciesStayWithTheirOwnExecution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := make(chan struct{}, 2)
	resume := make(chan struct{})
	var validations, releases [2]atomic.Int32
	denied := errors.New("fixture second-turn acceptance denied")
	runner := autoTurnRunner{prepare: func(_ context.Context, request core.ExecutorRequest) (preparedAutoTurn, error) {
		index := 0
		if request.FrontendSessionID == "second" {
			index = 1
		}
		return preparedAutoTurn{
			Runner: turnRunnerFunc(func(ctx context.Context, request core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
				started <- struct{}{}
				select {
				case <-resume:
					return core.ExecutorResult{}, emit(request.FrontendSessionID)
				case <-ctx.Done():
					return core.ExecutorResult{}, ctx.Err()
				}
			}),
			Release: func() { releases[index].Add(1) },
			Validate: func() error {
				validations[index].Add(1)
				if index == 1 {
					return denied
				}
				return nil
			},
		}, nil
	}}
	type outcome struct {
		id, output string
		err        error
	}
	done := make(chan outcome, 2)
	for _, id := range []string{"first", "second"} {
		go func() {
			text := ""
			_, err := runner.Run(ctx, core.ExecutorRequest{Prompt: "fixture request", FrontendSessionID: id}, func(part string) error { text += part; return nil })
			done <- outcome{id: id, output: text, err: err}
		}()
	}
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("concurrent preparations did not reach independent execution")
		}
	}
	close(resume)
	for range 2 {
		result := <-done
		if result.id == "first" && (result.err != nil || result.output != "first") {
			t.Fatal("first turn inherited the second turn's completion policy")
		}
		if result.id == "second" && (!errors.Is(result.err, denied) || result.output != "") {
			t.Fatal("denied second turn inherited successful acceptance or emitted a final")
		}
	}
	for index := range 2 {
		if validations[index].Load() != 1 || releases[index].Load() != 1 {
			t.Fatal("concurrent turn lost its validator or resource lifetime")
		}
	}
}

func TestScopedPreparedAutoTurnRetainsProductionDAGValidation(t *testing.T) {
	a := autoTestApp(t, t.TempDir(), "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	cfg, _ := a.Store.Load()
	state, _ := a.Store.LoadState()
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	id, _ := session.NewID()
	now := time.Now().UTC()
	root := t.TempDir()
	value := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true, CurrentPrimary: "codex", InitialPlanner: "codex", PrimaryExecutor: "codex", WorkingDirectory: root, PrimaryModel: session.UnknownModel(), MaxWorkers: 2, State: session.StateRunning, Coordinator: "native", SwarmID: "native_" + id, MemoryStatus: "disabled", ContextStatus: "disabled", ServerStatus: "not-connected"}
	if err := store.Create(value); err != nil {
		t.Fatal(err)
	}
	for _, completion := range []string{"no DAG", "incomplete task", "completed task"} {
		t.Run(completion, func(t *testing.T) {
			base := turnRunnerFunc(func(_ context.Context, _ core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
				if completion != "no DAG" {
					_, err := store.Update(id, func(v *session.Session) error {
						v.CurrentPhase = "synthesizing"
						status := session.StatePrimary
						if completion == "completed task" {
							status = session.StateCompleted
						}
						v.Tasks = []session.TaskMetadata{{ID: "fixture", Role: "research", Tier: "LIGHT", Model: session.UnknownModel(), State: status, ExecutionMode: "primary"}}
						return nil
					})
					if err != nil {
						return core.ExecutorResult{}, err
					}
				}
				return core.ExecutorResult{}, emit("bounded fixture final")
			})
			runner := a.scopeAutoRunner(base, cfg, state, store, id, root, t.TempDir(), "unused", nil, nil, &autoTurnState{}, opencodebridge.DefaultCatalog())
			emitted := false
			_, err := runner.Run(context.Background(), core.ExecutorRequest{Executor: "codex", Prompt: "Read institutional documentation only"}, func(string) error { emitted = true; return nil })
			if (err == nil) != (completion == "completed task") || emitted != (completion == "completed task") {
				t.Fatalf("production DAG completion validation changed: err=%v emitted=%v", err, emitted)
			}
			if completion == "no DAG" && core.FailureClass(err) != "DAG_INCOMPLETE" || completion == "incomplete task" && core.FailureClass(err) != "ACCEPTANCE_INCOMPLETE" {
				t.Fatalf("production failure class changed: %v", err)
			}
		})
	}
}
