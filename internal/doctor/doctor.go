package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/connections"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/headroom"
	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/policy"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/skills"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
	"golang.org/x/sys/unix"
)

type Component struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Path      string `json:"path,omitempty"`
	Managed   bool   `json:"managed"`
	Fixture   bool   `json:"fixture,omitempty"`
	Hooks     bool   `json:"hooks,omitempty"`
}
type ManagedComponent struct {
	Component
	Healthy    bool   `json:"healthy"`
	Revision   string `json:"revision,omitempty"`
	Source     string `json:"source,omitempty"`
	License    string `json:"license,omitempty"`
	TrustLevel string `json:"trust_level,omitempty"`
}
type Auth struct {
	Installed     bool   `json:"installed"`
	Version       string `json:"version,omitempty"`
	Authenticated bool   `json:"authenticated"`
}
type Server struct {
	Configured         bool   `json:"configured"`
	Reachable          bool   `json:"reachable"`
	TLS                bool   `json:"tls"`
	ProtocolCompatible bool   `json:"protocol_compatible"`
	URL                string `json:"url,omitempty"`
}
type Orchestration struct {
	Enabled          bool   `json:"enabled"`
	BridgeAvailable  bool   `json:"bridge_available"`
	SessionDirectory string `json:"session_directory"`
	SessionPerms     string `json:"session_permissions"`
	MaxWorkers       int    `json:"max_workers"`
	CodexWorker      bool   `json:"codex_worker_capable"`
	ClaudeWorker     bool   `json:"claude_worker_capable"`
}
type QuotaProbe struct {
	Ready            bool               `json:"ready"`
	Authenticated    bool               `json:"authenticated"`
	Eligible         bool               `json:"eligible"`
	Source           string             `json:"source"`
	FiveHourSource   string             `json:"five_hour_source,omitempty"`
	WeeklySource     string             `json:"weekly_source"`
	MonthlySource    string             `json:"monthly_source"`
	IndividualSource string             `json:"individual_source,omitempty"`
	Windows          []QuotaWindowProbe `json:"windows,omitempty"`
	Reason           string             `json:"reason,omitempty"`
}
type QuotaWindowProbe struct {
	Kind             quota.Kind           `json:"kind"`
	DurationMinutes  int64                `json:"duration_minutes,omitempty"`
	TelemetryState   quota.TelemetryState `json:"telemetry_state"`
	RemainingPercent *float64             `json:"remaining_percent,omitempty"`
	ResetsAt         *time.Time           `json:"resets_at,omitempty"`
	Source           string               `json:"source"`
}
type Automatic struct {
	Enabled                  bool                  `json:"enabled"`
	DefaultPlanner           string                `json:"default_planner"`
	AutomaticFailover        bool                  `json:"automatic_failover"`
	CheckpointReady          bool                  `json:"checkpoint_ready"`
	CodexToClaude            bool                  `json:"codex_to_claude"`
	ClaudeToCodex            bool                  `json:"claude_to_codex"`
	Quota                    map[string]QuotaProbe `json:"quota"`
	SchedulerReady           bool                  `json:"scheduler_ready"`
	ParallelRuntime          bool                  `json:"parallel_worker_runtime"`
	CodexModelRouting        string                `json:"codex_model_routing"`
	ClaudeModelRouting       string                `json:"claude_model_routing"`
	CodexEffortControl       string                `json:"codex_effort_control"`
	ClaudeEffortControl      string                `json:"claude_effort_control"`
	SharedKnowledgeBootstrap bool                  `json:"shared_knowledge_bootstrap"`
}
type SkillControlPlane struct {
	RegistryReadable  bool   `json:"registry_readable"`
	RegistryWritable  bool   `json:"registry_writable"`
	RegistrySchema    int    `json:"registry_schema"`
	Active            int    `json:"active"`
	Staged            int    `json:"staged"`
	Quarantined       int    `json:"quarantined"`
	ProvenanceHealth  string `json:"provenance_health"`
	PolicyEngineReady bool   `json:"policy_engine_ready"`
	StagingRootHealth string `json:"staging_root_health"`
}
type Report struct {
	Overall             string               `json:"overall"`
	OS                  string               `json:"os"`
	Architecture        string               `json:"architecture"`
	Version             string               `json:"ivoai_version"`
	TestMode            bool                 `json:"test_mode"`
	ConfigPath          string               `json:"config_path"`
	StatePath           string               `json:"state_path"`
	SecretPath          string               `json:"secret_path"`
	SecretPermissions   string               `json:"secret_permissions"`
	Codex               Auth                 `json:"codex"`
	CodexCodeModeHost   Component            `json:"codex_code_mode_host"`
	Claude              Auth                 `json:"claude"`
	OpenCode            ManagedComponent     `json:"opencode"`
	Headroom            headroom.Status      `json:"headroom"`
	Caveman             ManagedComponent     `json:"caveman"`
	CompressionProvider string               `json:"compression_provider"`
	Memory              Component            `json:"ai_memory"`
	Ruflo               orchestration.Status `json:"ruflo"`
	Server              Server               `json:"server"`
	Orchestration       Orchestration        `json:"orchestration"`
	Automatic           Automatic            `json:"automatic_orchestration"`
	ComponentMatrix     core.Matrix          `json:"component_matrix"`
	SkillControlPlane   SkillControlPlane    `json:"skill_control_plane"`
	Issues              []string             `json:"issues"`
}
type Doctor struct {
	Store        *config.Store
	Runner       platform.Runner
	Version      string
	HTTPClient   *http.Client
	QuotaManager *quota.Manager
}

