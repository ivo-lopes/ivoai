package routing

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

const discoveryLimit = 1 << 20

type Discoverer struct {
	CodexPath  string
	ClaudePath string
	CachePath  string
}

func (d Discoverer) Discover(ctx context.Context) Registry {
	versions := map[string]string{}
	if trustedBinary(d.CodexPath, "codex") == nil {
		versions["codex"], _ = commandOutput(ctx, d.CodexPath, "--version")
	}
	if trustedBinary(d.ClaudePath, "claude") == nil {
		versions["claude"], _ = commandOutput(ctx, d.ClaudePath, "--version")
	}
	if cached, ok := d.loadCache(versions); ok {
		return cached
	}
	result := Registry{Providers: map[string]ProviderCapability{}}
	if capability, err := d.codex(ctx); err == nil {
		result.Providers["codex"] = capability
	}
	if capability, err := d.claude(ctx); err == nil {
		result.Providers["claude"] = capability
	}
	d.saveCache(result)
	return result
}

type capabilityCache struct {
	Providers map[string]ProviderCapability `json:"providers"`
}

func (d Discoverer) loadCache(versions map[string]string) (Registry, bool) {
	if d.CachePath == "" {
		return Registry{}, false
	}
	info, err := os.Lstat(d.CachePath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return Registry{}, false
	}
	body, err := platform.ReadRegularFile(d.CachePath, discoveryLimit)
	if err != nil {
		return Registry{}, false
	}
	var cached capabilityCache
	if json.Unmarshal(body, &cached) != nil || len(cached.Providers) == 0 {
		return Registry{}, false
	}
	for provider, version := range versions {
		if version == "" || cached.Providers[provider].Version != version {
			return Registry{}, false
		}
	}
	return Registry{Providers: cached.Providers}, true
}

func (d Discoverer) saveCache(value Registry) {
	if d.CachePath == "" || len(value.Providers) == 0 {
		return
	}
	body, err := json.MarshalIndent(capabilityCache{Providers: value.Providers}, "", "  ")
	if err != nil || len(body) > discoveryLimit {
		return
	}
	if platform.EnsurePrivateDir(filepath.Dir(d.CachePath)) != nil {
		return
	}
	_ = platform.AtomicWritePrivate(append(body, '\n'), d.CachePath)
}

func (d Discoverer) codex(ctx context.Context) (ProviderCapability, error) {
	if err := trustedBinary(d.CodexPath, "codex"); err != nil {
		return ProviderCapability{}, err
	}
	version, _ := commandOutput(ctx, d.CodexPath, "--version")
	models, err := codexModels(ctx, d.CodexPath)
	if err != nil {
		return ProviderCapability{Provider: "codex", Version: version, Authenticated: true, WorkerCapable: true, Source: SourceUnknown}, nil
	}
	return ProviderCapability{Provider: "codex", Version: version, Authenticated: true, WorkerCapable: true, Models: models, SupportsEffort: hasEfforts(models), Source: SourceRuntimeVerified}, nil
}

func (d Discoverer) claude(ctx context.Context) (ProviderCapability, error) {
	if err := trustedBinary(d.ClaudePath, "claude"); err != nil {
		return ProviderCapability{}, err
	}
	version, _ := commandOutput(ctx, d.ClaudePath, "--version")
	help, err := commandOutput(ctx, d.ClaudePath, "--help")
	if err != nil {
		return ProviderCapability{}, err
	}
	efforts := parseClaudeEfforts(help)
	models := []ModelCapability{{Provider: "claude", IsDefault: true, SupportedEfforts: efforts, Source: SourceDefault}}
	return ProviderCapability{Provider: "claude", Version: version, Authenticated: true, WorkerCapable: true, Models: models, SupportsEffort: len(efforts) > 0, Source: SourceRuntimeVerified}, nil
}

