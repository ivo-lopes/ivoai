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

// AuthenticationChanged invalidates provider-local quota before optionally
// probing the official client in the new authentication context. No provider
// credentials or account fingerprints are inspected.
func (m Manager) AuthenticationChanged(ctx context.Context, provider Provider, reprobe bool) (ProviderQuota, error) {
	if err := m.Store.Invalidate(provider); err != nil {
		return ProviderQuota{Provider: provider}, err
	}
	if !reprobe {
		return ProviderQuota{Provider: provider}, nil
	}
	return m.Probe(ctx, provider, true)
}

// CanDispatch is the authoritative per-provider/model gate. Unknown telemetry
// is not treated as zero; only authentication, hard limits, or an exact
// authoritative model window can block dispatch.
func (m Manager) CanDispatch(ctx context.Context, provider Provider, model string, force bool) (ProviderQuota, bool, error) {
	value, err := m.Probe(ctx, provider, force)
	if checker, ok := m.Probes[provider].(interface{ CanModel(string) bool }); ok && !checker.CanModel(model) {
		return value, false, err
	}
	if provider == ProviderOpenCode && err != nil {
		return value, false, err
	}
	return value, eligibleForModel(value, model), err
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
				for index := range previous.Windows {
					if previous.Windows[index].Available {
						previous.Windows[index].State = TelemetryStale
					}
				}
				return previous, err
			}
		}
		return value, err
	}
	value.Eligible = value.Authenticated && !value.HardLimitReached
	// Native providers expose no authoritative quota reset. Use a bounded
	// cooldown after an observed runtime limit, not permanent exhaustion or an
	// invented reset window. Attempted-executor exclusion still prevents loops.
	if provider == ProviderOpenCode {
		if snapshot, loadErr := m.Store.Load(); loadErr == nil && snapshot.Providers[provider].HardLimitReached && now.Sub(snapshot.Providers[provider].ObservedAt) < m.ttl() {
			value.HardLimitReached, value.Eligible = true, false
			value.ObservedAt = snapshot.Providers[provider].ObservedAt
			value.Reason = "native provider cooldown; quota telemetry unknown"
		}
	}
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
	return m.ResolveCandidates(ctx, requested, model, force, nil)
}

// ResolveCandidates lets the control plane exclude attempted providers without
// creating a second scheduler. Explicit selections use CanDispatch, never this.
func (m Manager) ResolveCandidates(ctx context.Context, requested Provider, model string, force bool, excluded map[Provider]bool) (Decision, error) {
	if !Supported(requested) {
		return Decision{}, errors.New("unknown executor")
	}
	var failures []error
	var unavailable []ProviderQuota
	for _, provider := range Priority(requested) {
		if excluded[provider] || m.Probes[provider] == nil {
			continue
		}
		value, eligible, err := m.CanDispatch(ctx, provider, model, force)
		if eligible {
			why := ""
			if len(unavailable) > 0 {
				why = unavailable[0].Reason
				if why == "" {
					why = "requested provider is unavailable"
				}
			}
			return Decision{Requested: requested, Resolved: provider, Fallback: provider != requested, Reason: why, Quota: value}, nil
		}
		unavailable = append(unavailable, value)
		failures = append(failures, err)
	}
	return Decision{Requested: requested}, errors.Join(append(failures, fmt.Errorf("no subscription-backed or authenticated native LLM is currently available"))...)
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