func (d Doctor) Run(ctx context.Context) Report {
	cfg, cfgErr := d.Store.Load()
	state, stateErr := d.Store.LoadState()
	r := Report{Overall: "READY", OS: runtime.GOOS, Architecture: runtime.GOARCH, Version: d.Version, TestMode: os.Getenv("IVOAI_TEST_MODE") == "1", ConfigPath: d.Store.Paths.Config, StatePath: d.Store.Paths.State, SecretPath: d.Store.Paths.Secrets, CompressionProvider: cfg.Compression.Provider}
	if cfgErr != nil {
		r.Issues = append(r.Issues, cfgErr.Error())
	}
	if stateErr != nil {
		r.Issues = append(r.Issues, stateErr.Error())
	}
	r.SecretPermissions = permissions(d.Store.Paths.Secrets)
	r.Codex = d.agent(ctx, "codex", []string{"login", "status"}, state.Components["codex"])
	r.CodexCodeModeHost = componentFromState(state.Components["codex-code-mode-host"])
	r.Claude = d.agent(ctx, "claude", []string{"auth", "status"}, state.Components["claude-code"])
	r.OpenCode = d.managedComponent("opencode", state.Components["opencode"])
	d.probeManagedVersion(ctx, &r.OpenCode)
	r.Headroom = (headroom.Manager{Runner: d.Runner, Binary: state.Components["headroom"].Path}).Inspect(ctx, cfg.Headroom.Enabled)
	if fixture := state.Components["headroom"]; !r.Headroom.Installed && strings.HasSuffix(fixture.Version, "-fixture") {
		r.Headroom.Installed, r.Headroom.Healthy, r.Headroom.CodexCompatible, r.Headroom.ClaudeCompatible, r.Headroom.Version, r.Headroom.InteractiveLaunch = true, true, true, true, fixture.Version, "fixture"
	}
	r.Caveman = d.managedComponent("caveman", state.Components["caveman"])
	r.Memory = componentFromState(state.Components["ai-memory"])
	r.Memory.Hooks = hooksInstalled(d.Store.Paths.HooksDir)
	if r.Memory.Fixture {
		r.Memory.Hooks = true
	}
	r.Ruflo = (orchestration.Manager{Runner: d.Runner, Binary: state.Components["ruflo"].Path, ProfileDir: d.Store.Paths.DataDir}).Inspect(ctx)
	if fixture := state.Components["ruflo"]; !r.Ruflo.Installed && strings.HasSuffix(fixture.Version, "-fixture") {
		r.Ruflo.Installed, r.Ruflo.Version = true, fixture.Version
	}
	r.Server = d.server(ctx, cfg.Connections.Server)
	r.Orchestration = d.orchestration(ctx, cfg, state)
	r.Automatic = d.automatic(ctx, cfg, state, r)
	r.ComponentMatrix = componentMatrix(cfg, state, r)
	r.SkillControlPlane = d.skillControlPlane()
	if !r.SkillControlPlane.RegistryReadable {
		r.Issues = append(r.Issues, "skill registry is unreadable or invalid")
	}
	if !r.SkillControlPlane.RegistryWritable {
		r.Issues = append(r.Issues, "skill registry private state root is not writable")
	}
	if !r.SkillControlPlane.PolicyEngineReady {
		r.Issues = append(r.Issues, "skill policy engine is not ready")
	}
	if r.SkillControlPlane.StagingRootHealth == "unhealthy" {
		r.Issues = append(r.Issues, "supply-chain staging root is unsafe")
	}
	required := map[string]Component{"Codex": componentFromAuth(r.Codex), "Claude Code": componentFromAuth(r.Claude), "Headroom": {Installed: r.Headroom.Installed}, "ai-memory": r.Memory, "Ruflo": {Installed: r.Ruflo.Installed}}
	if state.Components["codex"].Managed {
		required["Codex code-mode host"] = r.CodexCodeModeHost
	}
	for name, component := range required {
		if !component.Installed {
			r.Issues = append(r.Issues, name+" is not installed")
		}
	}
	if cfg.Memory.Enabled && r.Memory.Installed && !r.Memory.Hooks {
		r.Issues = append(r.Issues, "ai-memory hooks are not installed")
	}
	if cfg.Orchestration.Enabled {
		if !r.Ruflo.Installed {
			r.Issues = append(r.Issues, "Ruflo orchestration is enabled but Ruflo is not installed")
		} else if !r.Ruflo.SafeMode || r.Ruflo.ProviderExecution || r.Ruflo.DurableMemory {
			r.Issues = append(r.Issues, "Ruflo safe profile is not active or provider execution is enabled")
		}
		if !r.Orchestration.BridgeAvailable {
			r.Issues = append(r.Issues, "local orchestrator bridge is unavailable")
		}
		if r.Orchestration.SessionPerms != "0700" {
			r.Issues = append(r.Issues, "session state directory must be 0700")
		}
		if !r.Orchestration.CodexWorker {
			r.Issues = append(r.Issues, "Codex official non-interactive worker capability is unavailable")
		}
		if !r.Orchestration.ClaudeWorker {
			r.Issues = append(r.Issues, "Claude official non-interactive worker capability is unavailable")
		}
	}
	if r.SecretPermissions != "not-created" && r.SecretPermissions != "0600" {
		r.Issues = append(r.Issues, "secret file permissions must be 0600")
	}
	for _, directory := range []string{d.Store.Paths.ConfigDir, d.Store.Paths.DataDir, d.Store.Paths.StateDir} {
		if mode := permissions(directory); mode != "0700" {
			r.Issues = append(r.Issues, fmt.Sprintf("private directory %s must be 0700 (got %s)", directory, mode))
		}
	}
	if cfg.Connections.ChatGPT.Status == "connected" && !r.Codex.Authenticated {
		r.Issues = append(r.Issues, "ChatGPT connection authentication is not valid")
	}
	if cfg.Connections.Claude.Status == "connected" && !r.Claude.Authenticated {
		r.Issues = append(r.Issues, "Claude connection authentication is not valid")
	}
	if r.Server.Configured && (!r.Server.Reachable || !r.Server.ProtocolCompatible || (!r.Server.TLS && !loopbackServer(r.Server.URL))) {
		r.Issues = append(r.Issues, "configured server is unreachable, incompatible, or not protected by TLS")
	}
	if len(r.Issues) > 0 {
		r.Overall = "DEGRADED"
	}
	return r
}

