package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/codexfrontend"
	"github.com/ivo-lopes/ivoai/internal/quota"
)

func TestManagedFrontendFailureRefusesUngatedDirectFallback(t *testing.T) {
	for _, cause := range []string{"backend exited before publishing listener", "readiness timeout", "version mismatch", "native frontend protocol refused"} {
		t.Run(cause, func(t *testing.T) {
			a := autoTestApp(t, t.TempDir(), "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
			a.QuotaManager = &quota.Manager{Store: quota.Store{Root: a.Store.Paths.QuotaDir}, Probes: map[quota.Provider]quota.Probe{quota.ProviderCodex: probeFunc(func(context.Context) (quota.ProviderQuota, error) { return available(quota.ProviderCodex), nil })}}
			a.StartCodexNative = func(context.Context, codexfrontend.Options) (nativeCodexFrontend, error) {
				return nil, errors.New(cause)
			}
			err := a.Auto(context.Background(), "codex", nil)
			if err == nil || !strings.Contains(err.Error(), cause) {
				t.Fatal("orchestrated admission bypassed")
			}

			values, err := a.SessionList()
			if err != nil || len(values) != 1 || values[0].Frontend != "codex" || len(values[0].TurnAttempts) != 0 || len(values[0].Workers) != 0 {
				t.Fatal("unexpected frontend or executor started")
			}
		})
	}
}
