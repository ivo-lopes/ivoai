package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivo-lopes/ivoai/internal/app"
)

func runSkills(ctx context.Context, a *app.App, args []string) error {
	if len(args) == 0 {
		return a.PrintNativeCapabilities(ctx, "")
	}
	id := ""
	if len(args) > 2 {
		return errors.New("usage: ivoai skills <list|show|doctor|update|enable|disable|pin|unpin|rollback> [source-id]")
	}
	if len(args) == 2 {
		id = args[1]
	}
	switch args[0] {
	case "list", "show":
		return a.PrintNativeCapabilities(ctx, id)
	case "doctor":
		rows, err := a.NativeCapabilities(ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.Status == "integrity-failure" || row.Status == "quarantined" {
				return errors.New("native capability integrity failure: " + row.ID)
			}
		}
		return a.PrintNativeCapabilities(ctx, id)
	case "update", "enable", "disable", "pin", "unpin", "rollback":
		if err := a.NativeCapabilityAction(ctx, args[0], id); err != nil {
			return err
		}
		fmt.Fprintln(a.Out, "Native capability policy/update completed; effective for future workers.")
		return nil
	default:
		return errors.New("unknown skills command")
	}
}
