package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/terminalui"
)

type ServerRunner func(context.Context, []string, io.Reader, io.Writer, io.Writer) error

var serverRunner ServerRunner

func RegisterServerRunner(runner ServerRunner) { serverRunner = runner }

func Run(ctx context.Context, a *app.App, args []string) error {
	if len(args) > 0 && args[0] == "--debug" {
		_ = os.Setenv("IVOAI_LOG_LEVEL", "debug")
		args = args[1:]
	}
	if len(args) == 0 {
		platform.DebugLog(a.Err, "cli.menu", nil)
		return menu(ctx, a)
	}
	if label, enabled := commandProgress(args); enabled {
		return runProgress(ctx, a, label, func() error { return runCommand(ctx, a, args) })
	}
	if commandHeaderEnabled(args) && terminalui.HumanOutput(a.Out) {
		fmt.Fprint(a.Out, terminalui.ScreenHeader(a.Out, a.Version))
		if args[0] == "version" {
			return nil
		}
	}
	return runCommand(ctx, a, args)
}

func commandHeaderEnabled(args []string) bool {
	if len(args) == 0 || args[0] == "_register-install" || args[0] == "_orchestrator-serve" || args[0] == "_quota-statusline" || args[0] == "_update-metadata" || args[0] == "_update-migrate" {
		return false
	}
	if args[0] == "doctor" && contains(args, "--json") {
		return false
	}
	if (args[0] == "monitor" || args[0] == "session") && contains(args, "--json") {
		return false
	}
	if len(args) >= 3 && args[0] == "server" && (args[1] == "gateway" || args[1] == "context") && args[2] == "serve" {
		return false
	}
	return true
}

