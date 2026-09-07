package opencodebridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPartialResponseRequiresSuccessfulCompletion(t *testing.T) {
	for _, tc := range []struct{ name, end, class string }{
		{"missing", "", "executor_stream_incomplete"},
		{"failed", `printf '%s\n' '{"type":"turn.failed","error":{"message":"Failed to authenticate: OAuth session expired private-token"}}'`, "executor_auth_failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := fixtureExecutable(t, root, "codex", "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"thread_fixture\"}' '{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"partial private-prompt\"}}'\n"+tc.end+"\n")
			result, err := (CLIRunner{Codex: ExecutorSpec{Path: path, Dir: root}}).Run(context.Background(), ExecutorRequest{Executor: "codex"}, func(string) error { return nil })
			if FailureClass(err) != tc.class || result.Trace == nil || !result.Trace.FinalResponse {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			encoded, _ := json.Marshal(result.Trace)
			if strings.Contains(string(encoded), "private-") {
				t.Fatal("trace leaked consumer content")
			}
		})
	}
}

func TestMalformedStreamTerminatesWritingChild(t *testing.T) {
	root := t.TempDir()
	path := fixtureExecutable(t, root, "codex", "#!/bin/sh\nprintf '{invalid-json\\n'\nwhile :; do printf '%4096s' data; done\n")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := (CLIRunner{Codex: ExecutorSpec{Path: path, Dir: root}}).Run(ctx, ExecutorRequest{Executor: "codex"}, func(string) error { return nil })
	if FailureClass(err) != "executor_stream_incomplete" || ctx.Err() != nil {
		t.Fatalf("stream did not terminate promptly: %v / %v", err, ctx.Err())
	}
}

func TestClaudeMCPTracePreservesFailureWithoutPayload(t *testing.T) {
	root := t.TempDir()
	path := fixtureExecutable(t, root, "claude", `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"claude_fixture"}'
printf '%s\n' '{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","id":"tool_one","name":"mcp__ivoai-memory__memory_read_page","input":{"secret":"private-input"}}}}'
printf '%s\n' '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool_one","is_error":true,"content":"Unexpected response type private-result"}]}}'
printf '%s\n' '{"type":"result","is_error":true,"result":"Unexpected response type private-result"}'
`)
	result, err := (CLIRunner{Claude: ExecutorSpec{Path: path, Dir: root}}).Run(context.Background(), ExecutorRequest{Executor: "claude"}, func(string) error { return nil })
	if FailureClass(err) != "mcp_result_decode_failure" || result.Trace == nil || len(result.Trace.Tools) != 1 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	tool := result.Trace.Tools[0]
	if tool.Name != "memory_read_page" || tool.Failure != "mcp_result_decode_failure" {
		t.Fatalf("tool=%+v", tool)
	}
	encoded, _ := json.Marshal(result.Trace)
	if strings.Contains(string(encoded), "private-") {
		t.Fatal("trace leaked MCP payload")
	}
}
