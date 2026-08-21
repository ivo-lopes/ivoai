package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/terminalui"
)

type menuSession struct {
	ctx      context.Context
	app      *app.App
	reader   *bufio.Reader
	progress *terminalui.Progress
}

type menuAction struct {
	id          string
	label       string
	description string
	disabled    string
	long        bool
	run         func() (bool, error)
}

// PublicMenuActionIDs provides a stable testable inventory. Internal service
// entrypoints such as gateway/context serve are intentionally excluded.
func PublicMenuActionIDs() []string {
	return []string{
		"status", "doctor", "version", "setup", "update", "rollback", "uninstall",
		"connect.list", "connect.chatgpt", "disconnect.chatgpt", "connect.claude", "disconnect.claude", "connect.server", "disconnect.server",
		"mcp.list", "mcp.add", "mcp.remove", "launch.codex", "launch.claude", "memory.status", "memory.configure",
		"session.direct.codex", "session.direct.claude", "session.orchestrated.codex", "session.orchestrated.claude", "session.list", "session.monitor", "session.stop",
		"project.status", "project.init", "config.show", "config.headroom", "config.memory", "config.ruflo", "config.session-mode", "config.primary", "config.reviewer", "config.workers",
		"server.setup", "server.status", "server.doctor", "server.start", "server.stop", "server.restart", "server.logs",
		"server.enrollment.create", "server.enrollment.list", "server.enrollment.revoke",
		"server.web-access.create", "server.web-access.list", "server.web-access.revoke",
		"server.connector.list", "server.connector.add", "server.connector.remove", "server.context.status", "server.memory.status",
		"server.gateway.configure", "server.backup", "server.restore", "remote.status", "remote.doctor", "remote.connector.list",
	}
}

func menu(ctx context.Context, a *app.App) error {
	session := &menuSession{ctx: ctx, app: a, reader: bufio.NewReader(a.In), progress: &terminalui.Progress{Out: a.Err, Version: a.Version, ShowHeader: true}}
	for {
		snapshot, err := a.MenuSnapshot()
		if err != nil {
			return err
		}
		badges := snapshotBadges(snapshot)
		actions := []menuAction{
			{id: "dashboard", label: "Dashboard", description: "Status, diagnostics, and version information", run: session.dashboard},
			{id: "maintenance", label: "Setup & Maintenance", description: "Install, repair, update, rollback, or uninstall", run: session.maintenance},
			{id: "connections", label: "Connections", description: "ChatGPT, Claude, ivoai server, and external MCPs", run: session.connections},
			{id: "agents", label: "Agents", description: "Launch Codex or Claude through the ivoai runtime", run: session.agents},
			{id: "sessions", label: "Session Control", description: "Start observable direct or Ruflo-orchestrated sessions", run: session.sessions},
			{id: "memory", label: "Memory", description: "Inspect or reconfigure persistent operational memory", run: session.memory},
			{id: "project", label: "Project", description: "Host identity and optional project override", run: session.project},
			{id: "configuration", label: "Configuration", description: "Headroom, ai-memory, and Ruflo safe settings", run: session.configuration},
			{id: "server", label: "Server Administration", description: "Local services, enrollment, connectors, backup, and gateway", run: session.server},
			{id: "remote", label: "Remote Server", description: "Read-only administration through the connected gateway", disabled: disabledUnless(snapshot.ServerConnected, "server not connected"), run: session.remote},
			{id: "exit", label: "Exit", description: "Return to your shell", run: func() (bool, error) { return true, nil }},
		}
		id, err := session.choose("Personal AI runtime", actions, badges)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			fmt.Fprintf(a.Err, "%s %s\n", terminalui.Failure("Error:", terminalui.ColorEnabled(a.Err)), UserError(err))
			continue
		}
		if id == "" {
			return nil
		}
		action := findAction(actions, id)
		exit, actionErr := session.execute(action)
		if actionErr != nil {
			fmt.Fprintf(a.Err, "%s %s\n", terminalui.Failure("Error:", terminalui.ColorEnabled(a.Err)), UserError(actionErr))
		}
		if exit {
			return actionErr
		}
	}
}

