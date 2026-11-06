package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/quota"
)

type nativeAutoFixture struct{ executor string }

func (n *nativeAutoFixture) Run(_ context.Context, r opencodebridge.ExecutorRequest, emit func(string) error) (opencodebridge.ExecutorResult, error) {
	n.executor = r.Executor
	return opencodebridge.ExecutorResult{ExecutorSessionID: "native_fixture_session", Model: "fixture/model"}, emit("native fixture final")
}
func TestAutoCannotSelectRetainedOpenCodeWhenOnlyEligible(t *testing.T) {
	for _, planner := range []string{"", "opencode"} {
		t.Run(planner, func(t *testing.T) {
			a := autoTestApp(t, t.TempDir(), "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
			runner := &nativeAutoFixture{}
			a.OpenCodeBridgeRunner = runner
			a.QuotaManager = &quota.Manager{Store: quota.Store{Root: a.Store.Paths.QuotaDir}, Probes: map[quota.Provider]quota.Probe{
				quota.ProviderCodex:  probeFunc(func(context.Context) (quota.ProviderQuota, error) { return exhausted(quota.ProviderCodex), nil }),
				quota.ProviderClaude: probeFunc(func(context.Context) (quota.ProviderQuota, error) { return exhausted(quota.ProviderClaude), nil }),
				quota.ProviderOpenCode: probeFunc(func(context.Context) (quota.ProviderQuota, error) {
					return quota.ProviderQuota{Provider: quota.ProviderOpenCode, Authenticated: true, Eligible: true, TelemetryUnknown: true}, nil
				}),
			}}
			if err := a.Auto(context.Background(), planner, nil); err == nil {
				t.Fatal("removed only provider selected")
			}
			if runner.executor != "" {
				t.Fatal("removed executor dispatched")
			}
			sessions, err := a.SessionList()
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range sessions {
				if len(value.Workers) != 0 || value.PrimaryExecutor == "opencode" || value.Frontend == "opencode" {
					t.Fatal("removed frontend/executor started")
				}
			}
		})
	}
}

func TestAutoExplicitOpenCodeUnavailableFailsClosed(t *testing.T) {
	a := autoTestApp(t, t.TempDir(), "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
	runner := &nativeAutoFixture{}
	a.OpenCodeBridgeRunner = runner
	a.QuotaManager = &quota.Manager{Store: quota.Store{Root: a.Store.Paths.QuotaDir}, Probes: map[quota.Provider]quota.Probe{quota.ProviderCodex: probeFunc(func(context.Context) (quota.ProviderQuota, error) { return available(quota.ProviderCodex), nil }), quota.ProviderOpenCode: probeFunc(func(context.Context) (quota.ProviderQuota, error) {
		return quota.ProviderQuota{Provider: quota.ProviderOpenCode, TelemetryUnknown: true}, nil
	})}}
	if err := a.Auto(context.Background(), "opencode", nil); err == nil || !strings.Contains(err.Error(), "OPENCODE_MANAGED_DEPRECATED") {
		t.Fatal("explicit native selection fell back", err)
	}
	if runner.executor != "" {
		t.Fatal("ineligible explicit executor launched")
	}
}
func TestNativeOpenCodeProjectionPreservesWorkerPolicyAndCredentialIsolation(t *testing.T) {
	root := t.TempDir()
	binary := appExecutable(t, root, "opencode", "#!/bin/sh\nexit 0\n")
	state := config.State{Components: map[string]config.ComponentState{"opencode": {Path: binary, Installed: true, Managed: true, Version: "fixture"}}}
	cfg := config.Default()
	cfg.OpenCode.PermissionMode = "full"
	cfg.MCP.Servers = map[string]config.MCPServer{"untrusted-write": {Enabled: true, URL: "https://untrusted.invalid"}}
	environment := []string{"HOME=" + root, "IVOAI_KNOWLEDGE_SESSION_TOKEN=fixture-capability", "IVOAI_MEMORY_MCP_URL=http://127.0.0.1:1234/mcp/memory", "OPENAI_API_KEY=must-not-copy"}
	for _, worker := range []bool{false, true} {
		if (&App{}).nativeOpenCode(cfg, state, root, filepath.Join(root, "runtime"), environment, worker) != nil {
			t.Fatal("removed native projection reactivated")
		}
	}
}