func runCommand(ctx context.Context, a *app.App, args []string) error {
	platform.DebugLog(a.Err, "cli.command", map[string]string{"command": args[0]})
	switch args[0] {
	case "help", "--help", "-h":
		usage(a.Out)
		return nil
	case "setup":
		fs := flag.NewFlagSet("setup", flag.ContinueOnError)
		fs.SetOutput(a.Err)
		mode := fs.String("mode", "client", "client or server")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		modeExplicit := false
		for _, argument := range args[1:] {
			if argument == "--mode" || strings.HasPrefix(argument, "--mode=") {
				modeExplicit = true
				break
			}
		}
		if !modeExplicit {
			serverInstall, err := a.ExistingServerInstallation()
			if err != nil {
				return err
			}
			if serverInstall {
				return runServer(ctx, append([]string{"setup"}, fs.Args()...), a)
			}
		}
		if *mode == "server" {
			return runServer(ctx, append([]string{"setup"}, fs.Args()...), a)
		}
		if *mode != "client" {
			return fmt.Errorf("invalid setup mode %q", *mode)
		}
		return a.Setup(ctx)
	case "status":
		return a.Status(ctx)
	case "doctor":
		return runDoctor(ctx, a, args[1:])
	case "version":
		fmt.Fprintln(a.Out, a.Version)
		return nil
	case "update":
		fs := flag.NewFlagSet("update", flag.ContinueOnError)
		fs.SetOutput(a.Err)
		rollback := fs.Bool("rollback", false, "restore the binary retained by the last update")
		dryRun := fs.Bool("dry-run", false, "stage and probe the verified candidate, then print the plan without committing managed changes")
		force := fs.Bool("force", false, "allow rollback to overwrite managed files changed since the update")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("update accepts no positional arguments")
		}
		if *rollback && *dryRun {
			return errors.New("update --rollback and --dry-run cannot be combined")
		}
		if *force && !*rollback {
			return errors.New("update --force requires --rollback")
		}
		if *rollback {
			if *force {
				return a.ForceRollbackUpdate(ctx)
			}
			return a.RollbackUpdate(ctx)
		}
		if *dryRun {
			return a.UpdateDryRun(ctx)
		}
		return a.Update(ctx)
	case "uninstall":
		return a.Uninstall(ctx)
	case "_register-install":
		return a.RegisterInstall()
	case "_update-metadata":
		if len(args) != 1 {
			return errors.New("invalid update metadata invocation")
		}
		return json.NewEncoder(a.Out).Encode(a.UpdateCompatibility())
	case "_update-migrate":
		if len(args) != 1 {
			return errors.New("invalid prepared update migration invocation")
		}
		return a.ApplyPreparedUpdateMigration(ctx)
	case "connect":
		return runConnect(ctx, a, args[1:])
	case "disconnect":
		return runDisconnect(ctx, a, args[1:])
	case "codex", "claude":
		return a.Launch(ctx, args[0], trimDoubleDash(args[1:]))
	case "auto":
		fs := flag.NewFlagSet("auto", flag.ContinueOnError)
		fs.SetOutput(a.Err)
		planner := fs.String("planner", "", "codex or claude")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.Auto(ctx, *planner, trimDoubleDash(fs.Args()))
	case "session":
		return runSession(ctx, a, args[1:])
	case "monitor":
		return runMonitor(ctx, a, args[1:])
	case "_orchestrator-serve":
		fs := flag.NewFlagSet("_orchestrator-serve", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("session", "", "session ID")
		if err := fs.Parse(args[1:]); err != nil || *id == "" || fs.NArg() != 0 {
			return errors.New("invalid local orchestrator invocation")
		}
		return a.OrchestratorServe(ctx, *id)
	case "_quota-statusline":
		fs := flag.NewFlagSet("_quota-statusline", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		id := fs.String("session", "", "session ID")
		if err := fs.Parse(args[1:]); err != nil || *id == "" || fs.NArg() != 0 {
			return errors.New("invalid quota statusline invocation")
		}
		body, err := io.ReadAll(io.LimitReader(a.In, (64<<10)+1))
		if err != nil || len(body) > 64<<10 {
			return errors.New("invalid quota statusline payload")
		}
		line, err := a.QuotaStatusline(*id, body)
		if err != nil {
			return err
		}
		fmt.Fprintln(a.Out, line)
		return nil
	case "memory":
		return runMemory(ctx, a, args[1:])
	case "config":
		return runConfig(a, args[1:])
	case "project":
		return runProject(a, args[1:])
	case "server":
		return runServer(ctx, args[1:], a)
	default:
		return fmt.Errorf("unknown command %q; run ivoai help", args[0])
	}
}

func runSession(ctx context.Context, a *app.App, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ivoai session <start|list|show|stop>")
	}
	switch args[0] {
	case "start":
		fs := flag.NewFlagSet("session start", flag.ContinueOnError)
		fs.SetOutput(a.Err)
		executor := fs.String("executor", "codex", "codex or claude")
		mode := fs.String("mode", "direct", "direct or orchestrated")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return a.SessionStart(ctx, *executor, session.Mode(*mode), trimDoubleDash(fs.Args()))
	case "list":
		fs := flag.NewFlagSet("session list", flag.ContinueOnError)
		fs.SetOutput(a.Err)
		jsonOutput := fs.Bool("json", false, "JSON output")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			return errors.New("usage: ivoai session list [--json]")
		}
		values, err := a.SessionList()
		if err != nil {
			return err
		}
		return writeSessions(a.Out, values, *jsonOutput)
	case "show":
		fs := flag.NewFlagSet("session show", flag.ContinueOnError)
		fs.SetOutput(a.Err)
		jsonOutput := fs.Bool("json", false, "JSON output")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 1 {
			return errors.New("usage: ivoai session show [--json] <session-id>")
		}
		value, err := a.SessionShow(fs.Arg(0))
		if err != nil {
			return err
		}
		return writeSessions(a.Out, []session.Session{value}, *jsonOutput)
	case "stop":
		if len(args) != 2 {
			return errors.New("usage: ivoai session stop <session-id>")
		}
		return a.SessionStop(args[1])
	default:
		return fmt.Errorf("unknown session action %q", args[0])
	}
}

