package app

import (
	"errors"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/quota"
)

func TestConversationAccountProofSurvivesUnavailableQuotaTelemetry(t *testing.T) {
	lookupCalled := false
	got, err := conversationAccountReference(func() (quota.ProviderQuota, error) {
		return quota.ProviderQuota{}, errors.New("fixture usage endpoint unavailable")
	}, func() (string, error) {
		lookupCalled = true
		return "account_fixture", nil
	})
	if err != nil || got != "account_fixture" || !lookupCalled {
		t.Fatalf("fresh official identity was blocked by quota telemetry: got=%q lookup=%v err=%v", got, lookupCalled, err)
	}
}

func TestConversationAccountProofFailsClosed(t *testing.T) {
	for _, telemetryError := range []bool{false, true} {
		for _, lookupError := range []bool{false, true} {
			got, err := conversationAccountReference(func() (quota.ProviderQuota, error) {
				if telemetryError {
					return quota.ProviderQuota{}, errors.New("fixture telemetry unavailable")
				}
				return quota.ProviderQuota{Authenticated: true}, nil
			}, func() (string, error) {
				if lookupError {
					return "account_unverified", errors.New("fixture identity unavailable")
				}
				return "", nil
			})
			if err == nil || got != "" {
				t.Fatalf("missing/failed identity accepted: telemetryError=%v lookupError=%v", telemetryError, lookupError)
			}
		}
	}
}

func TestConversationSyntheticRunnerKeepsQuotaAdmission(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		_, err := conversationAccountReference(func() (quota.ProviderQuota, error) {
			return quota.ProviderQuota{Authenticated: authenticated}, nil
		}, nil)
		if (err == nil) != authenticated {
			t.Fatalf("synthetic admission changed: authenticated=%v err=%v", authenticated, err)
		}
	}
}
