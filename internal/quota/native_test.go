package quota

import (
	"context"
	"errors"
	"testing"
)

func TestAutoNativeOpenCodeEligibilityMatrix(t *testing.T) {
	for _, tc := range []struct {
		name      string
		eligible  []Provider
		preferred Provider
		want      Provider
	}{
		{"all-preserve-priority", Providers(), ProviderCodex, ProviderCodex},
		{"codex-opencode", []Provider{ProviderCodex, ProviderOpenCode}, ProviderCodex, ProviderCodex},
		{"claude-opencode", []Provider{ProviderClaude, ProviderOpenCode}, ProviderCodex, ProviderClaude},
		{"only-opencode", []Provider{ProviderOpenCode}, ProviderCodex, ProviderOpenCode},
		{"native-unavailable", []Provider{ProviderClaude}, ProviderCodex, ProviderClaude},
		{"none", nil, ProviderCodex, ""},
		{"explicit-native", Providers(), ProviderOpenCode, ProviderOpenCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := Manager{Store: Store{Root: t.TempDir()}, Probes: map[Provider]Probe{}}
			for _, provider := range Providers() {
				authenticated := false
				for _, allowed := range tc.eligible {
					authenticated = authenticated || provider == allowed
				}
				manager.Probes[provider] = &fakeProbe{value: ProviderQuota{Provider: provider, Authenticated: authenticated, TelemetryUnknown: provider == ProviderOpenCode}}
			}
			got, err := manager.Resolve(context.Background(), tc.preferred, "", true)
			if tc.want == "" {
				if err == nil {
					t.Fatal("no eligible executor accepted")
				}
				return
			}
			if err != nil || got.Resolved != tc.want {
				t.Fatalf("got %s want %s err=%v", got.Resolved, tc.want, err)
			}
			if got.Resolved == ProviderOpenCode && !got.Quota.TelemetryUnknown {
				t.Fatal("invented native quota")
			}
		})
	}
}

func TestAutoNativeFailoverIsBoundedAndReprobeFailsClosed(t *testing.T) {
	native := &fakeProbe{value: ProviderQuota{Provider: ProviderOpenCode, Authenticated: true, TelemetryUnknown: true}}
	manager := Manager{Store: Store{Root: t.TempDir()}, Probes: map[Provider]Probe{ProviderOpenCode: native, ProviderCodex: &fakeProbe{value: ProviderQuota{Provider: ProviderCodex, Authenticated: true}}}}
	ctx := context.Background()
	got, err := manager.ResolveCandidates(ctx, ProviderCodex, "", true, map[Provider]bool{ProviderCodex: true})
	if err != nil || got.Resolved != ProviderOpenCode {
		t.Fatal("fallback to native", err)
	}
	got, err = manager.ResolveCandidates(ctx, ProviderOpenCode, "", true, map[Provider]bool{ProviderOpenCode: true})
	if err != nil || got.Resolved != ProviderCodex {
		t.Fatal("failover from native", err)
	}
	if _, err = manager.ResolveCandidates(ctx, ProviderCodex, "", true, map[Provider]bool{ProviderCodex: true, ProviderOpenCode: true}); err == nil {
		t.Fatal("revisited failed executor")
	}
	native.value.Authenticated = false
	if _, eligible, err := manager.CanDispatch(ctx, ProviderOpenCode, "", true); err != nil || eligible {
		t.Fatal("native auth transition not enforced")
	}
	native.err = errors.New("metadata unavailable")
	if _, eligible, _ := manager.CanDispatch(ctx, ProviderOpenCode, "", true); eligible {
		t.Fatal("stale native auth eligible")
	}
}
