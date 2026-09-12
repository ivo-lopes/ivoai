package config

// MCPTool is bounded, untrusted inventory metadata, never an authorization.
// Schema bodies and credentials are deliberately not persisted.
type MCPTool struct {
	Name           string `toml:"name" json:"name"`
	Description    string `toml:"description,omitempty" json:"description,omitempty"`
	Classification string `toml:"classification" json:"classification"`
	Provenance     string `toml:"provenance" json:"provenance"`
	SchemaSHA256   string `toml:"schema_sha256" json:"schema_sha256"`
}

func (s MCPServer) ResolvedMCPPolicy() string {
	if s.Policy == "" {
		return "read_auto_ask_mutating"
	}
	return s.Policy
}
func (s MCPServer) ResolvedDirectPolicy() string {
	if s.DirectPolicy == "" {
		return "read_only"
	}
	return s.DirectPolicy
}
func (s MCPServer) MCPHealth() string {
	if !s.Enabled {
		return "DISABLED"
	}
	if s.Health == "" {
		return "UNKNOWN"
	}
	return s.Health
}
