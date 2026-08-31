package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/agents"
	"github.com/ivo-lopes/ivoai/internal/components"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/connections"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/doctor"
	"github.com/ivo-lopes/ivoai/internal/memory"
	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/project"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/secrets"
	"github.com/ivo-lopes/ivoai/internal/server"
	"github.com/ivo-lopes/ivoai/internal/serverpool"
	"github.com/ivo-lopes/ivoai/internal/skills"
	"github.com/ivo-lopes/ivoai/internal/terminalui"
	"github.com/ivo-lopes/ivoai/internal/update"
	"golang.org/x/term"
)

type App struct {
	Version          string
	Store            *config.Store
	Runner           platform.Runner
	In               io.Reader
	Out, Err         io.Writer
	QuotaManager     *quota.Manager
	AutoPollInterval time.Duration
	HTTPClient       *http.Client
	// ExecutablePath is accepted only in IVOAI_TEST_MODE so hermetic update
	// matrix tests never replace the running go test binary.
	ExecutablePath string
}

const liveServiceProbeTimeout = 8 * time.Second

// MenuSnapshot is a non-secret, read-only view used by the interactive UI.
// It deliberately contains no endpoint credentials or raw configuration.
type MenuSnapshot struct {
	SetupComplete         bool
	ComponentsReady       bool
	ChatGPTConnected      bool
	ClaudeConnected       bool
	ServerConnected       bool
	MemoryEnabled         bool
	HeadroomEnabled       bool
	RufloEnabled          bool
	DefaultMode           string
	PrimaryExecutor       string
	ReviewExecutor        string
	MaxWorkers            int
	AutoEnabled           bool
	DefaultPlanner        string
	AutomaticFailover     bool
	CheckpointEnabled     bool
	AutoMaxWorkers        int
	AutoStrategy          string
	AutoParallelism       bool
	SharedBootstrap       bool
	ProgressiveEscalation bool
}

func New(version string, in io.Reader, out, errOut io.Writer) (*App, error) {
	paths, err := config.ResolvePaths()
	if err != nil {
		return nil, err
	}
	return &App{Version: version, Store: config.NewStore(paths), Runner: platform.ExecRunner{}, In: in, Out: out, Err: errOut}, nil
}

func (a *App) MenuSnapshot() (MenuSnapshot, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return MenuSnapshot{}, err
	}
	state, err := a.Store.LoadState()
	if err != nil {
		return MenuSnapshot{}, err
	}
	return MenuSnapshot{
		SetupComplete:         !state.SetupCompletedAt.IsZero(),
		ComponentsReady:       requiredComponentsReady(state),
		ChatGPTConnected:      cfg.Connections.ChatGPT.Status == "connected",
		ClaudeConnected:       cfg.Connections.Claude.Status == "connected",
		ServerConnected:       len(cfg.Connections.Servers) > 0 || cfg.Connections.Server.Status == "connected",
		MemoryEnabled:         cfg.Memory.Enabled,
		HeadroomEnabled:       cfg.Headroom.Enabled,
		RufloEnabled:          cfg.Orchestration.Enabled,
		DefaultMode:           cfg.Orchestration.DefaultMode,
		PrimaryExecutor:       cfg.Orchestration.PrimaryExecutor,
		ReviewExecutor:        cfg.Orchestration.ReviewExecutor,
		MaxWorkers:            cfg.Orchestration.MaxWorkers,
		AutoEnabled:           cfg.Orchestration.Auto.Enabled,
		DefaultPlanner:        cfg.Orchestration.Auto.DefaultPlanner,
		AutomaticFailover:     cfg.Orchestration.Auto.AutomaticFailover,
		CheckpointEnabled:     cfg.Orchestration.Auto.CheckpointEnabled,
		AutoMaxWorkers:        cfg.Orchestration.Auto.MaxWorkers,
		AutoStrategy:          cfg.Orchestration.Auto.Optimization.Strategy,
		AutoParallelism:       cfg.Orchestration.Auto.Optimization.Parallelism,
		SharedBootstrap:       cfg.Orchestration.Auto.Optimization.SharedContextBootstrap,
		ProgressiveEscalation: cfg.Orchestration.Auto.Optimization.ProgressiveEscalation,
	}, nil
}

func (a *App) Setup(ctx context.Context) error {
	if err := a.Store.Ensure(); err != nil {
		return err
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return err
	}
	if err := a.Store.Save(cfg); err != nil {
		return err
	}
	secretStore := secrets.Store{Path: a.Store.Paths.Secrets}
	if _, err := os.Stat(a.Store.Paths.Secrets); os.IsNotExist(err) {
		if err := secretStore.Save(secrets.Data{}); err != nil {
			return err
		}
	} else {
		data, err := secretStore.Load()
		if err != nil {
			return err
		}
		if err := secretStore.Save(data); err != nil {
			return err
		}
	}
	installer := components.Installer{Runner: a.Runner, Store: a.Store, Out: a.Out}
	if err := installer.Setup(ctx); err != nil {
		return err
	}
	state, err := a.Store.LoadState()
	if err != nil {
		return err
	}
	if err := a.orchestrationManager(state).Configure(ctx, cfg.Orchestration.Enabled); err != nil {
		a.warn("Ruflo safe profile is degraded", err)
	}
	mem := a.memoryManager(state)
	if cfg.Memory.Enabled && len(cfg.Connections.Servers) > 0 {
		if err := a.ReconfigureMemory(ctx); err != nil {
			a.warn("remote ai-memory integration is degraded; Codex and Claude Code remain usable", err)
		}
	} else if cfg.Memory.Enabled {
		if err := mem.Configure(ctx, core.MemoryConfiguration{InstallMCP: true, InstallHooks: true}); err != nil {
			a.warn("ai-memory hooks are degraded; Codex and Claude Code remain usable", err)
		}
	} else if err := mem.Disable(ctx); err != nil {
		a.warn("ai-memory is disabled but its previous integration could not be removed", err)
	} else if len(cfg.Connections.Servers) > 0 {
		if err := a.agentMCP(state).RemoveRemote(ctx); err != nil {
			a.warn("legacy global server MCP entries could not be removed", err)
		}
	}
	state.SetupCompletedAt = time.Now().UTC()
	if err := a.Store.SaveState(state); err != nil {
		return err
	}
	a.success("ivoai client setup complete")
	return nil
}

