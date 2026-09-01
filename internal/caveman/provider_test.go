package caveman

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

type versionRunner struct{ document string }

func (versionRunner) LookPath(string) (string, error) { return "", errors.New("unused") }
func (r versionRunner) Run(context.Context, string, []string, platform.RunOptions) (platform.Result, error) {
	return platform.Result{Stdout: r.document}, nil
}

func TestCavemanProxyHelper(t *testing.T) {
	separator := -1
	for index, value := range os.Args {
		if value == "--" {
			separator = index
			break
		}
	}
	if separator < 0 {
		return
	}
	args := os.Args[separator+1:]
	if len(args) == 0 || args[0] != "serve" {
		return
	}
	configBody, err := os.ReadFile(os.Getenv("CAVEMAN_CONFIG"))
	if err != nil {
		os.Exit(2)
	}
	address := ""
	for _, line := range strings.Split(string(configBody), "\n") {
		if strings.HasPrefix(line, "listen: ") {
			address = strings.TrimSpace(strings.TrimPrefix(line, "listen: "))
		}
	}
	if address == "" || !strings.HasPrefix(address, "127.0.0.1:") || os.Getenv("CAVEMAN_HOME") == "" || os.Getenv("CAVE_CAPTURE_DIR") != "" {
		os.Exit(2)
	}
	if contains(args, "no-ready") {
		time.Sleep(5 * time.Second)
		return
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		os.Exit(3)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "service": "caveman-proxy", "schema": healthSchema})
	})
	server := &http.Server{Handler: mux}
	if contains(args, "crash") {
		go func() {
			time.Sleep(250 * time.Millisecond)
			os.Exit(17)
		}()
	}
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		os.Exit(4)
	}
}

