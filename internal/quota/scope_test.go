package quota

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
)

type scopeProbe struct {
	calls    int
	provider Provider
}

func (p *scopeProbe) Probe(context.Context) (ProviderQuota, error) {
	p.calls++
	return ProviderQuota{Provider: p.provider, Authenticated: true, Eligible: true, ObservedAt: time.Now()}, nil
}

func TestRestrictedManagerDeniesBeforeCacheProbeAndWrites(t *testing.T) {
	store := Store{Root: t.TempDir()}
	if err := store.Put(ProviderQuota{Provider: ProviderOpenCode, Authenticated: true, Eligible: true, ObservedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.path())
	if err != nil {
		t.Fatal(err)
	}
	legacyProbe := &scopeProbe{provider: ProviderOpenCode}
	codex := &scopeProbe{provider: ProviderCodex}
	original := Manager{Store: store, Probes: map[Provider]Probe{ProviderOpenCode: legacyProbe, ProviderCodex: codex}}
	restricted := original.RestrictedTo(ProviderCodex, ProviderClaude)
	for _, force := range []bool{false, true} {
		value, err := restricted.Probe(context.Background(), ProviderOpenCode, force)
		if !errors.Is(err, ErrProviderNotAllowed) || value.Eligible {
			t.Fatal("cached excluded provider admitted")
		}
		_, eligible, err := restricted.CanDispatch(context.Background(), ProviderOpenCode, "", force)
		if eligible || !errors.Is(err, ErrProviderNotAllowed) {
			t.Fatal("excluded dispatch admitted")
		}
	}
	if _, err := restricted.AuthenticationChanged(context.Background(), ProviderOpenCode, true); !errors.Is(err, ErrProviderNotAllowed) {
		t.Fatal("excluded invalidation admitted")
	}
	if err := restricted.MarkExhausted(ProviderOpenCode, "fixture"); !errors.Is(err, ErrProviderNotAllowed) {
		t.Fatal("excluded write admitted")
	}
	after, _ := os.ReadFile(store.path())
	if string(before) != string(after) || legacyProbe.calls != 0 {
		t.Fatal("excluded provider probed or quota history changed")
	}
	if !original.Allows(ProviderOpenCode) || original.Probes[ProviderOpenCode] != legacyProbe {
		t.Fatal("original manager modified")
	}
	if !reflect.DeepEqual(restricted.Providers(), []Provider{ProviderCodex, ProviderClaude}) {
		t.Fatal("wrong restricted order")
	}
	if decision, err := restricted.Resolve(context.Background(), ProviderCodex, "", true); err != nil || decision.Resolved != ProviderCodex {
		t.Fatal("native route broken", err)
	}
	if _, err := restricted.RestrictedTo(ProviderOpenCode).Resolve(context.Background(), ProviderOpenCode, "", true); err == nil {
		t.Fatal("nested restriction widened its parent")
	}
	if _, err := original.Probe(context.Background(), ProviderOpenCode, true); err != nil || legacyProbe.calls != 1 {
		t.Fatal("legacy manager no longer works")
	}
}
