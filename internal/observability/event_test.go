package observability

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEventUsesExplicitAllowlistAndRedactsReasons(t *testing.T) {
	value, err := Normalize(Event{Category: CategoryFallback, Operation: OperationFallbackRoute, State: StateSelected, Provider: "codex", Executor: "claude", RoutingReason: "Authorization: Bearer secret-value", FallbackReason: "access_token=private-value"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(body)
	if strings.Contains(encoded, "secret-value") || strings.Contains(encoded, "private-value") || !strings.Contains(encoded, `"redacted"`) {
		t.Fatalf("event was not centrally redacted: %s", encoded)
	}
	if strings.Contains(encoded, "prompt") || strings.Contains(encoded, "response") || strings.Contains(encoded, "environment") {
		t.Fatalf("event exposed a forbidden arbitrary field: %s", encoded)
	}
}

func TestSkillControlPlaneEventsPersistMetadataOnly(t *testing.T) {
	event, err := Normalize(Event{
		Category: CategorySkillPolicy, Operation: OperationPolicyDecision, State: StateApprovalRequired,
		SkillID: "synthetic-skill", RiskTier: "high", PolicyDecision: "REQUIRE_APPROVAL", RoutingReason: ReasonApprovalRequired,
	})
	if err != nil || event.SkillID != "synthetic-skill" {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"prompt", "body", "script", "readme", "authorization", "environment"} {
		if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
			t.Fatalf("event leaked forbidden field %q: %s", forbidden, encoded)
		}
	}
	if _, err := Normalize(Event{Category: CategorySupplyChain, Operation: OperationSupplyStage, State: StateFailed, ArtifactID: "token=secret"}); err == nil {
		t.Fatal("secret-shaped artifact ID accepted")
	}
	if _, err := Normalize(Event{Category: CategorySupplyChain, Operation: OperationSupplyPromote, State: StatePromoted, ArtifactID: "component", Revision: "main"}); err == nil {
		t.Fatal("floating revision accepted in observability")
	}
}

func TestEventRejectsUnknownDimensionsAndInvalidCorrelation(t *testing.T) {
	now := time.Now().UTC()
	for name, event := range map[string]Event{
		"operation": {ObservedAt: now, Category: CategoryQuota, Operation: "quota.raw", State: StateCompleted},
		"provider":  {ObservedAt: now, Category: CategoryQuota, Operation: OperationQuotaProbe, State: StateCompleted, Provider: "arbitrary"},
		"task":      {ObservedAt: now, Category: CategoryDAG, Operation: OperationDAGPlan, State: StateCompleted, TaskID: "../../task"},
		"percent":   {ObservedAt: now, Category: CategoryQuota, Operation: OperationQuotaProbe, State: StateCompleted, RemainingPercent: floatp(101)},
		"reason":    {ObservedAt: now, Category: CategoryQuota, Operation: OperationQuotaProbe, State: StateCompleted, RoutingReason: "arbitrary free text"},
	} {
		if _, err := Normalize(event); err == nil {
			t.Fatalf("%s event accepted", name)
		}
	}
}

func TestEventPreservesBoundedCorrelationAndComponentRoutingReason(t *testing.T) {
	value, err := Normalize(Event{
		Category: CategoryCapability, Operation: OperationCapabilityResolve, State: StateSelected,
		SessionID: "sess_0123456789abcdef0123456789abcdef", TaskID: "inventory", WorkerID: "worker_0123456789abcdef0123456789abcdef",
		Provider: "codex", Executor: "codex", Component: "codex", RoutingReason: ReasonCapabilityMatch,
	})
	if err != nil {
		t.Fatal(err)
	}
	if value.SessionID == "" || value.TaskID != "inventory" || value.WorkerID == "" || value.RoutingReason != ReasonCapabilityMatch {
		t.Fatalf("correlation was not preserved: %+v", value)
	}
}

func TestCompressionTelemetryIsBoundedMetadataOnly(t *testing.T) {
	event, err := Normalize(Event{Category: CategoryCompression, Operation: OperationCompressionResult, State: StateCompleted, Provider: "caveman", Executor: "codex", Component: "compression", PayloadType: "json", FidelityClass: "compressible", BytesBefore: 4096, BytesAfter: 1024, TokensEstimatedBefore: 1000, TokensEstimatedAfter: 250, TokenBasis: "inferred", CompressionRatio: .25, RecoveryCount: 1, CompressionResult: "applied", RoutingReason: ReasonCompressionApplied, DurationMilliseconds: 12})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"prompt", "authorization", "cookie", "api_key", "environment", "raw_output", "diff_content"} {
		if strings.Contains(strings.ToLower(string(body)), forbidden) {
			t.Fatalf("compression telemetry leaked %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(string(body), `"token_basis":"inferred"`) {
		t.Fatalf("estimated basis missing: %s", body)
	}
	for name, invalid := range map[string]Event{
		"payload": {Category: CategoryCompression, Operation: OperationCompressionResult, State: StateCompleted, PayloadType: "../../secret"},
		"basis":   {Category: CategoryCompression, Operation: OperationCompressionResult, State: StateCompleted, TokenBasis: "provider_reported"},
		"ratio":   {Category: CategoryCompression, Operation: OperationCompressionResult, State: StateCompleted, CompressionRatio: 1.1},
	} {
		if _, err := Normalize(invalid); err == nil {
			t.Fatalf("invalid %s telemetry accepted", name)
		}
	}
}

func TestAuthoritativeKnowledgeBypassTelemetryIsAllowlisted(t *testing.T) {
	event, err := Normalize(Event{
		Category: CategoryCompression, Operation: OperationCompressionSelect, State: StateSelected,
		Provider: "direct", RequestedProvider: "caveman", Executor: "codex", Component: "compression",
		RoutingReason: ReasonAuthoritativeSharedKnowledge, CompressionBypassed: true,
		AuthoritativeKnowledge: true, SelectedSourceCount: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(body)
	for _, forbidden := range []string{"authorization", "bearer", "memory body", "context body", "prompt", "environment"} {
		if strings.Contains(strings.ToLower(encoded), forbidden) {
			t.Fatalf("bypass telemetry leaked %q: %s", forbidden, encoded)
		}
	}
	if !strings.Contains(encoded, `"requested_provider":"caveman"`) || !strings.Contains(encoded, `"provider":"direct"`) || !strings.Contains(encoded, `"compression_bypassed":true`) {
		t.Fatalf("bypass metadata missing: %s", encoded)
	}
	if _, err := Normalize(Event{Category: CategoryCompression, Operation: OperationCompressionSelect, State: StateSelected, Provider: "direct", RequestedProvider: "../../caveman", RoutingReason: ReasonAuthoritativeSharedKnowledge}); err == nil {
		t.Fatal("unsafe requested provider accepted")
	}
}

func TestKnowledgeRouteTelemetryIsBoundedMetadataOnly(t *testing.T) {
	event, err := Normalize(Event{
		Category: CategoryConnection, Operation: OperationKnowledgeRoute, State: StateCompleted,
		SourceID: "srv_0123456789abcdef", SourceAlias: "mindsite-primary", Purpose: "mindsite",
		SelectedSourceCount: 2, Failover: true, PartialFailure: true,
		RoutingReason: ReasonKnowledgeDegraded, DurationMilliseconds: 18,
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"authorization", "bearer", "token-a", "memory body", "context body", "raw_payload", "environment"} {
		if strings.Contains(strings.ToLower(string(body)), forbidden) {
			t.Fatalf("knowledge routing telemetry leaked %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(string(body), `"selected_source_count":2`) || !strings.Contains(string(body), `"failover":true`) {
		t.Fatalf("routing metadata missing: %s", body)
	}
	if _, err := Normalize(Event{Category: CategoryConnection, Operation: OperationKnowledgeRoute, State: StateFailed, SourceAlias: "voicecorp\nAuthorization: Bearer secret"}); err == nil {
		t.Fatal("unsafe source alias accepted")
	}
}

func floatp(value float64) *float64 { return &value }
