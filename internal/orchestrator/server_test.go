package orchestrator

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workers"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type rufloRunner struct{ calls [][]string }

type bridgeProbe struct{ value quota.ProviderQuota }

func (p bridgeProbe) Probe(context.Context) (quota.ProviderQuota, error) { return p.value, nil }

func (r *rufloRunner) LookPath(string) (string, error) { return "/managed/ruflo", nil }
func (r *rufloRunner) Run(_ context.Context, _ string, args []string, _ platform.RunOptions) (platform.Result, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	if len(args) > 1 && args[0] == "task" && args[1] == "create" {
		return platform.Result{Stdout: "task-fixture-123"}, nil
	}
	return platform.Result{}, nil
}

type fixedWorkerAdapter struct {
	result workers.Result
	err    error
}

func (a fixedWorkerAdapter) Run(_ context.Context, _ workers.Request, observe func(workers.Observation)) (workers.Result, error) {
	if observe != nil {
		observe(workers.Observation{})
	}
	return a.result, a.err
}

func TestWorkingContextKeepsLargeRawOutputOutOfPrimaryAndRecoversExactEvidence(t *testing.T) {
	root := t.TempDir()
	store := session.Store{Root: filepath.Join(root, "sessions")}
	id, _ := session.NewID()
	now := time.Now().UTC()
	value := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeOrchestrated, PrimaryExecutor: "codex", WorkingDirectory: root, PrimaryModel: session.UnknownModel(), RufloEnabled: true, RufloHealthy: true, RufloSafeMode: true, SwarmID: "swarm-fixture", Workers: []session.Worker{}, MaxWorkers: 2, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected", State: session.StateRunning}
	if err := store.Create(value); err != nil {
		t.Fatal(err)
	}
	artifactStore, err := workingcontext.NewLocalStore(filepath.Join(root, "cache", "working-context"), workingcontext.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tail := "UNIQUE-RAW-TAIL-MUST-NOT-ENTER-PRIMARY"
	raw := strings.Repeat("bounded evidence line\n", 150_000) + tail
	server := &Server{Store: store, SessionID: id, Directory: root, RuntimeDir: filepath.Join(root, "runtime"), ReviewExecutor: "codex", Adapter: fixedWorkerAdapter{result: workers.Result{Text: raw}}, Control: fakeLifecycle{}, WorkingContext: artifactStore}
	server.initialize()
	response, err := server.delegate(context.Background(), toolRequest(map[string]any{"role": "reviewer", "task": "review", "preferred_executor": "codex"}))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(response.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(tail)) || len(encoded) > workingcontext.MaxSummaryBytes*2 {
		t.Fatalf("raw output entered bounded response: bytes=%d", len(encoded))
	}
	updated, err := store.Get(id)
	if err != nil || len(updated.Workers) != 1 || len(updated.Workers[0].ResultRefs) != 1 {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	ref := updated.Workers[0].ResultRefs[0].Artifact
	readResult, err := server.artifactRead(context.Background(), toolRequest(map[string]any{"artifact_id": ref.ID}))
	if err != nil {
		t.Fatal(err)
	}
	readMap := readResult.StructuredContent.(map[string]any)
	recovered, err := base64.StdEncoding.DecodeString(readMap["data"].(string))
	if err != nil || !bytes.Contains(recovered, []byte(tail)) {
		t.Fatalf("exact recovery failed: bytes=%d err=%v", len(recovered), err)
	}
	rangeResult, err := server.artifactReadRange(context.Background(), toolRequest(map[string]any{"artifact_id": ref.ID, "offset": 0, "length": 32}))
	if err != nil {
		t.Fatal(err)
	}
	rangeMap := rangeResult.StructuredContent.(map[string]any)
	part, err := base64.StdEncoding.DecodeString(rangeMap["data"].(string))
	if err != nil || len(part) != 32 || !bytes.Equal(part, recovered[:32]) {
		t.Fatalf("range recovery failed: %q err=%v", part, err)
	}
	sessionBody, _ := os.ReadFile(filepath.Join(store.Root, id+".json"))
	if bytes.Contains(sessionBody, []byte(tail)) || bytes.Contains(sessionBody, []byte("bounded evidence line")) {
		t.Fatal("raw worker content persisted in session metadata")
	}
	if _, _, err := artifactStore.Read(context.Background(), workingcontext.Ownership{SessionID: "sess_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, ref.ID); err == nil {
		t.Fatal("cross-session recovery accepted")
	}

	// A replacement bridge process (including a Codex/Claude primary failover)
	// rehydrates only bounded ResultRefs from session metadata. Exact evidence
	// remains in the same private store and is not duplicated into the handoff.
	replacement := &Server{Store: store, SessionID: id, Directory: root, RuntimeDir: filepath.Join(root, "runtime"), WorkingContext: artifactStore}
	replacement.initialize()
	workerID := updated.Workers[0].ID
	rehydrated, err := replacement.result(context.Background(), toolRequest(map[string]any{"worker_id": workerID}))
	if err != nil {
		t.Fatal(err)
	}
	rehydratedBody, _ := json.Marshal(rehydrated.StructuredContent)
	if bytes.Contains(rehydratedBody, []byte(tail)) || !bytes.Contains(rehydratedBody, []byte(ref.ID)) {
		t.Fatalf("unsafe failover result: %s", rehydratedBody)
	}
	recoveredAfterFailover, err := replacement.artifactReadRange(context.Background(), toolRequest(map[string]any{"artifact_id": ref.ID, "offset": ref.Size - int64(len(tail)), "length": int64(len(tail))}))
	if err != nil {
		t.Fatal(err)
	}
	afterMap := recoveredAfterFailover.StructuredContent.(map[string]any)
	afterBody, err := base64.StdEncoding.DecodeString(afterMap["data"].(string))
	if err != nil || string(afterBody) != tail {
		t.Fatalf("failover exact recovery=%q err=%v", afterBody, err)
	}
}

func TestWorkingContextPreservesFailedAndCancelledWorkerEvidence(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		err    error
		status workingcontext.ResultStatus
	}{
		{name: "failed", err: os.ErrPermission, status: workingcontext.ResultFailed},
		{name: "cancelled", err: context.Canceled, status: workingcontext.ResultCancelled},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			store := session.Store{Root: filepath.Join(root, "sessions")}
			id, _ := session.NewID()
			now := time.Now().UTC()
			value := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeOrchestrated, PrimaryExecutor: "codex", WorkingDirectory: root, PrimaryModel: session.UnknownModel(), RufloEnabled: true, RufloHealthy: true, RufloSafeMode: true, SwarmID: "swarm-fixture", Workers: []session.Worker{}, MaxWorkers: 2, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected", State: session.StateRunning}
			if err := store.Create(value); err != nil {
				t.Fatal(err)
			}
			artifactStore, err := workingcontext.NewLocalStore(filepath.Join(root, "working-context"), workingcontext.LocalOptions{})
			if err != nil {
				t.Fatal(err)
			}
			server := &Server{Store: store, SessionID: id, Directory: root, RuntimeDir: filepath.Join(root, "runtime"), ReviewExecutor: "codex", Adapter: fixedWorkerAdapter{result: workers.Result{Stderr: "test failure evidence", ExitCode: 9}, err: fixture.err}, Control: fakeLifecycle{}, WorkingContext: artifactStore}
			server.initialize()
			if _, err := server.delegate(context.Background(), toolRequest(map[string]any{"role": "reviewer", "task": "review", "preferred_executor": "codex"})); !errors.Is(err, fixture.err) {
				t.Fatalf("delegate error=%v", err)
			}
			updated, err := store.Get(id)
			if err != nil || len(updated.Workers) != 1 || len(updated.Workers[0].ResultRefs) != 1 {
				t.Fatalf("updated=%+v err=%v", updated, err)
			}
			server.mu.Lock()
			projected := server.results[updated.Workers[0].ID].Result
			server.mu.Unlock()
			if projected.Status != fixture.status || len(projected.ImportantErrors) == 0 || len(projected.Evidence) != 1 {
				t.Fatalf("projected=%+v", projected)
			}
			reader, _, err := artifactStore.Read(context.Background(), workingcontext.Ownership{SessionID: id}, projected.Evidence[0].Artifact.ID)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(reader)
			reader.Close()
			if !bytes.Contains(body, []byte("test failure evidence")) {
				t.Fatalf("failure evidence was not preserved: %q", body)
			}
		})
	}
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
if [ "$1" = "mcp" ]; then
  printf '[]'
  exit 0
fi
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

func TestAutomaticBridgeBlocksWorkerBeforeRufloWhenBothProvidersExhausted(t *testing.T) {
	root := t.TempDir()
	store, id := automaticBridgeSession(t, root)
	now := time.Now().UTC()
	exhausted := func(provider quota.Provider) quota.ProviderQuota {
		return quota.ProviderQuota{Provider: provider, Authenticated: true, HardLimitReached: true, Reason: "subscription exhausted", Source: "fixture", ObservedAt: now}
	}
	runner := &rufloRunner{}
	server := &Server{Store: store, SessionID: id, Directory: root, RuntimeDir: filepath.Join(root, "runtime"), ReviewExecutor: "codex", Control: orchestration.ControlPlane{Manager: orchestration.Manager{Runner: runner, Binary: "/managed/ruflo"}, RuntimeDir: filepath.Join(root, "runtime")}, Quota: &quota.Manager{Store: quota.Store{Root: filepath.Join(root, "quota")}, Probes: map[quota.Provider]quota.Probe{
		quota.ProviderCodex: bridgeProbe{exhausted(quota.ProviderCodex)}, quota.ProviderClaude: bridgeProbe{exhausted(quota.ProviderClaude)},
	}}}
	request := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: []byte(`{"role":"reviewer","task":"must not run","preferred_executor":"codex"}`)}}
	if _, err := server.delegate(context.Background(), request); err == nil {
		t.Fatal("exhausted providers accepted worker")
	}
	updated, _ := store.Get(id)
	if len(updated.Workers) != 0 || len(runner.calls) != 0 {
		t.Fatalf("work started before quota gate: workers=%+v calls=%+v", updated.Workers, runner.calls)
	}
}

