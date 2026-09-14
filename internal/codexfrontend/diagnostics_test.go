package codexfrontend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/routing"
)

func TestStartupDiagnosticsBoundedAndNoRawDisclosure(t *testing.T) {
	tail := &startupTail{}
	secret := "opaque-startup-canary"
	_, _ = tail.Write([]byte(strings.Repeat(secret, 1000) + " error loading config " + secret))
	if len(tail.value) > 4096 {
		t.Fatal("unbounded startup diagnostic")
	}
	f := &Facade{ctx: context.Background()}
	f.recordProcessExit(127, time.Now(), tail)
	if f.TurnError() == nil {
		t.Fatal("missing causal startup error")
	}
	message := f.TurnError().Error()
	if strings.Contains(message, secret) || !strings.Contains(message, "exit_code=127") || !strings.Contains(message, "reason=configuration_load_failed") {
		t.Fatal("unsafe or missing operational diagnostic")
	}
	tail.stop()
	_, _ = tail.Write([]byte("raw prompt must never remain after initialize"))
	if len(tail.value) != 0 {
		t.Fatal("startup capture persisted past initialization")
	}
}

func TestExitedAppServerHasCausalSafeDiagnostic(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "codex")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho 'error loading config secret-startup-canary' >&2\nexit 78\n"), 0700); err != nil {
		t.Fatal(err)
	}
	catalog := opencodebridge.CatalogFromRegistry(routing.Registry{Providers: map[string]routing.ProviderCapability{"codex": {Provider: "codex", Authenticated: true, Models: []routing.ModelCapability{{Name: "fixture-strong", CapabilityTier: routing.TierStrong, SupportedEfforts: []string{"high"}, DefaultEffort: "high", Source: routing.SourceRuntimeVerified}}}}})
	bridge, err := opencodebridge.Start(opencodebridge.Options{Frontend: "codex", Catalog: catalog, Runner: &fixtureRunner{}, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() opencodebridge.Status { return opencodebridge.Status{Frontend: "codex"} }})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	f, err := Start(context.Background(), Options{Binary: binary, Directory: root, RuntimeDir: root, SessionID: "startup-fixture", Bridge: bridge, Environment: []string{"HOME=" + root, "PATH=/usr/bin:/bin"}})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	select {
	case <-f.ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("child exit not observed")
	}
	if f.TurnError() == nil {
		t.Fatal("child exit lost behind transport reset")
	}
	message := f.TurnError().Error()
	if !strings.Contains(message, "exit_code=78") || strings.Contains(message, "secret-startup-canary") {
		t.Fatal("missing or unsafe child diagnostic")
	}
}

func TestStartupDiagnosticsDoesNotReclassifyNormalCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &Facade{ctx: ctx}
	f.recordProcessExit(-1, time.Now(), &startupTail{})
	if f.TurnError() != nil {
		t.Fatal("normal cancellation classified as startup failure")
	}
}
