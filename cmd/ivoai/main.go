package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/ivo-lopes/ivoai/internal/agents"
	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/cli"
	"github.com/ivo-lopes/ivoai/internal/servercmd"
)

var version = "dev"

func main() {
	cli.RegisterServerRunner(servercmd.New(version))
	a, err := app.New(version, os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ivoai:", cli.UserError(err))
		os.Exit(1)
	}
	if err := cli.Run(context.Background(), a, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ivoai:", cli.UserError(err))
		var exitErr *agents.ExitError
		if errors.As(err, &exitErr) && exitErr.Code > 0 {
			os.Exit(exitErr.Code)
		}
		os.Exit(1)
	}
}