func (s *menuSession) dashboard() (bool, error) {
	return s.loop("Dashboard", []menuAction{
		{id: "status", label: "Status", description: "Show component and connection readiness", run: s.simple(func() error { return s.app.Status(s.ctx) })},
		{id: "doctor", label: "Doctor", description: "Run live diagnostics", long: true, run: s.simple(func() error { return runDoctor(s.ctx, s.app, nil) })},
		{id: "version", label: "Version", description: "Print the installed ivoai version", run: s.simple(func() error { fmt.Fprintln(s.app.Out, s.app.Version); return nil })},
	})
}

func (s *menuSession) maintenance() (bool, error) {
	return s.loop("Setup & Maintenance", []menuAction{
		{id: "setup", label: "Setup / Repair", description: "Install and reconcile all client components", long: true, run: s.simple(func() error { return s.app.Setup(s.ctx) })},
		{id: "update", label: "Update", description: "Check and install compatible ivoai/component updates", long: true, run: s.simple(func() error { return s.app.Update(s.ctx) })},
		{id: "rollback", label: "Rollback Update", description: "Restore the binary retained by the previous update", run: s.confirmedProgress("ROLLBACK", "Rolling back ivoai", func() error { return s.app.RollbackUpdate(s.ctx) })},
		{id: "uninstall", label: "Uninstall", description: "Remove only items managed by ivoai", run: s.confirmedProgressExit("UNINSTALL", "Uninstalling ivoai-managed files", func() error { return s.app.Uninstall(s.ctx) })},
	})
}

func (s *menuSession) connections() (bool, error) {
	snapshot, _ := s.app.MenuSnapshot()
	return s.loop("Connections", []menuAction{
		{id: "connect.list", label: "Connection Status", run: s.simple(func() error { return s.app.Status(s.ctx) })},
		{id: "connect.chatgpt", label: "Connect ChatGPT / Codex", description: "Use the official Codex login flow", run: s.simple(func() error { return s.app.ConnectAgent(s.ctx, "chatgpt") })},
		{id: "disconnect.chatgpt", label: "Disconnect ChatGPT state", disabled: disabledUnless(snapshot.ChatGPTConnected, "not connected"), run: s.simple(func() error { return s.app.DisconnectAgent(s.ctx, "chatgpt") })},
		{id: "connect.claude", label: "Connect Claude", description: "Use the official Claude Code login flow", run: s.simple(func() error { return s.app.ConnectAgent(s.ctx, "claude") })},
		{id: "disconnect.claude", label: "Disconnect Claude state", disabled: disabledUnless(snapshot.ClaudeConnected, "not connected"), run: s.simple(func() error { return s.app.DisconnectAgent(s.ctx, "claude") })},
		{id: "connect.server", label: "Connect ivoai Server", description: "Discover and enroll with a one-time code", run: s.connectServer},
		{id: "disconnect.server", label: "Disconnect ivoai Server", disabled: disabledUnless(snapshot.ServerConnected, "not connected"), run: s.confirmed("DISCONNECT", func() error { return s.app.DisconnectServer(s.ctx) })},
		{id: "mcp", label: "External MCP Registry", run: s.mcp},
	})
}

func (s *menuSession) agents() (bool, error) {
	return s.loop("Agents", []menuAction{
		{id: "launch.codex", label: "Launch Codex", description: "Use Headroom when healthy, with direct fallback", run: func() (bool, error) { return true, s.app.Launch(s.ctx, "codex", nil) }},
		{id: "launch.claude", label: "Launch Claude", description: "Use Headroom when healthy, with direct fallback", run: func() (bool, error) { return true, s.app.Launch(s.ctx, "claude", nil) }},
	})
}