// ExistingServerInstallation detects the stable server marker without
// modifying it. The published v0.5.0 updater invokes a new binary as plain
// `ivoai setup`; this keeps that legacy bridge on the server setup path.
func (a *App) ExistingServerInstallation() (bool, error) {
	serverRoot := testServerRoot()
	if os.Geteuid() != 0 && serverRoot == "" {
		return false, nil
	}
	layout := server.DefaultLayout(serverRoot)
	path := filepath.Join(layout.ConfigDir, "server.toml")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, errors.New("existing server marker must be a regular non-symlink file")
	}
	return true, nil
}

func (a *App) Status(ctx context.Context) error {
	cfg, err := a.Store.Load()
	if err != nil {
		return err
	}
	if _, err := serverpool.New(cfg.Connections.Servers); err != nil {
		return fmt.Errorf("validate server profiles: %w", err)
	}
	secretData, err := (secrets.Store{Path: a.Store.Paths.Secrets}).Load()
	if err != nil {
		return err
	}
	state, err := a.Store.LoadState()
	if err != nil {
		return err
	}
	probeContext, cancelProbes := context.WithTimeout(ctx, liveServiceProbeTimeout)
	defer cancelProbes()
	rufloResult := make(chan orchestration.Status, 1)
	go func() { rufloResult <- a.orchestrationManager(state).Inspect(probeContext) }()
	serverProfiles, serverHealth := a.probeServerProfiles(probeContext, cfg)
	rufloHealth := <-rufloResult
	compressionStatus := readyStatus(false)
	switch cfg.Compression.Provider {
	case "direct":
		compressionStatus = statusValue{Text: "ready / direct", Kind: terminalui.StatusSuccess}
	case "headroom":
		compressionStatus = headroomStatus(state.Components["headroom"], cfg.Headroom.Enabled)
	case "caveman":
		compressionStatus = optionalManagedStatus(state.Components["caveman"], "selected / caveman")
	}
	rows := []struct {
		name   string
		status statusValue
	}{
		{"ivoai", readyStatus(state.SetupCompletedAt.IsZero())},
		{"Codex", componentStatus(state.Components["codex"], cfg.Connections.ChatGPT.Status)},
		{"Codex tools", codexToolHostStatus(state)},
		{"Claude Code", componentStatus(state.Components["claude-code"], cfg.Connections.Claude.Status)},
		{"OpenCode", optionalManagedStatus(state.Components["opencode"], "ready / direct primary")},
		{"Headroom", headroomStatus(state.Components["headroom"], cfg.Headroom.Enabled)},
		{"Compression", compressionStatus},
		{"Context", contextHealthStatus(cfg, serverHealth)},
		{"ai-memory", memoryHealthStatus(cfg, state.Components["ai-memory"], serverHealth)},
		{"Research", researchPriorityStatus(cfg)},
		{"Ruflo", safeStatus(rufloHealth)},
		{"Server", liveServerStatus(serverHealth)},
	}
	for _, profile := range serverProfiles {
		status := liveServerStatus(profile.Health)
		_, credentialConfigured := secretData.Servers[profile.ID]
		features := []string{}
		if profile.ContextMCPURL != "" {
			features = append(features, "context")
		}
		if profile.MemoryMCPURL != "" {
			features = append(features, "memory")
		}
		status.Text = fmt.Sprintf("%s / purpose=%s / protocol=%d / credential=%t / features=%s", status.Text, profile.Purpose, profile.Protocol, credentialConfigured, strings.Join(features, ","))
		if profile.RedundancyGroup != "" {
			status.Text += fmt.Sprintf(" / group=%s priority=%d", profile.RedundancyGroup, profile.Priority)
		}
		rows = append(rows, struct {
			name   string
			status statusValue
		}{"Server " + profile.Alias, status})
	}
	quotaSnapshot, quotaErr := (quota.Store{Root: a.Store.Paths.QuotaDir}).Load()
	autoReady := cfg.Orchestration.Auto.Enabled && cfg.Orchestration.Auto.Quota.Enabled && componentPresent(state.Components["codex"]) && componentPresent(state.Components["claude-code"]) && rufloHealth.SafeMode && !rufloHealth.ProviderExecution && !rufloHealth.DurableMemory
	autoStatus := "disabled"
	autoKind := terminalui.StatusNeutral
	if autoReady {
		autoStatus = "ready / default " + displayProvider(cfg.Orchestration.Auto.DefaultPlanner)
		autoKind = terminalui.StatusSuccess
	} else if cfg.Orchestration.Auto.Enabled {
		autoStatus = "degraded"
		autoKind = terminalui.StatusWarning
	}
	routingStatus := "N/A / telemetry not captured"
	if quotaErr == nil && len(quotaSnapshot.Providers) > 0 {
		routingStatus = "ready / cached"
	}
	rows = append(rows,
		struct {
			name   string
			status statusValue
		}{"Skill registry", skillRegistryStatus(skills.Store{Path: skills.RegistryPath(a.Store.Paths.StateDir)})},
		struct {
			name   string
			status statusValue
		}{"Auto", statusValue{autoStatus, autoKind}},
		struct {
			name   string
			status statusValue
		}{"Quota routing", statusValue{routingStatus, map[bool]terminalui.StatusKind{true: terminalui.StatusSuccess, false: terminalui.StatusNeutral}[quotaErr == nil && len(quotaSnapshot.Providers) > 0]}},
		struct {
			name   string
			status statusValue
		}{"Failover", optionalStatus(cfg.Orchestration.Auto.AutomaticFailover)},
		struct {
			name   string
			status statusValue
		}{"Codex 5h", quotaStatusDuration(quota.ProviderCodex, quotaSnapshot.Providers[quota.ProviderCodex], 300)},
		struct {
			name   string
			status statusValue
		}{"Codex weekly", quotaStatus(quota.ProviderCodex, quotaSnapshot.Providers[quota.ProviderCodex], quota.KindWeekly)},
		struct {
			name   string
			status statusValue
		}{"Claude 5h", quotaStatus(quota.ProviderClaude, quotaSnapshot.Providers[quota.ProviderClaude], quota.KindSession)},
		struct {
			name   string
			status statusValue
		}{"Claude weekly", quotaStatus(quota.ProviderClaude, quotaSnapshot.Providers[quota.ProviderClaude], quota.KindWeekly)},
	)
	if sessions, sessionErr := a.SessionList(); sessionErr == nil {
		active, orchestrated := 0, 0
		for _, value := range sessions {
			if value.Active() {
				active++
				if value.Mode == "orchestrated" {
					orchestrated++
				}
			}
		}
		if active > 0 {
			rows = append(rows, struct {
				name   string
				status statusValue
			}{"Sessions", statusValue{fmt.Sprintf("%d active / %d orchestrated", active, orchestrated), terminalui.StatusSuccess}})
		}
	}
	for _, row := range rows {
		fmt.Fprintf(a.Out, "%-14s %s\n", row.name, terminalui.Semantic(row.status.Text, row.status.Kind, terminalui.ColorEnabled(a.Out)))
	}
	if state.SetupCompletedAt.IsZero() {
		fmt.Fprintf(a.Out, "\nOverall: %s\n", terminalui.Warning("SETUP REQUIRED", terminalui.ColorEnabled(a.Out)))
	} else if !requiredComponentsReady(state) {
		fmt.Fprintf(a.Out, "\nOverall: %s\n", terminalui.Warning("DEGRADED — run ivoai setup to repair components", terminalui.ColorEnabled(a.Out)))
	} else if !rufloHealth.SafeMode || rufloHealth.ProviderExecution || rufloHealth.DurableMemory {
		fmt.Fprintf(a.Out, "\nOverall: %s\n", terminalui.Warning("DEGRADED — run ivoai setup to repair the Ruflo safe profile", terminalui.ColorEnabled(a.Out)))
	} else if serverHealth.Configured && (!serverHealth.Reachable || !serverHealth.ProtocolCompatible || (!serverHealth.TLS && !loopbackURL(serverHealth.URL))) {
		fmt.Fprintf(a.Out, "\nOverall: %s\n", terminalui.Warning("DEGRADED", terminalui.ColorEnabled(a.Out))+" — local agents available; run ivoai doctor")
	} else if cfg.Connections.ChatGPT.Status != "connected" || cfg.Connections.Claude.Status != "connected" || !serverHealth.Configured {
		fmt.Fprintf(a.Out, "\nOverall: %s\n", terminalui.Success("READY", terminalui.ColorEnabled(a.Out))+" — external connections pending")
	} else {
		fmt.Fprintf(a.Out, "\nOverall: %s\n", terminalui.Success("READY — all connections active", terminalui.ColorEnabled(a.Out)))
	}
	return nil
}

