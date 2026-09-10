package app

import (
	"context"
	"errors"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
)

type finalFixtureRunner struct{ calls *int }

func (r finalFixtureRunner) Run(_ context.Context, _ opencodebridge.ExecutorRequest, emit func(string) error) (opencodebridge.ExecutorResult, error) {
	*r.calls++
	return opencodebridge.ExecutorResult{}, emit("fixture final response")
}

func TestAutoFinalResponseRequiresDAGAcceptanceAndClosesCapability(t *testing.T) {
	for _, sufficient := range []bool{false, true} {
		for _, complete := range []bool{false, true} {
			calls, released, emitted := 0, 0, 0
			runner := autoTurnRunner{
				prepare: func(context.Context, opencodebridge.ExecutorRequest) (opencodebridge.ExecutorRunner, func(), error) {
					return finalFixtureRunner{&calls}, func() { released++ }, nil
				},
				validate: func() error {
					if !complete {
						return errors.New("DAG_INCOMPLETE")
					}
					return nil
				},
			}
			prompt := "corrija o projeto"
			if sufficient {
				prompt = "Read VERSION and report its value. Acceptance: return only the version."
			}
			_, err := runner.Run(context.Background(), opencodebridge.ExecutorRequest{Prompt: prompt}, func(string) error { emitted++; return nil })
			if (err == nil) != (sufficient && complete) {
				t.Fatal("incorrect turn completion")
			}
			if !sufficient && (calls != 0 || released != 0 || emitted != 0) {
				t.Fatal("work started before intake")
			}
			if sufficient && (calls != 1 || released != 1) {
				t.Fatal("execution capability leaked")
			}
			if !complete && emitted != 0 {
				t.Fatal("partial response advertised as success")
			}
		}
	}
}
