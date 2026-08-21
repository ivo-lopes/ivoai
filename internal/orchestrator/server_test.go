package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workers"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type rufloRunner struct{ calls [][]string }

func (r *rufloRunner) LookPath(string) (string, error) { return "/managed/ruflo", nil }
func (r *rufloRunner) Run(_ context.Context, _ string, args []string, _ platform.RunOptions) (platform.Result, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	if len(args) > 1 && args[0] == "task" && args[1] == "create" {
		return platform.Result{Stdout: "task-fixture-123"}, nil
	}
	return platform.Result{}, nil
}

func TestLocalBridgeDelegatesToOfficialWorkerAndPersistsMetadataOnly(t *testing.T) {
	root := t.TempDir()
	store := session.Store{Root: filepath.Join(root, "sessions")}
	id, _ := session.NewID()
	now := time.Now().UTC()
	value := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeOrchestrated, PrimaryExecutor: "codex", WorkingDirectory: root, PrimaryModel: session.UnknownModel(), RufloEnabled: true, RufloHealthy: true, RufloSafeMode: true, SwarmID: "swarm-fixture", SwarmState: "active", Workers: []session.Worker{}, MaxWorkers: 2, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected", State: session.StateRunning}
	if err := store.Create(value); err != nil {
		t.Fatal(err)
	}
	codex := executable(t, root, "codex", `#!/bin/sh
result=""
previous=""
for arg in "$@"; do
  [ "$previous" = "--output-last-message" ] && result="$arg"
  previous="$arg"
done
cat >/dev/null
printf 'bounded worker answer' > "$result"
`)
	runner := &rufloRunner{}
	server := &Server{Store: store, SessionID: id, Directory: root, RuntimeDir: filepath.Join(root, "runtime"), ReviewExecutor: "claude", Adapter: workers.Adapter{Runner: platform.ExecRunner{}, CodexPath: codex}, Control: orchestration.ControlPlane{Manager: orchestration.Manager{Runner: runner, Binary: "/managed/ruflo"}, RuntimeDir: filepath.Join(root, "runtime")}}
	server.initialize()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.protocolServer().Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	response, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "orchestration_delegate", Arguments: map[string]any{"role": "reviewer", "task": "sensitive prompt must not persist", "preferred_executor": "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Content[0].(*mcp.TextContent).Text, "bounded worker answer") {
		t.Fatalf("response=%#v", response)
	}
	updated, err := store.Get(id)
	if err != nil || len(updated.Workers) != 1 || updated.Workers[0].State != session.StateCompleted || updated.Workers[0].RufloTaskID == "" {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	body, _ := os.ReadFile(filepath.Join(store.Root, id+".json"))
	if strings.Contains(string(body), "sensitive prompt") || strings.Contains(string(body), "bounded worker answer") {
		t.Fatalf("prompt or result persisted: %s", body)
	}
	for _, call := range runner.calls {
		if strings.Contains(strings.Join(call, " "), "sensitive prompt") {
			t.Fatalf("prompt leaked to Ruflo: %#v", call)
		}
	}
}

func TestBridgeEnforcesWorkerLimitBeforeExecution(t *testing.T) {
	root := t.TempDir()
	store := session.Store{Root: filepath.Join(root, "sessions")}
	id, _ := session.NewID()
	now := time.Now().UTC()
	value := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeOrchestrated, PrimaryExecutor: "codex", WorkingDirectory: root, PrimaryModel: session.UnknownModel(), RufloEnabled: true, RufloHealthy: true, RufloSafeMode: true, SwarmID: "swarm-fixture", Workers: []session.Worker{{ID: "worker_0123456789abcdef0123456789abcdef", Role: "busy", Executor: "codex", Model: session.UnknownModel(), State: session.StateRunning, StartedAt: now}}, MaxWorkers: 1, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected", State: session.StateRunning}
	if err := store.Create(value); err != nil {
		t.Fatal(err)
	}
	server := &Server{Store: store, SessionID: id, Directory: root, RuntimeDir: filepath.Join(root, "runtime"), ReviewExecutor: "codex"}
	server.initialize()
	request := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: []byte(`{"role":"reviewer","task":"x","preferred_executor":"codex"}`)}}
	if _, err := server.delegate(context.Background(), request); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("worker limit error=%v", err)
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