type statusServerProfile struct {
	config.ServerProfile
	Health doctor.Server
}

func (a *App) probeServerProfiles(ctx context.Context, cfg config.Config) ([]statusServerProfile, doctor.Server) {
	if len(cfg.Connections.Servers) == 0 {
		legacy := doctor.ProbeServer(ctx, cfg.Connections.Server, a.statusHTTPClient())
		return nil, legacy
	}
	aliases := make([]string, 0, len(cfg.Connections.Servers))
	for alias := range cfg.Connections.Servers {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	type outcome struct {
		alias  string
		health doctor.Server
	}
	channel := make(chan outcome, len(aliases))
	for _, alias := range aliases {
		profile := cfg.Connections.Servers[alias]
		go func(alias string, profile config.ServerProfile) {
			health := doctor.ProbeServer(ctx, config.Connection{Status: profile.Status, URL: profile.URL, Protocol: profile.Protocol}, a.statusHTTPClient())
			channel <- outcome{alias: alias, health: health}
		}(alias, profile)
	}
	byAlias := map[string]doctor.Server{}
	for range aliases {
		value := <-channel
		byAlias[value.alias] = value.health
	}
	result := make([]statusServerProfile, 0, len(aliases))
	aggregate := doctor.Server{Configured: true, TLS: true, ProtocolCompatible: true}
	for _, alias := range aliases {
		profile := cfg.Connections.Servers[alias]
		health := byAlias[alias]
		result = append(result, statusServerProfile{ServerProfile: profile, Health: health})
		aggregate.Reachable = aggregate.Reachable || health.Reachable
		aggregate.TLS = aggregate.TLS && health.TLS
		aggregate.ProtocolCompatible = aggregate.ProtocolCompatible && health.ProtocolCompatible
	}
	if len(result) == 1 {
		aggregate.URL = result[0].URL
	}
	return result, aggregate
}

func skillRegistryStatus(store skills.Store) statusValue {
	registry, err := store.Load()
	if err != nil {
		return statusValue{"unhealthy", terminalui.StatusWarning}
	}
	active, staged, quarantined := 0, 0, 0
	for _, entry := range registry.Entries {
		switch entry.Lifecycle {
		case skills.LifecycleActive:
			active++
		case skills.LifecycleStaged:
			staged++
		case skills.LifecycleQuarantined:
			quarantined++
		}
	}
	if len(registry.Entries) == 0 {
		return statusValue{"ready / empty", terminalui.StatusSuccess}
	}
	return statusValue{fmt.Sprintf("ready / active=%d staged=%d quarantined=%d", active, staged, quarantined), terminalui.StatusSuccess}
}

func requiredComponentsReady(state config.State) bool {
	for _, name := range []string{"codex", "claude-code", "headroom", "ai-memory", "ruflo"} {
		if !componentPresent(state.Components[name]) {
			return false
		}
	}
	if state.Components["codex"].Managed && !componentPresent(state.Components["codex-code-mode-host"]) {
		return false
	}
	return true
}

func componentPresent(component config.ComponentState) bool {
	if !component.Installed {
		return false
	}
	if strings.HasSuffix(component.Version, "-fixture") {
		return true
	}
	if component.Path == "" {
		return false
	}
	info, err := os.Stat(component.Path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
}

type statusValue struct {
	Text string
	Kind terminalui.StatusKind
}

func readyStatus(notSetup bool) statusValue {
	if notSetup {
		return statusValue{"setup required", terminalui.StatusWarning}
	}
	return statusValue{"ready", terminalui.StatusSuccess}
}
func componentStatus(s config.ComponentState, connection string) statusValue {
	if !componentPresent(s) {
		return statusValue{"not installed", terminalui.StatusFailure}
	}
	if connection != "connected" {
		return statusValue{"installed / not connected", terminalui.StatusNeutral}
	}
	return statusValue{"ready", terminalui.StatusSuccess}
}
func codexToolHostStatus(state config.State) statusValue {
	if !state.Components["codex"].Managed {
		return statusValue{"external client", terminalui.StatusNeutral}
	}
	if !componentPresent(state.Components["codex-code-mode-host"]) {
		return statusValue{"missing — run ivoai setup", terminalui.StatusFailure}
	}
	return statusValue{"ready", terminalui.StatusSuccess}
}
func headroomStatus(s config.ComponentState, enabled bool) statusValue {
	if !componentPresent(s) {
		return statusValue{"not installed", terminalui.StatusFailure}
	}
	if !enabled {
		return statusValue{"installed / disabled", terminalui.StatusNeutral}
	}
	return statusValue{"installed / enabled / interactive not validated", terminalui.StatusWarning}
}
func optionalManagedStatus(s config.ComponentState, ready string) statusValue {
	if !componentPresent(s) {
		return statusValue{"not installed / optional", terminalui.StatusNeutral}
	}
	if !s.Managed {
		return statusValue{"external / unmanaged", terminalui.StatusNeutral}
	}
	return statusValue{ready, terminalui.StatusSuccess}
}
func safeStatus(s orchestration.Status) statusValue {
	if !s.Installed {
		return statusValue{"not installed", terminalui.StatusFailure}
	}
	if s.ProviderExecution || s.DurableMemory {
		return statusValue{"unsafe profile — run ivoai setup", terminalui.StatusFailure}
	}
	if !s.SafeMode {
		return statusValue{"safe profile not verified — run ivoai setup", terminalui.StatusWarning}
	}
	return statusValue{"ready / provider execution disabled", terminalui.StatusSuccess}
}

func liveServerStatus(health doctor.Server) statusValue {
	if !health.Configured {
		return statusValue{"not configured", terminalui.StatusNeutral}
	}
	if !health.TLS && !loopbackURL(health.URL) {
		return statusValue{"TLS invalid — run ivoai doctor", terminalui.StatusFailure}
	}
	if !health.Reachable {
		return statusValue{"unreachable — run ivoai doctor", terminalui.StatusWarning}
	}
	if !health.ProtocolCompatible {
		return statusValue{"protocol incompatible — run ivoai doctor", terminalui.StatusFailure}
	}
	return statusValue{"reachable / compatible", terminalui.StatusSuccess}
}

func contextHealthStatus(cfg config.Config, health doctor.Server) statusValue {
	contextConfigured := false
	if server, ok := cfg.MCP.Servers["ivoai-context"]; ok && server.Enabled {
		contextConfigured = true
	}
	if !contextConfigured && cfg.Connections.Server.Status != "connected" {
		return statusValue{"not configured", terminalui.StatusNeutral}
	}
	if !serverUsable(health) {
		return statusValue{"degraded / Server unreachable", terminalui.StatusWarning}
	}
	return statusValue{"ready", terminalui.StatusSuccess}
}

func memoryHealthStatus(cfg config.Config, component config.ComponentState, health doctor.Server) statusValue {
	if !componentPresent(component) {
		return statusValue{"not installed", terminalui.StatusFailure}
	}
	if !cfg.Memory.Enabled {
		return statusValue{"installed / disabled", terminalui.StatusNeutral}
	}
	if cfg.Connections.Server.Status != "connected" {
		return statusValue{"installed / offline hooks", terminalui.StatusNeutral}
	}
	if !serverUsable(health) {
		return statusValue{"degraded / Server unreachable", terminalui.StatusWarning}
	}
	return statusValue{"ready", terminalui.StatusSuccess}
}

func serverUsable(health doctor.Server) bool {
	return health.Configured && health.Reachable && health.ProtocolCompatible && (health.TLS || loopbackURL(health.URL))
}

func researchPriorityStatus(cfg config.Config) statusValue {
	memoryServer, memoryRegistered := cfg.MCP.Servers["ivoai-memory"]
	contextServer, contextRegistered := cfg.MCP.Servers["ivoai-context"]
	memoryReady := cfg.Memory.Enabled && memoryRegistered && memoryServer.Enabled
	contextReady := contextRegistered && contextServer.Enabled
	if memoryReady && contextReady {
		return statusValue{"memory -> context -> web", terminalui.StatusSuccess}
	}
	if memoryReady || contextReady {
		return statusValue{"degraded / internal source missing", terminalui.StatusWarning}
	}
	return statusValue{"unavailable / connect server", terminalui.StatusNeutral}
}

func (a *App) statusHTTPClient() *http.Client {
	if a.HTTPClient != nil {
		return a.HTTPClient
	}
	client := connections.SecureHTTPClient()
	client.Timeout = liveServiceProbeTimeout
	return client
}

func loopbackURL(raw string) bool {
	base, err := connections.ValidateBaseURL(raw)
	if err != nil {
		return false
	}
	host := base.Hostname()
	return strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func optionalStatus(enabled bool) statusValue {
	if enabled {
		return statusValue{"ready", terminalui.StatusSuccess}
	}
	return statusValue{"disabled", terminalui.StatusNeutral}
}

func quotaStatus(provider quota.Provider, value quota.ProviderQuota, kind quota.Kind) statusValue {
	text := quotaValueFor(provider, value, kind)
	window, ok := value.Window(kind)
	if !ok || window.TelemetryState() == quota.TelemetryPending || window.TelemetryState() == quota.TelemetryNotExposed {
		return statusValue{text, terminalui.StatusNeutral}
	}
	if window.TelemetryState() == quota.TelemetryStale || window.TelemetryState() == quota.TelemetryExhausted {
		return statusValue{text, terminalui.StatusWarning}
	}
	return statusValue{text, terminalui.StatusSuccess}
}

func quotaStatusDuration(provider quota.Provider, value quota.ProviderQuota, durationMinutes int64) statusValue {
	text := quotaValueForDuration(provider, value, durationMinutes)
	window, ok := value.WindowByDuration(durationMinutes)
	if !ok || window.TelemetryState() == quota.TelemetryPending || window.TelemetryState() == quota.TelemetryNotExposed {
		return statusValue{text, terminalui.StatusNeutral}
	}
	if window.TelemetryState() == quota.TelemetryStale || window.TelemetryState() == quota.TelemetryExhausted {
		return statusValue{text, terminalui.StatusWarning}
	}
	return statusValue{text, terminalui.StatusSuccess}
}

func (a *App) Doctor(ctx context.Context) doctor.Report {
	return (doctor.Doctor{Store: a.Store, Runner: a.Runner, Version: a.Version, QuotaManager: a.QuotaManager, HTTPClient: a.HTTPClient}).Run(ctx)
}

func (a *App) ConnectAgent(ctx context.Context, target string) error {
	state, err := a.Store.LoadState()
	if err != nil {
		return err
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return err
	}
	key, provider := "codex", quota.ProviderCodex
	if target == "claude" {
		key, provider = "claude-code", quota.ProviderClaude
	}
	manager := a.automaticQuotaManager(cfg, state)
	if _, err := manager.AuthenticationChanged(ctx, provider, false); err != nil {
		return fmt.Errorf("invalidate %s quota after authentication transition: %w", provider, err)
	}
	if err := (connections.AgentAuth{Runner: a.Runner, Store: a.Store, In: a.In, Out: a.Out, Err: a.Err, Binary: state.Components[key].Path}).Connect(ctx, target); err != nil {
		return err
	}
	if _, err := manager.Probe(ctx, provider, true); err != nil {
		a.warn(fmt.Sprintf("%s connected; quota telemetry will refresh on the next successful official probe", displayProvider(string(provider))), err)
	}
	return nil
}
func (a *App) DisconnectAgent(ctx context.Context, target string) error {
	if err := (connections.AgentAuth{Runner: a.Runner, Store: a.Store, In: a.In, Out: a.Out, Err: a.Err}).Disconnect(ctx, target); err != nil {
		return err
	}
	provider := quota.ProviderCodex
	if target == "claude" {
		provider = quota.ProviderClaude
	}
	return (quota.Store{Root: a.Store.Paths.QuotaDir}).Invalidate(provider)
}

func (a *App) ConnectServer(ctx context.Context, serverURL, code string) error {
	return a.ConnectServerProfile(ctx, connections.ConnectOptions{Alias: "default", Purpose: "default", BaseURL: serverURL, Code: code})
}

func (a *App) ConnectServerProfile(ctx context.Context, options connections.ConnectOptions) error {
	connector := connections.ServerConnector{Store: a.Store, Secrets: secrets.Store{Path: a.Store.Paths.Secrets}}
	options.ClientName = project.Identity(currentDir())
	result, err := connector.ConnectProfile(ctx, options)
	if err != nil {
		return err
	}
	for _, warning := range result.Warnings {
		a.warn(warning, nil)
	}
	data, err := (secrets.Store{Path: a.Store.Paths.Secrets}).Load()
	if err != nil {
		return err
	}
	if _, exists := data.Servers[result.Profile.ID]; !exists {
		return errors.New("server connected without stored credential")
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return err
	}
	state, _ := a.Store.LoadState()
	mem := a.memoryManager(state)
	quietMem := mem
	quietMem.Manager.Out = nil
	quietMem.Manager.Err = nil
	if err := a.agentMCP(state).RemoveRemote(ctx); err != nil {
		a.warn("legacy global server MCP entries could not be fully removed", err)
	}
	if cfg.Memory.Enabled {
		if err := mem.Configure(ctx, core.MemoryConfiguration{InstallHooks: true}); err != nil {
			a.warn("server connected, but generic ai-memory hooks are degraded", err)
		}
	} else {
		if err := quietMem.Disable(ctx); err != nil {
			a.warn("ai-memory is disabled but its previous integration could not be removed", err)
		}
	}
	a.success("ivoai server connected: " + result.Profile.Alias)
	return nil
}

type ServerView struct {
	ID                   string                    `json:"server_id"`
	Alias                string                    `json:"alias"`
	URL                  string                    `json:"url"`
	Purpose              string                    `json:"purpose"`
	RedundancyGroup      string                    `json:"redundancy_group,omitempty"`
	Priority             int                       `json:"priority"`
	Protocol             int                       `json:"protocol"`
	Enabled              bool                      `json:"enabled"`
	Status               string                    `json:"status"`
	CredentialConfigured bool                      `json:"credential_configured"`
	Features             map[string]bool           `json:"features,omitempty"`
	Health               connections.ProfileHealth `json:"health,omitempty"`
}

func (a *App) ListServers() ([]ServerView, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return nil, err
	}
	pool, err := serverpool.New(cfg.Connections.Servers)
	if err != nil {
		return nil, err
	}
	data, err := (secrets.Store{Path: a.Store.Paths.Secrets}).Load()
	if err != nil {
		return nil, err
	}
	result := make([]ServerView, 0, len(cfg.Connections.Servers))
	for _, profile := range pool.Profiles() {
		_, credential := data.Servers[profile.ID]
		result = append(result, serverView(profile, credential))
	}
	return result, nil
}

func (a *App) ShowServer(alias string) (ServerView, error) {
	values, err := a.ListServers()
	if err != nil {
		return ServerView{}, err
	}
	for _, value := range values {
		if value.Alias == alias {
			return value, nil
		}
	}
	return ServerView{}, fmt.Errorf("server profile %q was not found", alias)
}

func (a *App) TestServer(ctx context.Context, alias string) (ServerView, error) {
	cfg, err := a.Store.Load()
	if err != nil {
		return ServerView{}, err
	}
	if _, err := serverpool.New(cfg.Connections.Servers); err != nil {
		return ServerView{}, fmt.Errorf("validate server profiles: %w", err)
	}
	profile, exists := cfg.Connections.Servers[alias]
	if !exists {
		return ServerView{}, fmt.Errorf("server profile %q was not found", alias)
	}
	credential, exists, err := (secrets.Store{Path: a.Store.Paths.Secrets}).Get(profile.ID)
	if err != nil {
		return ServerView{}, err
	}
	if !exists {
		return serverView(profile, false), errors.New("server credential is not configured")
	}
	health, err := (connections.ServerConnector{Client: a.statusHTTPClient()}).TestProfile(ctx, profile, credential)
	value := serverView(profile, true)
	value.Health = health
	return value, err
}

func serverView(profile config.ServerProfile, credential bool) ServerView {
	return ServerView{ID: profile.ID, Alias: profile.Alias, URL: profile.URL, Purpose: profile.Purpose, RedundancyGroup: profile.RedundancyGroup, Priority: profile.Priority, Protocol: profile.Protocol, Enabled: profile.Enabled, Status: profile.Status, CredentialConfigured: credential, Features: profile.Features}
}
func (a *App) DisconnectServer(ctx context.Context) error {
	return a.DisconnectServerProfile(ctx, "default", false)
}

func (a *App) DisconnectServerProfile(ctx context.Context, alias string, all bool) error {
	cfg, err := a.Store.Load()
	if err != nil {
		return err
	}
	state, _ := a.Store.LoadState()
	if err := a.agentMCP(state).RemoveRemote(ctx); err != nil {
		a.warn("server MCP entries could not be fully removed from the agent clients", err)
	}
	mem := a.memoryManager(state)
	connector := connections.ServerConnector{Store: a.Store, Secrets: secrets.Store{Path: a.Store.Paths.Secrets}}
	if all {
		if err := connector.DisconnectAll(); err != nil {
			return err
		}
	} else if err := connector.DisconnectProfile(alias); err != nil {
		return err
	}
	if cfg.Memory.Enabled {
		remaining, _ := a.Store.Load()
		configuration := core.MemoryConfiguration{InstallHooks: true}
		if len(remaining.Connections.Servers) == 0 {
			configuration.InstallMCP = true
		}
		if err := mem.Configure(ctx, configuration); err != nil {
			a.warn("server disconnected; offline ai-memory hooks could not be restored", err)
		}
	}
	return nil
}

func (a *App) Launch(ctx context.Context, target string, args []string) error {
	return a.LaunchWithKnowledge(ctx, target, args, nil)
}

func (a *App) LaunchWithKnowledge(ctx context.Context, target string, args, selectors []string) error {
	cfg, err := a.Store.Load()
	if err != nil {
		return err
	}
	state, _ := a.Store.LoadState()
	key := target
	if target == "claude" {
		key = "claude-code"
	}
	if err := validateManagedAgentRuntime(target, state); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	skillResult, err := a.evaluateSessionSkills(ctx, target, cwd, args)
	if err != nil {
		return err
	}
	runtimeRoot := filepath.Join(a.Store.Paths.StateDir, "direct-runtime")
	if err := platform.EnsurePrivateDir(runtimeRoot); err != nil {
		return err
	}
	runtimeDir, err := os.MkdirTemp(runtimeRoot, "session-")
	if err != nil {
		return err
	}
	if err := os.Chmod(runtimeDir, 0o700); err != nil {
		_ = os.RemoveAll(runtimeDir)
		return err
	}
	defer os.RemoveAll(runtimeDir)
	knowledge, err := a.prepareSessionKnowledge(ctx, cfg, selectors, target, runtimeDir, os.Environ(), nil)
	if err != nil {
		return err
	}
	defer knowledge.close()
	cfg = knowledge.config
	args = append(knowledge.args, managedAgentArgs(target, args, cfg, skillResult.Instructions)...)
	environment := knowledge.environment
	cleanup := func() {}
	if target == "opencode" {
		environment, cleanup, err = openCodeInstructionEnvironment(environment, a.Store.Paths.StateDir, managedInstructions(skillResult.Instructions))
		if err != nil {
			return err
		}
	}
	defer cleanup()
	compressionProvider, compressionEnabled, requestedCompression := a.sessionCompression(cfg, state, target, runtimeDir)
	if compressionBypassedForSharedKnowledge(cfg) {
		compressionEnabled = false
	}
	if !compressionEnabled && compressionBypassedForSharedKnowledge(cfg) {
		a.warn(sharedKnowledgeCompressionBypass(requestedCompression), nil)
	}
	executor, err := agents.ExecutorFor(target, agents.Runtime{Runner: a.Runner, In: a.In, Out: a.Out, Err: a.Err, AgentPath: state.Components[key].Path, HeadroomPath: state.Components["headroom"].Path, Compression: compressionProvider, Environment: environment, RuntimeDir: runtimeDir}, state.Components[key].Version, state.Components[key].Managed)
	if err != nil {
		return err
	}
	return executor.StartSession(ctx, core.SessionRequest{Args: args, CompressionEnabled: compressionEnabled}, nil)
}

func (a *App) MemoryStatus(ctx context.Context) error {
	state, _ := a.Store.LoadState()
	status, err := a.memoryManager(state).Status(ctx)
	if err == nil {
		fmt.Fprintln(a.Out, semanticStatus(status, terminalui.ColorEnabled(a.Out)))
	}
	return err
}
func (a *App) ReconfigureMemory(ctx context.Context) error {
	cfg, err := a.Store.Load()
	if err != nil {
		return err
	}
	state, _ := a.Store.LoadState()
	mem := a.memoryManager(state)
	quietMem := mem
	quietMem.Manager.Out = nil
	quietMem.Manager.Err = nil
	if !cfg.Memory.Enabled {
		if err := quietMem.Disable(ctx); err != nil {
			return err
		}
		if len(cfg.Connections.Servers) > 0 {
			if err := a.agentMCP(state).RemoveRemote(ctx); err != nil {
				a.warn("legacy global server MCP entries could not be removed", err)
			}
		}
		return nil
	}
	if len(cfg.Connections.Servers) == 0 {
		return mem.Configure(ctx, core.MemoryConfiguration{InstallMCP: true, InstallHooks: true})
	}
	if err := quietMem.Disable(ctx); err != nil {
		return err
	}
	if err := a.agentMCP(state).RemoveRemote(ctx); err != nil {
		a.warn("legacy global server MCP entries could not be removed", err)
	}
	return mem.Configure(ctx, core.MemoryConfiguration{InstallHooks: true})
}

func (a *App) memoryManager(state config.State) memory.AIMemoryBackend {
	component := state.Components["ai-memory"]
	return memory.AIMemoryBackend{Manager: memory.Manager{Runner: a.Runner, Out: a.Out, Err: a.Err, HooksDir: a.Store.Paths.HooksDir, Binary: component.Path}, Version: component.Version, Managed: component.Managed}
}

func (a *App) agentMCP(state config.State) connections.AgentMCP {
	return connections.AgentMCP{Runner: a.Runner, CodexBinary: state.Components["codex"].Path, ClaudeBinary: state.Components["claude-code"].Path}
}

func enabledServerMCPs(cfg config.Config) map[string]config.MCPServer {
	servers := make(map[string]config.MCPServer, 2)
	for name, server := range cfg.MCP.Servers {
		if name == "ivoai-memory" && !cfg.Memory.Enabled {
			continue
		}
		servers[name] = server
	}
	return servers
}

func (a *App) orchestrationManager(state config.State) orchestration.Manager {
	return orchestration.Manager{Runner: a.Runner, Binary: state.Components["ruflo"].Path, CodexBinary: state.Components["codex"].Path, ClaudeBinary: state.Components["claude-code"].Path, ProfileDir: a.Store.Paths.DataDir}
}

func setProcessEnvironment(environment []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}

func (a *App) warn(message string, err error) {
	label := terminalui.Warning("warning:", terminalui.ColorEnabled(a.Err))
	if err == nil {
		fmt.Fprintf(a.Err, "%s %s\n", label, message)
		return
	}
	fmt.Fprintf(a.Err, "%s %s: %v\n", label, message, err)
}

func (a *App) success(message string) {
	color := terminalui.ColorEnabled(a.Out)
	fmt.Fprintf(a.Out, "%s %s\n", terminalui.Success("OK", color), terminalui.Success(message, color))
}

func semanticStatus(value string, color bool) string {
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "not installed"), strings.Contains(lower, "unhealthy"), strings.Contains(lower, "failed"):
		return terminalui.Failure(value, color)
	case strings.Contains(lower, "not connected"), strings.Contains(lower, "disabled"), strings.Contains(lower, "setup required"), strings.Contains(lower, "degraded"):
		return terminalui.Warning(value, color)
	case strings.Contains(lower, "ready"), strings.Contains(lower, "connected"), strings.Contains(lower, "installed"):
		return terminalui.Success(value, color)
	default:
		return value
	}
}

