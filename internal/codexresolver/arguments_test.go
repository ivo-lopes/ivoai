package codexresolver

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestProjectTrustArgsAreProcessLocalAndPathSafe(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "project.with dots and \"quotes\"")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(directory, link); err != nil {
		t.Fatal(err)
	}
	args := ProjectTrustArgs(link)
	if len(args) != 2 || args[0] != "-c" || !strings.HasPrefix(args[1], "projects={") {
		t.Fatalf("invalid process-local override: %q", args)
	}
	var parsed struct {
		Projects map[string]struct {
			Trust string `toml:"trust_level"`
		}
	}
	if err := toml.Unmarshal([]byte(args[1]), &parsed); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, directory} {
		if parsed.Projects[path].Trust != "untrusted" {
			t.Fatal("missing explicit untrusted directory")
		}
	}
	if len(parsed.Projects) != 2 {
		t.Fatal("unrelated project trust added")
	}
}

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
