package opencodebridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/caveman"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

type captureFidelityProvider struct {
	core.CompressionProvider
	seen []core.CompressionFidelity
}

func (p *captureFidelityProvider) Prepare(ctx context.Context, request core.CompressionRequest) (core.CompressionLease, error) {
	p.seen = append(p.seen, request.Fidelity)
	return p.CompressionProvider.Prepare(ctx, request)
}

type forbiddenFidelityProbe struct{ t *testing.T }

func (p forbiddenFidelityProbe) LookPath(string) (string, error) {
	p.t.Fatal("EXACT looked up a compressor")
	return "", os.ErrNotExist
}
func (p forbiddenFidelityProbe) Run(context.Context, string, []string, platform.RunOptions) (platform.Result, error) {
	p.t.Fatal("EXACT executed a compressor probe")
	return platform.Result{}, os.ErrPermission
}

func TestCLIRunnerExactEvidenceThreeTurnsNeverStartsCaveman(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("CODEX_HOME", filepath.Join(root, "codehome"))
	argsFile := filepath.Join(root, "args")
	runtimeDir := filepath.Join(root, "must-not-create-proxy-runtime")
	binary := fixtureExecutable(t, root, "codex", `#!/bin/sh
printf '%s\n' "$*" >> "$ARGS_FILE"
cat >/dev/null
printf '%s\n' '{"type":"thread.started","thread_id":"thread_exact"}' '{"type":"item.completed","item":{"type":"agent_message","text":"exact fixture"}}' '{"type":"turn.completed"}'
`)
	// The real provider must return its direct decision before touching runtime,
	// integrity hooks, probes or process startup; it is not a fake proxy response.
	provider := &captureFidelityProvider{CompressionProvider: caveman.Provider{Binary: "/must-not-probe/caveman", Managed: true, Runner: forbiddenFidelityProbe{t}, IntegrityCheck: func() error { t.Fatal("EXACT called compressor integrity/probe"); return errors.New("forbidden") }}}
	runner := CLIRunner{Codex: ExecutorSpec{Path: binary, Dir: root, Env: []string{"PATH=/usr/bin:/bin", "ARGS_FILE=" + argsFile}, Compression: provider, CompressionEnabled: true, CompressionFidelity: core.CompressionExactRequired, RuntimeDir: runtimeDir}}
	thread := ""
	for range 3 {
		result, err := runner.Run(context.Background(), ExecutorRequest{Executor: "codex", ExecutorSessionID: thread, Prompt: "exact fixture"}, func(string) error { return nil })
		if err != nil || result.CompressionUsed || result.CompressionProvider != "direct" {
			t.Fatalf("exact turn result=%+v err=%v", result, err)
		}
		thread = result.ExecutorSessionID
	}
	if len(provider.seen) != 3 {
		t.Fatal("compression contract was not propagated for all turns")
	}
	for _, fidelity := range provider.seen {
		if fidelity != core.CompressionExactRequired {
			t.Fatal("runner weakened EXACT to Compressible")
		}
	}
	if _, err := os.Lstat(runtimeDir); !os.IsNotExist(err) {
		t.Fatal("EXACT created proxy state", err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(args), "ivoai-caveman") || strings.Contains(string(args), "/chatgpt") {
		t.Fatal("EXACT modified the certified native provider configuration")
	}
}

func TestCLIRunnerLegacyCompressionDefaultsToCompressible(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("CODEX_HOME", filepath.Join(root, "codehome"))
	binary := fixtureExecutable(t, root, "codex", `#!/bin/sh
cat >/dev/null
printf '%s\n' '{"type":"thread.started","thread_id":"thread_legacy"}' '{"type":"item.completed","item":{"type":"agent_message","text":"legacy fixture"}}' '{"type":"turn.completed"}'
`)
	provider := &captureFidelityProvider{CompressionProvider: caveman.Provider{Binary: "/unavailable-reviewed-caveman", Managed: true}}
	runner := CLIRunner{Codex: ExecutorSpec{Path: binary, Dir: root, Env: []string{"PATH=/usr/bin:/bin"}, Compression: provider, CompressionEnabled: true, RuntimeDir: root}}
	if _, err := runner.Run(context.Background(), ExecutorRequest{Executor: "codex", Prompt: "fixture"}, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(provider.seen) != 1 || provider.seen[0] != core.CompressionCompressible {
		t.Fatal("legacy zero-value fidelity changed")
	}
}

type violatingFidelityProvider struct {
	caveman.Provider
	closed int
}

func (p *violatingFidelityProvider) Prepare(_ context.Context, r core.CompressionRequest) (core.CompressionLease, error) {
	return violatingFidelityLease{p, core.CompressionDecision{Command: r.DirectPath, Args: r.Args, Environment: r.Environment, Provider: "caveman", Used: true}}, nil
}

type violatingFidelityLease struct {
	provider *violatingFidelityProvider
	decision core.CompressionDecision
}

func (l violatingFidelityLease) Decision() core.CompressionDecision { return l.decision }
func (l violatingFidelityLease) Done() <-chan error                 { return nil }
func (l violatingFidelityLease) Close(context.Context) error        { l.provider.closed++; return nil }
func TestCLIRunnerRejectsCompressionFidelityViolationBeforeExecutor(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("CODEX_HOME", filepath.Join(root, "codehome"))
	marker := filepath.Join(root, "executed")
	binary := fixtureExecutable(t, root, "codex", "#!/bin/sh\nprintf unsafe > \"$MARKER\"\n")
	provider := &violatingFidelityProvider{}
	runner := CLIRunner{Codex: ExecutorSpec{Path: binary, Dir: root, Env: []string{"PATH=/usr/bin:/bin", "MARKER=" + marker}, Compression: provider, CompressionEnabled: true, CompressionFidelity: core.CompressionExactRequired}}
	_, err := runner.Run(context.Background(), ExecutorRequest{Executor: "codex", Prompt: "fixture"}, func(string) error { return nil })
	if FailureClass(err) != "compression_fidelity_violation" || provider.closed != 1 {
		t.Fatal("violating compressor was accepted or leaked", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("executor ran after fidelity violation")
	}
}
