package app

import (
	"context"
	"encoding/json"
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
func TestAutoCanSelectNativeOpenCodeWhenOnlyEligible(t *testing.T) {
	for _, planner := range []string{"codex", "opencode"} {
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
			if err := a.Auto(context.Background(), planner, nil); err != nil {
				t.Fatal(err)
			}
			if runner.executor != "opencode" {
				t.Fatal("native executor not dispatched")
			}
			sessions, err := a.SessionList()
			if err != nil || len(sessions) != 1 || sessions[0].Frontend != "opencode" || sessions[0].PrimaryExecutor != "opencode" {
				t.Fatal("control plane/session ownership drift", err)
			}
			gate := false
			for _, event := range sessions[0].Observability {
				gate = gate || event.Operation == "skill.gate"
			}
			if !gate {
				t.Fatal("native AUTO bypassed Skill Gate")
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
	if err := a.Auto(context.Background(), "opencode", nil); err == nil || !strings.Contains(err.Error(), "explicit OpenCode") {
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
	native := (&App{}).nativeOpenCode(cfg, state, root, filepath.Join(root, "runtime"), environment, true)
	if native == nil {
		t.Fatal("native projection unavailable")
	}
	if native.Options.Bridge != nil || !native.Options.NativeExecutor {
		t.Fatal("recursive bridge")
	}
	if native.Options.NativePermissions["*"] != "deny" || native.Options.NativePermissions["read"].(map[string]string)["*"] != "allow" || native.Options.NativePermissions["read"].(map[string]string)["*.env"] != "deny" || native.Options.NativePermissions["ivoai-memory_memory_query"] != "allow" {
		t.Fatal("worker write policy bypassed")
	}
	body, _ := json.Marshal(native.Options.NativeMCP)
	if strings.Contains(string(body), "must-not-copy") || strings.Contains(string(body), "fixture-capability") || strings.Contains(string(body), "untrusted") || !strings.Contains(string(body), "{env:IVOAI_KNOWLEDGE_SESSION_TOKEN}") {
		t.Fatal("credential/config isolation failed")
	}
	if !strings.Contains(native.Options.Instructions, "IvoAI research-source policy") {
		t.Fatal("knowledge policy lost")
	}
	environment[2] = "IVOAI_MEMORY_MCP_URL=https://remote.invalid/mcp"
	if (&App{}).nativeOpenCode(cfg, state, root, root, environment, true) != nil {
		t.Fatal("non-session MCP projection accepted")
	}
}
