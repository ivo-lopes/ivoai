package opencodebridge

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/routing"
)

func golden(t *testing.T, name string, value any) {
	t.Helper()
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, '\n')
	path := filepath.Join("testdata", "presentation", name+".golden.json")
	if os.Getenv("UPDATE_GOLDENS") == "1" {
		if os.Getenv("CI") != "" {
			t.Fatal("CI cannot update goldens")
		}
		if err = os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, expected) {
		t.Fatalf("%s changed; review before UPDATE_GOLDENS=1\n%s", name, body)
	}
}

func TestManagedModelVariantAndErrorGoldens(t *testing.T) {
	registry := routing.Registry{Providers: map[string]routing.ProviderCapability{}}
	for _, executor := range []string{"codex", "claude", "opencode"} {
		model := "fixture-model"
		if executor == "opencode" {
			model = "native-fixture/fixture-model"
		}
		registry.Providers[executor] = routing.ProviderCapability{Provider: executor, Authenticated: true, Models: []routing.ModelCapability{{Name: model, DisplayName: model, SupportedEfforts: []string{"low", "high"}, Source: routing.SourceRuntimeVerified}}}
	}
	catalog := CatalogFromRegistry(registry)
	golden(t, "model-picker", catalog.Entries())
	golden(t, "variant-picker", catalog.OpenCodeModels())
	response := httptest.NewRecorder()
	writeOpenAIErrorCode(response, 502, "IVOAI executor failed", "executor_stream_incomplete")
	var value any
	if json.Unmarshal(response.Body.Bytes(), &value) != nil {
		t.Fatal("invalid error response")
	}
	golden(t, "sanitized-error", value)
	for _, entry := range catalog.Entries() {
		if entry.Mode == "explicit" {
			selected, ok := catalog.Resolve(entry.ID, "high")
			if !ok || selected.Executor != entry.Executor || selected.Model != entry.UpstreamModel || selected.Effort != "high" {
				t.Fatal("picker changed labels only")
			}
		}
	}
}

func TestNativeProviderMetadataProjectionDiscardsSensitiveFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"all":[{"id":"fixture","name":"Fixture","key":"MUST_NOT_ESCAPE","options":{"apiKey":"MUST_NOT_ESCAPE"},"models":{"fixture":{"id":"fixture","capabilities":{"toolcall":true},"headers":{"Authorization":"MUST_NOT_ESCAPE"},"variants":{"high":{"apiKey":"MUST_NOT_ESCAPE"}}}}}],"connected":["fixture"],"default":{"fixture":"fixture"}}`))
	}))
	defer server.Close()
	managed := &Managed{URL: server.URL}
	body, err := managed.APIRequest(context.Background(), "providers", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "MUST_NOT_ESCAPE") || strings.Contains(string(body), "Authorization") || strings.Contains(string(body), "apiKey") {
		t.Fatal("upstream credentials escaped the projection")
	}
	var catalog NativeCatalog
	if json.Unmarshal(body, &catalog) != nil || !catalog.All[0].Models["fixture"].Capabilities.ToolCall {
		t.Fatal("capability metadata was lost")
	}
}

func TestGoldenCatalogContainsNoCredentialFields(t *testing.T) {
	body, _ := json.Marshal(DefaultCatalog().Entries())
	for _, forbidden := range []string{"access_token", "refresh_token", "auth.json", "Authorization"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatal("credential field in catalog")
		}
	}
}
