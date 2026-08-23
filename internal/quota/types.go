// Package quota normalizes subscription-backed quota telemetry without reading
// or storing provider credentials.
package quota

import "time"

type Provider string

const (
	ProviderCodex  Provider = "codex"
	ProviderClaude Provider = "claude"
)

type Kind string

const (
	KindContext     Kind = "context"
	KindSession     Kind = "session"
	KindWeekly      Kind = "weekly"
	KindMonthly     Kind = "monthly"
	KindModelWeekly Kind = "model_weekly"
	KindCredits     Kind = "credits"
)

type TelemetryState string

const (
	TelemetryPending    TelemetryState = "pending"
	TelemetryAvailable  TelemetryState = "available"
	TelemetryNotExposed TelemetryState = "not_exposed"
	TelemetryStale      TelemetryState = "stale"
	TelemetryExhausted  TelemetryState = "exhausted"
)

type Window struct {
	Kind             Kind           `json:"kind"`
	Model            string         `json:"model,omitempty"`
	UsedPercent      float64        `json:"used_percent,omitempty"`
	RemainingPercent float64        `json:"remaining_percent,omitempty"`
	ResetsAt         *time.Time     `json:"resets_at,omitempty"`
	Source           string         `json:"source"`
	ObservedAt       time.Time      `json:"observed_at"`
	Authoritative    bool           `json:"authoritative"`
	Available        bool           `json:"available"`
	State            TelemetryState `json:"state,omitempty"`
}

type ProviderQuota struct {
	Provider         Provider  `json:"provider"`
	Model            string    `json:"model,omitempty"`
	Windows          []Window  `json:"windows"`
	HardLimitReached bool      `json:"hard_limit_reached"`
	Authenticated    bool      `json:"authenticated"`
	Eligible         bool      `json:"eligible"`
	Reason           string    `json:"reason,omitempty"`
	Source           string    `json:"source"`
	ObservedAt       time.Time `json:"observed_at"`
}

type Snapshot struct {
	Providers map[Provider]ProviderQuota `json:"providers"`
	UpdatedAt time.Time                  `json:"updated_at"`
}

type Decision struct {
	Requested Provider      `json:"requested"`
	Resolved  Provider      `json:"resolved,omitempty"`
	Fallback  bool          `json:"fallback"`
	Reason    string        `json:"reason,omitempty"`
	Quota     ProviderQuota `json:"quota"`
}

func Clamp(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func FromUsed(kind Kind, used float64, reset *time.Time, source string, observed time.Time) Window {
	used = Clamp(used)
	state := TelemetryAvailable
	if used >= 100 {
		state = TelemetryExhausted
	}
	return Window{Kind: kind, UsedPercent: used, RemainingPercent: Clamp(100 - used), ResetsAt: reset, Source: source, ObservedAt: observed, Authoritative: true, Available: true, State: state}
}

func Unavailable(kind Kind, state TelemetryState, source string, observed time.Time) Window {
	return Window{Kind: kind, State: state, Source: source, ObservedAt: observed.UTC()}
}

func (w Window) TelemetryState() TelemetryState {
	if w.State != "" {
		return w.State
	}
	if w.Available && w.RemainingPercent <= 0 {
		return TelemetryExhausted
	}
	if w.Available {
		return TelemetryAvailable
	}
	return TelemetryNotExposed
}

func (q ProviderQuota) Window(kind Kind) (Window, bool) {
	for _, value := range q.Windows {
		if value.Kind == kind {
			return value, true
		}
	}
	return Window{Kind: kind}, false
}

func Other(provider Provider) Provider {
	if provider == ProviderCodex {
		return ProviderClaude
	}
	return ProviderCodex
}
