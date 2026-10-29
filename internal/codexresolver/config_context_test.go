package codexresolver

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

func TestConfigContextFreezesDirectoryEnvironmentAndPolicy(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("IVOAI_SCOPE_TEST", "before")
	scope, err := NewConfigContext(platform.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	before := scope.Environment()
	policy := scope.ProjectTrustArguments()
	t.Setenv("IVOAI_SCOPE_TEST", "after")
	t.Chdir(t.TempDir())
	if scope.Directory() != root || !reflect.DeepEqual(before, scope.Environment()) || !reflect.DeepEqual(policy, scope.ProjectTrustArguments()) {
		t.Fatal("configuration scope changed after snapshot")
	}
	opts := scope.RunOptions()
	opts.Env[0] = "MUTATED=fixture"
	policy[0] = "mutated"
	if !reflect.DeepEqual(before, scope.Environment()) || scope.ProjectTrustArguments()[0] != "-c" || !opts.CleanEnv {
		t.Fatal("configuration scope aliases a mutable output")
	}
	explicit := []string{"HOME=synthetic-secret-fixture"}
	private, _ := NewConfigContext(platform.RunOptions{Dir: root, Env: explicit, CleanEnv: true})
	explicit[0] = "changed"
	if private.Environment()[0] != "HOME=synthetic-secret-fixture" {
		t.Fatal("configuration scope aliases input")
	}
	for _, render := range []string{fmt.Sprintf("%v", private), fmt.Sprintf("%+v", private), fmt.Sprintf("%#v", private), fmt.Sprintf("%+v", &private)} {
		if strings.Contains(render, "synthetic-secret-fixture") {
			t.Fatal("context formatting exposed environment")
		}
	}
	empty, _ := NewConfigContext(platform.RunOptions{Dir: root, CleanEnv: true})
	if empty.Environment() == nil || len(empty.Environment()) != 0 {
		t.Fatal("clean nil became inherited")
	}
	if (ConfigContext{}).Validate() == nil {
		t.Fatal("zero context admitted")
	}
}

func TestConfigContextRejectsDirectoryOverridesBeforeInventory(t *testing.T) {
	scope, _ := NewConfigContext(platform.RunOptions{Dir: t.TempDir(), CleanEnv: true})
	for _, args := range [][]string{{"-C", "/synthetic-private"}, {"-C/synthetic-private"}, {"-C=/synthetic-private"}, {"--cd", "/synthetic-private"}, {"--cd=/synthetic-private"}, {"--output-schema", "--", "--cd", "/synthetic-private"}} {
		_, err := scope.ManagedArguments(args)
		if err == nil || !strings.Contains(err.Error(), "CODEX_MANAGED_CONFIG_SCOPE_UNSUPPORTED") || strings.Contains(err.Error(), "synthetic-private") {
			t.Fatalf("directory override not rejected safely: %v", err)
		}
	}
	for _, args := range [][]string{{"--output-schema", "--cd"}, {"-c", "-C/synthetic-private"}, {"--output-schema=--cd"}, {"--", "--cd", "/synthetic-private"}, {"--output-schema", "--", "--", "-C/synthetic-private"}} {
		if _, err := scope.ManagedArguments(args); err != nil {
			t.Fatalf("literal value was treated as directory option: %v", err)
		}
	}
}

func TestMCPInventoryArgumentsPreserveConfigScopeOnly(t *testing.T) {
	root := t.TempDir()
	scope, _ := NewConfigContext(platform.RunOptions{Dir: root, CleanEnv: true})
	input := []string{"-c", `projects={"/caller"={trust_level="trusted"}}`, "--config=project_root_markers=[\".git\"]", "-cfeatures.plugins=true", "-c=agents.enabled=true", "--profile", "synthetic", "-pcompact", "--disable", "synthetic_feature", "-c", `mcp_servers.owned.command="/synthetic/core"`, "-c", `mcp_servers.owned.enabled=true`, "--output-schema", "--config=NOT_A_SETTING", "--output-last-message", "--profile", "exec", "--json", "-", "--", "--profile", "NOT_A_PROFILE", "-c", "MUST_NOT_BE_FORWARDED"}
	managed, err := scope.ManagedArguments(input)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := MCPInventoryArguments(managed)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"--profile", "synthetic", "-pcompact", "--disable", "synthetic_feature", "mcp", "list", "--json", "-c", input[1], input[2], input[3], input[4], "-c", input[11], "-c", input[13]}
	expected = append(expected, scope.ProjectTrustArguments()...)
	restrictions, _ := ManagedAgentArgs(nil)
	expected = append(expected, restrictions...)
	if !reflect.DeepEqual(inventory, expected) {
		t.Fatalf("inventory configuration/precedence changed:\ngot %q\nwant %q", inventory, expected)
	}
	if strings.LastIndex(strings.Join(inventory, " "), `trust_level="untrusted"`) < strings.Index(strings.Join(inventory, " "), `trust_level="trusted"`) {
		t.Fatal("caller trust overrode managed trust")
	}
	for _, args := range [][]string{{"-c"}, {"--config"}, {"--profile"}, {"-p"}, {"--disable"}} {
		if _, err := MCPInventoryArguments(args); err == nil {
			t.Fatal("incomplete configuration entered inventory")
		}
	}
}

func TestMCPInventoryPreservesEveryExplicitLocalConfigSpelling(t *testing.T) {
	for _, args := range [][]string{
		{"-c", `future_scope.fixture=true`},
		{`--config=developer_instructions="synthetic private local text"`},
		{`-cmodel_provider="synthetic"`},
		{`-c=projects={}`},
		{"--config", `mcp_servers={synthetic={command="/synthetic/core"}}`},
		{"-c", `--ignore-user-config`},
	} {
		got, err := MCPInventoryArguments(args)
		want := append([]string{"mcp", "list", "--json"}, args...)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("explicit local config was omitted/reinterpreted")
		}
	}
}
