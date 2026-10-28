package orchestrator

import (
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func (s *Server) providerNames() []string {
	if s.Quota == nil {
		return quota.ProviderNames()
	}
	result := []string{}
	for _, provider := range s.Quota.Providers() {
		result = append(result, string(provider))
	}
	return result
}

func (s *Server) providerAllowed(provider string) error {
	if provider == "" || s.Quota == nil || s.Quota.Allows(quota.Provider(provider)) {
		return nil
	}
	if provider == "opencode" {
		return session.ErrOpenCodeManagedDeprecated
	}
	return quota.ErrProviderNotAllowed
}
