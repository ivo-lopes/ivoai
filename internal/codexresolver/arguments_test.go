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

func TestCompactConfigurationCannotMoveAfterHostPolicy(t *testing.T) {
	input := []string{"exec", "-cagents.enabled=true", "-c=features.multi_agent_v2=true", "--json", "--", "-cliteral=prompt"}
	want := []string{"-cagents.enabled=true", "-c=features.multi_agent_v2=true", "exec", "--json", "--", "-cliteral=prompt"}
	if got := ConfigurationArgs(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("compact configuration changed precedence: got %q want %q", got, want)
	}
}

func TestManagedAgentPolicyWinsCallerConfigAndPreservesLiteralBoundary(t *testing.T) {
	input := []string{"--profile", "hostile", "exec", "-cagents.enabled=true", "--config=features.multi_agent_v2=true", "-c=features.plugins=true", "--", "--enable=multi_agent_v2", "literal prompt"}
	original := append([]string(nil), input...)
	managed, err := ManagedAgentArgs(input)
	if err != nil {
		t.Fatal(err)
	}
	args := ConfigurationArgs(managed)
	settings := map[string]string{}
	boundary := -1
	for i, arg := range args {
		if arg == "--" {
			boundary = i
			break
		}
		var setting string
		if arg == "-c" && i+1 < len(args) {
			setting = args[i+1]
		} else if strings.HasPrefix(arg, "--config=") {
			setting = strings.TrimPrefix(arg, "--config=")
		} else if strings.HasPrefix(arg, "-c") && len(arg) > 2 {
			setting = strings.TrimPrefix(strings.TrimPrefix(arg, "-c"), "=")
		}
		if key, value, ok := strings.Cut(setting, "="); ok {
			settings[key] = value
		}
	}
	for _, key := range []string{"agents.enabled", "features.multi_agent_v2", "features.multi_agent", "features.plugins", "features.hooks", "features.apps", "features.remote_plugin"} {
		if settings[key] != "false" {
			t.Fatalf("caller enabled managed capability %s: %q", key, settings[key])
		}
	}
	if boundary < 0 || !reflect.DeepEqual(args[boundary+1:], []string{"--enable=multi_agent_v2", "literal prompt"}) || !reflect.DeepEqual(input, original) {
		t.Fatal("managed policy mutated input or interpreted a literal prompt as options")
	}
}

func TestManagedAgentPolicyRejectsFeatureToggles(t *testing.T) {
	for _, input := range [][]string{{"--enable", "multi_agent_v2"}, {"--enable=multi_agent_v2"}, {"exec", "--enable", "multi_agent"}, {"--enable=plugins"}, {"--output-schema", "--", "--enable", "multi_agent_v2"}} {
		if _, err := ManagedAgentArgs(input); err == nil {
			t.Fatalf("feature toggle can override host policy: %q", input)
		}
	}
}
