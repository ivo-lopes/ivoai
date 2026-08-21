package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

type recordingRunner struct {
	calls [][]string
}

func (r *recordingRunner) LookPath(name string) (string, error) { return "/managed/" + name, nil }
func (r *recordingRunner) Run(_ context.Context, command string, args []string, _ platform.RunOptions) (platform.Result, error) {
	r.calls = append(r.calls, append([]string{command}, args...))
	if len(args) == 1 && args[0] == "--version" {
		return platform.Result{Stdout: "ruflo v3.38.12\n"}, nil
	}
	return platform.Result{}, nil
}

func TestSafeProfileFiltersProvidersExecutionAndDurableMemory(t *testing.T) {
	t.Setenv("IVOAI_TEST_MODE", "")
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "codex"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "claude"))
	runner := &recordingRunner{}
	manager := Manager{Runner: runner, Binary: "/managed/ruflo", CodexBinary: "/managed/codex", ClaudeBinary: "/managed/claude", ProfileDir: t.TempDir()}
	if err := manager.Configure(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	status := manager.Inspect(context.Background())
	if !status.Installed || !status.SafeMode || status.ProviderExecution || status.DurableMemory {
		t.Fatalf("unexpected status %#v", status)
	}
	wrapper, err := os.ReadFile(manager.wrapperPath())
	if err != nil {
		t.Fatal(err)
	}
	text := string(wrapper)
	for _, required := range []string{"unset ANTHROPIC_API_KEY OPENAI_API_KEY", "RUFLO_PROVIDER=ivoai-disabled", "CLAUDE_FLOW_MEMORY_BACKEND=memory", "CLAUDE_FLOW_MCP_TOOLS="} {
		if !strings.Contains(text, required) {
			t.Fatalf("wrapper missing %q:\n%s", required, text)
		}
	}
	if strings.Contains(strings.Join(safeTools, ","), "memory") || strings.Contains(strings.Join(safeTools, ","), "execute") || strings.Contains(strings.Join(safeTools, ","), "workflow") {
		t.Fatalf("unsafe tool allowlist %#v", safeTools)
	}
	if strings.Contains(strings.Join(safeTools, ","), "agent_spawn") || strings.Contains(strings.Join(safeTools, ","), "agent_terminate") {
		t.Fatalf("provider-capable agent lifecycle leaked into safe tools: %#v", safeTools)
	}
	joined := make([]string, 0, len(runner.calls))
	for _, call := range runner.calls {
		joined = append(joined, strings.Join(call, " "))
	}
	registrations := strings.Join(joined, "\n")
	if !strings.Contains(registrations, "/managed/codex mcp add "+serverName) || !strings.Contains(registrations, "/managed/claude mcp add --scope user") {
		t.Fatalf("official MCP registrations missing:\n%s", registrations)
	}
	if err := manager.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(manager.wrapperPath()); !os.IsNotExist(err) {
		t.Fatalf("wrapper retained after disable: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(manager.profilePath())); err != nil {
		t.Fatal(err)
	}
}

func TestInspectRejectsTamperedProfile(t *testing.T) {
	t.Setenv("IVOAI_TEST_MODE", "1")
	manager := Manager{Runner: &recordingRunner{}, Binary: "/managed/ruflo", ProfileDir: t.TempDir()}
	if err := manager.Configure(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manager.profilePath(), []byte(`{"version":1,"tools":["agent_execute"],"provider_execution":false,"durable_memory":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if status := manager.Inspect(context.Background()); status.SafeMode {
		t.Fatalf("tampered profile reported safe: %#v", status)
	}
}
