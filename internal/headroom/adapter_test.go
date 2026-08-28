package headroom

import (
	"context"
	"strings"
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

func TestHeadroomAdapterPreservesWrapInvocationAndDirectFallback(t *testing.T) {
	provider := HeadroomCompressionProvider{Manager: Manager{Runner: adapterRunner{}, Binary: "/managed/headroom"}, Enabled: true, Managed: true}
	status := provider.Probe(context.Background())
	if !status.Available || !status.Capabilities.Supports(core.CapabilityCompressionWrap) || !status.Fallback.Allowed {
		t.Fatalf("probe = %+v", status)
	}
	lease, err := provider.Prepare(context.Background(), core.CompressionRequest{Executor: core.ComponentCodex, DirectPath: "/managed/bin/codex", Args: []string{"--model", "fixture"}, Environment: []string{"PATH=/usr/bin"}})
	if err != nil {
		t.Fatal(err)
	}
	decision := lease.Decision()
	if !decision.Used || decision.Command != "/managed/headroom" || strings.Join(decision.Args, " ") != "wrap codex -- --model fixture" || decision.Environment[0] != "PATH=/managed/bin:/usr/bin" {
		t.Fatalf("decision = %+v", decision)
	}
	provider.Enabled = false
	directLease, err := provider.Prepare(context.Background(), core.CompressionRequest{Executor: core.ComponentCodex, DirectPath: "/managed/bin/codex", Args: []string{"--help"}})
	direct := directLease.Decision()
	if err != nil || direct.Used || direct.Command != "/managed/bin/codex" {
		t.Fatalf("direct = %+v err=%v", direct, err)
	}
}
