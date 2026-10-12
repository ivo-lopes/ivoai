package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ivo-lopes/ivoai/internal/agents"
	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/cli"
	"github.com/ivo-lopes/ivoai/internal/codeintel"
	"github.com/ivo-lopes/ivoai/internal/servercmd"
	"github.com/ivo-lopes/ivoai/internal/terminalui"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "_code-sandbox" {
		if err := codeintel.SandboxHelper(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "ivoai: code sandbox unavailable")
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cli.RegisterServerRunner(servercmd.New(version))
	a, err := app.New(version, os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		writeError(err)
		os.Exit(1)
	}
	if err := cli.Run(ctx, a, os.Args[1:]); err != nil {
		writeError(err)
		var exitErr *agents.ExitError
		if errors.As(err, &exitErr) && exitErr.Code > 0 {
			os.Exit(exitErr.Code)
		}
		if errors.Is(err, context.Canceled) {
			os.Exit(130)
		}
		os.Exit(1)
	}
}

func writeError(err error) {
	color := terminalui.ColorEnabled(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s %s\n", terminalui.Failure("ivoai:", color), cli.UserError(err))
}
