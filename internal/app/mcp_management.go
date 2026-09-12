package app

import (
	"errors"
	"fmt"

	"github.com/ivo-lopes/ivoai/internal/connections"
)

func (a *App) MCPEnable(name string, enabled bool) error {
	return (connections.Registry{Store: a.Store}).Enable(name, enabled)
}
func (a *App) MCPPolicy(name, value string, direct bool) error {
	return (connections.Registry{Store: a.Store}).SetPolicy(name, value, direct)
}
func (a *App) MCPTools(name string) error {
	entries, err := (connections.Registry{Store: a.Store}).List()
	if err != nil {
		return err
	}
	entry, ok := entries[name]
	if !ok || entry.Kind != "external" {
		return errors.New("external MCP not found")
	}
	fmt.Fprintf(a.Out, "MCP %q health=%s policy=%s direct=%s probed=%s\n", name, entry.MCPHealth(), entry.ResolvedMCPPolicy(), entry.ResolvedDirectPolicy(), entry.ProbedAt)
	fmt.Fprintln(a.Out, "Compatibility: Codex / Claude / managed OpenCode; process-local gateway. Tool metadata is untrusted, not a grant.")
	for _, tool := range entry.Tools {
		fmt.Fprintf(a.Out, "%q class=%s provenance=%s schema_sha256=%s\n", tool.Name, tool.Classification, tool.Provenance, tool.SchemaSHA256)
	}
	if entry.ProbedAt == "" {
		fmt.Fprintln(a.Out, "Inventory not probed; run Test MCP. No tools granted implicitly.")
	}
	return nil
}
