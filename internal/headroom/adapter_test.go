package headroom

import (
	"context"
	"errors"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

type adapterRunner struct{}

func (adapterRunner) LookPath(name string) (string, error) { return "/managed/" + name, nil }
func (adapterRunner) Run(_ context.Context, _ string, args []string, _ platform.RunOptions) (platform.Result, error) {
	if len(args) == 1 && args[0] == "--version" {
		return platform.Result{Stdout: "headroom 0.36.0\n"}, nil
	}
	return platform.Result{}, nil
}

func TestHeadroomAdapterSeparatesBinaryHealthFromManagedWrapperPolicy(t *testing.T) {
	provider := HeadroomCompressionProvider{Manager: Manager{Runner: adapterRunner{}, Binary: "/managed/headroom"}, Enabled: true, Managed: true}
	status := provider.Probe(context.Background())
	if status.Available || !status.Installed || status.Health != core.HealthHealthy || status.Capabilities.Supports(core.CapabilityCompressionWrap) || !status.Fallback.Allowed || status.Compatibility.Reason != "unavailable_by_policy" {
		t.Fatalf("probe = %+v", status)
	}
	for _, executor := range []core.ComponentID{core.ComponentCodex, core.ComponentClaude} {
		// A nil Runner would panic on any probe, proving refusal precedes it.
		provider.Manager = Manager{}
		lease, err := provider.Prepare(context.Background(), core.CompressionRequest{Executor: executor, DirectPath: "/managed/bin/executor", Fidelity: core.CompressionCompressible})
		if !errors.Is(err, ErrUnavailableByPolicy) || lease != nil {
			t.Fatalf("unsafe wrapper admitted: %v", err)
		}
		for _, fidelity := range []core.CompressionFidelity{core.CompressionExactRequired, core.CompressionBypass, core.CompressionUnsupported} {
			lease, err := provider.Prepare(context.Background(), core.CompressionRequest{Executor: executor, DirectPath: "/managed/bin/executor", Fidelity: fidelity})
			if err != nil || lease == nil || lease.Decision().Used || lease.Decision().Command != "/managed/bin/executor" {
				t.Fatal("fidelity bypass did not stay Direct")
			}
		}
	}
	provider.Enabled = false
	directLease, err := provider.Prepare(context.Background(), core.CompressionRequest{Executor: core.ComponentCodex, DirectPath: "/managed/bin/codex", Args: []string{"--help"}})
	direct := directLease.Decision()
	if err != nil || direct.Used || direct.Command != "/managed/bin/codex" {
		t.Fatalf("direct = %+v err=%v", direct, err)
	}
}
