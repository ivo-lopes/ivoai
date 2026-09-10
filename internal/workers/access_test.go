package workers

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

func TestScopedWorkersDoNotInheritPeerMCPOrCredentials(t *testing.T) {
	root := t.TempDir()
	a, err := NewAccess(root, false, []MCPGrant{{Name: "plane", URL: "http://127.0.0.1:1234/mcp", Tools: []string{"list_projects"}, Token: "fixture-ephemeral-capability-a"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewAccess(root, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	base := []string{"HOME=" + root, "IVOAI_SERVER_TOKEN=parent", "IVOAI_KNOWLEDGE_SESSION_TOKEN=parent", "AI_MEMORY_AUTH_TOKEN=parent", "IVOAI_WORKER_MCP_TOKEN_A=other-worker", "GIT_DIR=/other/repository"}
	first := strings.Join(a.environment(base), "\n")
	second := strings.Join(b.environment(base), "\n")
	if !strings.Contains(first, "fixture-ephemeral-capability-a") || strings.Contains(first, "parent") || strings.Contains(first, "other-worker") || strings.Contains(second, "TOKEN") || strings.Contains(second, "GIT_DIR") {
		t.Fatal("worker projection inherited another capability")
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", a, a.grants[0]), "fixture-ephemeral") {
		t.Fatal("grant diagnostic disclosed credential")
	}
	if _, err := NewAccess(root, false, []MCPGrant{{Name: "plane", URL: "https://upstream.example.invalid", Tools: []string{"list_projects"}, Token: "must-not-use-upstream"}}); err == nil {
		t.Fatal("direct upstream grant accepted")
	}
}

func TestScopedNativeOpenCodeReusesControlledPolicyWithoutRecursion(t *testing.T) {
	root := t.TempDir()
	access, err := NewAccess(root, true, []MCPGrant{{Name: "plane", URL: "http://127.0.0.1:1234/mcp", Tools: []string{"list_projects"}, Token: "fixture-worker-capability-only"}})
	if err != nil {
		t.Fatal(err)
	}
	options := access.ConfigureNative(opencodebridge.ManagedOptions{Environment: []string{"HOME=" + root, "OPENAI_API_KEY=must-not-inherit", "IVOAI_SERVER_TOKEN=parent", "IVOAI_EXTERNAL_MCP_SESSION_TOKEN=parent"}, NativeMCP: map[string]any{"personal": true, "ivoai-orchestrator": true}})
	if !options.NativeExecutor || options.Bridge != nil || options.Directory != root || len(options.NativeMCP) != 1 || options.NativePermissions["*"] != "deny" || options.NativePermissions["external_directory"] != "deny" || options.NativePermissions["edit"] != "allow" {
		t.Fatal("native worker control boundary changed")
	}
	env := strings.Join(options.Environment, "\n")
	if strings.Contains(env, "parent") || strings.Contains(env, "must-not-inherit") || !strings.Contains(env, "fixture-worker-capability-only") {
		t.Fatal("native worker credential isolation failed")
	}
	for name, config := range options.NativeMCP {
		if name == "personal" || name == "ivoai-orchestrator" || strings.Contains(fmt.Sprint(config), "fixture-worker-capability-only") {
			t.Fatal("recursive MCP or secret copied into native config")
		}
	}
}

func TestScopedCodexArgsEnforceDefaultDenyAndWorkspaceSandbox(t *testing.T) {
	root := t.TempDir()
	path := executable(t, root, "codex", "#!/bin/sh\nprintf '%s' '[{\"name\":\"personal\"},{\"name\":\"ivoai-memory\"}]'\n")
	access, err := NewAccess(root, true, []MCPGrant{{Name: "plane", URL: "http://127.0.0.1:1234/mcp", Tools: []string{"list_projects"}, Token: "fixture-capability-never-argv"}})
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Access: access, Executor: "codex", Directory: root, Runtime: filepath.Join(root, "runtime"), Task: "bounded task"}
	args, _, err := workerArgs(request)
	if err != nil {
		t.Fatal(err)
	}
	args, err = (Adapter{Runner: platform.ExecRunner{}}).isolateScopedMCPs(context.Background(), path, request, args)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, "\n")
	for _, want := range []string{"mcp_servers.personal.enabled=false", "mcp_servers.ivoai-memory.enabled=false", "workspace-write", "--skip-git-repo-check", "enabled_tools=[\"list_projects\"]", "IVOAI_WORKER_MCP_TOKEN_A"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing boundary %q", want)
		}
	}
	if strings.Contains(joined, "fixture-capability-never-argv") {
		t.Fatal("capability in argv")
	}
}

func TestClaudeWritesRequireOfficialRestrictedCapability(t *testing.T) {
	root := t.TempDir()
	access, _ := NewAccess(root, true, nil)
	request := Request{Access: access, Executor: "claude", Directory: root, Runtime: root, Task: "fixture"}
	args, _, err := workerArgs(request)
	if err != nil || !strings.Contains(strings.Join(args, " "), "--restricted") {
		t.Fatal("unrestricted Claude writer")
	}
	for _, supported := range []bool{false, true} {
		body := "#!/bin/sh\nprintf '%s' '--print --strict-mcp-config'\n"
		if supported {
			body = "#!/bin/sh\nprintf '%s' '--restricted --permission-mode acceptEdits'\n"
		}
		path := executable(t, t.TempDir(), "claude", body)
		_, err := (Adapter{Runner: platform.ExecRunner{}}).isolateScopedMCPs(context.Background(), path, request, args)
		if (err == nil) != supported {
			t.Fatalf("supported=%v error=%v", supported, err)
		}
	}
}