func (a *App) MCPList() error {
	entries, err := (connections.Registry{Store: a.Store}).List()
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		enabled := terminalui.Warning("false", terminalui.ColorEnabled(a.Out))
		if entries[n].Enabled {
			enabled = terminalui.Success("true", terminalui.ColorEnabled(a.Out))
		}
		fmt.Fprintf(a.Out, "%s\t%s\t%s\n", n, entries[n].URL, enabled)
	}
	return nil
}
func (a *App) MCPAdd(name, endpoint string) error {
	return (connections.Registry{Store: a.Store}).Add(name, config.MCPServer{URL: endpoint, Enabled: true, Kind: "external"})
}
func (a *App) MCPRemove(name string) error {
	return (connections.Registry{Store: a.Store}).Remove(name)
}

func (a *App) ConfigShow() error {
	b, err := os.ReadFile(a.Store.Paths.Config)
	if err != nil {
		return err
	}
	_, err = a.Out.Write(b)
	return err
}
func (a *App) ConfigSet(key, value string) error {
	c, err := a.Store.Load()
	if err != nil {
		return err
	}
	switch key {
	case "headroom.enabled":
		b, err := parseBool(value)
		if err != nil {
			return err
		}
		c.Headroom.Enabled = b
	case "memory.enabled":
		b, err := parseBool(value)
		if err != nil {
			return err
		}
		c.Memory.Enabled = b
	case "orchestration.enabled":
		b, err := parseBool(value)
		if err != nil {
			return err
		}
		c.Orchestration.Enabled = b
	case "orchestration.default_mode":
		c.Orchestration.DefaultMode = strings.ToLower(value)
	case "orchestration.primary_executor":
		c.Orchestration.PrimaryExecutor = strings.ToLower(value)
	case "orchestration.review_executor":
		c.Orchestration.ReviewExecutor = strings.ToLower(value)
	case "orchestration.max_workers":
		parsed, parseErr := strconv.Atoi(value)
		if parseErr != nil {
			return errors.New("max_workers must be an integer between 1 and 3")
		}
		c.Orchestration.MaxWorkers = parsed
	case "orchestration.auto.enabled":
		parsed, parseErr := parseBool(value)
		if parseErr != nil {
			return parseErr
		}
		c.Orchestration.Auto.Enabled = parsed
	case "orchestration.auto.default_planner":
		c.Orchestration.Auto.DefaultPlanner = strings.ToLower(value)
	case "orchestration.auto.automatic_failover":
		parsed, parseErr := parseBool(value)
		if parseErr != nil {
			return parseErr
		}
		c.Orchestration.Auto.AutomaticFailover = parsed
	case "orchestration.auto.checkpoint_enabled":
		parsed, parseErr := parseBool(value)
		if parseErr != nil {
			return parseErr
		}
		c.Orchestration.Auto.CheckpointEnabled = parsed
	case "orchestration.auto.quota_refresh_seconds":
		parsed, parseErr := strconv.Atoi(value)
		if parseErr != nil {
			return errors.New("quota_refresh_seconds must be an integer between 30 and 300")
		}
		c.Orchestration.Auto.QuotaRefreshSeconds = parsed
	case "orchestration.auto.max_workers":
		parsed, parseErr := strconv.Atoi(value)
		if parseErr != nil {
			return errors.New("auto max_workers must be an integer between 1 and 3")
		}
		c.Orchestration.Auto.MaxWorkers = parsed
	case "orchestration.auto.optimization.parallelism":
		parsed, parseErr := parseBool(value)
		if parseErr != nil {
			return parseErr
		}
		c.Orchestration.Auto.Optimization.Parallelism = parsed
	case "orchestration.auto.optimization.shared_context_bootstrap":
		parsed, parseErr := parseBool(value)
		if parseErr != nil {
			return parseErr
		}
		c.Orchestration.Auto.Optimization.SharedContextBootstrap = parsed
	case "orchestration.auto.optimization.progressive_escalation":
		parsed, parseErr := parseBool(value)
		if parseErr != nil {
			return parseErr
		}
		c.Orchestration.Auto.Optimization.ProgressiveEscalation = parsed
	case "orchestration.auto.quota.enabled":
		parsed, parseErr := parseBool(value)
		if parseErr != nil {
			return parseErr
		}
		c.Orchestration.Auto.Quota.Enabled = parsed
	case "orchestration.auto.quota.show_weekly":
		parsed, parseErr := parseBool(value)
		if parseErr != nil {
			return parseErr
		}
		c.Orchestration.Auto.Quota.ShowWeekly = parsed
	case "orchestration.auto.quota.show_monthly":
		parsed, parseErr := parseBool(value)
		if parseErr != nil {
			return parseErr
		}
		c.Orchestration.Auto.Quota.ShowMonthly = parsed
	case "orchestration.auto.quota.show_session":
		parsed, parseErr := parseBool(value)
		if parseErr != nil {
			return parseErr
		}
		c.Orchestration.Auto.Quota.ShowSession = parsed
	case "orchestration.auto.quota.show_context":
		parsed, parseErr := parseBool(value)
		if parseErr != nil {
			return parseErr
		}
		c.Orchestration.Auto.Quota.ShowContext = parsed
	case "orchestration.auto.quota.show_model_scoped":
		parsed, parseErr := parseBool(value)
		if parseErr != nil {
			return parseErr
		}
		c.Orchestration.Auto.Quota.ShowModelScoped = parsed
	default:
		return fmt.Errorf("unsupported config key %q", key)
	}
	return a.Store.Save(c)
}
func parseBool(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "true", "on", "1":
		return true, nil
	case "false", "off", "0":
		return false, nil
	}
	return false, fmt.Errorf("expected true or false, got %q", s)
}

