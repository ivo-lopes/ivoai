package orchestration

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/core"
)

func TestRufloAdapterNeverAdvertisesProviderExecution(t *testing.T) {
	root := t.TempDir()
	t.Setenv("IVOAI_TEST_MODE", "1")
	manager := Manager{Runner: &recordingRunner{}, Binary: "/managed/ruflo", ProfileDir: root}
	if err := manager.Configure(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	adapter := RufloOrchestratorAdapter{Control: ControlPlane{Manager: manager, RuntimeDir: filepath.Join(root, "runtime")}, Managed: true}
	status := adapter.Probe(context.Background())
	if !status.Available || !status.Capabilities.Supports(core.CapabilityOrchestrationSwarm) || status.Implementation != "ruflo" {
		t.Fatalf("probe = %+v", status)
	}
	if status.Fallback.Allowed {
		t.Fatalf("unsafe orchestration fallback advertised: %+v", status.Fallback)
	}
}

func TestRufloAdapterRejectsUnsafeProfile(t *testing.T) {
	root := t.TempDir()
	adapter := RufloOrchestratorAdapter{Control: ControlPlane{Manager: Manager{Runner: &recordingRunner{}, Binary: filepath.Join(root, "ruflo"), ProfileDir: root}, RuntimeDir: filepath.Join(root, "runtime")}}
	status := adapter.Probe(context.Background())
	if status.Available || status.Health != core.HealthDegraded {
		t.Fatalf("probe = %+v", status)
	}
}
