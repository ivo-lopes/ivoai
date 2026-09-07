package codexresolver

import "strings"

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