func (s *menuSession) sessions() (bool, error) {
	return s.loop("Session Control", []menuAction{
		{id: "session.direct.codex", label: "Direct Session — Codex", description: "Official Codex runtime with session observability; Ruflo is not started", run: func() (bool, error) { return true, s.app.SessionStart(s.ctx, "codex", "direct", nil) }},
		{id: "session.direct.claude", label: "Direct Session — Claude", description: "Official Claude runtime with session observability; Ruflo is not started", run: func() (bool, error) { return true, s.app.SessionStart(s.ctx, "claude", "direct", nil) }},
		{id: "session.orchestrated.codex", label: "Orchestrated Session — Codex", description: "Safe Ruflo swarm with official Codex primary and bounded workers", run: func() (bool, error) { return true, s.app.SessionStart(s.ctx, "codex", "orchestrated", nil) }},
		{id: "session.orchestrated.claude", label: "Orchestrated Session — Claude", description: "Safe Ruflo swarm with official Claude primary and bounded workers", run: func() (bool, error) { return true, s.app.SessionStart(s.ctx, "claude", "orchestrated", nil) }},
		{id: "session.list", label: "List Sessions", description: "Show non-sensitive lifecycle metadata", run: s.simple(func() error { return runSession(s.ctx, s.app, []string{"list"}) })},
		{id: "session.monitor", label: "Monitor Latest Session", description: "Show primary, swarm, workers, and service health", run: s.simple(func() error { return runMonitor(s.ctx, s.app, nil) })},
		{id: "session.stop", label: "Stop Session", description: "Stop only processes whose PID identity matches the session", run: s.sessionStop},
	})
}