func (d Doctor) skillControlPlane() SkillControlPlane {
	result := SkillControlPlane{RegistrySchema: skills.RegistrySchemaVersion, ProvenanceHealth: "not_initialized", StagingRootHealth: "not_initialized"}
	registryPath := skills.RegistryPath(d.Store.Paths.StateDir)
	registry, err := (skills.Store{Path: registryPath}).Load()
	result.RegistryReadable = err == nil
	registryRoot := filepath.Dir(registryPath)
	if _, statErr := os.Lstat(registryRoot); errors.Is(statErr, os.ErrNotExist) {
		registryRoot = d.Store.Paths.StateDir
	}
	result.RegistryWritable = privateWritableDirectory(registryRoot)
	if err == nil {
		if len(registry.Entries) > 0 {
			result.ProvenanceHealth = "healthy"
		}
		for _, entry := range registry.Entries {
			if (entry.Lifecycle == skills.LifecycleActive || entry.Lifecycle == skills.LifecycleStaged) && !entry.Provenance.Integrity.Verified {
				result.ProvenanceHealth = "pending"
			}
			switch entry.Lifecycle {
			case skills.LifecycleActive:
				result.Active++
			case skills.LifecycleStaged:
				result.Staged++
			case skills.LifecycleQuarantined:
				result.Quarantined++
			}
		}
	} else {
		result.ProvenanceHealth = "unhealthy"
	}
	result.PolicyEngineReady = policy.DefaultEngine().Validate() == nil
	supplyRoot := filepath.Join(d.Store.Paths.DataDir, "supply-chain")
	if _, statErr := os.Lstat(supplyRoot); statErr == nil {
		if privateWritableDirectory(supplyRoot) {
			if pointerErr := supplychain.ValidateRoot(supplyRoot); pointerErr == nil {
				result.StagingRootHealth = "healthy"
			} else {
				result.StagingRootHealth = "unhealthy"
			}
		} else {
			result.StagingRootHealth = "unhealthy"
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		result.StagingRootHealth = "unhealthy"
	}
	return result
}

func (d Doctor) managedComponent(id string, state config.ComponentState) ManagedComponent {
	result := ManagedComponent{Component: componentFromState(state)}
	result.Healthy = result.Installed
	source, root, err := (supplychain.Manager{Root: filepath.Join(d.Store.Paths.DataDir, "supply-chain")}).Active(id)
	if err != nil {
		return result
	}
	want := filepath.Join(root, filepath.FromSlash(source.PayloadPath))
	if filepath.Clean(state.Path) != filepath.Clean(want) {
		return result
	}
	result.Revision, result.Source, result.License, result.TrustLevel = source.Revision, source.Source, source.License, source.Integrity.TrustLevel
	return result
}

func (d Doctor) probeManagedVersion(ctx context.Context, component *ManagedComponent) {
	if component == nil || !component.Installed || component.Path == "" || d.Runner == nil {
		return
	}
	result, err := d.Runner.Run(ctx, component.Path, []string{"--version"}, platform.RunOptions{Timeout: 10 * time.Second, CleanEnv: true, Env: []string{"PATH=/usr/bin:/bin"}})
	if err != nil {
		component.Healthy = false
		return
	}
	if version := strings.TrimSpace(result.Stdout); version != "" {
		component.Version = version
	}
}

func privateWritableDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink == 0 && info.IsDir() && info.Mode().Perm() == 0o700 && unix.Access(path, unix.W_OK|unix.X_OK) == nil
}

