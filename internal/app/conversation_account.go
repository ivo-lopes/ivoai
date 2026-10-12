package app

import (
	"errors"

	"github.com/ivo-lopes/ivoai/internal/quota"
)

func conversationAccountReference(probe func() (quota.ProviderQuota, error), reference func() (string, error)) (string, error) {
	observed, err := probe()
	// Synthetic runners have no provider account. Keep their existing admission
	// contract; production always supplies an official identity lookup below.
	if reference == nil {
		if err != nil || !observed.Authenticated {
			return "", errors.New("executor authentication unavailable")
		}
		return "", nil
	}
	// Usage telemetry can fail while account/read still proves the current
	// identity. Refresh telemetry above, but do not equate that error with an
	// authentication failure. Routing/quota enforcement remains independent.
	identity, err := reference()
	if err != nil || identity == "" {
		return "", errors.New("executor authentication unavailable")
	}
	return identity, nil
}
