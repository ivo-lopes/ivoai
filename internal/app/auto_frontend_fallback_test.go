package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/quota"
)

func TestManagedFrontendFailureRefusesUngatedDirectFallback(t *testing.T) {
	for _, cause := range []string{"backend exited before publishing listener", "readiness timeout", "version mismatch", "another managed OpenCode frontend is already active"} {
		t.Run(cause, func(t *testing.T) {
			a := autoTestApp(t, t.TempDir(), "#!/bin/sh\nexit 0\n", "#!/bin/sh\nexit 0\n")
			a.QuotaManager = &quota.Manager{Store: quota.Store{Root: a.Store.Paths.QuotaDir}, Probes: map[quota.Provider]quota.Probe{quota.ProviderCodex: probeFunc(func(context.Context) (quota.ProviderQuota, error) { return available(quota.ProviderCodex), nil })}}
			a.StartOpenCodeManaged = func(context.Context, opencodebridge.ManagedOptions) (managedOpenCodeFrontend, error) {
				return nil, errors.New(cause)
			}
			var diagnostic bytes.Buffer
			a.Err = &diagnostic
			err := a.Auto(context.Background(), "codex", nil)
			if err == nil || !strings.Contains(err.Error(), "direct fallback refused") {
				t.Fatal("orchestrated admission bypassed")
			}
			if !strings.Contains(diagnostic.String(), "ORCHESTRATION_STATE=DEGRADED") || !strings.Contains(diagnostic.String(), "No direct session") {
				t.Fatal("safe failure not visible")
			}
			values, err := a.SessionList()
			if err != nil || len(values) != 1 || values[0].Frontend != "opencode" || values[0].PrimaryPID != 0 {
				t.Fatal("unexpected frontend or executor started")
			}
		})
	}
}
