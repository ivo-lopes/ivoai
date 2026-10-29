package workers

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/codexresolver"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

type inventoryContextRunner struct {
	run func([]string, platform.RunOptions) (platform.Result, error)
}

func (r inventoryContextRunner) LookPath(n string) (string, error) { return n, nil }
func (r inventoryContextRunner) Run(_ context.Context, _ string, args []string, options platform.RunOptions) (platform.Result, error) {
	return r.run(args, options)
}

func TestCodexMCPInventoryIncludesExecutionConfig(t *testing.T) {
	policy := codexresolver.ProjectTrustArgs(t.TempDir())
	called := false
	a := Adapter{Runner: inventoryContextRunner{run: func(args []string, _ platform.RunOptions) (platform.Result, error) {
		called = true
		if !strings.Contains(strings.Join(args, "\n"), policy[1]) {
			t.Error("inventory lost the execution project-trust policy")
		}
		if !strings.Contains(strings.Join(args, "\n"), `project_root_markers=[".git"]`) {
			t.Error("inventory lost caller configuration")
		}
		return platform.Result{Stdout: "[]"}, nil
	}}}
	_, err := a.IsolateCodexMCPs(context.Background(), "synthetic-client", append(policy, "-c", `project_root_markers=[".git"]`))
	if err != nil || !called {
		t.Fatalf("inventory unavailable: %v", err)
	}
}

func TestCodexScopedInventorySharesContextAndPreservesProjection(t *testing.T) {
	root := t.TempDir()
	scope, err := codexresolver.NewConfigContext(platform.RunOptions{Dir: root, Env: []string{"HOME=synthetic-home", "CODEX_HOME=synthetic-codehome"}, CleanEnv: true})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	a := Adapter{Runner: inventoryContextRunner{run: func(args []string, o platform.RunOptions) (platform.Result, error) {
		called = true
		if o.Dir != scope.Directory() || !o.CleanEnv || !reflect.DeepEqual(o.Env, scope.Environment()) || o.Timeout != 15*time.Second {
			t.Fatal("inventory scope differs from exec context")
		}
		joined := strings.Join(args, "\n")
		for _, required := range []string{`project_root_markers=[".git"]`, `mcp_servers.owned.command="/synthetic/core"`, `mcp_servers.owned.enabled=true`, `agents.enabled=false`, `features.multi_agent_v2=false`, `developer_instructions="synthetic-instructions-private"`} {
			if !strings.Contains(joined, required) {
				t.Fatalf("inventory missing admitted scope setting %q", required)
			}
		}
		if strings.Contains(joined, "literal-prompt-private") {
			t.Fatal("inventory forwarded literal prompt data")
		}
		if strings.LastIndex(joined, scope.ProjectTrustArguments()[1]) < strings.Index(joined, `projects={caller={trust_level="trusted"}}`) {
			t.Fatal("caller trust won over process-local trust")
		}
		return platform.Result{Stdout: `[{"name":"owned"},{"name":"personal"}]`}, nil
	}}}
	input := []string{"-c", `projects={caller={trust_level="trusted"}}`, "-c", `project_root_markers=[".git"]`, "-cmcp_servers.owned.command=\"/synthetic/core\"", "-c", `mcp_servers.owned.enabled=true`, "-c", `developer_instructions="synthetic-instructions-private"`, "--", "literal-prompt-private", "-c", "mcp_servers.after.enabled=true"}
	args, err := a.IsolateCodexMCPsInContext(context.Background(), "synthetic-client", input, scope)
	if err != nil || !called {
		t.Fatalf("scoped isolation failed: %v", err)
	}
	joined := strings.Join(args, "\n")
	if strings.Index(joined, "mcp_servers.owned.enabled=false") > strings.Index(joined, "mcp_servers.owned.enabled=true") || !strings.Contains(joined, "mcp_servers.personal.enabled=false") {
		t.Fatal("deny/projection precedence changed")
	}
	if !strings.HasSuffix(joined, "--\nliteral-prompt-private\n-c\nmcp_servers.after.enabled=true") {
		t.Fatal("literal boundary data changed")
	}
}

