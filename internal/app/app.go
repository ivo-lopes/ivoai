package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
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
	"github.com/ivo-lopes/ivoai/internal/doctor"
	"github.com/ivo-lopes/ivoai/internal/memory"
	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/project"
	"github.com/ivo-lopes/ivoai/internal/secrets"
	"github.com/ivo-lopes/ivoai/internal/terminalui"
	"github.com/ivo-lopes/ivoai/internal/update"
	"golang.org/x/term"
)

type App struct {
	Version  string
	Store    *config.Store
	Runner   platform.Runner
	In       io.Reader
	Out, Err io.Writer
}

// MenuSnapshot is a non-secret, read-only view used by the interactive UI.
// It deliberately contains no endpoint credentials or raw configuration.
type MenuSnapshot struct {
	SetupComplete    bool
	ComponentsReady  bool
	ChatGPTConnected bool
	ClaudeConnected  bool
	ServerConnected  bool
	MemoryEnabled    bool
	HeadroomEnabled  bool
	RufloEnabled     bool
	DefaultMode      string
	PrimaryExecutor  string
	ReviewExecutor   string
	MaxWorkers       int
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
		SetupComplete:    !state.SetupCompletedAt.IsZero(),
		ComponentsReady:  requiredComponentsReady(state),
		ChatGPTConnected: cfg.Connections.ChatGPT.Status == "connected",
		ClaudeConnected:  cfg.Connections.Claude.Status == "connected",
		ServerConnected:  cfg.Connections.Server.Status == "connected",
		MemoryEnabled:    cfg.Memory.Enabled,
		HeadroomEnabled:  cfg.Headroom.Enabled,
		RufloEnabled:     cfg.Orchestration.Enabled,
		DefaultMode:      cfg.Orchestration.DefaultMode,
		PrimaryExecutor:  cfg.Orchestration.PrimaryExecutor,
		ReviewExecutor:   cfg.Orchestration.ReviewExecutor,
		MaxWorkers:       cfg.Orchestration.MaxWorkers,
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
	if cfg.Memory.Enabled && cfg.Connections.Server.Status == "connected" {
		if err := a.ReconfigureMemory(ctx); err != nil {
			a.warn("remote ai-memory integration is degraded; Codex and Claude remain usable", err)
		}
	} else if cfg.Memory.Enabled {
		if err := mem.Configure(ctx, "", ""); err != nil {
			a.warn("ai-memory hooks are degraded; Codex and Claude remain usable", err)
		}
	} else if err := mem.Disable(ctx); err != nil {
		a.warn("ai-memory is disabled but its previous integration could not be removed", err)
	} else if cfg.Connections.Server.Status == "connected" {
		a.reconcileAgentMCP(ctx, state, cfg)
	}
	state.SetupCompletedAt = time.Now().UTC()
	if err := a.Store.SaveState(state); err != nil {
		return err
	}
	a.success("ivoai client setup complete")
	return nil
}

func (a *App) Status(ctx context.Context) error {
	cfg, err := a.Store.Load()
	if err != nil {
		return err
	}
	state, err := a.Store.LoadState()
	if err != nil {
		return err
	}
	rows := []struct{ name, status string }{
		{"ivoai", ready(state.SetupCompletedAt.IsZero())},
		{"Codex", componentStatus(state.Components["codex"], cfg.Connections.ChatGPT.Status)},
		{"Claude Code", componentStatus(state.Components["claude-code"], cfg.Connections.Claude.Status)},
		{"Headroom", enabledStatus(state.Components["headroom"], cfg.Headroom.Enabled)},
		{"ai-memory", componentStatus(state.Components["ai-memory"], cfg.Connections.Server.Status)},
		{"Ruflo", safeStatus(state.Components["ruflo"])},
		{"Server", cfg.Connections.Server.Status},
	}
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
			rows = append(rows, struct{ name, status string }{"Sessions", fmt.Sprintf("%d active / %d orchestrated", active, orchestrated)})
		}
	}
	for _, row := range rows {
		fmt.Fprintf(a.Out, "%-14s %s\n", row.name, semanticStatus(row.status, terminalui.ColorEnabled(a.Out)))
	}
	if state.SetupCompletedAt.IsZero() {
		fmt.Fprintf(a.Out, "\nOverall: %s\n", terminalui.Warning("SETUP REQUIRED", terminalui.ColorEnabled(a.Out)))
	} else if !requiredComponentsReady(state) {
		fmt.Fprintf(a.Out, "\nOverall: %s\n", terminalui.Warning("DEGRADED — run ivoai setup to repair components", terminalui.ColorEnabled(a.Out)))
	} else if cfg.Connections.ChatGPT.Status != "connected" || cfg.Connections.Claude.Status != "connected" || cfg.Connections.Server.Status != "connected" {
		fmt.Fprintf(a.Out, "\nOverall: %s\n", terminalui.Success("READY", terminalui.ColorEnabled(a.Out))+" — external connections pending")
	} else {
		fmt.Fprintf(a.Out, "\nOverall: %s\n", terminalui.Success("READY — all connections active", terminalui.ColorEnabled(a.Out)))
	}
	return nil
}

