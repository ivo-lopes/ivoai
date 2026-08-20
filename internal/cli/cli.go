package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/platform"
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
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("update accepts no positional arguments")
		}
		if *rollback {
			return a.RollbackUpdate(ctx)
		}
		return a.Update(ctx)
	case "uninstall":
		return a.Uninstall(ctx)
	case "_register-install":
		return a.RegisterInstall()
	case "connect":
		return runConnect(ctx, a, args[1:])
	case "disconnect":
		return runDisconnect(ctx, a, args[1:])
	case "codex", "claude":
		return a.Launch(ctx, args[0], trimDoubleDash(args[1:]))
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

func runDoctor(ctx context.Context, a *app.App, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	jsonOutput := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
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
	fmt.Fprintf(a.Out, "ivoai doctor\nOS: %s\nArchitecture: %s\nivoai: %s\nConfig: %s\nState: %s\nSecret permissions: %s\n", report.OS, report.Architecture, report.Version, report.ConfigPath, report.StatePath, report.SecretPermissions)
	fmt.Fprintf(a.Out, "\nCodex: installed=%t version=%s authenticated=%t\n", report.Codex.Installed, report.Codex.Version, report.Codex.Authenticated)
	fmt.Fprintf(a.Out, "Claude: installed=%t version=%s authenticated=%t\n", report.Claude.Installed, report.Claude.Version, report.Claude.Authenticated)
	fmt.Fprintf(a.Out, "Headroom: installed=%t enabled=%t healthy=%t version=%s\nCodex via Headroom: %s\nClaude via Headroom: %s\n", report.Headroom.Installed, report.Headroom.Enabled, report.Headroom.Healthy, report.Headroom.Version, okFail(report.Headroom.CodexCompatible), okFail(report.Headroom.ClaudeCompatible))
	fmt.Fprintf(a.Out, "ai-memory: installed=%t version=%s hooks=%t server=%s\n", report.Memory.Installed, report.Memory.Version, report.Memory.Hooks, configured(report.Server.Configured))
	fmt.Fprintf(a.Out, "Ruflo: installed=%t version=%s safe-mode=%t provider-execution=%t\n", report.Ruflo.Installed, report.Ruflo.Version, report.Ruflo.SafeMode, report.Ruflo.ProviderExecution)
	fmt.Fprintf(a.Out, "Server: configured=%t reachable=%t TLS=%t protocol-compatible=%t\nOverall: %s\n", report.Server.Configured, report.Server.Reachable, report.Server.TLS, report.Server.ProtocolCompatible, report.Overall)
	for _, issue := range report.Issues {
		fmt.Fprintf(a.Out, "- %s\n", issue)
	}
	return nil
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
	return a.ConnectServer(ctx, *serverURL, *code)
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
	return errors.New("usage: ivoai config [show|set <key> <true|false>]")
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

func menu(ctx context.Context, a *app.App) error {
	reader := bufio.NewReader(a.In)
	for {
		fmt.Fprintln(a.Out, "\nivoai\n1) Status\n2) Setup\n3) Connections\n4) ChatGPT\n5) Claude\n6) Server\n7) Doctor\n8) Update\n9) Configuration\n10) Launch Codex\n11) Launch Claude\n0) Exit")
		fmt.Fprint(a.Out, "> ")
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		switch strings.TrimSpace(line) {
		case "1":
			menuResult(a, a.Status(ctx))
		case "2":
			menuResult(a, a.Setup(ctx))
		case "3":
			menuResult(a, connectionList(a))
		case "4":
			menuResult(a, a.ConnectAgent(ctx, "chatgpt"))
		case "5":
			menuResult(a, a.ConnectAgent(ctx, "claude"))
		case "6":
			fmt.Fprintln(a.Out, "Use: ivoai connect server")
		case "7":
			menuResult(a, runDoctor(ctx, a, nil))
		case "8":
			menuResult(a, a.Update(ctx))
		case "9":
			menuResult(a, a.ConfigShow())
		case "10":
			return a.Launch(ctx, "codex", nil)
		case "11":
			return a.Launch(ctx, "claude", nil)
		case "0", "", "q", "quit", "exit":
			return nil
		default:
			fmt.Fprintln(a.Err, "invalid selection")
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

func menuResult(a *app.App, err error) {
	if err != nil {
		fmt.Fprintf(a.Err, "Error: %s\n", UserError(err))
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, `ivoai - personal AI client and server platform

Usage:
  ivoai                         interactive menu
  ivoai setup [--mode client]
  ivoai status | doctor [--json] | version | update [--rollback] | uninstall
  ivoai connect [list|chatgpt|claude|server|mcp ...]
  ivoai disconnect <chatgpt|claude|server>
  ivoai codex [-- agent arguments...]
  ivoai claude [-- agent arguments...]
  ivoai memory [status|configure]
  ivoai config [show|set <key> <true|false>]
  ivoai project [init|status]
  ivoai server ...`)
}

func UserError(err error) string { return platform.Redact(err.Error()) }