func componentMatrix(cfg config.Config, state config.State, report Report) core.Matrix {
	health := func(available bool) core.HealthState {
		if available {
			return core.HealthHealthy
		}
		return core.HealthUnavailable
	}
	compatibility := func(available bool, reason string) core.Compatibility {
		if available {
			return core.Compatibility{State: core.CompatibilityCompatible}
		}
		return core.Compatibility{State: core.CompatibilityUnknown, Reason: reason}
	}
	executor := func(id core.ComponentID, auth Auth, component config.ComponentState, worker bool) core.ComponentStatus {
		workerSupport := core.SupportUnsupported
		if worker {
			workerSupport = core.SupportSupported
		}
		return core.ComponentStatus{
			ID: id, Implementation: "official-cli", Active: true,
			Installed: auth.Installed, Managed: component.Managed, Available: auth.Installed,
			Health: health(auth.Installed), Lifecycle: core.LifecycleStopped,
			Provenance: core.Provenance{Source: "official_probe", Version: auth.Version, Path: component.Path},
			Capabilities: core.CapabilitySet{
				core.CapabilitySessionStart:    core.SupportSupported,
				core.CapabilitySessionAbort:    core.SupportSupported,
				core.CapabilityAdvisoryExecute: workerSupport,
			},
			Compatibility: compatibility(auth.Installed, "official client is not available"),
		}
	}
	values := []core.ComponentStatus{
		executor(core.ComponentCodex, report.Codex, state.Components["codex"], report.Orchestration.CodexWorker),
		executor(core.ComponentClaude, report.Claude, state.Components["claude-code"], report.Orchestration.ClaudeWorker),
		executor(core.ComponentOpenCode, Auth{Installed: report.OpenCode.Installed && report.OpenCode.Healthy, Version: report.OpenCode.Version}, state.Components["opencode"], false),
	}
	memory := state.Components["ai-memory"]
	memoryAvailable := report.Memory.Installed && (!cfg.Memory.Enabled || report.Memory.Hooks)
	values = append(values, core.ComponentStatus{
		ID: core.ComponentMemory, Implementation: "ai-memory", Active: cfg.Memory.Enabled,
		Installed: report.Memory.Installed, Managed: memory.Managed, Available: memoryAvailable,
		Health: health(memoryAvailable), Lifecycle: core.LifecycleStopped,
		Provenance: core.Provenance{Source: "state_and_hook_probe", Version: report.Memory.Version, Path: memory.Path},
		Capabilities: core.CapabilitySet{
			core.CapabilityMemoryConfigure: core.SupportSupported,
			core.CapabilityMemoryHooks:     core.SupportSupported,
			core.CapabilityMemoryStatus:    core.SupportSupported,
		},
		Compatibility: compatibility(report.Memory.Installed, "ai-memory client is not installed"),
	})
	cavemanAvailable := report.Caveman.Installed && report.Caveman.Healthy
	values = append(values, core.ComponentStatus{
		ID: core.ComponentCompression, Implementation: "caveman", Active: cfg.Compression.Provider == "caveman",
		Installed: report.Caveman.Installed, Managed: state.Components["caveman"].Managed, Available: cavemanAvailable,
		Health: health(cavemanAvailable), Lifecycle: core.LifecycleStopped,
		Provenance: core.Provenance{Source: "managed_supply_chain", Version: report.Caveman.Version, Path: state.Components["caveman"].Path},
		Capabilities: core.CapabilitySet{
			core.CapabilityCompressionWrap:   map[bool]core.SupportState{true: core.SupportSupported, false: core.SupportUnsupported}[cavemanAvailable],
			core.CapabilityCompressionBypass: core.SupportSupported,
		},
		Compatibility: compatibility(cavemanAvailable, "managed Caveman runtime is unavailable"),
		Fallback:      core.Fallback{Allowed: true, Reason: "direct official executor remains available before launch"},
	})
	headroomAvailable := cfg.Headroom.Enabled && report.Headroom.Healthy && (report.Headroom.CodexCompatible || report.Headroom.ClaudeCompatible)
	values = append(values, core.ComponentStatus{
		ID: core.ComponentCompression, Implementation: "headroom", Active: cfg.Compression.Provider == "headroom" && cfg.Headroom.Enabled,
		Installed: report.Headroom.Installed, Managed: state.Components["headroom"].Managed, Available: headroomAvailable,
		Health: health(headroomAvailable), Lifecycle: core.LifecycleStopped,
		Provenance: core.Provenance{Source: "official_probe", Version: report.Headroom.Version, Path: state.Components["headroom"].Path},
		Capabilities: core.CapabilitySet{
			core.CapabilityCompressionWrap:   map[bool]core.SupportState{true: core.SupportSupported, false: core.SupportUnsupported}[headroomAvailable],
			core.CapabilityCompressionBypass: core.SupportSupported,
		},
		Compatibility: compatibility(headroomAvailable, "wrapper compatibility is unavailable"),
		Fallback:      core.Fallback{Allowed: true, Reason: "direct official executor remains available"},
	})
	orchestrationAvailable := cfg.Orchestration.Enabled && report.Ruflo.Installed && report.Ruflo.SafeMode && !report.Ruflo.ProviderExecution && !report.Ruflo.DurableMemory
	values = append(values, core.ComponentStatus{
		ID: core.ComponentOrchestration, Implementation: "ruflo", Active: cfg.Orchestration.Enabled,
		Installed: report.Ruflo.Installed, Managed: state.Components["ruflo"].Managed, Available: orchestrationAvailable,
		Health: health(orchestrationAvailable), Lifecycle: core.LifecycleStopped,
		Provenance: core.Provenance{Source: "safe_profile_probe", Version: report.Ruflo.Version, Path: state.Components["ruflo"].Path},
		Capabilities: core.CapabilitySet{
			core.CapabilityOrchestrationSwarm:     map[bool]core.SupportState{true: core.SupportSupported, false: core.SupportUnsupported}[orchestrationAvailable],
			core.CapabilityOrchestrationLifecycle: map[bool]core.SupportState{true: core.SupportSupported, false: core.SupportUnsupported}[orchestrationAvailable],
		},
		Compatibility: compatibility(orchestrationAvailable, "safe coordination profile is unavailable"),
		Fallback:      core.Fallback{Allowed: false, Reason: "orchestrated modes fail closed"},
	})
	contextConfigured := false
	for _, server := range cfg.MCP.Servers {
		if server.Kind == "context" && server.Enabled {
			contextConfigured = true
			break
		}
	}
	contextAvailable := contextConfigured && report.Server.Reachable && report.Server.ProtocolCompatible
	values = append(values, core.ComponentStatus{
		ID: core.ComponentContext, Implementation: "remote-context-mcp", Active: contextConfigured,
		Installed: contextConfigured, Available: contextAvailable, Health: health(contextAvailable), Lifecycle: core.LifecycleStopped,
		Provenance: core.Provenance{Source: "server_discovery"},
		Capabilities: core.CapabilitySet{
			core.CapabilityContextSearch: core.SupportSupported,
			core.CapabilityContextRead:   core.SupportSupported,
			core.CapabilityContextRecent: core.SupportSupported,
			core.CapabilityContextStatus: core.SupportSupported,
			core.CapabilityContextIngest: core.SupportUnsupported,
		},
		Compatibility: compatibility(contextAvailable, "compatible remote Context MCP is unavailable"),
	})
	matrix, err := core.NewMatrix(values...)
	if err != nil {
		return core.Matrix{}
	}
	return matrix
}

