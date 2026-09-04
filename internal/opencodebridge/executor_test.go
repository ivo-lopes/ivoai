package opencodebridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unicode/utf8"
)

func TestExecutorProcessGroupDiesWithIVOAI(t *testing.T) {
	attributes := executorProcessAttributes()
	if attributes == nil || !attributes.Setpgid || attributes.Pdeathsig != syscall.SIGKILL {
		t.Fatalf("unsafe executor process attributes: %+v", attributes)
	}
}

func TestCLIRunnerParsesCodexAndResumesWithoutCredentials(t *testing.T) {
	root := t.TempDir()
	argsPath := filepath.Join(root, "args")
	stdinPath := filepath.Join(root, "stdin")
	script := fixtureExecutable(t, root, "codex", `#!/bin/sh
printf '%s\n' "$*" >> "$ARGS_PATH"
cat > "$STDIN_PATH"
printf '%s\n' '{"type":"thread.started","thread_id":"thread_fixture"}'
printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"hello"}}'
`)
	runner := CLIRunner{Codex: ExecutorSpec{Path: script, Env: []string{"PATH=/usr/bin:/bin", "ARGS_PATH=" + argsPath, "STDIN_PATH=" + stdinPath}, Dir: root}}
	var output strings.Builder
	result, err := runner.Run(context.Background(), ExecutorRequest{Executor: "codex", Model: "gpt-fixture", Effort: "high", SelectionMode: "explicit", Prompt: "fixture prompt"}, func(value string) error { output.WriteString(value); return nil })
	if err != nil || result.ExecutorSessionID != "thread_fixture" || output.String() != "hello" {
		t.Fatalf("result=%+v output=%q err=%v", result, output.String(), err)
	}
	_, err = runner.Run(context.Background(), ExecutorRequest{Executor: "codex", Model: "gpt-fixture", Effort: "low", SelectionMode: "explicit", Prompt: "next", ExecutorSessionID: result.ExecutorSessionID}, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(argsPath)
	if !strings.Contains(string(args), `exec --json --color never --model gpt-fixture -c model_reasoning_effort="high"`) || !strings.Contains(string(args), `exec resume --json --model gpt-fixture -c model_reasoning_effort="low" thread_fixture`) {
		t.Fatalf("args=%q", args)
	}
	stdin, _ := os.ReadFile(stdinPath)
	if string(stdin) != "next" {
		t.Fatalf("stdin=%q", stdin)
	}
}

func TestCLIRunnerParsesClaudeStreamingAndSession(t *testing.T) {
	root := t.TempDir()
	argsPath := filepath.Join(root, "args")
	script := fixtureExecutable(t, root, "claude", `#!/bin/sh
printf '%s\n' "$*" > "$ARGS_PATH"
printf '%s\n' '{"type":"system","subtype":"init","session_id":"claude_fixture"}'
printf '%s\n' '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"stream "}},"session_id":"claude_fixture"}'
printf '%s\n' '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"ok"}},"session_id":"claude_fixture"}'
`)
	runner := CLIRunner{Claude: ExecutorSpec{Path: script, Env: []string{"PATH=/usr/bin:/bin", "ARGS_PATH=" + argsPath}, Dir: root}}
	var output strings.Builder
	result, err := runner.Run(context.Background(), ExecutorRequest{Executor: "claude", Model: "sonnet", Effort: "max", SelectionMode: "explicit", Prompt: "fixture"}, func(value string) error { output.WriteString(value); return nil })
	if err != nil || result.ExecutorSessionID != "claude_fixture" || output.String() != "stream ok" {
		t.Fatalf("result=%+v output=%q err=%v", result, output.String(), err)
	}
	args, _ := os.ReadFile(argsPath)
	if !strings.Contains(string(args), "--model sonnet --effort max") {
		t.Fatalf("claude selection did not reach official CLI: %q", args)
	}
}

func TestCLIRunnerPreservesSafeActivityWithoutReplayingToolInputs(t *testing.T) {
	root := t.TempDir()
	script := fixtureExecutable(t, root, "codex", `#!/bin/sh
printf '%s\n' '{"type":"thread.started","thread_id":"thread_fixture"}'
printf '%s\n' '{"type":"item.completed","item":{"type":"command_execution","command":"printf secret","aggregated_output":"secret-output","status":"completed"}}'
printf '%s\n' '{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"memory_read_page\u001b[2J","arguments":{"token":"secret"},"status":"completed"}}'
printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"done\u001b[2J"}}'
`)
	runner := CLIRunner{Codex: ExecutorSpec{Path: script, Env: []string{"PATH=/usr/bin:/bin"}, Dir: root}}
	var output strings.Builder
	_, err := runner.Run(context.Background(), ExecutorRequest{Executor: "codex", Prompt: "fixture"}, func(value string) error { output.WriteString(value); return nil })
	if err != nil {
		t.Fatal(err)
	}
	value := output.String()
	for _, expected := range []string{"[command · completed]", "[MCP tool: memory_read_page[2J · completed]", "done[2J"} {
		if !strings.Contains(value, expected) {
			t.Fatalf("output %q does not contain %q", value, expected)
		}
	}
	for _, secret := range []string{"printf secret", "secret-output", `"token"`} {
		if strings.Contains(value, secret) {
			t.Fatalf("activity stream leaked %q: %q", secret, value)
		}
	}
}

func TestExecutorTextRemovesBidirectionalTerminalControls(t *testing.T) {
	value := safeExecutorText("safe\u202esecret\u2066text\u001b[2J", 128)
	if value != "safesecrettext[2J" {
		t.Fatalf("unsafe rendered executor text: %q", value)
	}
}

func TestExecutorTextTruncationPreservesUTF8(t *testing.T) {
	value := safeExecutorText("açúcar", 2)
	if value != "a" || !utf8.ValidString(value) {
		t.Fatalf("UTF-8 truncation was invalid: %q", value)
	}
}

func TestCLIRunnerClassifiesAuthenticationWithoutLeakingStderr(t *testing.T) {
	root := t.TempDir()
	script := fixtureExecutable(t, root, "codex", "#!/bin/sh\necho 'please log in token=private' >&2\nexit 7\n")
	runner := CLIRunner{Codex: ExecutorSpec{Path: script, Env: []string{"PATH=/usr/bin:/bin"}, Dir: root}}
	_, err := runner.Run(context.Background(), ExecutorRequest{Executor: "codex", Prompt: "fixture"}, func(string) error { return nil })
	if FailureClass(err) != "executor_auth_failure" || strings.Contains(err.Error(), "private") {
		t.Fatalf("unsafe or incorrect failure: %v", err)
	}
}

func TestCLIRunnerClassifiesClaudeStructuredAuthenticationFailure(t *testing.T) {
	root := t.TempDir()
	script := fixtureExecutable(t, root, "claude", `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"claude_fixture"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":true,"result":"Your organization disabled subscription access; use an Anthropic API key secret-value"}'
exit 1
`)
	runner := CLIRunner{Claude: ExecutorSpec{Path: script, Env: []string{"PATH=/usr/bin:/bin"}, Dir: root}}
	_, err := runner.Run(context.Background(), ExecutorRequest{Executor: "claude", Prompt: "fixture"}, func(string) error { return nil })
	if FailureClass(err) != "executor_auth_failure" || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("unsafe or incorrect structured failure: %v", err)
	}
}

func TestCLIRunnerPreservesClaudeToolLifecycle(t *testing.T) {
	root := t.TempDir()
	script := fixtureExecutable(t, root, "claude", `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"claude_fixture"}'
printf '%s\n' '{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"Read","input":{"file_path":"private"}}}}'
printf '%s\n' '{"type":"user","message":{"content":[{"type":"tool_result","content":"private result"}]}}'
printf '%s\n' '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"done"}}}'
`)
	runner := CLIRunner{Claude: ExecutorSpec{Path: script, Env: []string{"PATH=/usr/bin:/bin"}, Dir: root}}
	var output strings.Builder
	_, err := runner.Run(context.Background(), ExecutorRequest{Executor: "claude", Prompt: "fixture"}, func(value string) error { output.WriteString(value); return nil })
	if err != nil {
		t.Fatal(err)
	}
	value := output.String()
	if !strings.Contains(value, "[tool: Read · started]") || !strings.Contains(value, "[tool · completed]") {
		t.Fatalf("tool lifecycle missing: %q", value)
	}
	if strings.Contains(value, "private") {
		t.Fatalf("tool payload leaked: %q", value)
	}
}

func fixtureExecutable(t *testing.T, root, name, body string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
