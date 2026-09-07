package routing

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeCatalogUsesPerModelCapabilities(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "claude")
	body := `#!/bin/sh
read -r request
printf '%s\n' '{"type":"control_response","response":{"subtype":"success","request_id":"ivoai_catalog","response":{"models":[{"value":"default","resolvedModel":"model-a","displayName":"Default","supportsEffort":true,"supportedEffortLevels":["low","high"]},{"value":"alias-a","resolvedModel":"model-a","displayName":"Alias"},{"value":"model-b","displayName":"B","supportsEffort":false,"supportedEffortLevels":["max"]}]}}}'
`
	if err := os.WriteFile(path, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	models, err := claudeModels(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].Name != "model-a" || len(models[0].SupportedEfforts) != 2 || len(models[1].SupportedEfforts) != 0 {
		t.Fatalf("invented or duplicate model capability: %+v", models)
	}
}

func TestDiscoveryNeverReusesStaleAuthenticatedCatalog(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "capabilities.json")
	body, _ := json.Marshal(capabilityCache{Providers: map[string]ProviderCapability{"codex": {Provider: "codex", Authenticated: true, Version: "codex-cli 0.153.4", Models: []ModelCapability{{Name: "stale-model"}}}}})
	if err := os.WriteFile(cache, body, 0600); err != nil {
		t.Fatal(err)
	}
	got := (Discoverer{CodexPath: "/missing/codex", CachePath: cache}).Discover(context.Background())
	if len(got.Providers) != 0 {
		t.Fatalf("stale cache appeared available: %+v", got)
	}
}

func TestCodexThreadReadReportsConfigurationWithoutEchoingRequest(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "codex")
	body := `#!/bin/sh
read -r request
printf '%s\n' '{"id":1,"result":{}}'
read -r request
read -r request
printf '%s\n' '{"id":2,"result":{"thread":{"model":"reported-model","reasoningEffort":"high"}}}'
`
	if err := os.WriteFile(path, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	model, effort, err := CodexThreadConfiguration(context.Background(), path, "thread-fixture", nil)
	if err != nil || model != "reported-model" || effort != "high" {
		t.Fatalf("%q %q %v", model, effort, err)
	}
}
