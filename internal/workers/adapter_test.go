package workers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestCodexAdapterUsesOfficialExecAndDoesNotExposeProviderKeys(t *testing.T) {
	root := t.TempDir()
	argsFile := filepath.Join(root, "args")
	envFile := filepath.Join(root, "env")
	codex := executable(t, root, "codex", `#!/bin/sh
printf '%s\n' "$@" > "`+argsFile+`"
env > "`+envFile+`"
result=""
previous=""
for arg in "$@"; do
  [ "$previous" = "--output-last-message" ] && result="$arg"
  previous="$arg"
done
cat >/dev/null
printf 'worker result' > "$result"
`)
	t.Setenv("OPENAI_API_KEY", "must-not-leak")
	adapter := Adapter{Runner: platform.ExecRunner{}, CodexPath: codex}
	var observation Observation
	result, err := adapter.Run(context.Background(), Request{Executor: "codex", Task: "review safely", Model: "fixture-model", Directory: root, Runtime: filepath.Join(root, "runtime")}, func(value Observation) { observation = value })
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "worker result" || result.Model.Source != session.ModelArgument || observation.PID <= 0 || observation.HeadroomUsed {
		t.Fatalf("result=%+v observation=%+v", result, observation)
	}
	args, _ := os.ReadFile(argsFile)
	if !strings.HasPrefix(string(args), "-c\ndeveloper_instructions=") || !strings.Contains(string(args), "exec\n--json\n--output-last-message\n") || !strings.Contains(string(args), "--model\nfixture-model\n-\n") {
		t.Fatalf("unexpected Codex argv: %q", args)
	}
	assertResearchPriority(t, string(args))
	environment, _ := os.ReadFile(envFile)
	if strings.Contains(string(environment), "OPENAI_API_KEY") {
		t.Fatal("provider credential reached worker")
	}
}

func TestClaudeAdapterParsesStructuredResultAndSetsUpdateGuard(t *testing.T) {
	root := t.TempDir()
	envFile := filepath.Join(root, "env")
	argsFile := filepath.Join(root, "args")
	claude := executable(t, root, "claude", `#!/bin/sh
env > "`+envFile+`"
printf '%s\n' "$@" > "`+argsFile+`"
cat >/dev/null
printf '%s' '{"type":"result","result":"review complete"}'
`)
	adapter := Adapter{Runner: platform.ExecRunner{}, ClaudePath: claude}
	result, err := adapter.Run(context.Background(), Request{Executor: "claude", Task: "review", Directory: root, Runtime: filepath.Join(root, "runtime")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "review complete" || result.Model.Source != session.ModelUnknown {
		t.Fatalf("result=%+v", result)
	}
	args, _ := os.ReadFile(argsFile)
	if !strings.HasPrefix(string(args), "--append-system-prompt\n") || !strings.Contains(string(args), "--print\n--output-format\njson\n") {
		t.Fatalf("unexpected Claude argv: %q", args)
	}
	assertResearchPriority(t, string(args))
	environment, _ := os.ReadFile(envFile)
	if !strings.Contains(string(environment), "DISABLE_AUTOUPDATER=1") {
		t.Fatal("Claude worker did not receive update guard")
	}
}

func assertResearchPriority(t *testing.T, value string) {
	t.Helper()
	memory := strings.Index(value, "(1) ivoai-memory")
	context := strings.Index(value, "(2) ivoai-context")
	web := strings.Index(value, "(3) web")
	if memory < 0 || context <= memory || web <= context {
		t.Fatalf("worker research order is not memory -> context -> web: %q", value)
	}
}

func TestAdapterRejectsUnsafeExecutorAndOversizedTask(t *testing.T) {
	root := t.TempDir()
	adapter := Adapter{Runner: platform.ExecRunner{}}
	if _, err := adapter.Run(context.Background(), Request{Executor: "/bin/sh", Task: "x", Directory: root, Runtime: root}, nil); err == nil {
		t.Fatal("unsafe executor accepted")
	}
	if _, err := adapter.Run(context.Background(), Request{Executor: "codex", Task: strings.Repeat("x", MaxTaskBytes+1), Directory: root, Runtime: root}, nil); err == nil {
		t.Fatal("oversized task accepted")
	}
}

func executable(t *testing.T, directory, name, body string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
