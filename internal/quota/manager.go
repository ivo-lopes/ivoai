package quota

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

type Probe interface {
	Probe(context.Context) (ProviderQuota, error)
}

type Manager struct {
	Store  Store
	Probes map[Provider]Probe
	TTL    time.Duration
	Now    func() time.Time
}

func (m Manager) Probe(ctx context.Context, provider Provider, force bool) (ProviderQuota, error) {
	now := m.now()
	if !force {
		if snapshot, err := m.Store.Load(); err == nil {
			if cached, ok := snapshot.Providers[provider]; ok && now.Sub(cached.ObservedAt) <= m.ttl() {
				return cached, nil
			}
		}
	}
	probe := m.Probes[provider]
	if probe == nil {
		return ProviderQuota{Provider: provider, Eligible: false, Reason: "quota probe unavailable", ObservedAt: now}, errors.New("quota probe unavailable")
	}
	value, err := probe.Probe(ctx)
	if value.Provider == "" {
		value.Provider = provider
	}
	if value.ObservedAt.IsZero() {
		value.ObservedAt = now
	}
	if err != nil {
		if cached, loadErr := m.Store.Load(); loadErr == nil {
			if previous, ok := cached.Providers[provider]; ok {
				previous.Eligible = previous.Authenticated && !previous.HardLimitReached
				previous.Reason = "stale quota telemetry: " + platform.Redact(err.Error())
				return previous, err
			}
		}
		return value, err
	}
	value.Eligible = value.Authenticated && !value.HardLimitReached
	if !value.Authenticated && value.Reason == "" {
		value.Reason = "authentication unavailable"
	}
	if value.HardLimitReached && value.Reason == "" {
		value.Reason = "subscription quota exhausted"
	}
	if saveErr := m.Store.Put(value); saveErr != nil {
		return value, saveErr
	}
	return value, nil
}

func (m Manager) Resolve(ctx context.Context, requested Provider, model string, force bool) (Decision, error) {
	if requested != ProviderCodex && requested != ProviderClaude {
		return Decision{}, errors.New("planner must be codex or claude")
	}
	first, firstErr := m.Probe(ctx, requested, force)
	if eligibleForModel(first, model) {
		return Decision{Requested: requested, Resolved: requested, Quota: first}, nil
	}
	alternate := Other(requested)
	second, secondErr := m.Probe(ctx, alternate, force)
	if eligibleForModel(second, model) {
		reason := first.Reason
		if reason == "" {
			reason = "requested provider is unavailable"
		}
		return Decision{Requested: requested, Resolved: alternate, Fallback: true, Reason: reason, Quota: second}, nil
	}
	return Decision{Requested: requested, Reason: combinedReason(first, second)}, errors.Join(firstErr, secondErr, fmt.Errorf("no subscription-backed LLM is currently available"))
}

func eligibleForModel(value ProviderQuota, model string) bool {
	if !value.Eligible || value.HardLimitReached {
		return false
	}
	for _, window := range value.Windows {
		// A provider-level route must not be disabled by an unnamed or unrelated
		// model bucket. Enforce this limit only when the caller selected the exact
		// model identified by authoritative telemetry.
		if model != "" && window.Kind == KindModelWeekly && window.Available && window.RemainingPercent <= 0 && window.Model == model {
			return false
		}
	}
	return true
}

func combinedReason(first, second ProviderQuota) string {
	return fmt.Sprintf("%s: %s; %s: %s", first.Provider, reason(first), second.Provider, reason(second))
}

func reason(value ProviderQuota) string {
	if value.Reason != "" {
		return value.Reason
	}
	return "unavailable"
}

func (m Manager) MarkExhausted(provider Provider, reason string) error {
	value := ProviderQuota{Provider: provider, HardLimitReached: true, Authenticated: true, Eligible: false, Reason: reason, Source: "runtime limit signal", ObservedAt: m.now()}
	if snapshot, err := m.Store.Load(); err == nil {
		if current, ok := snapshot.Providers[provider]; ok {
			value.Windows = current.Windows
			value.Model = current.Model
		}
	}
	return m.Store.Put(value)
}

func (m Manager) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

func (m Manager) ttl() time.Duration {
	if m.TTL < 30*time.Second || m.TTL > 5*time.Minute {
		return 45 * time.Second
	}
	return m.TTL
}