func runDoctor(ctx context.Context, a *app.App, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	jsonOutput := fs.Bool("json", false, "JSON output")
	inventory := fs.Bool("inventory", false, "include sanitized compatibility inventory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *inventory {
		value := a.SupportInventory(ctx)
		if *jsonOutput {
			return json.NewEncoder(a.Out).Encode(value)
		}
		fmt.Fprintf(a.Out, "ivoai compatibility inventory\nVersion: %s\nPlatform: %s/%s\nModes: %s\nExecutable: %s\nConfig root: %s\nData root: %s\nState root: %s\nCache root: %s\nSchemas: config=%d state=%d ownership=%d server-protocol=%d\nInstall provenance: %s\nRollback available: %t\nInventory: %s\n", value.IVOAI, value.OS, value.Architecture, strings.Join(value.Modes, ", "), value.Paths.Executable, value.Paths.ConfigRoot, value.Paths.DataRoot, value.Paths.StateRoot, value.Paths.CacheRoot, value.Schemas.Config, value.Schemas.State, value.Schemas.Ownership, value.ServerProtocol, value.InstallProvenance, value.RollbackAvailable, value.InventoryOverall)
		for _, component := range value.Components {
			fmt.Fprintf(a.Out, "  %s installed=%t managed=%t version=%s path=%s\n", component.Name, component.Installed, component.Managed, component.Version, component.Path)
		}
		return nil
	}
	report := a.Doctor(ctx)
	if *jsonOutput {
		b, err := report.JSON()
		if err != nil {
			return err
		}
		fmt.Fprintln(a.Out, string(b))
		return nil
	}
	color := terminalui.ColorEnabled(a.Out)
	fmt.Fprintf(a.Out, "ivoai doctor\nOS: %s\nArchitecture: %s\nivoai: %s\nConfig: %s\nState: %s\nSecret permissions: %s\n", report.OS, report.Architecture, report.Version, report.ConfigPath, report.StatePath, report.SecretPermissions)
	fmt.Fprintf(a.Out, "\nCodex: installed=%s version=%s authenticated=%s\n", semanticBool(report.Codex.Installed, color), report.Codex.Version, semanticOptionalBool(report.Codex.Authenticated, color))
	fmt.Fprintf(a.Out, "Claude Code: installed=%s version=%s authenticated=%s\n", semanticBool(report.Claude.Installed, color), report.Claude.Version, semanticOptionalBool(report.Claude.Authenticated, color))
	fmt.Fprintf(a.Out, "Headroom: installed=%s enabled=%s healthy=%s version=%s interactive-launch=%s\nCodex via Headroom: %s\nClaude Code via Headroom: %s\n", semanticBool(report.Headroom.Installed, color), semanticOptionalBool(report.Headroom.Enabled, color), semanticBool(report.Headroom.Healthy, color), report.Headroom.Version, report.Headroom.InteractiveLaunch, semanticOK(report.Headroom.CodexCompatible, color), semanticOK(report.Headroom.ClaudeCompatible, color))
	fmt.Fprintf(a.Out, "ai-memory: installed=%s version=%s hooks=%s server=%s\n", semanticBool(report.Memory.Installed, color), report.Memory.Version, semanticBool(report.Memory.Hooks, color), configured(report.Server.Configured))
	fmt.Fprintf(a.Out, "Ruflo: installed=%s version=%s safe-mode=%s provider-execution=%s\n", semanticBool(report.Ruflo.Installed, color), report.Ruflo.Version, semanticBool(report.Ruflo.SafeMode, color), semanticDisabledIsSafe(report.Ruflo.ProviderExecution, color))
	fmt.Fprintf(a.Out, "Orchestration: enabled=%s bridge=%s session-permissions=%s max-workers=%d codex-worker=%s claude-worker=%s\n", semanticOptionalBool(report.Orchestration.Enabled, color), semanticBool(report.Orchestration.BridgeAvailable, color), report.Orchestration.SessionPerms, report.Orchestration.MaxWorkers, semanticBool(report.Orchestration.CodexWorker, color), semanticBool(report.Orchestration.ClaudeWorker, color))
	fmt.Fprintf(a.Out, "\nAutomatic Orchestration\n  enabled=%s default=%s failover=%s checkpoint=%s\n", semanticOptionalBool(report.Automatic.Enabled, color), report.Automatic.DefaultPlanner, semanticOptionalBool(report.Automatic.AutomaticFailover, color), semanticBool(report.Automatic.CheckpointReady, color))
	fmt.Fprintf(a.Out, "  scheduler=%s parallel-runtime=%s shared-bootstrap=%s\n  codex-routing=%s effort=%s\n  claude-routing=%s effort=%s\n", semanticBool(report.Automatic.SchedulerReady, color), semanticBool(report.Automatic.ParallelRuntime, color), semanticBool(report.Automatic.SharedKnowledgeBootstrap, color), report.Automatic.CodexModelRouting, report.Automatic.CodexEffortControl, report.Automatic.ClaudeModelRouting, report.Automatic.ClaudeEffortControl)
	for _, provider := range []string{"codex", "claude"} {
		probe := report.Automatic.Quota[provider]
		if provider == "claude" {
			fmt.Fprintf(a.Out, "  Claude Code probe=%s auth=%s eligible=%s 5h-source=%s weekly-source=%s\n", semanticBool(probe.Ready, color), semanticOptionalBool(probe.Authenticated, color), semanticOptionalBool(probe.Eligible, color), probe.FiveHourSource, probe.WeeklySource)
			continue
		}
		fmt.Fprintf(a.Out, "  Codex probe=%s auth=%s eligible=%s weekly-source=%s monthly-source=%s\n", semanticBool(probe.Ready, color), semanticOptionalBool(probe.Authenticated, color), semanticOptionalBool(probe.Eligible, color), probe.WeeklySource, probe.MonthlySource)
	}
	fmt.Fprintf(a.Out, "  failover Codex->Claude Code=%s Claude Code->Codex=%s\n", semanticOptionalBool(report.Automatic.CodexToClaude, color), semanticOptionalBool(report.Automatic.ClaudeToCodex, color))
	overall := terminalui.Success(report.Overall, color)
	if report.Overall != "READY" {
		overall = terminalui.Warning(report.Overall, color)
	}
	fmt.Fprintf(a.Out, "Server: configured=%s reachable=%s TLS=%s protocol-compatible=%s\nOverall: %s\n", semanticOptionalBool(report.Server.Configured, color), semanticOptionalBool(report.Server.Reachable, color), semanticOptionalBool(report.Server.TLS, color), semanticOptionalBool(report.Server.ProtocolCompatible, color), overall)
	for _, issue := range report.Issues {
		fmt.Fprintf(a.Out, "%s %s\n", terminalui.Warning("-", color), issue)
	}
	return nil
}
func semanticBool(value, color bool) string {
	if value {
		return terminalui.Success("true", color)
	}
	return terminalui.Failure("false", color)
}
func semanticOptionalBool(value, color bool) string {
	if value {
		return terminalui.Success("true", color)
	}
	return terminalui.Warning("false", color)
}
func semanticDisabledIsSafe(value, color bool) string {
	if value {
		return terminalui.Warning("true", color)
	}
	return terminalui.Success("false", color)
}
func semanticOK(value, color bool) string {
	if value {
		return terminalui.Success("OK", color)
	}
	return terminalui.Failure("FAIL", color)
}
func okFail(v bool) string {
	if v {
		return "OK"
	}
	return "FAIL"
}
func configured(v bool) string {
	if v {
		return "connected"
	}
	return "not connected"
}

