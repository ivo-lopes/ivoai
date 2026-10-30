//go:build linux

package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func authenticatedSyntheticReady() subscriptionAuthenticatedObservation {
	return subscriptionAuthenticatedObservation{reported: true, reaped: true, waitZero: true, contextActive: true, fixtureUnchanged: true, identityPreserved: true, networkPreserved: true, pidContained: true}
}

func TestPrimaryPrefetchSubscriptionAuthenticatedSyntheticNotCertified(t *testing.T) {
	result := subscriptionAuthenticatedOutcome(authenticatedSyntheticReady())
	if result.Outcome != "SYNTHETIC_DATA_ONLY_NOT_CERTIFIED" || result.AuthenticatedPositiveCertified || result.ClientReported || result.CohortConnected || result.BillingCertified || result.ExclusiveInteractiveCredentialCertified || result.AtomicSnapshotCertified {
		t.Fatal("synthetic observations became official or broader certification")
	}
}

func TestPrimaryPrefetchSubscriptionAuthenticatedRefusals(t *testing.T) {
	cases := []struct {
		name   string
		change func(*subscriptionAuthenticatedObservation)
	}{
		{"missing_report", func(v *subscriptionAuthenticatedObservation) { v.reported = false }},
		{"unreaped", func(v *subscriptionAuthenticatedObservation) { v.reaped = false }},
		{"nonzero_native", func(v *subscriptionAuthenticatedObservation) { v.waitZero = false }},
		{"canceled", func(v *subscriptionAuthenticatedObservation) { v.contextActive = false }},
		{"changed_fixture", func(v *subscriptionAuthenticatedObservation) { v.fixtureUnchanged = false }},
		{"different_identity", func(v *subscriptionAuthenticatedObservation) { v.identityPreserved = false }},
		{"different_network_namespace", func(v *subscriptionAuthenticatedObservation) { v.networkPreserved = false }},
		{"missing_pid_containment", func(v *subscriptionAuthenticatedObservation) { v.pidContained = false }},
		{"api_or_logged_out", func(v *subscriptionAuthenticatedObservation) { v.probeErr = prefetchSubscriptionAuthDenied }},
		{"managed_policy", func(v *subscriptionAuthenticatedObservation) { v.probeErr = prefetchSubscriptionRequirementsDenied }},
		{"custom_route", func(v *subscriptionAuthenticatedObservation) { v.probeErr = prefetchSubscriptionRouteDenied }},
		{"malformed_response", func(v *subscriptionAuthenticatedObservation) { v.probeErr = prefetchSubscriptionRPCInvalid }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := authenticatedSyntheticReady()
			value.officialWitness = true
			tc.change(&value)
			out := subscriptionAuthenticatedOutcome(value)
			if out.Outcome != "STOP_METADATA_QUALIFICATION_NOT_MET" || out.AuthenticatedPositiveCertified || out.DriverModelPromptIssued || out.CohortConnected {
				t.Fatal("failed predicate or RPC was certified")
			}
		})
	}
}

func TestPrimaryPrefetchSubscriptionAuthenticatedClosedPrivacy(t *testing.T) {
	private := "fixture-private-auth-header-email-path-canary"
	value := authenticatedSyntheticReady()
	value.probeErr = errors.New(private)
	result := subscriptionAuthenticatedOutcome(value)
	if result.Reason != "UNKNOWN" {
		t.Fatal("unclassified error entered a free reason")
	}
	for _, item := range []any{value, result} {
		body, e := json.Marshal(item)
		if e != nil {
			t.Fatal("closed serialization failed")
		}
		for _, rendered := range []string{string(body), fmt.Sprintf("%v", item), fmt.Sprintf("%+v", item), fmt.Sprintf("%#v", item)} {
			if strings.Contains(rendered, private) {
				t.Fatal("private error escaped host observation")
			}
		}
	}
	body, _ := json.Marshal(result)
	var fields map[string]any
	if json.Unmarshal(body, &fields) != nil {
		t.Fatal("closed outcome is invalid")
	}
	allowed := map[string]bool{"STOP_METADATA_QUALIFICATION_NOT_MET": true, "UNKNOWN": true, "NOT_APPLICABLE_NO_MODEL_PROMPT_ISSUED": true}
	for _, value := range fields {
		switch v := value.(type) {
		case bool:
		case string:
			if !allowed[v] {
				t.Fatal("outcome retained an unapproved string")
			}
		default:
			t.Fatal("outcome retained structured/raw data")
		}
	}
}
