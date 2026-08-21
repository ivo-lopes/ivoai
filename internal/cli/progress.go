package cli

import (
	"context"

	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/terminalui"
)

func runProgress(ctx context.Context, a *app.App, label string, operation func() error) error {
	progress := &terminalui.Progress{Out: a.Err, ShowHeader: true}
	if progress.Animated() {
		originalOut, originalErr := a.Out, a.Err
		a.Out, a.Err = progress.Writer(originalOut), progress.Writer(originalErr)
		defer func() { a.Out, a.Err = originalOut, originalErr }()
	}
	return progress.Run(ctx, label, func(context.Context) error { return operation() })
}

func commandProgress(args []string) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	switch args[0] {
	case "setup":
		if (contains(args, "--mode") && contains(args, "server")) || contains(args, "--mode=server") {
			return "Setting up ivoai server", true
		}
		return "Setting up ivoai client", true
	case "doctor":
		return "Running diagnostics", !contains(args, "--json")
	case "update":
		if contains(args, "--rollback") {
			return "Rolling back ivoai", true
		}
		return "Updating ivoai", true
	case "uninstall":
		return "Uninstalling ivoai-managed files", true
	case "memory":
		return "Reconfiguring ai-memory", len(args) > 1 && args[1] == "configure"
	case "server":
		if len(args) < 2 {
			return "", false
		}
		switch args[1] {
		case "setup":
			return "Setting up ivoai server", true
		case "doctor":
			return "Running server diagnostics", true
		case "start":
			return "Starting server services", true
		case "stop":
			return "Stopping server services", true
		case "restart":
			return "Restarting server services", true
		case "backup":
			return "Creating server backup", true
		case "restore":
			return "Restoring server backup", true
		case "connector":
			if len(args) > 2 && (args[2] == "add" || args[2] == "remove") {
				if args[2] == "add" {
					return "Adding server connector", true
				}
				return "Removing server connector", true
			}
		case "enrollment":
			if len(args) > 2 && (args[2] == "create" || args[2] == "revoke") {
				return "Updating server enrollment", true
			}
		case "web-access":
			if len(args) > 2 && (args[2] == "create" || args[2] == "revoke") {
				return "Updating Web MCP access", true
			}
		case "gateway":
			if len(args) > 2 && args[2] == "configure" {
				return "Configuring server gateway", true
			}
		case "remote":
			return "Contacting remote server", true
		}
	}
	return "", false
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
