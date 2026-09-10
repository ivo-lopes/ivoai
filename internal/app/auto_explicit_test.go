package app

import (
	"context"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/quota"
)

func TestAutoExplicitSubscriptionExecutorUnavailableFailsClosed(t *testing.T) {
	for _, requested := range []string{"codex", "claude"} {
		t.Run(requested, func(t *testing.T) {
			a := autoTestApp(t, t.TempDir(), "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
			a.QuotaManager = &quota.Manager{Store: quota.Store{Root: a.Store.Paths.QuotaDir}, Probes: map[quota.Provider]quota.Probe{}}
			for _, provider := range []quota.Provider{quota.ProviderCodex, quota.ProviderClaude} {
				a.QuotaManager.Probes[provider] = probeFunc(func(context.Context) (quota.ProviderQuota, error) {
					if string(provider) == requested {
						return exhausted(provider), nil
					}
					return available(provider), nil
				})
			}
			if err := a.Auto(context.Background(), requested, nil); err == nil || !strings.Contains(err.Error(), "explicit executor unavailable") {
				t.Fatalf("explicit provider substituted: %v", err)
			}
			values, _ := a.SessionList()
			for _, v := range values {
				if v.CurrentPrimary != requested || len(v.Workers) != 0 {
					t.Fatal("unauthorized executor dispatched")
				}
			}
		})
	}
}
