package routing

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/connections"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

func nativeAuth(ctx context.Context, path, provider string) bool {
	args := []string{"login", "status"}
	if provider == "claude" {
		args = []string{"auth", "status"}
	}
	result, err := (platform.ExecRunner{}).Run(ctx, path, args, platform.RunOptions{Timeout: 10 * time.Second, Env: []string{"DISABLE_AUTOUPDATER=1"}})
	return connections.AuthenticationStatus(result, err)
}

// Claude's official Agent SDK initialize response exposes model IDs, resolved
// wire names and per-model effort levels. No inference or credential access is
// performed by IVOAI; the official client owns account discovery.
func claudeModels(parent context.Context, binary string) ([]ModelCapability, error) {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--setting-sources", "")
	cmd.Env = append(os.Environ(), "DISABLE_AUTOUPDATER=1")
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() { _ = in.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	if _, err := io.WriteString(in, `{"type":"control_request","request_id":"ivoai_catalog","request":{"subtype":"initialize"}}`+"\n"); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(io.LimitReader(out, discoveryLimit))
	scanner.Buffer(make([]byte, 4096), discoveryLimit)
	for scanner.Scan() {
		var response struct {
			Type     string `json:"type"`
			Response struct {
				Subtype   string `json:"subtype"`
				RequestID string `json:"request_id"`
				Response  struct {
					Models []struct {
						Value          string   `json:"value"`
						ResolvedModel  string   `json:"resolvedModel"`
						DisplayName    string   `json:"displayName"`
						Description    string   `json:"description"`
						SupportsEffort bool     `json:"supportsEffort"`
						Efforts        []string `json:"supportedEffortLevels"`
					} `json:"models"`
				} `json:"response"`
			} `json:"response"`
		}
		if json.Unmarshal(scanner.Bytes(), &response) != nil || response.Type != "control_response" || response.Response.RequestID != "ivoai_catalog" {
			continue
		}
		if response.Response.Subtype != "success" {
			return nil, errors.New("Claude catalog unavailable")
		}
		models := []ModelCapability{}
		seen := map[string]bool{}
		for _, v := range response.Response.Response.Models {
			name := v.ResolvedModel
			if name == "" {
				name = v.Value
			}
			if !safeModelName(name) || name == "default" || seen[name] {
				continue
			}
			seen[name] = true
			efforts := []string{}
			if v.SupportsEffort {
				for _, effort := range v.Efforts {
					if safeEffort(effort) {
						efforts = append(efforts, effort)
					}
				}
			}
			display := v.DisplayName
			if !safeModelName(display) {
				display = name
			}
			models = append(models, ModelCapability{Name: name, DisplayName: display, Provider: "claude", CapabilityTier: catalogTier(v.Description), SupportedEfforts: efforts, IsDefault: v.Value == "default", Source: SourceRuntimeVerified, Availability: "catalog_exposed", QuotaEligibility: "UNKNOWN"})
		}
		if len(models) == 0 {
			return nil, errors.New("Claude returned no supported models")
		}
		return models, nil
	}
	return nil, errors.New("Claude catalog stream incomplete")
}

// CodexThreadConfiguration reads official persisted configuration after a turn.
// The upstream schema explicitly says these are NOT per-turn usage telemetry.
// Consumers must label the evidence source and must not echo requested values
// when the official client cannot report a value.
func CodexThreadConfiguration(parent context.Context, binary, id string, environment []string) (string, string, error) {
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "app-server", "--stdio")
	cmd.Env = environment
	in, err := cmd.StdinPipe()
	if err != nil {
		return "", "", err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return "", "", err
	}
	defer func() { _ = in.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	encoder := json.NewEncoder(in)
	if err := encoder.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "ivoai", "version": "1"}}}); err != nil {
		return "", "", err
	}
	scanner := bufio.NewScanner(io.LimitReader(out, discoveryLimit))
	scanner.Buffer(make([]byte, 4096), discoveryLimit)
	for scanner.Scan() {
		var response struct {
			ID     int             `json:"id"`
			Error  json.RawMessage `json:"error"`
			Result struct {
				Thread struct {
					Model  string `json:"model"`
					Effort string `json:"reasoningEffort"`
				} `json:"thread"`
			} `json:"result"`
		}
		if json.Unmarshal(scanner.Bytes(), &response) != nil {
			continue
		}
		if response.ID == 1 {
			if len(response.Error) > 0 {
				return "", "", errors.New("Codex initialize failed")
			}
			_ = encoder.Encode(map[string]any{"method": "initialized"})
			if err := encoder.Encode(map[string]any{"id": 2, "method": "thread/read", "params": map[string]any{"threadId": id, "includeTurns": false}}); err != nil {
				return "", "", err
			}
			continue
		}
		if response.ID != 2 {
			continue
		}
		if len(response.Error) > 0 {
			return "", "", errors.New("Codex thread metadata unavailable")
		}
		model, effort := strings.TrimSpace(response.Result.Thread.Model), response.Result.Thread.Effort
		if !safeModelName(model) {
			model = ""
		}
		if !safeEffort(effort) {
			effort = ""
		}
		return model, effort, nil
	}
	return "", "", errors.New("Codex thread configuration unavailable")
}