func (d Doctor) automatic(ctx context.Context, cfg config.Config, state config.State, report Report) Automatic {
	result := Automatic{
		Enabled: cfg.Orchestration.Auto.Enabled, DefaultPlanner: cfg.Orchestration.Auto.DefaultPlanner,
		AutomaticFailover:        cfg.Orchestration.Auto.AutomaticFailover,
		CheckpointReady:          cfg.Orchestration.Auto.CheckpointEnabled && report.Memory.Installed && report.Memory.Hooks,
		Quota:                    map[string]QuotaProbe{},
		SchedulerReady:           cfg.Orchestration.Auto.Enabled && cfg.Orchestration.Auto.Optimization.Strategy == "efficient",
		ParallelRuntime:          cfg.Orchestration.Auto.Optimization.Parallelism && cfg.Orchestration.Auto.MaxWorkers > 1,
		SharedKnowledgeBootstrap: cfg.Orchestration.Auto.Optimization.SharedContextBootstrap,
		CodexModelRouting:        "degraded", ClaudeModelRouting: "degraded", CodexEffortControl: "unsupported", ClaudeEffortControl: "unsupported",
	}
	if report.TestMode {
		result.CodexModelRouting, result.ClaudeModelRouting = "ready", "ready"
		result.CodexEffortControl, result.ClaudeEffortControl = "supported", "supported"
		for _, provider := range []quota.Provider{quota.ProviderCodex, quota.ProviderClaude} {
			probe := QuotaProbe{Ready: true, Authenticated: true, Eligible: true, Source: "fixture", WeeklySource: "N/A / not exposed", MonthlySource: "N/A / not exposed"}
			if provider == quota.ProviderClaude {
				probe.FiveHourSource, probe.WeeklySource = "awaiting first response", "awaiting first response"
			}
			result.Quota[string(provider)] = probe
		}
	} else {
		registry := (routing.Discoverer{CodexPath: state.Components["codex"].Path, ClaudePath: state.Components["claude-code"].Path, CachePath: filepath.Join(d.Store.Paths.CacheDir, "capabilities.json")}).Discover(ctx)
		for provider, capability := range registry.Providers {
			routingState, effortState := "degraded", "unsupported"
			if capability.WorkerCapable && len(capability.Models) > 0 {
				routingState = "ready"
			}
			if capability.SupportsEffort {
				effortState = "supported"
			}
			if provider == "codex" {
				result.CodexModelRouting, result.CodexEffortControl = routingState, effortState
			}
			if provider == "claude" {
				result.ClaudeModelRouting, result.ClaudeEffortControl = routingState, effortState
			}
		}
		manager := d.QuotaManager
		if manager == nil {
			ttl := time.Duration(cfg.Orchestration.Auto.QuotaRefreshSeconds) * time.Second
			manager = &quota.Manager{Store: quota.Store{Root: d.Store.Paths.QuotaDir}, TTL: ttl, Probes: map[quota.Provider]quota.Probe{
				quota.ProviderCodex:  quota.CodexAdapter{Binary: state.Components["codex"].Path},
				quota.ProviderClaude: quota.ClaudeAdapter{Binary: state.Components["claude-code"].Path, Runner: d.Runner, Store: quota.Store{Root: d.Store.Paths.QuotaDir}, TTL: ttl},
			}}
		}
		for _, provider := range []quota.Provider{quota.ProviderCodex, quota.ProviderClaude} {
			value, err := manager.Probe(ctx, provider, true)
			result.Quota[string(provider)] = quotaProbeDiagnostic(value, err)
		}
	}
	codex := result.Quota[string(quota.ProviderCodex)]
	claude := result.Quota[string(quota.ProviderClaude)]
	result.CodexToClaude = result.AutomaticFailover && claude.Authenticated
	result.ClaudeToCodex = result.AutomaticFailover && codex.Authenticated
	return result
}

