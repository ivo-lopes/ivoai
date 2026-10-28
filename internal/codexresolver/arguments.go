package codexresolver

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
)

// ProjectTrustArgs keeps Codex exec's App Server from persisting inferred
// project trust when workspace-write is granted. An explicit, process-local
// untrusted entry also prevents project configuration from gaining authority.
// Sandbox and approval settings remain independent and are not relaxed.
// Use an inline table: Codex splits dotted override keys without TOML quoting.
func ProjectTrustArgs(directory string) []string {
	if directory == "" {
		directory = "."
	}
	directory, err := filepath.Abs(directory)
	if err != nil {
		return nil
	}
	paths := []string{filepath.Clean(directory)}
	if canonical, err := filepath.EvalSymlinks(directory); err == nil && canonical != paths[0] {
		paths = append(paths, canonical)
	}
	entries := make([]string, 0, len(paths))
	for _, path := range paths {
		entries = append(entries, strconv.Quote(path)+`={trust_level="untrusted"}`)
	}
	return []string{"-c", "projects={" + strings.Join(entries, ",") + "}"}
}

// ConfigurationArgs keeps all -c overrides at the same CLI parsing level.
// Codex 0.153.4 shadows root overrides when exec receives its own -c values;
// mixing levels silently discards MCP configuration and developer instructions.
// Preserve order (including last-value precedence) and the explicit -- boundary.
func ConfigurationArgs(args []string) []string {
	configuration, remaining := []string{}, []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			remaining = append(remaining, args[i:]...)
			break
		}
		if (arg == "-c" || arg == "--config") && i+1 < len(args) {
			configuration = append(configuration, arg, args[i+1])
			i++
		} else if strings.HasPrefix(arg, "--config=") || strings.HasPrefix(arg, "-c") && len(arg) > 2 {
			configuration = append(configuration, arg)
		} else {
			remaining = append(remaining, arg)
		}
	}
	return append(configuration, remaining...)
}

// ManagedAgentArgs keeps native worker creation subordinate to Core. Legacy
// multi_agent feature flags alone do not disable the namespaced worker handler.
// CLI --enable is rejected because Codex applies feature toggles after -c
// overrides regardless of their textual order. Callers keep ConfigurationArgs
// at the final exec boundary, including compact -c overrides.
func ManagedAgentArgs(args []string) ([]string, error) {
	boundary := len(args)
	for index, arg := range args {
		if arg == "--" {
			boundary = index
			break
		}
		key, _, _ := strings.Cut(arg, "=")
		if key == "--enable" {
			return nil, errors.New("managed Codex feature toggles are controlled by IVOAI")
		}
	}
	out := append([]string(nil), args[:boundary]...)
	out = append(out,
		"-c", "features.apps=false", "-c", "features.hooks=false",
		"-c", "features.multi_agent=false", "-c", "features.plugins=false",
		"-c", "features.remote_plugin=false", "-c", "agents.enabled=false",
		"-c", "features.multi_agent_v2=false")
	return append(out, args[boundary:]...), nil
}
