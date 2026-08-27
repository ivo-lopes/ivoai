package core

import (
	"context"
	"errors"
	"testing"
)

type fakeComponent struct{ status ComponentStatus }

func (f fakeComponent) ID() ComponentID                       { return f.status.ID }
func (f fakeComponent) Probe(context.Context) ComponentStatus { return f.status }

func TestCapabilitySetDoesNotInventUnsupportedCapabilities(t *testing.T) {
	set := CapabilitySet{CapabilitySessionStart: SupportSupported, CapabilitySessionAbort: SupportNotExposed}
	if !set.Supports(CapabilitySessionStart) || set.Supports(CapabilitySessionAbort) || set.Supports(Capability("session.diff")) {
		t.Fatalf("unexpected support result: %#v", set)
	}
}

func TestMatrixSelectsActiveThenExplicitFallback(t *testing.T) {
	active := ComponentStatus{ID: ComponentCompression, Implementation: "legacy", Active: true, Installed: true, Available: false, Health: HealthUnavailable, Lifecycle: LifecycleStopped, Capabilities: CapabilitySet{CapabilityCompressionWrap: SupportSupported}, Compatibility: Compatibility{State: CompatibilityCompatible}}
	fallback := ComponentStatus{ID: ComponentCompression, Implementation: "direct", Installed: true, Available: true, Health: HealthHealthy, Lifecycle: LifecycleRunning, Capabilities: CapabilitySet{CapabilityCompressionBypass: SupportSupported}, Compatibility: Compatibility{State: CompatibilityCompatible}, Fallback: Fallback{Allowed: true, Reason: "legacy provider unavailable"}}
	matrix, err := NewMatrix(active, fallback)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := matrix.Resolve(ComponentCompression, CapabilityCompressionBypass)
	if err != nil {
		t.Fatal(err)
	}
	if !selection.FallbackUsed || selection.Component.Implementation != "direct" || selection.FallbackReason == "" {
		t.Fatalf("selection = %+v", selection)
	}
}

func TestMatrixReturnsTypedUnsupportedError(t *testing.T) {
	value := ComponentStatus{ID: ComponentCodex, Implementation: "official-cli", Active: true, Installed: true, Available: true, Health: HealthHealthy, Lifecycle: LifecycleRunning, Capabilities: CapabilitySet{CapabilitySessionStart: SupportSupported}, Compatibility: Compatibility{State: CompatibilityCompatible}}
	matrix, err := NewMatrix(value)
	if err != nil {
		t.Fatal(err)
	}
	_, err = matrix.Resolve(ComponentCodex, Capability("session.diff"))
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) || unsupported.Capability != Capability("session.diff") {
		t.Fatalf("error = %v", err)
	}
}

func TestComponentContractAcceptsTestDouble(t *testing.T) {
	var component Component = fakeComponent{status: ComponentStatus{ID: ComponentTools, Implementation: "fake", Health: HealthUnknown, Lifecycle: LifecycleUnknown, Capabilities: CapabilitySet{}, Compatibility: Compatibility{State: CompatibilityUnknown}}}
	if got := component.Probe(context.Background()); got.ID != ComponentTools {
		t.Fatalf("probe = %+v", got)
	}
}
