package compression

import (
	"testing"

	"github.com/ivo-lopes/ivoai/internal/core"
)

func healthy(implementation string) core.ComponentStatus {
	return core.ComponentStatus{
		ID: core.ComponentCompression, Implementation: implementation, Active: true,
		Installed: true, Available: true, Health: core.HealthHealthy, Lifecycle: core.LifecycleRunning,
		Capabilities:  core.CapabilitySet{core.CapabilityCompressionWrap: core.SupportSupported},
		Compatibility: core.Compatibility{State: core.CompatibilityCompatible},
	}
}

func TestSelectNeverChainsCompressionProviders(t *testing.T) {
	selection, err := Select(core.CompressionCompressible,
		Candidate{Implementation: Caveman, Status: healthy("caveman")},
		Candidate{Implementation: Headroom, Status: healthy("headroom")},
	)
	if err != nil {
		t.Fatal(err)
	}
	if selection.Implementation != Caveman || selection.Fallback {
		t.Fatalf("selection = %+v", selection)
	}
}

func TestSelectFallsBackToOneLegacyProvider(t *testing.T) {
	caveman := healthy("caveman")
	caveman.Health = core.HealthUnavailable
	selection, err := Select(core.CompressionCompressible,
		Candidate{Implementation: Caveman, Status: caveman},
		Candidate{Implementation: Headroom, Status: healthy("headroom")},
	)
	if err != nil {
		t.Fatal(err)
	}
	if selection.Implementation != Headroom || !selection.Fallback {
		t.Fatalf("selection = %+v", selection)
	}
}

func TestExactRequiredAlwaysBypasses(t *testing.T) {
	selection, err := Select(core.CompressionExactRequired, Candidate{Implementation: Caveman, Status: healthy("caveman")})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Implementation != Direct {
		t.Fatalf("selection = %+v", selection)
	}
}
