package app

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/ivo-lopes/ivoai/internal/agents"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
)

var errClaudeNativeManagedFrontend = errors.New("CLAUDE_NATIVE_MANAGED_FRONTEND_UNAVAILABLE: this native managed handoff/planner has no verified Claude frontend; no OpenCode renderer or headless session was started; explicitly choose a supported native session or a deprecated legacy ivoai opencode command")

func (a *App) scopedQuotaManager(cfg config.Config, state config.State, legacy bool) *quota.Manager {
	manager := a.automaticQuotaManager(cfg, state)
	if legacy {
		return manager
	}
	return manager.RestrictedTo(quota.ProviderCodex, quota.ProviderClaude)
}

func (a *App) workerProviders(ctx context.Context, cfg config.Config, state config.State, value session.Session, runtimeDir string) (*agents.NativeOpenCode, *quota.Manager, routing.Registry) {
	legacy := !session.NativeProvidersOnly(value)
	manager := a.scopedQuotaManager(cfg, state, legacy)
	var native *agents.NativeOpenCode
	if legacy {
		native = a.nativeOpenCode(cfg, state, value.WorkingDirectory, filepath.Join(runtimeDir, "native-worker"), nil, true)
	}
	if native != nil {
		manager.Probes[quota.ProviderOpenCode] = native
	}
	registry := (routing.Discoverer{CodexPath: state.Components["codex"].Path, ClaudePath: state.Components["claude-code"].Path, CachePath: filepath.Join(a.Store.Paths.CacheDir, "capabilities.json")}).Discover(ctx)
	if nativeCapabilityAvailable(ctx, native) {
		registry.Providers["opencode"] = native.Capability()
	}
	if !legacy {
		registry = nativeProviderRegistry(registry)
	}
	return native, manager, registry
}

func nativeProviderRegistry(registry routing.Registry) routing.Registry {
	result := routing.Registry{Providers: map[string]routing.ProviderCapability{}}
	for _, provider := range []string{"codex", "claude"} {
		if capability, ok := registry.Providers[provider]; ok {
			result.Providers[provider] = capability
		}
	}
	return result
}