func quotaProbeDiagnostic(value quota.ProviderQuota, probeErr error) QuotaProbe {
	weekly, weeklyOK := value.Window(quota.KindWeekly)
	fiveHour, fiveHourOK := value.WindowByDuration(300)
	if value.Provider == quota.ProviderClaude && !fiveHourOK {
		fiveHour, fiveHourOK = value.Window(quota.KindSession)
	}
	monthly, monthlyOK := value.Window(quota.KindMonthly)
	individual, individualOK := value.Window(quota.KindIndividual)
	result := QuotaProbe{Ready: probeErr == nil && value.Authenticated, Authenticated: value.Authenticated, Eligible: value.Eligible, Source: value.Source, FiveHourSource: "N/A / not exposed", WeeklySource: "N/A / not exposed", MonthlySource: "N/A / not exposed", IndividualSource: "N/A / not exposed", Reason: value.Reason}
	if value.Provider == quota.ProviderClaude && !fiveHourOK {
		result.FiveHourSource = "awaiting first response"
	}
	if fiveHourOK {
		result.FiveHourSource = quotaDiagnosticSource(fiveHour)
	}
	if weeklyOK && weekly.Available && weekly.Authoritative {
		result.WeeklySource = weekly.Source
	} else if weeklyOK {
		result.WeeklySource = quotaDiagnosticSource(weekly)
	} else if value.Provider == quota.ProviderClaude {
		result.WeeklySource = "awaiting first response"
	}
	if monthlyOK && monthly.Available && monthly.Authoritative {
		result.MonthlySource = monthly.Source
	}
	if individualOK && individual.Available && individual.Authoritative {
		result.IndividualSource = individual.Source
	}
	for _, window := range value.Windows {
		entry := QuotaWindowProbe{Kind: window.Kind, DurationMinutes: window.DurationMinutes, TelemetryState: window.TelemetryState(), ResetsAt: window.ResetsAt, Source: window.Source}
		if window.Available && window.Authoritative {
			remaining := window.RemainingPercent
			entry.RemainingPercent = &remaining
		}
		result.Windows = append(result.Windows, entry)
	}
	return result
}

