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

func floatp(value float64) *float64 { return &value }
