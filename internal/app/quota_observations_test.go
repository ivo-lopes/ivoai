package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/quota"
)

func TestQuotaObservationsDistinguishMissingFromObservedZero(t *testing.T) {
	for _, tc := range []struct {
		name    string
		window  quota.Window
		present bool
	}{
		{"missing", quota.Window{}, false},
		{"not exposed", quota.Window{State: quota.TelemetryNotExposed}, false},
		{"stale without observation", quota.Window{State: quota.TelemetryStale}, false},
		{"exhausted", quota.Window{Available: true, Authoritative: true, State: quota.TelemetryExhausted}, true},
		{"stale observed", quota.Window{Available: true, State: quota.TelemetryStale, RemainingPercent: 42}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := quotaObservations(quota.ProviderCodex, quota.ProviderQuota{Windows: []quota.Window{tc.window}})[0]
			if (event.RemainingPercent != nil) != tc.present {
				t.Fatalf("remaining percent present=%v, want %v", event.RemainingPercent != nil, tc.present)
			}
			data, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "remaining_percent") != tc.present {
				t.Fatalf("incorrect wire presence: %s", data)
			}
			if tc.present && *event.RemainingPercent != tc.window.RemainingPercent {
				t.Fatal("observed value changed")
			}
		})
	}
}
