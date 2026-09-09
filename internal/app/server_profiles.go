package app

import (
	"github.com/ivo-lopes/ivoai/internal/connections"
	"github.com/ivo-lopes/ivoai/internal/secrets"
)

func (a *App) SetServerEnabled(alias string, enabled bool) error {
	connector := connections.ServerConnector{Store: a.Store, Secrets: secrets.Store{Path: a.Store.Paths.Secrets}}
	if err := connector.SetEnabled(alias, enabled); err != nil {
		return err
	}
	a.success("server setting saved; effective on the next managed session: " + alias)
	return nil
}

func (a *App) EditServerMetadata(alias string, metadata connections.ProfileMetadata) error {
	connector := connections.ServerConnector{Store: a.Store, Secrets: secrets.Store{Path: a.Store.Paths.Secrets}}
	if err := connector.EditMetadata(alias, metadata); err != nil {
		return err
	}
	a.success("server metadata saved; identity and credential preserved: " + alias)
	return nil
}
