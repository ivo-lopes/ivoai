package codexresolver

import (
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
		} else if strings.HasPrefix(arg, "--config=") || strings.HasPrefix(arg, "-c=") {
			configuration = append(configuration, arg)
		} else {
			remaining = append(remaining, arg)
		}
	}
	return append(configuration, remaining...)
}
