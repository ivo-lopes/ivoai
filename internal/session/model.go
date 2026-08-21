package session

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

func ResolveModel(runtimeName, argumentName, executor, configPath string) ModelInfo {
	if value := cleanModel(runtimeName); value != "" {
		return ModelInfo{Name: value, Source: ModelRuntimeVerified}
	}
	if value := cleanModel(argumentName); value != "" {
		return ModelInfo{Name: value, Source: ModelArgument}
	}
	if value := configuredModel(executor, configPath); value != "" {
		return ModelInfo{Name: value, Source: ModelConfigured}
	}
	return UnknownModel()
}

func ParseModelArgument(args []string) string {
	for index := 0; index < len(args); index++ {
		if args[index] == "--model" || args[index] == "-m" {
			if index+1 < len(args) {
				return cleanModel(args[index+1])
			}
		}
		if strings.HasPrefix(args[index], "--model=") {
			return cleanModel(strings.TrimPrefix(args[index], "--model="))
		}
	}
	return ""
}

func configuredModel(executor, filename string) string {
	if filename == "" {
		return ""
	}
	body, err := os.ReadFile(filename)
	if err != nil || len(body) > 1<<20 {
		return ""
	}
	if executor == "codex" {
		var value struct {
			Model string `toml:"model"`
		}
		if toml.Unmarshal(body, &value) == nil {
			return cleanModel(value.Model)
		}
		return ""
	}
	var value struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &value) == nil {
		return cleanModel(value.Model)
	}
	return ""
}

func cleanModel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "\r\n\x00\x1b") {
		return ""
	}
	return value
}