func quotaDiagnosticSource(window quota.Window) string {
	switch window.TelemetryState() {
	case quota.TelemetryPending:
		return "awaiting first response"
	case quota.TelemetryStale:
		return window.Source + " / stale"
	case quota.TelemetryAvailable, quota.TelemetryExhausted:
		if window.Available && window.Authoritative {
			return window.Source
		}
		return "N/A / not exposed"
	default:
		return "N/A / not exposed"
	}
}

func (d Doctor) orchestration(ctx context.Context, cfg config.Config, state config.State) Orchestration {
	result := Orchestration{Enabled: cfg.Orchestration.Enabled, SessionDirectory: d.Store.Paths.SessionsDir, SessionPerms: permissions(d.Store.Paths.SessionsDir), MaxWorkers: cfg.Orchestration.MaxWorkers}
	if executable, err := os.Executable(); err == nil {
		if info, statErr := os.Stat(executable); statErr == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			result.BridgeAvailable = true
		}
	}
	result.CodexWorker = d.workerCapability(ctx, state.Components["codex"], []string{"exec", "--help"})
	result.ClaudeWorker = d.workerCapability(ctx, state.Components["claude-code"], []string{"--help"})
	return result
}

func (d Doctor) workerCapability(ctx context.Context, component config.ComponentState, args []string) bool {
	if strings.HasSuffix(component.Version, "-fixture") {
		return component.Installed
	}
	if component.Path == "" || !component.Installed {
		return false
	}
	info, err := os.Stat(component.Path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return false
	}
	_, err = d.Runner.Run(ctx, component.Path, args, platform.RunOptions{Timeout: 15 * time.Second})
	return err == nil
}

