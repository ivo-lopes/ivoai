package cli

import (
	"bytes"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/doctor"
	"strings"
	"testing"
)

func TestDoctorCodeIntelligencePresentationPreservesUnknown(t *testing.T) {
	var out bytes.Buffer
	report := doctor.Report{Graft: doctor.ManagedComponent{Component: doctor.Component{Installed: true, Managed: true, Version: "fixture"}, TrustLevel: "checksum_only"}, CodeIntelligence: doctor.CodeIntelligence{Integrity: "verified", Coverage: "unknown", Node: "available", NodeIntegrity: "not_exposed", Libatomic: "loader_satisfied", LandlockABI: 1, Seccomp: true, Component: core.ComponentStatus{Health: core.HealthUnknown, Compatibility: core.Compatibility{State: core.CompatibilityUnknown, Reason: "execution unverified"}}}}
	printDoctorCodeIntelligence(&out, report)
	for _, want := range []string{"Graft:", "Code intelligence:", "health=unknown", "coverage=unknown", "integrity=verified", "execution unverified", "node-integrity=not_exposed"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "enabled=") || strings.Contains(out.String(), "healthy=true") {
		t.Fatal("invented toggle or health")
	}
}