func (a *App) ProjectInit() error {
	marker, err := project.Init(currentDir())
	if err == nil {
		a.success("project initialized " + marker.ID)
	}
	return err
}
func (a *App) ProjectStatus() { fmt.Fprintln(a.Out, project.Identity(currentDir())) }
func currentDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

func (a *App) Update(ctx context.Context) error {
	return a.transactionalUpdate(ctx, update.Checker{})
}

func (a *App) UpdateDryRun(ctx context.Context) error {
	return a.transactionalUpdateDryRun(ctx, update.Checker{})
}

func (a *App) RollbackUpdate(ctx context.Context) error {
	return a.transactionalRollback(ctx)
}

func (a *App) ForceRollbackUpdate(ctx context.Context) error {
	return a.transactionalRollbackMode(ctx, true)
}

func (a *App) Uninstall(ctx context.Context) error {
	state, err := a.Store.LoadState()
	if err != nil {
		return err
	}
	ownership, err := a.Store.LoadOwnership()
	if err != nil {
		return err
	}
	if memoryState := state.Components["ai-memory"]; memoryState.Installed {
		path := memoryState.Path
		if info, statErr := os.Stat(path); statErr == nil && info.Mode().IsRegular() {
			// Setup owns the ai-memory hook/MCP registration even when it reused a
			// pre-existing executable. Ask the official client to remove only that
			// integration; the executable and its authentication remain untouched.
			_, _ = a.Runner.Run(ctx, path, []string{"uninstall", "--apply"}, platform.RunOptions{Stdout: a.Out, Stderr: a.Err, Timeout: time.Minute})
		}
	}
	ivoai := ownership.Components["ivoai"]
	if ivoai.Managed {
		for _, launcher := range ivoai.Launchers {
			if err := a.removeOwnedLauncher(ctx, launcher, ivoai.Path); err != nil {
				return fmt.Errorf("remove ivoai launcher: %w", err)
			}
		}
	}
	if ivoai.Managed && ivoai.Path != "" {
		if err := a.removeOwnedExecutable(ctx, ivoai.Path); err != nil {
			return fmt.Errorf("remove ivoai executable: %w", err)
		}
	}
	// Keep ownership state available until every privileged launcher/executable
	// removal has succeeded, so a permission failure remains safely retryable.
	// Pre-existing agent clients and their authentication stores are preserved.
	for _, dir := range []string{a.Store.Paths.CacheDir, a.Store.Paths.DataDir, a.Store.Paths.ConfigDir, a.Store.Paths.StateDir} {
		if err := removeScoped(dir); err != nil {
			return err
		}
	}
	fmt.Fprintln(a.Out, "removed ivoai-managed configuration, state, hooks, and cache; third-party tools and logins were preserved")
	return nil
}

