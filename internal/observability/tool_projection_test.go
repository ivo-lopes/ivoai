package observability

import "testing"

func TestToolProjectionEvent(t *testing.T) {
	e := Event{Category: CategoryCapability, Operation: OperationCapabilityResolve, Component: "tools", Capability: "tool.projection", State: StateCompleted, BytesBefore: 4000, BytesAfter: 1000, TokenBasis: "unavailable"}
	if _, err := Normalize(e); err != nil {
		t.Fatal(err)
	}
	available, presented := 7, 0
	e.ToolsAvailable, e.ToolsPresented = &available, &presented
	if _, err := Normalize(e); err != nil {
		t.Fatal("observed zero rejected", err)
	}
	for _, invalid := range []int{-1, 8, 20001} {
		presented = invalid
		if _, err := Normalize(e); err == nil {
			t.Fatal("invalid projection accepted")
		}
	}
}