func codexModels(parent context.Context, binary string) ([]ModelCapability, error) {
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "app-server", "--stdio")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"ivoai","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"initialized","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"model/list","params":{"limit":100}}`,
	}
	for _, request := range requests {
		if _, err := io.WriteString(stdin, request+"\n"); err != nil {
			return nil, err
		}
	}
	scanner := bufio.NewScanner(io.LimitReader(stdout, discoveryLimit))
	scanner.Buffer(make([]byte, 4096), discoveryLimit)
	for scanner.Scan() {
		var response struct {
			ID     int `json:"id"`
			Result struct {
				Data []struct {
					Model                     string `json:"model"`
					Description               string `json:"description"`
					IsDefault                 bool   `json:"isDefault"`
					DefaultReasoningEffort    string `json:"defaultReasoningEffort"`
					SupportedReasoningEfforts []struct {
						Effort string `json:"reasoningEffort"`
					} `json:"supportedReasoningEfforts"`
				} `json:"data"`
			} `json:"result"`
		}
		if json.Unmarshal(scanner.Bytes(), &response) != nil || response.ID != 2 {
			continue
		}
		models := make([]ModelCapability, 0, len(response.Result.Data))
		for _, value := range response.Result.Data {
			if !safeModelName(value.Model) {
				continue
			}
			efforts := make([]string, 0, len(value.SupportedReasoningEfforts))
			for _, option := range value.SupportedReasoningEfforts {
				if safeEffort(option.Effort) {
					efforts = append(efforts, option.Effort)
				}
			}
			models = append(models, ModelCapability{Name: value.Model, Provider: "codex", CapabilityTier: catalogTier(value.Description), SupportedEfforts: efforts, DefaultEffort: value.DefaultReasoningEffort, IsDefault: value.IsDefault, Source: SourceRuntimeVerified})
		}
		if len(models) == 0 {
			return nil, errors.New("Codex model catalog returned no models")
		}
		return models, nil
	}
	return nil, errors.New("Codex model catalog unavailable")
}

func safeModelName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if char < 0x20 || char == 0x7f || char >= 0x80 && char <= 0x9f || char >= 0x202a && char <= 0x202e || char >= 0x2066 && char <= 0x2069 {
			return false
		}
	}
	return true
}

func catalogTier(description string) Tier {
	value := strings.ToLower(description)
	switch {
	case strings.Contains(value, "cost-efficient"), strings.Contains(value, "cost sensitive"), strings.Contains(value, "fast"):
		return TierLight
	case strings.Contains(value, "balanced"), strings.Contains(value, "everyday"):
		return TierBalanced
	case strings.Contains(value, "strong"):
		return TierStrong
	case strings.Contains(value, "frontier"), strings.Contains(value, "most capable"), strings.Contains(value, "hardest"):
		return TierMax
	default:
		return ""
	}
}

var effortListPattern = regexp.MustCompile(`(?m)--effort\s+<[^>]+>[^\n]*\n(?:[^\n]*\n)?[^\n]*\(([^)]*)\)`)

func parseClaudeEfforts(help string) []string {
	match := effortListPattern.FindStringSubmatch(help)
	if len(match) != 2 {
		// Current Claude prints the choices on the same wrapped option block.
		start := strings.Index(help, "--effort <")
		if start < 0 {
			return nil
		}
		end := start + 320
		if end > len(help) {
			end = len(help)
		}
		match = []string{"", help[start:end]}
	}
	values := []string{}
	for _, candidate := range []string{"low", "medium", "high", "xhigh", "max"} {
		if strings.Contains(match[1], candidate) {
			values = append(values, candidate)
		}
	}
	return values
}

func safeEffort(value string) bool {
	switch value {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra":
		return true
	default:
		return false
	}
}

func hasEfforts(models []ModelCapability) bool {
	for _, model := range models {
		if len(model.SupportedEfforts) > 0 {
			return true
		}
	}
	return false
}

func trustedBinary(path, name string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Base(path) != name {
		return fmt.Errorf("%s binary is not a trusted absolute path", name)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return fmt.Errorf("%s binary is unavailable", name)
	}
	return nil
}

func commandOutput(parent context.Context, binary string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	body, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
	if len(body) > discoveryLimit {
		return "", errors.New("capability output exceeded limit")
	}
	return strings.TrimSpace(string(body)), err
}
