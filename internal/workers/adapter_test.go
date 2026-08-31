package workers

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestResultEvidencePreservesProviderStreamsDeterministically(t *testing.T) {
	result := Result{Text: "result\x00bytes", Stdout: "stdout\n", Stderr: "stderr\xff", ExitCode: 7}
	first, second := result.Evidence(), result.Evidence()
	if !bytes.Equal(first, second) {
		t.Fatal("worker evidence envelope is not deterministic")
	}
	for _, expected := range [][]byte{[]byte("result\x00bytes"), []byte("stdout\n"), []byte("stderr\xff"), []byte("exit-code:7"), []byte("truncated:false")} {
		if !bytes.Contains(first, expected) {
			t.Fatalf("evidence is missing %q", expected)
		}
	}
}

func TestCodexAdapterUsesOfficialExecAndDoesNotExposeProviderKeys(t *testing.T) {
	root := t.TempDir()
	argsFile := filepath.Join(root, "args")
	envFile := filepath.Join(root, "env")
	codex := executable(t, root, "codex", `#!/bin/sh
if [ "$1" = "mcp" ]; then
  printf '%s' '[{"name":"external-write"},{"name":"ivoai-memory"},{"name":"ivoai-context"}]'
  exit 0
fi
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
	adapter := Adapter{Runner: platform.ExecRunner{}, CodexPath: codex, KnowledgeServers: testKnowledgeServers()}
	var observation Observation
	result, err := adapter.Run(context.Background(), Request{Executor: "codex", Task: "review safely", Model: "fixture-model", Directory: root, Runtime: filepath.Join(root, "runtime")}, func(value Observation) { observation = value })
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "worker result" || result.Model.Source != session.ModelArgument || observation.PID <= 0 || observation.HeadroomUsed {
		t.Fatalf("result=%+v observation=%+v", result, observation)
	}
	args, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(args), `mcp_servers."external-write".enabled=false`) || !strings.Contains(string(args), `mcp_servers."ivoai-memory".enabled_tools=["memory_query","memory_recent","memory_read_page","memory_status"]`) || !strings.Contains(string(args), "exec\n--sandbox\nread-only\n--json\n--output-last-message\n") || !strings.Contains(string(args), "--model\nfixture-model\n-\n") {
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
	adapter := Adapter{Runner: platform.ExecRunner{}, ClaudePath: claude, KnowledgeServers: testKnowledgeServers()}
	result, err := adapter.Run(context.Background(), Request{Executor: "claude", Task: "review", Directory: root, Runtime: filepath.Join(root, "runtime")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "review complete" || result.Model.Source != session.ModelUnknown {
		t.Fatalf("result=%+v", result)
	}
	args, _ := os.ReadFile(argsFile)
	if !strings.HasPrefix(string(args), "--strict-mcp-config\n--mcp-config\n") || !strings.Contains(string(args), `"ivoai-memory"`) || !strings.Contains(string(args), "mcp__ivoai-memory__memory_write_page") || !strings.Contains(string(args), "--permission-mode\nplan\n") || !strings.Contains(string(args), "--print\n--output-format\njson\n") {
		t.Fatalf("unexpected Claude argv: %q", args)
	}
	assertResearchPriority(t, string(args))
	environment, _ := os.ReadFile(envFile)
	if !strings.Contains(string(environment), "DISABLE_AUTOUPDATER=1") {
		t.Fatal("Claude worker did not receive update guard")
	}
}

func testKnowledgeServers() map[string]config.MCPServer {
	return map[string]config.MCPServer{
		"ivoai-memory":  {URL: "https://ai.example.com/v1/memory/mcp", Enabled: true, Kind: "memory"},
		"ivoai-context": {URL: "https://ai.example.com/v1/mcp/context", Enabled: true, Kind: "context"},
	}
}

func TestCodexMCPIsolationFailsClosedOnUnstructuredInventory(t *testing.T) {
	root := t.TempDir()
	codex := executable(t, root, "codex", "#!/bin/sh\nprintf 'not-json'\n")
	adapter := Adapter{Runner: platform.ExecRunner{}, CodexPath: codex, KnowledgeServers: testKnowledgeServers()}
	_, err := adapter.Run(context.Background(), Request{Executor: "codex", Task: "review", Directory: root, Runtime: filepath.Join(root, "runtime")}, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid structured server inventory") {
		t.Fatalf("unexpected isolation error: %v", err)
	}
}

func TestAuthoritativeWorkerKnowledgeBypassesHeadroom(t *testing.T) {
	root := t.TempDir()
	headroomMarker := filepath.Join(root, "headroom-invoked")
	codex := executable(t, root, "codex", `#!/bin/sh
if [ "$1" = "mcp" ]; then printf '%s' '[{"name":"ivoai-memory"}]'; exit 0; fi
result=""
previous=""
for arg in "$@"; do
  [ "$previous" = "--output-last-message" ] && result="$arg"
  previous="$arg"
done
cat >/dev/null
printf 'exact worker result' > "$result"
`)
	headroom := executable(t, root, "headroom", "#!/bin/sh\nprintf invoked > "+headroomMarker+"\nexit 99\n")
	adapter := Adapter{Runner: platform.ExecRunner{}, CodexPath: codex, HeadroomPath: headroom, HeadroomEnabled: true, KnowledgeServers: map[string]config.MCPServer{"ivoai-memory": {URL: "http://127.0.0.1:1234/mcp/memory", Enabled: true, Kind: "memory"}}}
	result, err := adapter.Run(context.Background(), Request{Executor: "codex", Task: "read authoritative memory", Directory: root, Runtime: filepath.Join(root, "runtime")}, nil)
	if err != nil || result.Text != "exact worker result" || result.HeadroomUsed {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(headroomMarker); !os.IsNotExist(err) {
		t.Fatalf("Headroom touched authoritative worker path: %v", err)
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

func TestWorkerArgumentsApplyOnlyExplicitVerifiedEffortAndSharedBrief(t *testing.T) {
	codex, _, err := workerArgs(Request{Executor: "codex", Effort: "low", SharedContextBrief: `{"facts":["one"]}`, Runtime: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(codex, "\n")
	if !strings.Contains(joined, `model_reasoning_effort="low"`) || !strings.Contains(joined, "SharedContextBrief") {
		t.Fatalf("Codex args missing effort or brief: %q", joined)
	}
	claude, _, err := workerArgs(Request{Executor: "claude", Effort: "high", Runtime: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(claude, "\n"), "--effort\nhigh") {
		t.Fatalf("Claude args missing effort: %#v", claude)
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
