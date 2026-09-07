package codexresolver

import (
	"reflect"
	"testing"
)

func TestConfigurationArgsPreservesMCPWhenEffortIsExplicit(t *testing.T) {
	input := []string{"-c", "mcp_servers.memory.url=loopback", "-c", "developer_instructions=research", "exec", "--json", "--model", "runtime-model", "-c", "model_reasoning_effort=medium", "--config=model_reasoning_effort=high", "--", "-c", "literal prompt"}
	want := []string{"-c", "mcp_servers.memory.url=loopback", "-c", "developer_instructions=research", "-c", "model_reasoning_effort=medium", "--config=model_reasoning_effort=high", "exec", "--json", "--model", "runtime-model", "--", "-c", "literal prompt"}
	if got := ConfigurationArgs(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
	if input[4] != "exec" {
		t.Fatal("mutated caller arguments")
	}
}
