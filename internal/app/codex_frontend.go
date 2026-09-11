package app

import (
	"context"
	"errors"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func (a *App) runCodexFrontend(ctx context.Context, bridge *opencodebridge.Bridge, id string) error {
	model, effort := bridge.InitialSelection()
	return bridge.RunTerminal(ctx, opencodebridge.TerminalOptions{SessionID: id, Model: model, Effort: effort, In: a.In, Out: a.Out})
}

// Invocation overrides resolve against the same runtime catalog as the picker.
func frontendSelection(catalog opencodebridge.ModelCatalog, provider string, args []string) (string, string, error) {
	model := "auto"
	effort := ""
	requested := session.ParseModelArgument(args)
	for i, arg := range args {
		setting := ""
		if (arg == "-c" || arg == "--config") && i+1 < len(args) {
			setting = args[i+1]
		}
		if strings.HasPrefix(arg, "--config=") {
			setting = strings.TrimPrefix(arg, "--config=")
		}
		if strings.HasPrefix(arg, "-c") && len(arg) > 2 {
			setting = strings.TrimPrefix(arg, "-c")
		}
		if strings.HasPrefix(setting, "model_reasoning_effort=") {
			effort = strings.Trim(strings.TrimPrefix(setting, "model_reasoning_effort="), `"'`)
		}
		if strings.HasPrefix(setting, "model=") {
			requested = strings.Trim(strings.TrimPrefix(setting, "model="), `"'`)
		}
		if strings.HasPrefix(arg, "-m") && len(arg) > 2 && !strings.HasPrefix(arg, "--") {
			requested = strings.TrimPrefix(arg, "-m")
		}
		if arg == "--effort" && i+1 < len(args) {
			effort = args[i+1]
		}
		if strings.HasPrefix(arg, "--effort=") {
			effort = strings.TrimPrefix(arg, "--effort=")
		}
	}
	if requested != "" {
		model = ""
		for _, entry := range catalog.Entries() {
			if entry.Executor == provider && (entry.UpstreamModel == requested || entry.ID == requested) {
				model = entry.ID
				break
			}
		}
		if model == "" {
			return "", "", errors.New("MODEL_UNAVAILABLE: explicit model absent from runtime catalog")
		}
	}
	if _, ok := catalog.Resolve(model, effort); !ok {
		return "", "", errors.New("REASONING_UNAVAILABLE: choose an explicit runtime model and supported effort")
	}
	return model, effort, nil
}