func TestAutomaticBridgeRoutesExhaustedRequestedWorkerToAlternate(t *testing.T) {
	root := t.TempDir()
	store, id := automaticBridgeSession(t, root)
	now := time.Now().UTC()
	codex := quota.ProviderQuota{Provider: quota.ProviderCodex, Authenticated: true, HardLimitReached: true, Reason: "weekly exhausted", Source: "fixture", ObservedAt: now}
	claude := quota.ProviderQuota{Provider: quota.ProviderClaude, Authenticated: true, Eligible: true, Source: "fixture", ObservedAt: now}
	claudeBinary := executable(t, root, "claude", "#!/bin/sh\ncat >/dev/null\nprintf '%s' '{\"type\":\"result\",\"result\":\"alternate answer\"}'\n")
	runner := &rufloRunner{}
	server := &Server{Store: store, SessionID: id, Directory: root, RuntimeDir: filepath.Join(root, "runtime"), ReviewExecutor: "codex", Adapter: workers.Adapter{Runner: platform.ExecRunner{}, ClaudePath: claudeBinary}, Control: orchestration.ControlPlane{Manager: orchestration.Manager{Runner: runner, Binary: "/managed/ruflo"}, RuntimeDir: filepath.Join(root, "runtime")}, Quota: &quota.Manager{Store: quota.Store{Root: filepath.Join(root, "quota")}, Probes: map[quota.Provider]quota.Probe{
		quota.ProviderCodex: bridgeProbe{codex}, quota.ProviderClaude: bridgeProbe{claude},
	}}}
	server.initialize()
	request := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: []byte(`{"role":"reviewer","task":"review","preferred_executor":"codex"}`)}}
	if _, err := server.delegate(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	updated, _ := store.Get(id)
	if len(updated.Workers) != 1 || updated.Workers[0].RequestedExecutor != "codex" || updated.Workers[0].Executor != "claude" || updated.Workers[0].FallbackReason == "" {
		t.Fatalf("worker route not recorded: %+v", updated.Workers)
	}
}

