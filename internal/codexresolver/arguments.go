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
	boundary := managedArgumentBoundary(args)
	for _, arg := range args[:boundary] {
		key, _, _ := strings.Cut(arg, "=")
		if key == "--enable" {
			return nil, errors.New("managed Codex feature toggles are controlled by IVOAI")
		}
	}
	if err := ValidateManagedConfigScope(args[:boundary]); err != nil {
		return nil, err
	}
	out := append([]string(nil), args[:boundary]...)
	out = append(out,
		"-c", "features.apps=false", "-c", "features.hooks=false",
		"-c", "features.multi_agent=false", "-c", "features.plugins=false",
		"-c", "features.remote_plugin=false", "-c", "agents.enabled=false",
		"-c", "features.multi_agent_v2=false")
	return append(out, args[boundary:]...), nil
}

// ValidateManagedConfigScope rejects configuration scopes that the managed MCP
// inventory cannot reproduce. Call it before starting the frontend so the fixed
// migration diagnostic reaches the user, and again before inventory as a guard.
// Denying names from a different scope can create transport-less tables; simply
// skipping inventory could instead admit system/cloud MCP servers.
func ValidateManagedConfigScope(args []string) error {
	if managedIgnoresUserConfig(args[:managedArgumentBoundary(args)]) {
		return errors.New("CODEX_MANAGED_CONFIG_SCOPE_UNSUPPORTED: managed MCP isolation cannot safely honor --ignore-user-config; omit that flag; IVOAI does not modify personal configuration files")
	}
	return nil
}

func managedArgumentBoundary(args []string) int {
	for index := 0; index < len(args); index++ {
		if args[index] == "--" {
			return index
		}
		option, _, inline := strings.Cut(args[index], "=")
		if !inline && (execOptions[option] || globalValueOptions[option]) && index+1 < len(args) {
			index++
		}
	}
	return len(args)
}

func managedIgnoresUserConfig(args []string) bool {
	execArgs := SplitExecArguments(args).ExecArgs
	for index := 0; index < len(execArgs); index++ {
		option, _, inline := strings.Cut(execArgs[index], "=")
		if option == "--ignore-user-config" {
			return true
		}
		if execOptions[option] && !inline && index+1 < len(execArgs) {
			index++
		}
	}
	return false
}