func requiredComponentsReady(state config.State) bool {
	for _, name := range []string{"codex", "claude-code", "headroom", "ai-memory", "ruflo"} {
		if !componentPresent(state.Components[name]) {
			return false
		}
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
func ready(notSetup bool) string {
	if notSetup {
		return "setup required"
	}
	return "ready"
}
func componentStatus(s config.ComponentState, connection string) string {
	if !componentPresent(s) {
		return "not installed"
	}
	if connection != "connected" {
		return "installed / not connected"
	}
	return "ready"
}
func enabledStatus(s config.ComponentState, enabled bool) string {
	if !s.Installed {
		return "not installed"
	}
	if !enabled {
		return "installed / disabled"
	}
	return "ready"
}
func safeStatus(s config.ComponentState) string {
	if !s.Installed {
		return "not installed"
	}
	return "ready / provider execution disabled"
}

func (a *App) Doctor(ctx context.Context) doctor.Report {
	return (doctor.Doctor{Store: a.Store, Runner: a.Runner, Version: a.Version}).Run(ctx)
}

func (a *App) ConnectAgent(ctx context.Context, target string) error {
	state, _ := a.Store.LoadState()
	key := target
	if target == "claude" {
		key = "claude-code"
	}
	return (connections.AgentAuth{Runner: a.Runner, Store: a.Store, In: a.In, Out: a.Out, Err: a.Err, Binary: state.Components[key].Path}).Connect(ctx, target)
}
func (a *App) DisconnectAgent(ctx context.Context, target string) error {
	return (connections.AgentAuth{Runner: a.Runner, Store: a.Store, In: a.In, Out: a.Out, Err: a.Err}).Disconnect(ctx, target)
}

func (a *App) ConnectServer(ctx context.Context, serverURL, code string) error {
	connector := connections.ServerConnector{Store: a.Store, Secrets: secrets.Store{Path: a.Store.Paths.Secrets}}
	result, err := connector.Connect(ctx, serverURL, code, project.Identity(currentDir()))
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
	if data.Server == nil {
		return errors.New("server connected without stored credential")
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return err
	}
	state, _ := a.Store.LoadState()
	mem := a.memoryManager(state)
	quietMem := mem
	quietMem.Out = nil
	quietMem.Err = nil
	a.reconcileAgentMCP(ctx, state, cfg)
	_, memoryAvailable := cfg.MCP.Servers["ivoai-memory"]
	if cfg.Memory.Enabled && memoryAvailable {
		if err := quietMem.Disable(ctx); err != nil {
			a.warn("previous ai-memory integration could not be fully removed", err)
		}
		if err := mem.ConfigureHooks(ctx, result.MemoryHooksURL, data.Server.Token); err != nil {
			a.warn("server connected, but ai-memory hooks are degraded", err)
		}
	} else if !cfg.Memory.Enabled {
		if err := quietMem.Disable(ctx); err != nil {
			a.warn("ai-memory is disabled but its previous integration could not be removed", err)
		}
	}
	a.success("ivoai server connected")
	return nil
}
func (a *App) DisconnectServer(ctx context.Context) error {
	cfg, err := a.Store.Load()
	if err != nil {
		return err
	}
	state, _ := a.Store.LoadState()
	if err := a.agentMCP(state).RemoveRemote(ctx); err != nil {
		a.warn("server MCP entries could not be fully removed from the agent clients", err)
	}
	mem := a.memoryManager(state)
	if err := mem.Disable(ctx); err != nil {
		a.warn("remote ai-memory integration could not be fully removed", err)
	}
	if err := (connections.ServerConnector{Store: a.Store, Secrets: secrets.Store{Path: a.Store.Paths.Secrets}}).Disconnect(); err != nil {
		return err
	}
	if cfg.Memory.Enabled {
		if err := mem.Configure(ctx, "", ""); err != nil {
			a.warn("server disconnected; offline ai-memory hooks could not be restored", err)
		}
	}
	return nil
}

func (a *App) Launch(ctx context.Context, target string, args []string) error {
	cfg, err := a.Store.Load()
	if err != nil {
		return err
	}
	state, _ := a.Store.LoadState()
	key := target
	if target == "claude" {
		key = "claude-code"
	}
	restoreEnvironment, err := a.exposeServerCredential()
	if err != nil {
		return err
	}
	defer restoreEnvironment()
	return (agents.Runtime{Runner: a.Runner, In: a.In, Out: a.Out, Err: a.Err, AgentPath: state.Components[key].Path, HeadroomPath: state.Components["headroom"].Path}).Launch(ctx, target, args, cfg.Headroom.Enabled)
}

func (a *App) MemoryStatus(ctx context.Context) error {
	state, _ := a.Store.LoadState()
	status, err := (memory.Manager{Runner: a.Runner, Binary: state.Components["ai-memory"].Path}).Status(ctx)
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
	data, err := (secrets.Store{Path: a.Store.Paths.Secrets}).Load()
	if err != nil {
		return err
	}
	state, _ := a.Store.LoadState()
	mem := a.memoryManager(state)
	quietMem := mem
	quietMem.Out = nil
	quietMem.Err = nil
	if !cfg.Memory.Enabled {
		if err := quietMem.Disable(ctx); err != nil {
			return err
		}
		if cfg.Connections.Server.Status == "connected" {
			a.reconcileAgentMCP(ctx, state, cfg)
		}
		return nil
	}
	if cfg.Connections.Server.Status != "connected" || data.Server == nil {
		return mem.Configure(ctx, "", "")
	}
	if err := quietMem.Disable(ctx); err != nil {
		return err
	}
	a.reconcileAgentMCP(ctx, state, cfg)
	if _, memoryAvailable := cfg.MCP.Servers["ivoai-memory"]; !memoryAvailable {
		return nil
	}
	memoryServer := cfg.MCP.Servers["ivoai-memory"]
	memoryHooksURL := memoryServer.HooksURL
	if memoryHooksURL == "" {
		memoryHooksURL = strings.TrimSuffix(memoryServer.URL, "/mcp")
	}
	return mem.ConfigureHooks(ctx, memoryHooksURL, data.Server.Token)
}

func (a *App) memoryManager(state config.State) memory.Manager {
	return memory.Manager{Runner: a.Runner, Out: a.Out, Err: a.Err, HooksDir: a.Store.Paths.HooksDir, Binary: state.Components["ai-memory"].Path}
}

func (a *App) agentMCP(state config.State) connections.AgentMCP {
	return connections.AgentMCP{Runner: a.Runner, CodexBinary: state.Components["codex"].Path, ClaudeBinary: state.Components["claude-code"].Path}
}

func (a *App) reconcileAgentMCP(ctx context.Context, state config.State, cfg config.Config) {
	manager := a.agentMCP(state)
	if err := manager.RemoveRemote(ctx); err != nil {
		a.warn("previous server MCP entries could not be fully removed", err)
	}
	if err := manager.ConfigureRemote(ctx, enabledServerMCPs(cfg)); err != nil {
		a.warn("server MCP registration is degraded", err)
	}
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

func (a *App) exposeServerCredential() (func(), error) {
	data, err := (secrets.Store{Path: a.Store.Paths.Secrets}).Load()
	if err != nil {
		return func() {}, err
	}
	if data.Server == nil || data.Server.Token == "" {
		return func() {}, nil
	}
	previous, existed := os.LookupEnv(connections.ServerTokenEnvironment)
	if err := os.Setenv(connections.ServerTokenEnvironment, data.Server.Token); err != nil {
		return func() {}, err
	}
	return func() {
		if existed {
			_ = os.Setenv(connections.ServerTokenEnvironment, previous)
		} else {
			_ = os.Unsetenv(connections.ServerTokenEnvironment)
		}
	}, nil
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
	checker := update.Checker{}
	release, available, err := checker.Check(ctx, a.Version)
	if err != nil {
		return err
	}
	if !available {
		fmt.Fprintf(a.Out, "ivoai %s is current\n", a.Version)
		return nil
	}
	fmt.Fprintf(a.Out, "updating ivoai to %s (%s)\n", release.Version, release.URL)
	if err := a.Store.Ensure(); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	rollback := filepath.Join(a.Store.Paths.StateDir, "updates", "ivoai.previous")
	if err := checker.Apply(ctx, release, executable, rollback); err != nil {
		return err
	}
	if _, err := a.Runner.Run(ctx, executable, []string{"setup"}, platform.RunOptions{Stdout: a.Out, Stderr: a.Err, Timeout: 30 * time.Minute}); err != nil {
		return fmt.Errorf("ivoai updated; new-version migration/setup failed (rollback binary: %s): %w", rollback, err)
	}
	if _, err := a.Runner.Run(ctx, executable, []string{"doctor", "--json"}, platform.RunOptions{Stdout: a.Out, Stderr: a.Err, Timeout: time.Minute}); err != nil {
		return fmt.Errorf("update installed but post-update doctor failed (rollback binary: %s): %w", rollback, err)
	}
	fmt.Fprintf(a.Out, "update complete; rollback binary: %s\n", rollback)
	return nil
}

func (a *App) RollbackUpdate(ctx context.Context) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	rollback := filepath.Join(a.Store.Paths.StateDir, "updates", "ivoai.previous")
	if err := (update.Checker{}).Rollback(ctx, executable, rollback); err != nil {
		return err
	}
	if _, err := a.Runner.Run(ctx, executable, []string{"doctor", "--json"}, platform.RunOptions{Stdout: a.Out, Stderr: a.Err, Timeout: time.Minute}); err != nil {
		return fmt.Errorf("rollback installed but post-rollback doctor failed: %w", err)
	}
	fmt.Fprintf(a.Out, "rollback complete; replaced binary retained at %s.newer\n", rollback)
	return nil
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
