package app

import (
	"context"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/promptgate"
)

func TestAutoTurnAdmitsNaturalInputBeforeExistingExecutionPolicy(t *testing.T) {
	for _, prompt := range []string{
		"Explique como funciona a autenticação deste projeto.",
		"Onde estão implementados os endpoints da API?",
		"Quais serviços dependem deste módulo?",
		"Corrija este bug.", "Implemente paginação neste endpoint.",
	} {
		t.Run(prompt, func(t *testing.T) {
			resolved, prepared, executed := 0, 0, 0
			runner := autoTurnRunner{
				resolve: func(r core.ExecutorRequest) (core.ExecutorRequest, error) {
					resolved++
					return r, nil
				},
				prepare: func(_ context.Context, r core.ExecutorRequest) (preparedAutoTurn, error) {
					prepared++
					if r.Prompt != prompt {
						t.Fatal("input admission rewrote the user's request")
					}
					return preparedAutoTurn{Runner: turnRunnerFunc(func(_ context.Context, request core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
						executed++
						if request.Prompt != prompt {
							t.Fatal("execution did not receive the original natural request")
						}
						return core.ExecutorResult{}, emit("fixture result")
					})}, nil
				},
			}
			if _, err := runner.Run(context.Background(), core.ExecutorRequest{Prompt: prompt}, func(string) error { return nil }); err != nil || resolved != 1 || prepared != 1 || executed != 1 {
				t.Fatal("valid natural request stopped before the existing execution policy")
			}
		})
	}
}

func TestAutoTurnInvalidInputHasNoExecutionEffects(t *testing.T) {
	for _, prompt := range []string{"", " \t\n", "private\x00", string([]byte{0xff}), strings.Repeat("a", promptgate.MaxPromptBytes+1)} {
		runner := autoTurnRunner{
			resolve: func(r core.ExecutorRequest) (core.ExecutorRequest, error) {
				t.Error("invalid input resolved an executor")
				return r, nil
			},
			prepare: func(context.Context, core.ExecutorRequest) (preparedAutoTurn, error) {
				t.Error("invalid input prepared execution")
				return preparedAutoTurn{}, nil
			},
		}
		_, err := runner.Run(context.Background(), core.ExecutorRequest{Prompt: prompt}, func(string) error { t.Error("invalid input emitted a final response"); return nil })
		if err == nil || core.FailureClass(err) != "PROMPT_INPUT_INVALID" || strings.Contains(err.Error(), "private") {
			t.Fatal("invalid input did not produce its bounded non-execution failure")
		}
	}
}