func TestAutomaticBridgeRetriesWorkerOnAlternateOnlyForLimitSignal(t *testing.T) {
	root := t.TempDir()
	store, id := automaticBridgeSession(t, root)
	now := time.Now().UTC()
	available := func(provider quota.Provider) quota.ProviderQuota {
		return quota.ProviderQuota{Provider: provider, Authenticated: true, Eligible: true, Source: "fixture", ObservedAt: now}
	}
	codexBinary := executable(t, root, "codex", `#!/bin/sh
if [ "$1" = "mcp" ]; then
  printf '[]'
  exit 0
fi
result=""
previous=""
for arg in "$@"; do
  [ "$previous" = "--output-last-message" ] && result="$arg"
  previous="$arg"
done
cat >/dev/null
echo 'subscription rate limit reached' >&2
exit 1
`)
	claudeBinary := executable(t, root, "claude", "#!/bin/sh\ncat >/dev/null\nprintf '%s' '{\"type\":\"result\",\"result\":\"recovered answer\"}'\n")
	runner := &rufloRunner{}
	server := &Server{Store: store, SessionID: id, Directory: root, RuntimeDir: filepath.Join(root, "runtime"), ReviewExecutor: "codex", Adapter: workers.Adapter{Runner: platform.ExecRunner{}, CodexPath: codexBinary, ClaudePath: claudeBinary}, Control: orchestration.ControlPlane{Manager: orchestration.Manager{Runner: runner, Binary: "/managed/ruflo"}, RuntimeDir: filepath.Join(root, "runtime")}, Quota: &quota.Manager{Store: quota.Store{Root: filepath.Join(root, "quota")}, Probes: map[quota.Provider]quota.Probe{
		quota.ProviderCodex: bridgeProbe{available(quota.ProviderCodex)}, quota.ProviderClaude: bridgeProbe{available(quota.ProviderClaude)},
	}}}
	server.initialize()
	request := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: []byte(`{"role":"reviewer","task":"review","preferred_executor":"codex"}`)}}
	response, err := server.delegate(context.Background(), request)
	if err != nil || !strings.Contains(response.Content[0].(*mcp.TextContent).Text, "recovered answer") {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	updated, _ := store.Get(id)
	if len(updated.Workers) != 1 || updated.Workers[0].Executor != "claude" || updated.Workers[0].State != session.StateCompleted || !strings.Contains(updated.Workers[0].FallbackReason, "limit") {
		t.Fatalf("runtime worker fallback not recorded: %+v", updated.Workers)
	}
}

func automaticBridgeSession(t *testing.T, root string) (session.Store, string) {
	t.Helper()
	store := session.Store{Root: filepath.Join(root, "sessions")}
	id, _ := session.NewID()
	now := time.Now().UTC()
	value := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true, InitialPlanner: "codex", CurrentPrimary: "codex", PrimaryExecutor: "codex", WorkingDirectory: root, PrimaryModel: session.UnknownModel(), RufloEnabled: true, RufloHealthy: true, RufloSafeMode: true, SwarmID: "swarm-fixture", SwarmState: "active", Workers: []session.Worker{}, MaxWorkers: 2, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected", State: session.StateRunning, Quota: map[quota.Provider]quota.ProviderQuota{}}
	if err := store.Create(value); err != nil {
		t.Fatal(err)
	}
	return store, id
}

func executable(t *testing.T, directory, name, body string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
