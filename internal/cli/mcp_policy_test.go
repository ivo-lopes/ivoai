package cli

import (
	"bufio"
	"strings"
	"testing"
)

func TestMCPControlPlaneTUIAndCLIUseSameRegistry(t *testing.T) {
	a, out := mcpTestApp(t)
	for _, name := range []string{"a", "b"} {
		if err := runMCP(a, []string{"add", name, "https://example.invalid/mcp"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ input, action string }{
		{"a\ndisabled\n", "enable"}, {"a\nread_only\n", "policy"}, {"a\ndisabled\n", "direct"}, {"a\n", "tools"},
	} {
		s := menuSession{app: a, reader: bufio.NewReader(strings.NewReader(tc.input))}
		var err error
		switch tc.action {
		case "enable":
			_, err = s.mcpEnable()
		case "policy":
			_, err = s.mcpPolicy()
		case "direct":
			_, err = s.mcpDirectPolicy()
		case "tools":
			_, err = s.mcpTools()
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := a.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MCP.Servers["a"].Enabled || cfg.MCP.Servers["a"].Policy != "read_only" || cfg.MCP.Servers["a"].DirectPolicy != "disabled" || !cfg.MCP.Servers["b"].Enabled {
		t.Fatal("TUI did not isolate target")
	}
	if err := runMCP(a, []string{"enable", "a"}); err != nil {
		t.Fatal(err)
	}
	if err := runMCP(a, []string{"tools", "a"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "not probed") || !strings.Contains(out.String(), "Compatibility") {
		t.Fatal("missing metadata/unknown semantics")
	}
}
