package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

type followupPrompts []string

func (v *followupPrompts) String() string     { return "" }
func (v *followupPrompts) Set(s string) error { *v = append(*v, s); return nil }

func runAntigravity(ctx context.Context, a *app.App, args []string) error {
	if len(args) == 0 || args[0] != "run" {
		return errors.New("usage: ivoai antigravity run --prompt TEXT [--directory PATH] [--model ID] [--effort LEVEL] [--follow-up TEXT]; experimental official ACP, no native TUI interception")
	}
	fs := flag.NewFlagSet("antigravity run", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	options := app.NativeRunOptions{}
	fs.StringVar(&options.Prompt, "prompt", "", "natural-language task (required)")
	fs.StringVar(&options.Directory, "directory", "", "isolated repository/worktree directory")
	fs.StringVar(&options.Model, "model", "", "exact model ID, validated against the official ACP catalog")
	fs.StringVar(&options.Effort, "effort", "", "official model effort suffix")
	var followups followupPrompts
	fs.Var(&followups, "follow-up", "related turn in the same managed session (repeatable, maximum 8)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("antigravity run accepts flags only")
	}
	options.Followups = []string(followups)
	options.Approve = func(approvalCtx context.Context, p opencodebridge.PermissionView) (bool, error) {
		file, ok := a.In.(*os.File)
		if !ok || !term.IsTerminal(int(file.Fd())) {
			fmt.Fprintln(a.Err, "Core approval denied: interactive terminal required for", p.ID)
			return false, nil
		}
		fmt.Fprintf(a.Err, "%s\nApprove exact decision %s? Type yes: ", p.Description, p.ID)
		answer := []byte{}
		for len(answer) < 16 {
			if approvalCtx.Err() != nil {
				return false, approvalCtx.Err()
			}
			poll := []unix.PollFd{{Fd: int32(file.Fd()), Events: unix.POLLIN}}
			if _, err := unix.Poll(poll, 100); err != nil {
				if errors.Is(err, unix.EINTR) {
					continue
				}
				return false, err
			}
			if poll[0].Revents&unix.POLLIN == 0 {
				continue
			}
			var one [1]byte
			n, err := unix.Read(int(file.Fd()), one[:])
			if err != nil {
				return false, err
			}
			if n == 0 {
				return false, nil
			}
			if one[0] == '\n' {
				return strings.TrimSpace(string(answer)) == "yes", nil
			}
			answer = append(answer, one[0])
		}
		return false, nil
	}
	return a.AntigravityRun(ctx, options)
}

func runNativeMCPProxy(ctx context.Context, a *app.App, args []string) error {
	fs := flag.NewFlagSet("_native-mcp-proxy", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	id := fs.String("session", "", "owned Core session")
	turn := fs.String("turn", "", "exact admitted receipt")
	server := fs.String("server", "", "host-projected MCP source")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("NATIVE_MCP_PROJECTION_INVALID")
	}
	return a.NativeMCPProxy(ctx, *id, *turn, *server)
}
