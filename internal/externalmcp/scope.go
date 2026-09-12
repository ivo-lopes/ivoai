package externalmcp

import "errors"

const MaxInventoryTools = 512

func ValidToolName(name string) bool { return safeName(name) }

// ValidateScope bounds exact task grants. Neither nil nor an empty list means all.
func ValidateScope(scope map[string][]string) error {
	if len(scope) > 32 {
		return errors.New("MCP scope exceeds server limit")
	}
	for server, tools := range scope {
		if !safeName(server) || len(tools) > 128 {
			return errors.New("invalid MCP scope")
		}
		seen := map[string]bool{}
		for _, tool := range tools {
			if !safeName(tool) || seen[tool] {
				return errors.New("invalid or duplicate MCP tool scope")
			}
			seen[tool] = true
		}
	}
	return nil
}
