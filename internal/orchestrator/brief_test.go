package orchestrator

import (
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestScopedBriefNeverBroadcastsWholeSession(t *testing.T) {
	shared := session.SharedContextBrief{Objective: "whole-private-prompt", Facts: []string{"unrelated-private-fact"}, References: []string{"ref-company-a", "ref-company-b"}}
	a, err := scopedBrief(shared, routing.TaskInput{Task: "Analyze component A", Acceptance: []string{"report the root cause"}, ContextReferences: []string{"ref-company-a"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := scopedBrief(shared, routing.TaskInput{Task: "Analyze component B", Acceptance: []string{"report the root cause"}, ContextReferences: []string{"ref-company-b"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(a, "ref-company-b") || strings.Contains(b, "ref-company-a") {
		t.Fatal("worker references crossed")
	}
	for _, v := range []string{a, b} {
		if strings.Contains(v, shared.Objective) || strings.Contains(v, shared.Facts[0]) {
			t.Fatal("global context broadcast")
		}
	}
}
