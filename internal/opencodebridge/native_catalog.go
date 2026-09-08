package opencodebridge

// NativeCatalog is an allowlisted projection of the pinned /provider response.
// In particular it cannot represent provider keys, options, headers or tokens.
type NativeCatalog struct {
	All       []NativeProvider  `json:"all"`
	Connected []string          `json:"connected"`
	Default   map[string]string `json:"default"`
}
type NativeProvider struct {
	ID     string                 `json:"id"`
	Name   string                 `json:"name"`
	Source string                 `json:"source"`
	Models map[string]NativeModel `json:"models"`
}
type NativeModel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	Capabilities struct {
		ToolCall  bool `json:"toolcall"`
		Reasoning bool `json:"reasoning"`
	} `json:"capabilities"`
	Variants map[string]struct{} `json:"variants"`
}