func runConnect(ctx context.Context, a *app.App, args []string) error {
	if len(args) == 0 || args[0] == "list" {
		return connectionList(a)
	}
	switch args[0] {
	case "chatgpt", "claude":
		return a.ConnectAgent(ctx, args[0])
	case "server":
		return connectServer(ctx, a, args[1:])
	case "mcp":
		return runMCP(a, args[1:])
	default:
		return fmt.Errorf("unsupported connection %q", args[0])
	}
}
func connectionList(a *app.App) error { return a.Status(context.Background()) }

func connectServer(ctx context.Context, a *app.App, args []string) error {
	fs := flag.NewFlagSet("connect server", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	serverURL := fs.String("url", "", "server base URL")
	code := fs.String("enrollment-code", "", "one-time enrollment code")
	codeStdin := fs.Bool("code-stdin", false, "read enrollment code from stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var err error
	if *serverURL == "" {
		*serverURL, err = a.Prompt("Server URL: ", false)
		if err != nil {
			return err
		}
	}
	if *codeStdin {
		b, readErr := io.ReadAll(io.LimitReader(a.In, 4096))
		if readErr != nil {
			return readErr
		}
		*code = strings.TrimSpace(string(b))
	} else if *code == "" {
		*code, err = a.Prompt("Enrollment code: ", true)
		if err != nil {
			return err
		}
	}
	return runProgress(ctx, a, "Connecting ivoai server", func() error { return a.ConnectServer(ctx, *serverURL, *code) })
}

func runDisconnect(ctx context.Context, a *app.App, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: ivoai disconnect <chatgpt|claude|server>")
	}
	switch args[0] {
	case "chatgpt", "claude":
		return a.DisconnectAgent(ctx, args[0])
	case "server":
		return a.DisconnectServer(ctx)
	default:
		return fmt.Errorf("unsupported connection %q", args[0])
	}
}
func runMCP(a *app.App, args []string) error {
	if len(args) == 0 || args[0] == "list" {
		return a.MCPList()
	}
	switch args[0] {
	case "add":
		if len(args) != 3 {
			return errors.New("usage: ivoai connect mcp add <name> <https-url>")
		}
		return a.MCPAdd(args[1], args[2])
	case "remove":
		if len(args) != 2 {
			return errors.New("usage: ivoai connect mcp remove <name>")
		}
		return a.MCPRemove(args[1])
	default:
		return fmt.Errorf("unknown MCP action %q", args[0])
	}
}
func runMemory(ctx context.Context, a *app.App, args []string) error {
	if len(args) == 0 || args[0] == "status" {
		return a.MemoryStatus(ctx)
	}
	if args[0] == "configure" {
		return a.ReconfigureMemory(ctx)
	}
	return fmt.Errorf("unknown memory action %q", args[0])
}
func runConfig(a *app.App, args []string) error {
	if len(args) == 0 || args[0] == "show" {
		return a.ConfigShow()
	}
	if args[0] == "set" && len(args) == 3 {
		return a.ConfigSet(args[1], args[2])
	}
	return errors.New("usage: ivoai config [show|set <key> <value>]")
}
func runProject(a *app.App, args []string) error {
	if len(args) == 0 || args[0] == "status" {
		a.ProjectStatus()
		return nil
	}
	if args[0] == "init" {
		return a.ProjectInit()
	}
	return fmt.Errorf("unknown project action %q", args[0])
}
func runServer(ctx context.Context, args []string, a *app.App) error {
	if serverRunner == nil {
		return errors.New("server support is not linked into this build")
	}
	return serverRunner(ctx, args, a.In, a.Out, a.Err)
}
func trimDoubleDash(args []string) []string {
	if len(args) > 0 && args[0] == "--" {
		return args[1:]
	}
	return args
}

func usage(w io.Writer) {
	fmt.Fprintln(w, `ivoai - personal AI client and server platform

Usage:
  ivoai                         interactive menu
  ivoai setup [--mode client]
  ivoai status | doctor [--json] [--inventory] | version | update [--dry-run|--rollback [--force]] | uninstall
  ivoai connect [list|chatgpt|claude|server|mcp ...]
  ivoai disconnect <chatgpt|claude|server>
  ivoai codex [-- agent arguments...]
  ivoai claude [-- agent arguments...]
  ivoai auto [--planner codex|claude] [-- agent arguments...]
  ivoai session start --executor <codex|claude> --mode <direct|orchestrated> [-- agent arguments...]
  ivoai session list [--json] | show [--json] <id> | stop <id>
  ivoai monitor [--watch] [--session <id>] [--json]
  ivoai memory [status|configure]
  ivoai config [show|set <key> <value>]
  ivoai project [init|status]
  ivoai server ...`)
}

func UserError(err error) string { return platform.Redact(err.Error()) }
