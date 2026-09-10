package orchestration

import (
	"testing"

	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/routing"
)

func TestNativeConcurrencyIsHostDAGAndCapAware(t *testing.T) {
	input := ConcurrencyInputs{Runnable: 6, ProviderSlots: 6, WorktreeSlots: 6, Writing: true, WorkerMemoryBytes: 1 << 30}
	small := Concurrency(HostResources{CPUs: 2, AvailableMemoryBytes: 2 << 30}, input)
	large := Concurrency(HostResources{CPUs: 16, AvailableMemoryBytes: 32 << 30}, input)
	if small != 2 || large != 6 {
		t.Fatalf("small=%d large=%d", small, large)
	}
	input.UserCap = 2
	if got := Concurrency(HostResources{CPUs: 16, AvailableMemoryBytes: 32 << 30}, input); got != 2 {
		t.Fatal(got)
	}
	input.Runnable = 1
	if got := Concurrency(HostResources{CPUs: 16, AvailableMemoryBytes: 32 << 30}, input); got != 1 {
		t.Fatal(got)
	}
	input.Runnable, input.WorktreeSlots = 6, 0
	if got := Concurrency(HostResources{CPUs: 16}, input); got != 1 {
		t.Fatal("unsafe parallel writing", got)
	}
	input.ProviderSlots = 0
	if got := Concurrency(HostResources{CPUs: 16}, input); got != 0 {
		t.Fatal("unavailable provider admitted", got)
	}
}

func TestNativeConservationThresholdOnlyProposes(t *testing.T) {
	for _, remaining := range []float64{11, 10, 9} {
		values := map[quota.Provider]quota.ProviderQuota{quota.ProviderCodex: {Authenticated: true, Eligible: true, Windows: []quota.Window{{Kind: quota.KindWeekly, Available: true, Authoritative: true, RemainingPercent: remaining}}}}
		got := QuotaConservation(values, 10)
		want := "normal"
		if remaining <= 10 {
			want = "conservation_pending_confirmation"
		}
		if got.State != want {
			t.Fatalf("%v: %s", remaining, got.State)
		}
		value := values[quota.ProviderCodex]
		value.Windows[0].State = quota.TelemetryStale
		values[quota.ProviderCodex] = value
		if got := QuotaConservation(values, 10); got.State != "normal" {
			t.Fatal("stale quota used")
		}
	}
}

func TestNativeProfilesAndToolDefaultDeny(t *testing.T) {
	primary, err := CapabilityProfile("synthesis")
	if err != nil || primary.MinimumTier != routing.TierStrong || primary.Write {
		t.Fatal(primary, err)
	}
	research, _ := CapabilityProfile("research")
	if research.Write || research.MinimumTier != routing.TierLight {
		t.Fatal(research)
	}
	writer, _ := CapabilityProfile("implementation")
	if !writer.Write || !writer.RequiresWorktree {
		t.Fatal(writer)
	}
	if _, err := SelectTools([]string{"plane"}, nil); err == nil {
		t.Fatal("registry tools inherited")
	}
	a, err := SelectTools([]string{"plane"}, []string{"plane"})
	if err != nil || len(a) != 1 {
		t.Fatal(a, err)
	}
	b, err := SelectTools(nil, []string{"plane"})
	if err != nil || len(b) != 0 {
		t.Fatal("other worker inherited tools", b, err)
	}
}
