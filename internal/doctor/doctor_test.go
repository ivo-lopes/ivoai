package doctor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/headroom"
	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/secrets"
	"github.com/ivo-lopes/ivoai/internal/skills"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

func TestDoctorReportsMultipleServersWithoutSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/.well-known/ivoai":
			json.NewEncoder(w).Encode(map[string]any{"protocol_version": 1, "health_endpoint": "/health", "ready_endpoint": "/ready"})
		case "/health", "/ready":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	paths := config.Paths{ConfigDir: root, StateDir: root, DataDir: root, Secrets: filepath.Join(root, "secrets.json")}
	cfg := config.Default()
	for _, alias := range []string{"mindsite", "voicecorp"} {
		id := "srv_id_" + alias
		cfg.Connections.Servers[alias] = config.ServerProfile{ID: id, Alias: alias, URL: server.URL, Status: "connected", Enabled: true, Purpose: alias, Protocol: 1, ContextMCPURL: server.URL + "/context", Features: map[string]bool{"context": true}}
		if err := (secrets.Store{Path: paths.Secrets}).Set(id, secrets.ClientCredential{Token: "not-reported-" + alias}); err != nil {
			t.Fatal(err)
		}
	}
	profiles := (Doctor{Store: config.NewStore(paths), HTTPClient: server.Client()}).serverProfiles(context.Background(), cfg)
	if len(profiles) != 2 || profiles[0].Alias != "mindsite" || profiles[1].Alias != "voicecorp" || !profiles[0].CredentialConfigured || !profiles[1].Reachable {
		t.Fatalf("profiles=%+v", profiles)
	}
	encoded, _ := json.Marshal(profiles)
	if strings.Contains(string(encoded), "not-reported") {
		t.Fatal("doctor leaked a server credential")
	}
}

type statusRunner struct {
	output string
}

func TestSkillControlPlaneDiagnosticTreatsAbsentRegistryAsReady(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{StateDir: filepath.Join(root, "state"), DataDir: filepath.Join(root, "data")}
	if err := platform.EnsurePrivateDir(paths.StateDir); err != nil {
		t.Fatal(err)
	}
	if err := platform.EnsurePrivateDir(paths.DataDir); err != nil {
		t.Fatal(err)
	}
	result := (Doctor{Store: config.NewStore(paths)}).skillControlPlane()
	if !result.RegistryReadable || !result.RegistryWritable || !result.PolicyEngineReady || result.RegistrySchema != skills.RegistrySchemaVersion || result.Active != 0 || result.ProvenanceHealth != "not_initialized" || result.StagingRootHealth != "not_initialized" {
		t.Fatalf("diagnostic=%+v", result)
	}
}

func TestSkillControlPlaneDiagnosticDetectsCorruptRegistryAndHealthyStaging(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{StateDir: filepath.Join(root, "state"), DataDir: filepath.Join(root, "data")}
	registry := skills.RegistryPath(paths.StateDir)
	if err := platform.EnsurePrivateDir(filepath.Dir(registry)); err != nil {
		t.Fatal(err)
	}
	if err := platform.EnsurePrivateDir(filepath.Join(paths.DataDir, "supply-chain")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registry, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := (Doctor{Store: config.NewStore(paths)}).skillControlPlane()
	if result.RegistryReadable || result.ProvenanceHealth != "unhealthy" || result.StagingRootHealth != "healthy" {
		t.Fatalf("diagnostic=%+v", result)
	}
	if pointers, err := supplychain.ListPointers(filepath.Join(paths.DataDir, "supply-chain")); err != nil || len(pointers) != 0 {
		t.Fatalf("pointers=%v err=%v", pointers, err)
	}
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
		"opencode":    {Installed: true, Managed: true, Version: "fixture", Path: "/managed/opencode"},
		"headroom":    {Installed: true, Managed: true, Version: "fixture", Path: "/managed/headroom"},
		"caveman":     {Installed: true, Managed: true, Version: "fixture", Path: "/managed/caveman"},
		"ai-memory":   {Installed: true, Managed: true, Version: "fixture", Path: "/managed/ai-memory"},
		"ruflo":       {Installed: true, Managed: true, Version: "fixture", Path: "/managed/ruflo"},
	}}
	report := Report{
		Codex: Auth{Installed: true, Version: "codex fixture"}, Claude: Auth{Installed: true, Version: "claude fixture"},
		Headroom:      headroom.Status{Installed: true, Healthy: true, CodexCompatible: true, ClaudeCompatible: true},
		Caveman:       ManagedComponent{Component: Component{Installed: true, Managed: true, Version: "fixture", Path: "/managed/caveman"}, Healthy: true},
		OpenCode:      ManagedComponent{Component: Component{Installed: true, Managed: true, Version: "fixture", Path: "/managed/opencode"}, Healthy: true},
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
	compressionEntries := matrix.Entries(core.ComponentCompression)
	if len(compressionEntries) != 2 || compressionEntries[0].Implementation != "headroom" || compressionEntries[1].Implementation != "caveman" || compressionEntries[1].Active {
		t.Fatalf("compression entries=%+v", compressionEntries)
	}
	cfg.Compression.Provider = "caveman"
	cavemanMatrix := componentMatrix(cfg, state, report)
	selected, err := cavemanMatrix.Resolve(core.ComponentCompression, core.CapabilityCompressionWrap)
	if err != nil || selected.Component.Implementation != "caveman" || !selected.Component.Active {
		t.Fatalf("caveman selection=%+v err=%v", selected, err)
	}
	for _, entry := range cavemanMatrix.Entries(core.ComponentCompression) {
		if entry.Implementation == "headroom" && entry.Active {
			t.Fatal("Headroom and Caveman were active simultaneously")
		}
	}
	if _, err := matrix.Resolve(core.ComponentContext, core.CapabilityContextIngest); err == nil {
		t.Fatal("read-only remote Context advertised ingestion")
	}
	opencode, err := matrix.Resolve(core.ComponentOpenCode, core.CapabilitySessionStart)
	if err != nil || opencode.Component.Capabilities.Supports(core.CapabilityAdvisoryExecute) {
		t.Fatalf("OpenCode direct-only selection=%+v err=%v", opencode, err)
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
