package app

import (
	"errors"
	"time"

	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/session"
)

var errOperationalQuotaStale = errors.New("stale operational quota")
var errOperationalQuotaUnavailable = errors.New("operational quota unavailable")
var errOperationalQuotaReadFailed = errors.New("operational quota read failed")

// Operational views read existing evidence; displaying progress never launches
// an executor or probes authentication. Dispatch retains its own live gates.
func operationalQuotas(store quota.Store, ttl time.Duration, now time.Time) (map[quota.Provider]quota.ProviderQuota, map[quota.Provider]error) {
	if ttl <= 0 {
		ttl = 45 * time.Second
	}
	snapshot, err := store.Load()
	failures := map[quota.Provider]error{}
	for _, provider := range quota.Providers() {
		if err != nil {
			failures[provider] = errOperationalQuotaReadFailed
			continue
		}
		value, ok := snapshot.Providers[provider]
		if !ok {
			failures[provider] = errOperationalQuotaUnavailable
			continue
		}
		if value.ObservedAt.IsZero() || value.ObservedAt.After(now.Add(time.Second)) || now.Sub(value.ObservedAt) > ttl {
			failures[provider] = errOperationalQuotaStale
		}
	}
	if err != nil {
		return nil, failures
	}
	return snapshot.Providers, failures
}

func appendTurnObservation(value *session.Session, attempt opencodebridge.TurnAttempt) error {
	state := observability.StateRunning
	switch attempt.State {
	case "completed":
		state = observability.StateCompleted
	case "failed":
		state = observability.StateFailed
	case "cancelled":
		state = observability.StateCancelled
	}
	if attempt.State == "running" {
		value.ObservationTurnID, value.ObservationAttemptID = attempt.TurnID, attempt.ID
	}
	return session.AppendObservation(value, observability.Event{Category: observability.CategoryExecutor, Operation: observability.OperationExecutorTurn, State: state, TurnID: attempt.TurnID, AttemptID: attempt.ID, Provider: attempt.EffectiveExecutor, Executor: attempt.EffectiveExecutor})
}
