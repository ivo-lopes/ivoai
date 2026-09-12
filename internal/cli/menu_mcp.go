package cli

import "errors"

func (s *menuSession) mcpTools() (bool, error) {
	name, err := s.promptValidated("MCP name", false, "", validateIdentifier)
	if err != nil {
		return false, err
	}
	return false, s.app.MCPTools(name)
}
func (s *menuSession) mcpEnable() (bool, error) {
	name, err := s.promptValidated("MCP name", false, "", validateIdentifier)
	if err != nil {
		return false, err
	}
	value, err := s.promptValidated("State (enabled/disabled)", false, "enabled", func(value string) error {
		if value != "enabled" && value != "disabled" {
			return errors.New("choose enabled or disabled")
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return false, s.app.MCPEnable(name, value == "enabled")
}
func (s *menuSession) mcpPolicy() (bool, error)       { return s.mcpPolicyChoice(false) }
func (s *menuSession) mcpDirectPolicy() (bool, error) { return s.mcpPolicyChoice(true) }
func (s *menuSession) mcpPolicyChoice(direct bool) (bool, error) {
	name, err := s.promptValidated("MCP name", false, "", validateIdentifier)
	if err != nil {
		return false, err
	}
	label, fallback := "Policy (read_only/read_auto_ask_mutating)", "read_auto_ask_mutating"
	if direct {
		label, fallback = "Direct policy (read_only/disabled)", "read_only"
	}
	value, err := s.promptValidated(label, false, fallback, func(value string) error {
		if value == "read_only" || direct && value == "disabled" || !direct && value == "read_auto_ask_mutating" {
			return nil
		}
		return errors.New("choose a listed policy")
	})
	if err != nil {
		return false, err
	}
	return false, s.app.MCPPolicy(name, value, direct)
}
