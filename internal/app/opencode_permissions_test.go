package app

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/quota"
)

func TestOpenCodePermissionModePersistsAndRejectsInvalid(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	out := &bytes.Buffer{}
	a, err := New("test", strings.NewReader(""), out, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	c, err := a.Store.Load()
	if err != nil || c.OpenCode.ResolvedPermissionMode() != "interactive" {
		t.Fatal("unsafe default", err)
	}
	for _, mode := range []string{"full", "interactive"} {
		if err := a.ConfigSet("opencode.permission_mode", mode); err != nil {
			t.Fatal(err)
		}
		b, err := New("test", strings.NewReader(""), out, &bytes.Buffer{})
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := b.Store.Load()
		if err != nil || loaded.OpenCode.PermissionMode != mode {
			t.Fatal("mode lost on restart", err)
		}
	}
	before, _ := os.ReadFile(a.Store.Paths.Config)
	if err := a.ConfigSet("opencode.permission_mode", "invalid"); err == nil {
		t.Fatal("invalid mode accepted")
	}
	after, _ := os.ReadFile(a.Store.Paths.Config)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid mode changed persisted config")
	}
	if !strings.Contains(out.String(), "next managed OpenCode session") {
		t.Fatal("missing restart notice")
	}
}

func TestAutoPassesPersistedPermissionModeToManagedBackend(t *testing.T) {
	for _, mode := range []string{"interactive", "full"} {
		t.Run(mode, func(t *testing.T) {
			a := autoTestApp(t, t.TempDir(), "#!/bin/sh\n", "#!/bin/sh\n")
			if err := a.ConfigSet("opencode.permission_mode", mode); err != nil {
				t.Fatal(err)
			}
			a.QuotaManager = &quota.Manager{Store: quota.Store{Root: a.Store.Paths.QuotaDir}, Probes: map[quota.Provider]quota.Probe{
				quota.ProviderCodex: probeFunc(func(context.Context) (quota.ProviderQuota, error) { return available(quota.ProviderCodex), nil }),
				quota.ProviderClaude: probeFunc(func(context.Context) (quota.ProviderQuota, error) {
					return quota.ProviderQuota{Provider: quota.ProviderClaude}, nil
				}),
			}}
			previous := a.StartOpenCodeManaged
			called := false
			a.StartOpenCodeManaged = func(ctx context.Context, options opencodebridge.ManagedOptions) (managedOpenCodeFrontend, error) {
				called = true
				if options.PermissionMode != mode {
					t.Fatal("permission mode lost at AUTO boundary")
				}
				return previous(ctx, options)
			}
			if err := a.Auto(context.Background(), "codex", nil); err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("managed frontend was bypassed")
			}
		})
	}
}
