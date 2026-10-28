package workers

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestClaudeWorkerBuiltinCatalogAndScopedMCPRemainIndependent(t *testing.T) {
	for _, mode := range []string{"advisory", "scoped-readonly", "scoped-write"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			request := Request{Executor: "claude", Directory: root, Runtime: root, Task: "inspect approved evidence"}
			if mode != "advisory" {
				var err error
				request.Access, err = NewAccess(root, mode == "scoped-write", []MCPGrant{{Name: "evidence", URL: "http://127.0.0.1:1234/mcp", Tools: []string{"read_evidence"}, Token: "fixture-worker-private-capability"}})
				if err != nil {
					t.Fatal(err)
				}
			}
			args, _, err := workerArgs(request)
			if err != nil {
				t.Fatal(err)
			}
			flags := map[string][]string{}
			for i := 0; i+1 < len(args); i++ {
				if strings.HasPrefix(args[i], "--") {
					flags[args[i]] = append(flags[args[i]], args[i+1])
				}
			}
			tools, permission := "Read,Glob,Grep", "plan"
			if mode == "scoped-write" {
				tools, permission = "Read,Glob,Grep,Edit,Write", "acceptEdits"
				if !strings.Contains(strings.Join(args, " "), "--restricted") || len(flags["--disallowedTools"]) != 0 {
					t.Fatal("writer contract changed")
				}
			} else {
				if len(flags["--disallowedTools"]) != 1 {
					t.Fatal("missing or ambiguous readonly deny list")
				}
				denied := "," + flags["--disallowedTools"][0] + ","
				for _, name := range []string{"Agent", "Task", "Bash", "Edit", "Write", "NotebookEdit", "mcp__ivoai-memory__memory_write_page", "mcp__ivoai-memory__memory_delete_page", "mcp__ivoai-memory__memory_feedback"} {
					if !strings.Contains(denied, ","+name+",") {
						t.Fatalf("readonly worker can request %s", name)
					}
				}
			}
			if !reflect.DeepEqual(flags["--tools"], []string{tools}) || !reflect.DeepEqual(flags["--permission-mode"], []string{permission}) {
				t.Fatalf("unexpected builtin catalog or permissions: %v", flags)
			}
			if mode == "scoped-readonly" {
				projected, err := (Adapter{}).isolateScopedMCPs(context.Background(), "", request, args)
				if err != nil {
					t.Fatal(err)
				}
				joined := strings.Join(projected, "\n")
				for _, want := range []string{"--strict-mcp-config", "--mcp-config", "--allowedTools", "__read_evidence", "IVOAI_WORKER_MCP_TOKEN_A", "--tools\nRead,Glob,Grep\n"} {
					if !strings.Contains(joined, want) {
						t.Fatalf("builtin restriction removed scoped MCP boundary %q", want)
					}
				}
				if strings.Contains(joined, "fixture-worker-private-capability") {
					t.Fatal("unexpected secret or personal configuration in arguments")
				}
			}
		})
	}
}
