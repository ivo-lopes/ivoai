package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivo-lopes/ivoai/internal/components"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func (a *App) UpdateCodex(ctx context.Context, rollback bool) error {
	sessions, err := a.SessionList()
	if err != nil {
		return err
	}
	for _, value := range sessions {
		if value.Active() && (session.ProcessMatches(value.PrimaryPID, value.PrimaryProcessStart) || session.ProcessMatches(value.FrontendPID, value.FrontendProcessStart)) {
			return errors.New("CODEX_SESSION_ACTIVE: finish active IVOAI sessions before updating or rolling back Codex")
		}
	}
	i := components.Installer{Runner: a.Runner, Store: a.Store, Out: a.Out, Client: a.HTTPClient}
	if err := i.UpdateCodex(ctx, rollback); err != nil {
		return err
	}
	fmt.Fprintln(a.Out, "Codex managed installation updated; existing user/system clients and authentication preserved.")
	return nil
}