func testProvider(t *testing.T, proxyArgs ...string) Provider {
	t.Helper()
	root := t.TempDir()
	binary := filepath.Join(root, "caveman-proxy")
	source, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	target, err := os.OpenFile(binary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(target, source); err != nil {
		t.Fatal(err)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
	args := []string{"-test.run=TestCavemanProxyHelper", "--", "serve"}
	args = append(args, proxyArgs...)
	return Provider{
		Binary: binary, Managed: true,
		Expected:       supplychain.ResolvedSource{ID: "caveman", LogicalVersion: "1.1.3"},
		Runner:         versionRunner{document: `{"schema":"caveman.proxy.run.v1","version":"dev","capabilities":["run_state"]}`},
		StartupTimeout: time.Second, ProxyArgs: args, IntegrityCheck: func() error { return nil },
	}
}

func TestProviderLifecycleIsPrivateLoopbackAndCleaned(t *testing.T) {
	provider := testProvider(t)
	runtimeRoot := filepath.Join(t.TempDir(), "session")
	if err := os.Mkdir(runtimeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	lease, err := provider.Prepare(context.Background(), core.CompressionRequest{Executor: core.ComponentClaude, DirectPath: "/bin/claude", RuntimeDir: runtimeRoot, Fidelity: core.CompressionCompressible, Environment: []string{"PATH=/usr/bin", "ANTHROPIC_AUTH_TOKEN=executor-owned", "CAVE_CAPTURE_DIR=/must-not-survive"}})
	if err != nil {
		t.Fatal(err)
	}
	decision := lease.Decision()
	if !decision.Used || decision.Provider != implementation || environmentValue(decision.Environment, "ANTHROPIC_AUTH_TOKEN") != "executor-owned" || environmentValue(decision.Environment, "CAVE_CAPTURE_DIR") != "" {
		t.Fatalf("unsafe decision: %+v", decision)
	}
	base := environmentValue(decision.Environment, "ANTHROPIC_BASE_URL")
	if !strings.HasPrefix(base, "http://127.0.0.1:") || !strings.HasSuffix(base, "/w/claude") {
		t.Fatalf("base URL = %q", base)
	}
	if err := lease.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(runtimeRoot, "caveman"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("runtime was not cleaned: entries=%d err=%v", len(entries), err)
	}
}

func TestProviderRetriesPortCollisionBeforeExecutorLaunch(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	occupiedPort := occupied.Addr().(*net.TCPAddr).Port
	var calls atomic.Int32
	provider := testProvider(t)
	provider.Port = func() (int, error) {
		if calls.Add(1) == 1 {
			return occupiedPort, nil
		}
		return availablePort()
	}
	runtimeRoot := t.TempDir()
	lease, err := provider.Prepare(context.Background(), core.CompressionRequest{Executor: core.ComponentCodex, DirectPath: "/bin/codex", RuntimeDir: runtimeRoot, Fidelity: core.CompressionCompressible})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close(context.Background())
	if calls.Load() < 2 {
		t.Fatal("occupied port was not retried")
	}
}

func TestProviderReadinessTimeoutFallsBackBeforeLaunch(t *testing.T) {
	provider := testProvider(t, "no-ready")
	provider.StartupTimeout = 100 * time.Millisecond
	_, err := provider.Prepare(context.Background(), core.CompressionRequest{Executor: core.ComponentClaude, DirectPath: "/bin/claude", RuntimeDir: t.TempDir(), Fidelity: core.CompressionCompressible})
	if err == nil || !strings.Contains(err.Error(), "readiness timeout") {
		t.Fatalf("error=%v", err)
	}
}

func TestProviderCrashAfterReadinessIsObservable(t *testing.T) {
	provider := testProvider(t, "crash")
	lease, err := provider.Prepare(context.Background(), core.CompressionRequest{Executor: core.ComponentClaude, DirectPath: "/bin/claude", RuntimeDir: t.TempDir(), Fidelity: core.CompressionCompressible})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close(context.Background())
	select {
	case err := <-lease.Done():
		if err == nil {
			t.Fatal("crash was reported as clean exit")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("proxy crash was not observed")
	}
}

func TestProviderProbeDoesNotInventVersion(t *testing.T) {
	provider := testProvider(t)
	status := provider.Probe(context.Background())
	if !status.Available || status.Provenance.Version != "1.1.3" {
		t.Fatalf("status=%+v", status)
	}
}

func TestPrepareExecutorPreservesSubscriptionAuthentication(t *testing.T) {
	codex, err := prepareExecutor(core.CompressionRequest{Executor: core.ComponentCodex, DirectPath: "/bin/codex", Args: []string{"--model", "fixture"}}, "http://127.0.0.1:4321")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(codex.Args, " ")
	if !strings.Contains(joined, "requires_openai_auth=true") || !strings.Contains(joined, "/chatgpt") || strings.Contains(joined, "OPENAI_API_KEY") {
		t.Fatalf("Codex args=%q", joined)
	}
}

func TestProviderRejectsOpenCodeBeforeProxyStartup(t *testing.T) {
	provider := testProvider(t)
	var ports atomic.Int32
	provider.Port = func() (int, error) {
		ports.Add(1)
		return availablePort()
	}
	_, err := provider.Prepare(context.Background(), core.CompressionRequest{Executor: core.ComponentOpenCode, DirectPath: "/bin/opencode", RuntimeDir: t.TempDir(), Fidelity: core.CompressionCompressible})
	if err == nil || !strings.Contains(err.Error(), "subscription-only") || ports.Load() != 0 {
		t.Fatalf("error=%v proxy_starts=%d", err, ports.Load())
	}
}

func TestOpenCodeExactRequiredRemainsDirect(t *testing.T) {
	provider := testProvider(t)
	var ports atomic.Int32
	provider.Port = func() (int, error) {
		ports.Add(1)
		return availablePort()
	}
	lease, err := provider.Prepare(context.Background(), core.CompressionRequest{Executor: core.ComponentOpenCode, DirectPath: "/bin/opencode", Args: []string{"run", "fixture"}, RuntimeDir: t.TempDir(), Fidelity: core.CompressionExactRequired})
	if err != nil || lease.Decision().Used || lease.Decision().Command != "/bin/opencode" || ports.Load() != 0 {
		t.Fatalf("lease=%+v error=%v proxy_starts=%d", lease, err, ports.Load())
	}
}

func TestExactAndBypassNeverStartProxy(t *testing.T) {
	provider := testProvider(t)
	for _, fidelity := range []core.CompressionFidelity{core.CompressionExactRequired, core.CompressionBypass, core.CompressionUnsupported} {
		lease, err := provider.Prepare(context.Background(), core.CompressionRequest{Executor: core.ComponentCodex, DirectPath: "/bin/codex", RuntimeDir: t.TempDir(), Fidelity: fidelity})
		if err != nil || lease.Decision().Used {
			t.Fatalf("fidelity=%s lease=%+v err=%v", fidelity, lease, err)
		}
	}
}

func TestVersionProbeShape(t *testing.T) {
	provider := testProvider(t)
	provider.Runner = versionRunner{document: fmt.Sprintf(`{"schema":%s,"version":"dev","capabilities":["run_state"]}`, strconv.Quote(versionSchema))}
	if status := provider.Probe(context.Background()); !status.Available {
		t.Fatalf("status=%+v", status)
	}
}