func TestCodexScopedInventoryFailsBeforeClientAndOnMalformedResults(t *testing.T) {
	scope, _ := codexresolver.NewConfigContext(platform.RunOptions{Dir: t.TempDir(), CleanEnv: true})
	for _, args := range [][]string{{"--ignore-user-config"}, {"--enable=multi_agent_v2"}, {"--cd=/synthetic"}, {"-C/synthetic"}} {
		calls := 0
		a := Adapter{Runner: inventoryContextRunner{run: func([]string, platform.RunOptions) (platform.Result, error) {
			calls++
			return platform.Result{Stdout: "[]"}, nil
		}}}
		if _, err := a.IsolateCodexMCPsInContext(context.Background(), "synthetic", args, scope); err == nil || calls != 0 {
			t.Fatal("unsupported scope entered client")
		}
	}
	for name, output := range map[string]string{"invalid": "not-json", "trailing": "[] {}", "duplicate": `[{"name":"same"},{"name":"same"}]`, "unsafe": `[{"name":"has.dot"}]`, "too_large": strings.Repeat(" ", 256<<10) + "[]", "too_many": "[" + strings.TrimSuffix(strings.Repeat(`{"name":"same"},`, 129), ",") + "]"} {
		t.Run(name, func(t *testing.T) {
			a := Adapter{Runner: inventoryContextRunner{run: func([]string, platform.RunOptions) (platform.Result, error) {
				return platform.Result{Stdout: output}, nil
			}}}
			if _, err := a.IsolateCodexMCPsInContext(context.Background(), "synthetic", nil, scope); err == nil {
				t.Fatal("malformed/over-limit inventory admitted")
			}
		})
	}
}

func TestWorkerInventoryAndExecShareFilteredEnvironment(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "synthetic-codehome")
	t.Setenv("CODEX_HOME", home)
	t.Setenv("OPENAI_API_KEY", "synthetic-parent-key")
	t.Setenv("IVOAI_EXTERNAL_MCP_SESSION_TOKEN", "synthetic-parent-token")
	access, err := NewAccess(root, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	script := executable(t, root, "codex", `#!/bin/sh
if [ "$1" = "mcp" ]; then printf '[]'; exit 0; fi
[ "$PWD" = '`+root+`' ] || exit 70
[ "$CODEX_HOME" = '`+home+`' ] || exit 71
[ -z "$OPENAI_API_KEY$IVOAI_EXTERNAL_MCP_SESSION_TOKEN" ] || exit 72
previous=""
for arg in "$@"; do
 [ "$previous" = "--output-last-message" ] && result="$arg"
 previous="$arg"
done
printf 'same frozen scope' > "$result"
`)
	a := Adapter{CodexPath: script, Runner: inventoryContextRunner{run: func(args []string, o platform.RunOptions) (platform.Result, error) {
		if o.Dir != root || !o.CleanEnv {
			t.Fatal("worker inventory did not use explicit child context")
		}
		env := strings.Join(o.Env, "\n")
		if !strings.Contains(env, "CODEX_HOME="+home) || strings.Contains(env, "synthetic-parent-key") || strings.Contains(env, "synthetic-parent-token") {
			t.Fatal("inventory used unfiltered parent environment")
		}
		// Mutation after inventory must not affect the exec snapshot.
		t.Setenv("CODEX_HOME", filepath.Join(root, "changed-after-inventory"))
		return (platform.ExecRunner{}).Run(context.Background(), script, args, o)
	}}}
	result, err := a.Run(context.Background(), Request{Executor: "codex", Task: "synthetic task", Directory: root, Runtime: filepath.Join(root, "runtime"), Access: access}, nil)
	if err != nil || result.Text != "same frozen scope" {
		t.Fatalf("worker exec diverged from inventory: %v", err)
	}
}

func TestCodexIncompleteOptionsFailBeforeInventory(t *testing.T) {
	scope, _ := codexresolver.NewConfigContext(platform.RunOptions{Dir: t.TempDir(), CleanEnv: true})
	for _, option := range []string{"-c", "--config", "--profile", "-p", "--disable", "--output-schema", "--output-last-message"} {
		t.Run(option, func(t *testing.T) {
			calls := 0
			a := Adapter{Runner: inventoryContextRunner{run: func([]string, platform.RunOptions) (platform.Result, error) {
				calls++
				return platform.Result{Stdout: "[]"}, nil
			}}}
			_, err := a.IsolateCodexMCPsInContext(context.Background(), "synthetic", []string{option}, scope)
			if err == nil || calls != 0 {
				t.Fatal("host policy was accepted as a missing caller option value")
			}
		})
	}
}

func TestScopedCodexImplicitContextUsesRequestDirectory(t *testing.T) {
	root := t.TempDir()
	access, err := NewAccess(root, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Executor: "codex", Directory: root, Runtime: filepath.Join(root, "runtime"), Access: access, Task: "synthetic"}
	args, _, err := workerArgs(request)
	if err != nil {
		t.Fatal(err)
	}
	a := Adapter{Runner: inventoryContextRunner{run: func(_ []string, o platform.RunOptions) (platform.Result, error) {
		if o.Dir != root || !o.CleanEnv {
			t.Error("implicit scoped inventory lost request directory")
		}
		return platform.Result{Stdout: "[]"}, nil
	}}}
	if _, err := a.isolateScopedMCPs(context.Background(), "/synthetic/codex", request, args); err != nil {
		t.Fatal(err)
	}
}