func loopbackServer(raw string) bool {
	base, err := connections.ValidateBaseURL(raw)
	if err != nil {
		return false
	}
	host := base.Hostname()
	return strings.EqualFold(host, "localhost") || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
}

func (d Doctor) agent(ctx context.Context, name string, statusArgs []string, state config.ComponentState) Auth {
	a := Auth{Installed: state.Installed, Version: state.Version}
	path := state.Path
	var err error
	if path != "" {
		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
			if !(state.Installed && strings.HasSuffix(state.Version, "-fixture")) {
				return a
			}
		}
	} else {
		path, err = d.Runner.LookPath(name)
	}
	if err != nil || path == "" {
		if state.Installed && strings.HasSuffix(state.Version, "-fixture") {
			a.Installed = true
		}
		return a
	}
	a.Installed = true
	if version, versionErr := d.Runner.Run(ctx, path, []string{"--version"}, platform.RunOptions{Timeout: 10 * time.Second}); versionErr == nil {
		a.Version = strings.TrimSpace(version.Stdout)
	}
	status, err := d.Runner.Run(ctx, path, statusArgs, platform.RunOptions{Timeout: 15 * time.Second})
	a.Authenticated = connections.AuthenticationStatus(status, err)
	return a
}
func (d Doctor) server(ctx context.Context, connection config.Connection) Server {
	s := Server{Configured: connection.Status == "connected", URL: connection.URL}
	if !s.Configured {
		return s
	}
	base, err := connections.ValidateBaseURL(connection.URL)
	if err != nil {
		return s
	}
	s.TLS = base.Scheme == "https"
	client := d.HTTPClient
	if client == nil {
		client = connections.SecureHTTPClient()
		// DNS on WSL and split-horizon VPN hosts can legitimately consume the
		// resolver's first five-second attempt. A five-second total deadline
		// therefore reported healthy remote services as unreachable before the
		// TCP/TLS request even began.
		client.Timeout = 8 * time.Second
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base.String(), "/")+"/.well-known/ivoai", nil)
	if err != nil {
		return s
	}
	resp, err := client.Do(req)
	if err != nil {
		return s
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return s
	}
	var discovery connections.Discovery
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&discovery); err != nil {
		return s
	}
	s.Reachable = true
	s.ProtocolCompatible = discovery.ProtocolVersion == connections.ProtocolVersion
	if !doctorProbe(ctx, client, base.String(), discovery.HealthEndpoint) || !doctorProbe(ctx, client, base.String(), discovery.ReadyEndpoint) {
		s.Reachable = false
	}
	return s
}

// ProbeServer is the bounded live-health contract shared by status, automatic
// preflight and Doctor. Callers may choose a shorter HTTP client timeout, but
// they cannot reinterpret stored configuration as a live connection.
func ProbeServer(ctx context.Context, connection config.Connection, client *http.Client) Server {
	return (Doctor{HTTPClient: client}).server(ctx, connection)
}

func doctorProbe(ctx context.Context, client *http.Client, base, endpoint string) bool {
	if !strings.HasPrefix(endpoint, "/") || strings.HasPrefix(endpoint, "//") || strings.ContainsAny(endpoint, "?#\\") {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+endpoint, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
func componentFromState(s config.ComponentState) Component {
	fixture := strings.HasSuffix(s.Version, "-fixture")
	installed := s.Installed && fixture
	if s.Installed && !fixture && s.Path != "" {
		info, err := os.Stat(s.Path)
		installed = err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
	}
	return Component{Installed: installed, Version: s.Version, Path: s.Path, Managed: s.Managed, Fixture: fixture}
}
func componentFromAuth(a Auth) Component {
	return Component{Installed: a.Installed, Version: a.Version}
}
func hooksInstalled(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) > 0
}
func permissions(path string) string {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "not-created"
	}
	if err != nil {
		return "unreadable"
	}
	return fmt.Sprintf("%04o", info.Mode().Perm())
}
func (r Report) JSON() ([]byte, error) { return json.MarshalIndent(r, "", "  ") }
