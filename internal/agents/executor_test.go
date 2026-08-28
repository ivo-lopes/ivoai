package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

func TestOfficialExecutorAdaptersPreserveDirectLaunch(t *testing.T) {
	for _, name := range []string{"codex", "claude", "opencode"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(root, "ran")
			binary := writeExecutable(t, root, name, "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'fixture 1.0.0'; exit 0; fi\nprintf '%s' \"$*\" > \"$1\"\n")
			executor, err := ExecutorFor(name, Runtime{Runner: platform.ExecRunner{}, AgentPath: binary}, "1.0.0", true)
			if err != nil {
				t.Fatal(err)
			}
			status := executor.Probe(context.Background())
			if !status.Available || !status.Capabilities.Supports(core.CapabilitySessionStart) || status.Capabilities.Supports(core.CapabilityAdvisoryExecute) {
				t.Fatalf("probe = %+v", status)
			}
			var observation core.SessionObservation
			if err := executor.StartSession(context.Background(), core.SessionRequest{Args: []string{marker, "arg"}}, func(value core.SessionObservation) { observation = value }); err != nil {
				t.Fatal(err)
			}
			if observation.PID <= 0 {
				t.Fatalf("observation = %+v", observation)
			}
		})
	}
}

func TestOpenCodeExecutorBypassesLegacyCompression(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "ran")
	binary := writeExecutable(t, root, "opencode", "#!/bin/sh\n: > \"$1\"\n")
	executor := OpenCodeExecutor{Runtime: Runtime{Runner: platform.ExecRunner{}, AgentPath: binary}}
	if err := executor.StartSession(context.Background(), core.SessionRequest{Args: []string{marker}, CompressionEnabled: true}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("OpenCode direct session did not run")
	}
}

func TestOpenCodeExecutorPropagatesOfficialClientExitStatus(t *testing.T) {
	root := t.TempDir()
	binary := writeExecutable(t, root, "opencode", "#!/bin/sh\nexit 23\n")
	executor := OpenCodeExecutor{Runtime: Runtime{Runner: platform.ExecRunner{}, AgentPath: binary}}
	err := executor.StartSession(context.Background(), core.SessionRequest{}, nil)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 23 {
		t.Fatalf("exit error=%v", err)
	}
}

func TestExecutorForRejectsUnknownImplementation(t *testing.T) {
	if _, err := ExecutorFor("future", Runtime{}, "", false); err == nil {
		t.Fatal("expected unsupported executor error")
	}
}