func (a *App) removeOwnedLauncher(ctx context.Context, launcher, executable string) error {
	launcher = filepath.Clean(launcher)
	executable = filepath.Clean(executable)
	if !filepath.IsAbs(launcher) || !filepath.IsAbs(executable) {
		return errors.New("owned paths must be absolute")
	}
	info, err := os.Lstat(launcher)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("refusing to remove non-symlink %s", launcher)
	}
	resolved, err := filepath.EvalSymlinks(launcher)
	if err != nil {
		return err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil || resolved != executable {
		return fmt.Errorf("refusing launcher that no longer resolves to owned executable")
	}
	return a.removePath(ctx, launcher)
}

func (a *App) removeOwnedExecutable(ctx context.Context, executable string) error {
	executable = filepath.Clean(executable)
	if !filepath.IsAbs(executable) || filepath.Base(executable) != "ivoai" {
		return fmt.Errorf("refusing unexpected executable %s", executable)
	}
	info, err := os.Lstat(executable)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("refusing non-regular executable %s", executable)
	}
	return a.removePath(ctx, executable)
}

func (a *App) removePath(ctx context.Context, path string) error {
	if err := os.Remove(path); err == nil || os.IsNotExist(err) {
		return nil
	} else if !errors.Is(err, os.ErrPermission) {
		return err
	}
	sudo, err := a.Runner.LookPath("sudo")
	if err != nil {
		return fmt.Errorf("permission denied removing %s; rerun uninstall with appropriate privileges", path)
	}
	_, err = a.Runner.Run(ctx, sudo, []string{"--", "rm", "--", path}, platform.RunOptions{Stdin: a.In, Stdout: a.Out, Stderr: a.Err, Timeout: time.Minute})
	return err
}
func removeScoped(path string) error {
	if filepath.Base(path) != "ivoai" {
		return fmt.Errorf("refusing to remove unexpected path %s", path)
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink %s", path)
	}
	return os.RemoveAll(path)
}

func (a *App) Prompt(label string, secret bool) (string, error) {
	fmt.Fprint(a.Out, label)
	if secret {
		if file, ok := a.In.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
			value, err := term.ReadPassword(int(file.Fd()))
			fmt.Fprintln(a.Out)
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(string(value)), nil
		}
	}
	reader := bufio.NewReader(a.In)
	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(value), nil
}
