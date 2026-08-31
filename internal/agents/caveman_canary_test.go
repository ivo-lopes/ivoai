package agents

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

type canaryCompressionProvider struct {
	prepare func(core.CompressionRequest) (core.CompressionLease, error)
}

func (canaryCompressionProvider) ID() core.ComponentID { return core.ComponentCompression }
func (canaryCompressionProvider) Probe(context.Context) core.ComponentStatus {
	return core.ComponentStatus{ID: core.ComponentCompression, Implementation: "caveman", Available: true, Health: core.HealthHealthy}
}
func (p canaryCompressionProvider) Prepare(_ context.Context, request core.CompressionRequest) (core.CompressionLease, error) {
	return p.prepare(request)
}

type canaryCompressionLease struct {
	decision core.CompressionDecision
	done     chan error
	closed   atomic.Int32
	once     sync.Once
}

func (l *canaryCompressionLease) Decision() core.CompressionDecision { return l.decision }
func (l *canaryCompressionLease) Done() <-chan error                 { return l.done }
func (l *canaryCompressionLease) Close(context.Context) error {
	l.once.Do(func() { l.closed.Add(1) })
	return nil
}

func TestCavemanRuntimeCanaryCoversThreeDirectExecutors(t *testing.T) {
	for _, executor := range []string{"codex", "claude", "opencode"} {
		t.Run(executor, func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(root, "launch-count")
			agent := writeExecutable(t, root, executor, "#!/bin/sh\ncount=0\n[ -f \"$1\" ] && count=$(cat \"$1\")\nprintf '%s' $((count + 1)) > \"$1\"\n")
			lease := &canaryCompressionLease{}
			provider := canaryCompressionProvider{prepare: func(request core.CompressionRequest) (core.CompressionLease, error) {
				lease.decision = core.CompressionDecision{Command: request.DirectPath, Args: request.Args, Environment: request.Environment, Used: true, Provider: "caveman"}
				return lease, nil
			}}
			var observed Observation
			runtime := Runtime{Runner: platform.ExecRunner{}, AgentPath: agent, Compression: provider, In: nil, Out: io.Discard, Err: io.Discard, RuntimeDir: root}
			if err := runtime.LaunchObserved(context.Background(), executor, []string{marker}, true, func(value Observation) { observed = value }); err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(marker)
			if err != nil || string(body) != "1" || !observed.CompressionUsed || observed.CompressionProvider != "caveman" || observed.CompressionFallback || lease.closed.Load() != 1 {
				t.Fatalf("executor=%s launch=%q observation=%+v closed=%d err=%v", executor, body, observed, lease.closed.Load(), err)
			}
		})
	}
}

func TestCavemanPreflightFailureFallsBackDirectExactlyOnce(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "launch-count")
	agent := writeExecutable(t, root, "codex", "#!/bin/sh\ncount=0\n[ -f \"$1\" ] && count=$(cat \"$1\")\nprintf '%s' $((count + 1)) > \"$1\"\n")
	provider := canaryCompressionProvider{prepare: func(core.CompressionRequest) (core.CompressionLease, error) {
		return nil, errors.New("controlled preflight failure")
	}}
	var observed Observation
	var stderr strings.Builder
	runtime := Runtime{Runner: platform.ExecRunner{}, AgentPath: agent, Compression: provider, Out: io.Discard, Err: &stderr, RuntimeDir: root}
	if err := runtime.LaunchObserved(context.Background(), "codex", []string{marker}, true, func(value Observation) { observed = value }); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(marker)
	if string(body) != "1" || !observed.CompressionFallback || observed.CompressionUsed || !strings.Contains(stderr.String(), "launching codex directly") {
		t.Fatalf("launch=%q observation=%+v stderr=%q", body, observed, stderr.String())
	}
}

func TestCavemanFailureAfterExecutorStartNeverDuplicatesSession(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "launch-count")
	started := filepath.Join(root, "started")
	agent := writeExecutable(t, root, "claude", `#!/bin/sh
count=0
[ -f "$1" ] && count=$(cat "$1")
printf '%s' $((count + 1)) > "$1"
printf started > "$2"
trap 'exit 0' TERM HUP INT
while :; do sleep 0.02; done
`)
	lease := &canaryCompressionLease{done: make(chan error, 1)}
	provider := canaryCompressionProvider{prepare: func(request core.CompressionRequest) (core.CompressionLease, error) {
		lease.decision = core.CompressionDecision{Command: request.DirectPath, Args: request.Args, Environment: request.Environment, Used: true, Provider: "caveman"}
		return lease, nil
	}}
	done := make(chan error, 1)
	runtime := Runtime{Runner: platform.ExecRunner{}, AgentPath: agent, Compression: provider, Out: io.Discard, Err: io.Discard, RuntimeDir: root}
	go func() { done <- runtime.Launch(context.Background(), "claude", []string{marker, started}, true) }()
	waitForFile(t, started, time.Second)
	lease.done <- errors.New("controlled proxy crash")
	close(lease.done)
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "compression provider failed after agent start") {
			t.Fatalf("runtime error=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runtime did not terminate after proxy crash")
	}
	body, _ := os.ReadFile(marker)
	if string(body) != "1" || lease.closed.Load() != 1 {
		t.Fatalf("session duplicated or lease leaked: launches=%q closed=%d", body, lease.closed.Load())
	}
}
