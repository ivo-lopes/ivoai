package doctor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/headroom"
	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/quota"
)

type statusRunner struct {
	output string
}

func TestQuotaDiagnosticExposesCodexFiveHourDurationWithoutInventingMonthly(t *testing.T) {
	now := time.Now().UTC()
	reset := now.Add(time.Hour)
	value := quota.ProviderQuota{Provider: quota.ProviderCodex, Authenticated: true, Eligible: false, HardLimitReached: true, Source: "fixture", Reason: "Codex 5-hour quota exhausted", Windows: []quota.Window{quota.FromUsedDuration(quota.KindRolling, 300, 100, &reset, "fixture", now), quota.FromUsedDuration(quota.KindWeekly, 10080, 20, nil, "fixture", now)}}
	diagnostic := quotaProbeDiagnostic(value, nil)
	if diagnostic.FiveHourSource != "fixture" || diagnostic.WeeklySource != "fixture" || diagnostic.MonthlySource != "N/A / not exposed" || len(diagnostic.Windows) != 2 {
		t.Fatalf("diagnostic=%+v", diagnostic)
	}
	if diagnostic.Windows[0].DurationMinutes != 300 || diagnostic.Windows[0].RemainingPercent == nil || *diagnostic.Windows[0].RemainingPercent != 0 || diagnostic.Windows[0].ResetsAt == nil {
		t.Fatalf("five-hour diagnostic=%+v", diagnostic.Windows[0])
	}
}

func TestComponentMatrixExplainsCapabilitiesHealthAndFallback(t *testing.T) {
	cfg := config.Default()
	cfg.MCP.Servers["ivoai-context"] = config.MCPServer{Kind: "context", Enabled: true}
	state := config.State{Components: map[string]config.ComponentState{
		"codex":       {Installed: true, Managed: true, Version: "fixture", Path: "/managed/codex"},
		"claude-code": {Installed: true, Managed: true, Version: "fixture", Path: "/managed/claude"},
		"headroom":    {Installed: true, Managed: true, Version: "fixture", Path: "/managed/headroom"},
		"ai-memory":   {Installed: true, Managed: true, Version: "fixture", Path: "/managed/ai-memory"},
		"ruflo":       {Installed: true, Managed: true, Version: "fixture", Path: "/managed/ruflo"},
	}}
	report := Report{
		Codex: Auth{Installed: true, Version: "codex fixture"}, Claude: Auth{Installed: true, Version: "claude fixture"},
		Headroom:      headroom.Status{Installed: true, Healthy: true, CodexCompatible: true, ClaudeCompatible: true},
		Memory:        Component{Installed: true, Hooks: true},
		Ruflo:         orchestration.Status{Installed: true, SafeMode: true},
		Server:        Server{Configured: true, Reachable: true, ProtocolCompatible: true},
		Orchestration: Orchestration{CodexWorker: true, ClaudeWorker: true},
	}
	matrix := componentMatrix(cfg, state, report)
	codex, err := matrix.Resolve(core.ComponentCodex, core.CapabilityAdvisoryExecute)
	if err != nil || codex.Component.Implementation != "official-cli" {
		t.Fatalf("codex selection=%+v err=%v", codex, err)
	}
	compression, err := matrix.Resolve(core.ComponentCompression, core.CapabilityCompressionWrap)
	if err != nil || !compression.Component.Fallback.Allowed {
		t.Fatalf("compression selection=%+v err=%v", compression, err)
	}
	if _, err := matrix.Resolve(core.ComponentContext, core.CapabilityContextIngest); err == nil {
		t.Fatal("read-only remote Context advertised ingestion")
	}
}

func (r statusRunner) LookPath(name string) (string, error) { return "/managed/" + name, nil }
func (r statusRunner) Run(_ context.Context, _ string, args []string, _ platform.RunOptions) (platform.Result, error) {
	if len(args) == 1 && args[0] == "--version" {
		return platform.Result{Stdout: "fixture 1.0.0"}, nil
	}
	return platform.Result{Stdout: r.output}, nil
}

func TestAgentAuthDoesNotTrustSuccessfulNegativeStatus(t *testing.T) {
	doctor := Doctor{Runner: statusRunner{output: `{"loggedIn": false}`}}
	status := doctor.agent(context.Background(), "codex", []string{"login", "status"}, config.ComponentState{Installed: true, Path: "/managed/codex"})
	if status.Authenticated {
		t.Fatalf("negative JSON status reported authenticated: %#v", status)
	}
}

func TestHooksInstalledRequiresMaterializedAssets(t *testing.T) {
	dir := t.TempDir()
	if hooksInstalled(dir) {
		t.Fatal("empty hooks directory reported installed")
	}
	if err := os.WriteFile(filepath.Join(dir, "session-start.sh"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if !hooksInstalled(dir) {
		t.Fatal("hook asset was not detected")
	}
}

func TestServerDoctorUsesLiveProtocolAndRefusesCrossOriginRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"protocol_version":1,"health_endpoint":"/health","ready_endpoint":"/ready"}`))
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()
	result := (Doctor{}).server(context.Background(), config.Connection{Status: "connected", URL: redirector.URL, Protocol: 1})
	if result.Reachable {
		t.Fatal("doctor followed a cross-origin redirect")
	}

	mismatch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"protocol_version":99,"health_endpoint":"/health","ready_endpoint":"/ready"}`))
	}))
	defer mismatch.Close()
	result = (Doctor{HTTPClient: mismatch.Client()}).server(context.Background(), config.Connection{Status: "connected", URL: mismatch.URL, Protocol: 1})
	if !result.Reachable || result.ProtocolCompatible {
		t.Fatalf("doctor trusted persisted protocol instead of discovery: %#v", result)
	}
}
