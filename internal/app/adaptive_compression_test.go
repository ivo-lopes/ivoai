package app

import (
	"encoding/json"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/session"
	"testing"
)

func TestAdaptiveExactCompressionSnapshotDoesNotMixConcurrentTurns(t *testing.T) {
	state := &autoTurnState{}
	state.setCompression("turn_exact", "attempt_exact", core.CompressionExactRequired)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 1000 {
			state.setCompression("turn_other", "attempt_other", "")
			state.setCompression("turn_exact", "attempt_exact", core.CompressionExactRequired)
		}
	}()
	for range 1000 {
		snapshot := state.compressionSnapshot()
		exact := snapshot.turn == "turn_exact" && snapshot.attempt == "attempt_exact" && snapshot.fidelity == core.CompressionExactRequired
		other := snapshot.turn == "turn_other" && snapshot.attempt == "attempt_other" && snapshot.fidelity == ""
		if !exact && !other {
			t.Fatal("concurrent callback captured another turn's fidelity")
		}
	}
	<-done
}

func TestAdaptiveExactBypassObservationPreservesDefaultAndAvoidsFalseFallback(t *testing.T) {
	policy := sharedKnowledgeCompressionPolicyFor(config.Default(), 0)
	observation := core.SessionObservation{CompressionProvider: "direct"}
	for range 3 {
		event := managedTurnCompressionObservation("codex", observation, policy, core.CompressionExactRequired)
		if event.Provider != "direct" || event.RequestedProvider != "caveman" || event.ProviderSource != config.CompressionSourceDefault || !event.CompressionBypassed || event.FidelityClass != string(core.CompressionExactRequired) || event.CompressionResult != "bypassed" || event.State != observability.StateSelected || event.RoutingReason != observability.ReasonExactRequired || event.FallbackReason != "" {
			t.Fatalf("misleading exact bypass: %+v", event)
		}
		if _, err := observability.Normalize(event); err != nil {
			t.Fatal("existing event contract rejected exact bypass", err)
		}
	}
	ordinary := managedTurnCompressionObservation("codex", observation, policy, "")
	if ordinary.RoutingReason != observability.ReasonCavemanFallback || ordinary.CompressionBypassed {
		t.Fatal("ordinary fallback observation changed")
	}
	actualCompression := managedTurnCompressionObservation("codex", core.SessionObservation{CompressionProvider: "caveman", CompressionUsed: true}, policy, core.CompressionExactRequired)
	if actualCompression.CompressionBypassed || actualCompression.RoutingReason == observability.ReasonExactRequired {
		t.Fatal("observed compression was falsely reported as bypassed")
	}
}

func TestAdaptiveExactMappingRejectsStaleAndForeignMetadata(t *testing.T) {
	policy := autoTurnCompressionPolicy{turn: "turn_current", attempt: "attempt_current", fidelity: core.CompressionExactRequired}
	trace := &core.ExecutionTrace{Executor: "codex", Executable: "/reviewed/codex", PID: 123}
	attempt := opencodebridge.TurnAttempt{TurnID: policy.turn, ID: policy.attempt, FrontendSessionID: "frontend_current", EffectiveExecutor: "codex", NativeSessionID: "thread_current", Trace: trace}
	raw, err := json.Marshal(attempt)
	if err != nil {
		t.Fatal(err)
	}
	value := session.Session{ObservationTurnID: policy.turn, ObservationAttemptID: policy.attempt, PrimaryTurn: &core.PrimaryTurnReceipt{TurnID: policy.turn, AttemptID: policy.attempt, Executor: "codex", FrontendSessionID: "frontend_current"}, TurnAttempts: []json.RawMessage{raw}}
	mapping := opencodebridge.Mapping{Executor: "codex", FrontendSessionID: "frontend_current", ExecutorSessionID: "thread_current", Trace: trace}
	if policy.forMapping(value, mapping) != core.CompressionExactRequired {
		t.Fatal("current owned mapping lost exact selection")
	}
	for _, kind := range []string{"stale turn", "stale attempt", "foreign frontend", "foreign executor", "foreign thread", "older process", "missing trace", "missing attempt"} {
		t.Run(kind, func(t *testing.T) {
			v, m := value, mapping
			switch kind {
			case "stale turn":
				v.ObservationTurnID = "turn_new"
			case "stale attempt":
				v.ObservationAttemptID = "attempt_new"
			case "foreign frontend":
				m.FrontendSessionID = "foreign_frontend"
			case "foreign executor":
				m.Executor = "claude"
			case "foreign thread":
				m.ExecutorSessionID = "foreign_thread"
			case "older process":
				old := *trace
				old.PID = 122
				m.Trace = &old
			case "missing trace":
				m.Trace = nil
			case "missing attempt":
				v.TurnAttempts = nil
			}
			if policy.forMapping(v, m) != "" {
				t.Fatal("stale or foreign mapping inherited exact selection")
			}
		})
	}
}
