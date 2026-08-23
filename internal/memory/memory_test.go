package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

type captureRunner struct {
	calls []struct {
		args []string
		env  []string
	}
	failContaining string
}

func (r *captureRunner) LookPath(string) (string, error) { return "/bin/ai-memory", nil }
func (r *captureRunner) Run(_ context.Context, _ string, args []string, o platform.RunOptions) (platform.Result, error) {
	r.calls = append(r.calls, struct {
		args []string
		env  []string
	}{append([]string{}, args...), append([]string{}, o.Env...)})
	if r.failContaining != "" && strings.Contains(strings.Join(args, " "), r.failContaining) {
		return platform.Result{}, errors.New("fixture failure")
	}
	return platform.Result{Stdout: "{}"}, nil
}

func TestConfigureHooksUsesHookBaseWithoutInstallingMCP(t *testing.T) {
	r := &captureRunner{}
	m := Manager{Runner: r, Binary: "/bin/ai-memory"}
	if err := m.ConfigureHooks(context.Background(), "https://ai.example.com", "secret-token"); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 2 {
		t.Fatalf("calls %d", len(r.calls))
	}
	for _, call := range r.calls {
		joined := strings.Join(call.args, " ")
		if !strings.HasPrefix(joined, "install-hooks ") || strings.Contains(joined, "install-mcp") {
			t.Fatalf("unexpected hook-only command %q", joined)
		}
		if !strings.Contains(joined, "--project-strategy repo-root") {
			t.Fatalf("hook project scope is not stable across checkout aliases: %q", joined)
		}
		environment := strings.Join(call.env, " ")
		if !strings.Contains(environment, "AI_MEMORY_SERVER_URL=https://ai.example.com") || strings.Contains(environment, "AI_MEMORY_AUTH_TOKEN") || strings.Contains(environment, "secret-token") {
			t.Fatalf("wrong environment %q", environment)
		}
	}
}

func TestDisableUsesOwnedUpstreamUninstaller(t *testing.T) {
	r := &captureRunner{}
	if err := (Manager{Runner: r, Binary: "/bin/ai-memory"}).Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 || strings.Join(r.calls[0].args, " ") != "uninstall --apply" {
		t.Fatalf("unexpected calls %#v", r.calls)
	}
}

func TestConfigureReportsHookFailure(t *testing.T) {
	r := &captureRunner{failContaining: "install-hooks"}
	err := (Manager{Runner: r, Binary: "/bin/ai-memory"}).Configure(context.Background(), "", "")
	if err == nil || !strings.Contains(err.Error(), "hooks") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestConfigureUsesIdempotentUpstreamCommandsAndSecretEnvironment(t *testing.T) {
	r := &captureRunner{}
	m := Manager{Runner: r}
	if err := m.Configure(context.Background(), "https://ai.example.com", "secret-token"); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 4 {
		t.Fatalf("calls %d", len(r.calls))
	}
	for _, call := range r.calls {
		joined := strings.Join(call.args, " ")
		if strings.Contains(joined, "secret-token") {
			t.Fatal("token exposed in argv")
		}
		if !strings.Contains(joined, "--apply") {
			t.Fatalf("not applying: %s", joined)
		}
		if strings.Contains(joined, "install-hooks") && !strings.Contains(joined, "--project-strategy repo-root") {
			t.Fatalf("hook project scope is not repository-stable: %s", joined)
		}
		environment := strings.Join(call.env, " ")
		if strings.Contains(joined, "install-mcp") && !strings.Contains(environment, "AI_MEMORY_AUTH_TOKEN=secret-token") {
			t.Fatal("token missing from transient MCP installer environment")
		}
		if strings.Contains(joined, "install-hooks") && strings.Contains(environment, "secret-token") {
			t.Fatal("token supplied to persistent hook installer")
		}
	}
}
