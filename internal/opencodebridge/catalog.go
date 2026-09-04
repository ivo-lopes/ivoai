package opencodebridge

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/routing"
)

// ModelSpec is an immutable, server-side mapping from an opaque OpenCode model
// ID to an official IVOAI executor selection. HTTP input is never parsed into
// executor arguments.
type ModelSpec struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Mode             string   `json:"mode"`
	Executor         string   `json:"executor,omitempty"`
	UpstreamModel    string   `json:"upstream_model,omitempty"`
	SupportedEfforts []string `json:"supported_efforts,omitempty"`
	DefaultEffort    string   `json:"default_effort,omitempty"`
	ModelSource      string   `json:"model_source,omitempty"`
}

type ModelCatalog struct {
	entries  []ModelSpec
	byID     map[string]ModelSpec
	revision string
}

type Selection struct {
	Mode            string `json:"mode"`
	RequestedID     string `json:"requested_id"`
	Executor        string `json:"executor,omitempty"`
	Model           string `json:"model,omitempty"`
	Effort          string `json:"effort,omitempty"`
	ModelSource     string `json:"model_source,omitempty"`
	EffortSource    string `json:"effort_source,omitempty"`
	CatalogRevision string `json:"catalog_revision"`
}

func CatalogFromRegistry(registry routing.Registry) ModelCatalog {
	entries := []ModelSpec{{ID: "auto", Name: "IVOAI Automatic Orchestration", Mode: "auto", ModelSource: "scheduler"}}
	providers := make([]string, 0, len(registry.Providers))
	for provider := range registry.Providers {
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	for _, provider := range providers {
		capability := registry.Providers[provider]
		if provider != "codex" && provider != "claude" || !capability.Authenticated {
			continue
		}
		for _, model := range capability.Models {
			upstream := strings.TrimSpace(model.Name)
			if provider == "codex" && upstream == "" {
				continue
			}
			idValue := upstream
			name := upstream
			if idValue == "" {
				idValue, name = "client-default", "client default"
			}
			id := provider + "-" + base64.RawURLEncoding.EncodeToString([]byte(idValue))
			efforts := normalizedEfforts(model.SupportedEfforts)
			defaultEffort := strings.TrimSpace(model.DefaultEffort)
			if !contains(efforts, defaultEffort) {
				defaultEffort = ""
			}
			entries = append(entries, ModelSpec{
				ID: id, Name: displayExecutor(provider) + " · " + name, Mode: "explicit", Executor: provider,
				UpstreamModel: upstream, SupportedEfforts: efforts, DefaultEffort: defaultEffort, ModelSource: string(model.Source),
			})
		}
	}
	return newCatalog(entries)
}

func newCatalog(entries []ModelSpec) ModelCatalog {
	if len(entries) == 0 || entries[0].ID != "auto" {
		entries = append([]ModelSpec{{ID: "auto", Name: "IVOAI Automatic Orchestration", Mode: "auto", ModelSource: "scheduler"}}, entries...)
	}
	byID := make(map[string]ModelSpec, len(entries))
	clean := make([]ModelSpec, 0, len(entries))
	for _, entry := range entries {
		if !safeCatalogID(entry.ID) || byID[entry.ID].ID != "" || !safeDisplayText(entry.Name) || entry.UpstreamModel != "" && !safeDisplayText(entry.UpstreamModel) {
			continue
		}
		entry.SupportedEfforts = normalizedEfforts(entry.SupportedEfforts)
		byID[entry.ID] = entry
		clean = append(clean, entry)
	}
	body, _ := json.Marshal(clean)
	digest := sha256.Sum256(body)
	return ModelCatalog{entries: clean, byID: byID, revision: hex.EncodeToString(digest[:8])}
}

func safeDisplayText(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, char := range value {
		if char < 0x20 || char == 0x7f || char >= 0x80 && char <= 0x9f || char >= 0x202a && char <= 0x202e || char >= 0x2066 && char <= 0x2069 {
			return false
		}
	}
	return true
}

func DefaultCatalog() ModelCatalog { return newCatalog(nil) }

func (c ModelCatalog) Entries() []ModelSpec {
	result := append([]ModelSpec(nil), c.entries...)
	for index := range result {
		result[index].SupportedEfforts = append([]string(nil), result[index].SupportedEfforts...)
	}
	return result
}
func (c ModelCatalog) Revision() string { return c.revision }

func (c ModelCatalog) Resolve(id, effort string) (Selection, bool) {
	entry, ok := c.byID[strings.TrimSpace(id)]
	if !ok {
		return Selection{}, false
	}
	effort = strings.TrimSpace(effort)
	if entry.Mode == "auto" {
		if effort != "" {
			return Selection{}, false
		}
		return Selection{Mode: "auto", RequestedID: entry.ID, ModelSource: entry.ModelSource, EffortSource: "scheduler", CatalogRevision: c.revision}, true
	}
	effortSource := "argument"
	if effort == "" {
		effort = entry.DefaultEffort
		effortSource = "default"
	}
	if effort != "" && !contains(entry.SupportedEfforts, effort) {
		return Selection{}, false
	}
	if effort == "" {
		effortSource = "unsupported"
	}
	return Selection{Mode: "explicit", RequestedID: entry.ID, Executor: entry.Executor, Model: entry.UpstreamModel, Effort: effort, ModelSource: entry.ModelSource, EffortSource: effortSource, CatalogRevision: c.revision}, true
}

func (c ModelCatalog) OpenCodeModels() map[string]any {
	models := make(map[string]any, len(c.entries))
	for _, entry := range c.entries {
		value := map[string]any{"name": entry.Name}
		if len(entry.SupportedEfforts) > 0 {
			value["reasoning"] = true
			variants := map[string]any{}
			for _, effort := range allEfforts {
				if contains(entry.SupportedEfforts, effort) {
					variants[effort] = map[string]any{"reasoningEffort": effort}
				} else {
					variants[effort] = map[string]any{"disabled": true}
				}
			}
			value["variants"] = variants
		}
		models[entry.ID] = value
	}
	return models
}

var allEfforts = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}

func normalizedEfforts(values []string) []string {
	result := []string{}
	for _, allowed := range allEfforts {
		if contains(values, allowed) {
			result = append(result, allowed)
		}
	}
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func safeCatalogID(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, r := range value {
		if r != '-' && r != '_' && r != '.' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func displayExecutor(value string) string {
	if value == "codex" {
		return "Codex"
	}
	return "Claude"
}