func (s *menuSession) sessionStop() (bool, error) {
	id, err := s.promptValidated("Session ID", false, "", func(value string) error {
		if !strings.HasPrefix(value, "sess_") || len(value) != 37 {
			return errors.New("invalid session ID")
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	if !s.confirm("STOP") {
		return false, nil
	}
	return false, s.app.SessionStop(id)
}

func (s *menuSession) memory() (bool, error) {
	return s.loop("Memory", []menuAction{
		{id: "memory.status", label: "Memory Status", run: s.simple(func() error { return s.app.MemoryStatus(s.ctx) })},
		{id: "memory.configure", label: "Reconfigure Memory", long: true, run: s.simple(func() error { return s.app.ReconfigureMemory(s.ctx) })},
	})
}

func (s *menuSession) project() (bool, error) {
	return s.loop("Project", []menuAction{
		{id: "project.status", label: "Project Identity", run: s.simple(func() error { s.app.ProjectStatus(); return nil })},
		{id: "project.init", label: "Initialize Project Override", run: s.simple(s.app.ProjectInit)},
	})
}

func (s *menuSession) configuration() (bool, error) {
	snapshot, _ := s.app.MenuSnapshot()
	return s.loop("Configuration", []menuAction{
		{id: "config.show", label: "Show Configuration", run: s.simple(s.app.ConfigShow)},
		{id: "config.headroom", label: toggleLabel("Headroom", snapshot.HeadroomEnabled), run: s.simple(func() error { return s.app.ConfigSet("headroom.enabled", opposite(snapshot.HeadroomEnabled)) })},
		{id: "config.memory", label: toggleLabel("ai-memory", snapshot.MemoryEnabled), run: s.simple(func() error { return s.app.ConfigSet("memory.enabled", opposite(snapshot.MemoryEnabled)) })},
		{id: "config.ruflo", label: toggleLabel("Ruflo", snapshot.RufloEnabled), run: s.simple(func() error { return s.app.ConfigSet("orchestration.enabled", opposite(snapshot.RufloEnabled)) })},
		{id: "config.session-mode", label: "Default Session Mode: " + strings.ToUpper(snapshot.DefaultMode), run: s.simple(func() error { return s.app.ConfigSet("orchestration.default_mode", otherMode(snapshot.DefaultMode)) })},
		{id: "config.primary", label: "Primary Executor: " + strings.ToUpper(snapshot.PrimaryExecutor), run: s.simple(func() error {
			return s.app.ConfigSet("orchestration.primary_executor", otherExecutor(snapshot.PrimaryExecutor))
		})},
		{id: "config.reviewer", label: "Review Executor: " + strings.ToUpper(snapshot.ReviewExecutor), run: s.simple(func() error {
			return s.app.ConfigSet("orchestration.review_executor", otherExecutor(snapshot.ReviewExecutor))
		})},
		{id: "config.workers", label: fmt.Sprintf("Maximum Workers: %d", snapshot.MaxWorkers), run: s.maxWorkers},
	})
}

func (s *menuSession) maxWorkers() (bool, error) {
	value, err := s.promptValidated("Maximum workers (1-3)", false, "2", func(value string) error {
		if value != "1" && value != "2" && value != "3" {
			return errors.New("maximum workers must be 1, 2, or 3")
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return false, s.app.ConfigSet("orchestration.max_workers", value)
}

func (s *menuSession) mcp() (bool, error) {
	return s.loop("External MCP Registry", []menuAction{
		{id: "mcp.list", label: "List MCPs", run: s.simple(s.app.MCPList)},
		{id: "mcp.add", label: "Add MCP", run: s.mcpAdd},
		{id: "mcp.remove", label: "Remove MCP", run: s.mcpRemove},
	})
}

func (s *menuSession) server() (bool, error) {
	installed := localServerInstalled()
	readRestriction, mutationRestriction := serverRestrictions(runtime.GOOS, os.Geteuid(), installed)
	setupRestriction := serverSetupRestriction(runtime.GOOS, os.Geteuid())
	return s.loop("Server Administration", []menuAction{
		{id: "server.setup", label: "Setup / Repair Server", disabled: setupRestriction, long: true, run: s.serverArgs("setup")},
		{id: "server.status", label: "Service Status", disabled: readRestriction, run: s.serverArgs("status")},
		{id: "server.doctor", label: "Server Doctor", disabled: readRestriction, long: true, run: s.serverArgs("doctor")},
		{id: "server.start", label: "Start Services", disabled: mutationRestriction, long: true, run: s.serverArgs("start")},
		{id: "server.stop", label: "Stop Services", disabled: mutationRestriction, run: s.confirmedServerProgress("STOP", "Stopping server services", "stop")},
		{id: "server.restart", label: "Restart Services", disabled: mutationRestriction, long: true, run: s.serverArgs("restart")},
		{id: "server.logs", label: "Service Logs", disabled: readRestriction, run: s.serverLogs},
		{id: "enrollment", label: "Enrollment", disabled: readRestriction, run: s.enrollment},
		{id: "web-access", label: "Web MCP Access", disabled: readRestriction, run: s.webAccess},
		{id: "connectors", label: "Connectors", disabled: readRestriction, run: s.connectors},
		{id: "server.context.status", label: "Context Status", disabled: readRestriction, run: s.serverArgs("context", "status")},
		{id: "server.memory.status", label: "Memory Status", disabled: readRestriction, run: s.serverArgs("memory", "status")},
		{id: "server.gateway.configure", label: "Configure Gateway", disabled: mutationRestriction, run: s.gatewayConfigure},
		{id: "server.backup", label: "Backup", disabled: mutationRestriction, run: s.backup},
		{id: "server.restore", label: "Restore", disabled: mutationRestriction, run: s.restore},
	})
}

func (s *menuSession) enrollment() (bool, error) {
	readRestriction, requiresRoot := serverRestrictions(runtime.GOOS, os.Geteuid(), localServerInstalled())
	return s.loop("Enrollment", []menuAction{
		{id: "server.enrollment.create", label: "Create One-Time Code", disabled: requiresRoot, run: s.enrollmentCreate},
		{id: "server.enrollment.list", label: "List Codes", disabled: readRestriction, run: s.serverArgs("enrollment", "list")},
		{id: "server.enrollment.revoke", label: "Revoke Code", disabled: requiresRoot, run: s.enrollmentRevoke},
	})
}

func (s *menuSession) webAccess() (bool, error) {
	readRestriction, requiresRoot := serverRestrictions(runtime.GOOS, os.Geteuid(), localServerInstalled())
	return s.loop("Web MCP Access", []menuAction{
		{id: "server.web-access.create", label: "Create Activation Code", disabled: requiresRoot, run: s.webAccessCreate},
		{id: "server.web-access.list", label: "List Activation Codes", disabled: readRestriction, run: s.serverArgs("web-access", "list")},
		{id: "server.web-access.revoke", label: "Revoke Activation Code", disabled: requiresRoot, run: s.webAccessRevoke},
	})
}

func (s *menuSession) connectors() (bool, error) {
	readRestriction, requiresRoot := serverRestrictions(runtime.GOOS, os.Geteuid(), localServerInstalled())
	return s.loop("Connectors", []menuAction{
		{id: "server.connector.list", label: "List Connectors", disabled: readRestriction, run: s.serverArgs("connector", "list")},
		{id: "server.connector.add", label: "Add Connector", disabled: requiresRoot, run: s.connectorAdd},
		{id: "server.connector.remove", label: "Remove Connector", disabled: requiresRoot, run: s.connectorRemove},
	})
}

func (s *menuSession) remote() (bool, error) {
	return s.loop("Remote Server", []menuAction{
		{id: "remote.status", label: "Remote Status", long: true, run: s.serverArgs("remote", "status")},
		{id: "remote.doctor", label: "Remote Doctor", long: true, run: s.serverArgs("remote", "doctor")},
		{id: "remote.connector.list", label: "Remote Connector List", long: true, run: s.serverArgs("remote", "connector", "list")},
	})
}

func (s *menuSession) loop(title string, actions []menuAction) (bool, error) {
	for {
		id, err := s.choose(title, actions, nil)
		if err != nil {
			return false, err
		}
		if id == "" {
			return false, nil
		}
		exit, runErr := s.execute(findAction(actions, id))
		if runErr != nil {
			fmt.Fprintf(s.app.Err, "%s %s\n", terminalui.Failure("Error:", terminalui.ColorEnabled(s.app.Err)), UserError(runErr))
		}
		if exit {
			return true, runErr
		}
		terminalui.Pause(s.app.In, s.app.Out)
	}
}

func (s *menuSession) choose(title string, actions []menuAction, badges []terminalui.Badge) (string, error) {
	items := make([]terminalui.Item, 0, len(actions))
	for _, action := range actions {
		items = append(items, terminalui.Item{ID: action.id, Label: action.label, Description: action.description, DisabledReason: action.disabled})
	}
	input := io.Reader(s.reader)
	if _, ok := s.app.In.(*os.File); ok {
		input = s.app.In
	}
	return (terminalui.Selector{Context: s.ctx, In: input, Out: s.app.Out, Version: s.app.Version}).Choose(title, items, badges)
}

func (s *menuSession) execute(action menuAction) (bool, error) {
	if action.run == nil {
		return false, nil
	}
	if !action.long {
		if terminalui.HumanOutput(s.app.Out) {
			fmt.Fprint(s.app.Out, terminalui.ScreenHeader(s.app.Out, s.app.Version))
		}
		return action.run()
	}
	originalOut, originalErr := s.app.Out, s.app.Err
	if s.progress.Animated() {
		s.app.Out, s.app.Err = s.progress.Writer(originalOut), s.progress.Writer(originalErr)
		defer func() { s.app.Out, s.app.Err = originalOut, originalErr }()
	}
	var exit bool
	err := s.progress.Run(s.ctx, action.label, func(context.Context) error {
		var runErr error
		exit, runErr = action.run()
		return runErr
	})
	return exit, err
}

func (s *menuSession) simple(operation func() error) func() (bool, error) {
	return func() (bool, error) { return false, operation() }
}

func (s *menuSession) confirmed(phrase string, operation func() error) func() (bool, error) {
	return func() (bool, error) {
		if !s.confirm(phrase) {
			fmt.Fprintln(s.app.Out, terminalui.Warning("Cancelled.", terminalui.ColorEnabled(s.app.Out)))
			return false, nil
		}
		return false, operation()
	}
}

func (s *menuSession) confirmedProgress(phrase, label string, operation func() error) func() (bool, error) {
	return func() (bool, error) {
		if !s.confirm(phrase) {
			fmt.Fprintln(s.app.Out, terminalui.Warning("Cancelled.", terminalui.ColorEnabled(s.app.Out)))
			return false, nil
		}
		return false, runProgress(s.ctx, s.app, label, operation)
	}
}

func (s *menuSession) confirmedProgressExit(phrase, label string, operation func() error) func() (bool, error) {
	return func() (bool, error) {
		if !s.confirm(phrase) {
			fmt.Fprintln(s.app.Out, terminalui.Warning("Cancelled.", terminalui.ColorEnabled(s.app.Out)))
			return false, nil
		}
		return true, runProgress(s.ctx, s.app, label, operation)
	}
}

func (s *menuSession) serverArgs(args ...string) func() (bool, error) {
	return s.simple(func() error { return runServer(s.ctx, args, s.app) })
}

func (s *menuSession) confirmedServerProgress(phrase, label string, args ...string) func() (bool, error) {
	return s.confirmedProgress(phrase, label, func() error { return runServer(s.ctx, args, s.app) })
}

func (s *menuSession) connectServer() (bool, error) {
	serverURL, err := s.promptValidated("Server URL", false, "", validateHTTPSURL)
	if err != nil {
		return false, err
	}
	code, err := s.promptValidated("Enrollment code", true, "", validateEnrollmentCode)
	if err != nil {
		return false, err
	}
	return false, runProgress(s.ctx, s.app, "Connecting ivoai server", func() error {
		return s.app.ConnectServer(s.ctx, serverURL, code)
	})
}

func (s *menuSession) mcpAdd() (bool, error) {
	name, err := s.promptValidated("MCP name", false, "", validateIdentifier)
	if err != nil {
		return false, err
	}
	endpoint, err := s.promptValidated("HTTPS endpoint", false, "", validateHTTPSURL)
	if err != nil {
		return false, err
	}
	return false, s.app.MCPAdd(name, endpoint)
}

func (s *menuSession) mcpRemove() (bool, error) {
	name, err := s.promptValidated("MCP name", false, "", validateIdentifier)
	if err != nil {
		return false, err
	}
	if !s.confirm("REMOVE") {
		return false, nil
	}
	return false, s.app.MCPRemove(name)
}

func (s *menuSession) serverLogs() (bool, error) {
	service, err := s.prompt("Service (blank for gateway)", false, "")
	if err != nil {
		return false, err
	}
	args := []string{"logs"}
	if service != "" {
		args = append(args, service)
	}
	return false, runServer(s.ctx, args, s.app)
}

func (s *menuSession) enrollmentCreate() (bool, error) {
	ttl, err := s.promptValidated("TTL", false, "10m", validatePositiveDuration)
	if err != nil {
		return false, err
	}
	return false, runServer(s.ctx, []string{"enrollment", "create", "--ttl", ttl}, s.app)
}

func (s *menuSession) enrollmentRevoke() (bool, error) {
	id, err := s.promptValidated("Enrollment ID", false, "", validateIdentifier)
	if err != nil {
		return false, err
	}
	if !s.confirm("REVOKE") {
		return false, nil
	}
	return false, runServer(s.ctx, []string{"enrollment", "revoke", id}, s.app)
}

func (s *menuSession) webAccessCreate() (bool, error) {
	ttl, err := s.promptValidated("Activation TTL", false, "10m", func(value string) error {
		duration, err := time.ParseDuration(value)
		if err != nil || duration <= 0 || duration > 24*time.Hour {
			return errors.New("TTL must be a duration between zero and 24h")
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return false, runServer(s.ctx, []string{"web-access", "create", "--ttl", ttl}, s.app)
}

func (s *menuSession) webAccessRevoke() (bool, error) {
	id, err := s.promptValidated("Web access ID", false, "", validateIdentifier)
	if err != nil {
		return false, err
	}
	if !s.confirm("REVOKE") {
		return false, nil
	}
	return false, runServer(s.ctx, []string{"web-access", "revoke", id}, s.app)
}

func (s *menuSession) connectorAdd() (bool, error) {
	name, err := s.promptValidated("Connector name", false, "", validateIdentifier)
	if err != nil {
		return false, err
	}
	kind, err := s.promptValidated("Type (filesystem or git)", false, "filesystem", validateConnectorType)
	if err != nil {
		return false, err
	}
	path, err := s.promptValidated("Absolute path", false, "", validateAbsolutePath)
	if err != nil {
		return false, err
	}
	return false, runProgress(s.ctx, s.app, "Adding server connector", func() error {
		return runServer(s.ctx, []string{"connector", "add", "--name", name, "--type", kind, "--path", path}, s.app)
	})
}

func (s *menuSession) connectorRemove() (bool, error) {
	name, err := s.promptValidated("Connector name", false, "", validateIdentifier)
	if err != nil {
		return false, err
	}
	if !s.confirm("REMOVE") {
		return false, nil
	}
	return false, runProgress(s.ctx, s.app, "Removing server connector", func() error {
		return runServer(s.ctx, []string{"connector", "remove", name}, s.app)
	})
}

func (s *menuSession) gatewayConfigure() (bool, error) {
	publicURL, err := s.promptValidated("Public HTTPS URL", false, "", validateHTTPSURL)
	if err != nil {
		return false, err
	}
	listen, err := s.promptValidated("Listen address", false, "127.0.0.1:7744", validateListenAddress)
	if err != nil {
		return false, err
	}
	proxy, err := s.prompt("Trusted proxy CIDR (blank for same-host proxy)", false, "")
	if err != nil {
		return false, err
	}
	tlsCert, err := s.prompt("TLS certificate path (blank for reverse proxy)", false, "")
	if err != nil {
		return false, err
	}
	tlsKey, err := s.prompt("TLS private key path (blank for reverse proxy)", true, "")
	if err != nil {
		return false, err
	}
	args := []string{"gateway", "configure", "--public-url", publicURL, "--listen", listen}
	if proxy != "" {
		if err := validateCIDR(proxy); err != nil {
			return false, err
		}
		args = append(args, "--trusted-proxy", proxy)
	}
	if (tlsCert == "") != (tlsKey == "") {
		return false, errors.New("TLS certificate and key paths must be provided together")
	}
	if tlsCert != "" {
		if err := validateAbsolutePath(tlsCert); err != nil {
			return false, fmt.Errorf("invalid TLS certificate path: %w", err)
		}
		if err := validateAbsolutePath(tlsKey); err != nil {
			return false, fmt.Errorf("invalid TLS private key path: %w", err)
		}
		args = append(args, "--tls-cert", tlsCert, "--tls-key", tlsKey)
	}
	return false, runProgress(s.ctx, s.app, "Configuring server gateway", func() error {
		return runServer(s.ctx, args, s.app)
	})
}

func (s *menuSession) backup() (bool, error) {
	path, err := s.prompt("Output path (blank for default)", false, "")
	if err != nil {
		return false, err
	}
	args := []string{"backup"}
	if path != "" {
		if err := validateAbsolutePath(path); err != nil {
			return false, fmt.Errorf("invalid output path: %w", err)
		}
		args = append(args, "--output", path)
	}
	return false, runProgress(s.ctx, s.app, "Creating server backup", func() error {
		return runServer(s.ctx, args, s.app)
	})
}

func (s *menuSession) restore() (bool, error) {
	path, err := s.promptValidated("Backup archive path", false, "", validateAbsolutePath)
	if err != nil {
		return false, err
	}
	if !s.confirm("RESTORE") {
		return false, nil
	}
	return false, runProgress(s.ctx, s.app, "Restoring server backup", func() error {
		return runServer(s.ctx, []string{"restore", "--input", path}, s.app)
	})
}

func (s *menuSession) prompt(label string, secret bool, defaultValue string) (string, error) {
	if file, ok := s.app.In.(*os.File); ok && file != nil {
		value, err := s.app.Prompt(label+defaultSuffix(defaultValue)+": ", secret)
		if value == "" {
			value = defaultValue
		}
		return strings.TrimSpace(value), err
	}
	fmt.Fprintf(s.app.Out, "%s%s: ", label, defaultSuffix(defaultValue))
	line, err := s.reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	value := strings.TrimSpace(line)
	if value == "" {
		value = defaultValue
	}
	return value, nil
}

func (s *menuSession) promptValidated(label string, secret bool, defaultValue string, validate func(string) error) (string, error) {
	value, err := s.prompt(label, secret, defaultValue)
	if err != nil {
		return "", err
	}
	if err := validate(value); err != nil {
		return "", fmt.Errorf("invalid %s: %w", strings.ToLower(label), err)
	}
	return value, nil
}

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func validateIdentifier(value string) error {
	if !identifierPattern.MatchString(value) {
		return errors.New("use 1-128 letters, digits, dots, underscores, or hyphens")
	}
	return nil
}

func validateHTTPSURL(value string) error {
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return errors.New("enter an HTTPS URL without embedded credentials")
	}
	return nil
}

func validateEnrollmentCode(value string) error {
	if len(value) < 32 || len(value) > 512 || !strings.HasPrefix(value, "ivoai-enroll_") || strings.ContainsAny(value, " \t\r\n") {
		return errors.New("enter the one-time code exactly as shown by the server")
	}
	return nil
}

func validatePositiveDuration(value string) error {
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return errors.New("use a positive duration such as 10m or 1h")
	}
	return nil
}

func validateConnectorType(value string) error {
	if value != "filesystem" && value != "git" {
		return errors.New("type must be filesystem or git")
	}
	return nil
}

func validateAbsolutePath(value string) error {
	if value == "" || !filepath.IsAbs(value) || filepath.Clean(value) != value {
		return errors.New("enter a clean absolute path")
	}
	return nil
}

func validateListenAddress(value string) error {
	_, port, err := net.SplitHostPort(value)
	if err != nil || port == "" {
		return errors.New("use host:port, for example 127.0.0.1:7744")
	}
	return nil
}

func validateCIDR(value string) error {
	if _, err := netip.ParsePrefix(value); err != nil {
		return errors.New("enter a valid CIDR, for example 192.0.2.0/24")
	}
	return nil
}

func (s *menuSession) confirm(phrase string) bool {
	value, err := s.prompt("Type "+phrase+" to confirm", false, "")
	return err == nil && value == phrase
}

func snapshotBadges(snapshot app.MenuSnapshot) []terminalui.Badge {
	overall, kind := "READY", "success"
	if !snapshot.SetupComplete {
		overall, kind = "SETUP REQUIRED", "warning"
	} else if !snapshot.ComponentsReady {
		overall, kind = "DEGRADED", "error"
	}
	return []terminalui.Badge{
		{Label: "Overall", Value: overall, Kind: kind},
		{Label: "ChatGPT", Value: connectedLabel(snapshot.ChatGPTConnected), Kind: connectionKind(snapshot.ChatGPTConnected)},
		{Label: "Claude", Value: connectedLabel(snapshot.ClaudeConnected), Kind: connectionKind(snapshot.ClaudeConnected)},
		{Label: "Server", Value: connectedLabel(snapshot.ServerConnected), Kind: connectionKind(snapshot.ServerConnected)},
	}
}

func findAction(actions []menuAction, id string) menuAction {
	for _, action := range actions {
		if action.id == id {
			return action
		}
	}
	return menuAction{}
}

func disabledUnless(available bool, reason string) string {
	if available {
		return ""
	}
	return reason
}

func localServerInstalled() bool {
	_, err := os.Stat("/etc/ivoai/compose.yaml")
	return err == nil
}

func serverSetupRestriction(goos string, euid int) string {
	if goos != "linux" {
		return "Linux server only"
	}
	if euid != 0 {
		return "requires root"
	}
	return ""
}

func serverRestrictions(goos string, euid int, installed bool) (read, mutation string) {
	if goos != "linux" {
		return "Linux server only", "Linux server only"
	}
	if !installed {
		read = "local server not installed"
	}
	if euid != 0 {
		mutation = "requires root"
	} else if !installed {
		mutation = "local server not installed"
	}
	return read, mutation
}

func connectedLabel(value bool) string {
	if value {
		return "connected"
	}
	return "pending"
}

func connectionKind(value bool) string {
	if value {
		return "success"
	}
	return "warning"
}

func toggleLabel(name string, enabled bool) string {
	action := "Enable "
	if enabled {
		action = "Disable "
	}
	return action + name
}

func opposite(value bool) string {
	if value {
		return "false"
	}
	return "true"
}

func otherMode(value string) string {
	if value == "orchestrated" {
		return "direct"
	}
	return "orchestrated"
}

func otherExecutor(value string) string {
	if value == "claude" {
		return "codex"
	}
	return "claude"
}

func defaultSuffix(value string) string {
	if value == "" {
		return ""
	}
	return " [" + value + "]"
}
