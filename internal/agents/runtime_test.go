package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

func TestLaunchFallsBackDirectWhenHeadroomUnavailable(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "direct-ran")
	agent := writeExecutable(t, root, "codex", "#!/bin/sh\n: > \"$1\"\n")
	runtime := Runtime{Runner: platform.ExecRunner{}, AgentPath: agent, HeadroomPath: filepath.Join(root, "missing-headroom")}
	if err := runtime.Launch(context.Background(), "codex", []string{marker}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("direct agent did not run after Headroom preflight failure")
	}
}

func TestDirectLaunchPreservesCWDArgumentsAndObservesPID(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "observation")
	agent := writeExecutable(t, root, "codex", "#!/bin/sh\nprintf '%s\\n%s\\n' \"$PWD\" \"$*\" > \"$1\"\n")
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	var observed Observation
	runtime := Runtime{Runner: platform.ExecRunner{}, AgentPath: agent}
	if err := runtime.LaunchObserved(context.Background(), "codex", []string{marker, "--model", "fixture-model"}, false, func(value Observation) { observed = value }); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), root+"\n"+marker+" --model fixture-model") {
		t.Fatalf("cwd/arguments not preserved: %q", body)
	}
	if observed.PID <= 0 || observed.HeadroomUsed {
		t.Fatalf("observation = %+v (pid %s)", observed, strconv.Itoa(observed.PID))
	}
}

func TestHeadroomCanResolveManagedAgentOutsideUserPath(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "wrapped-ran")
	agent := writeExecutable(t, root, "codex", "#!/bin/sh\n: > \"$1\"\n")
	headroom := writeExecutable(t, root, "headroom", `#!/bin/sh
if [ "$1" = "--version" ]; then echo "headroom 0.36.0"; exit 0; fi
if [ "$1" = "wrap" ] && [ "$3" = "--help" ]; then exit 0; fi
if [ "$1" = "wrap" ]; then agent="$2"; shift 3; exec "$agent" "$@"; fi
exit 2
`)
	t.Setenv("PATH", "/usr/bin:/bin")
	runtime := Runtime{Runner: platform.ExecRunner{}, AgentPath: agent, HeadroomPath: headroom}
	if err := runtime.Launch(context.Background(), "codex", []string{marker}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("Headroom did not resolve the managed agent through the scoped PATH")
	}
}

func TestStartedHeadroomExitIsNotMistakenForPreflightFailure(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "direct-must-not-run")
	agent := writeExecutable(t, root, "codex", "#!/bin/sh\n: > \"$1\"\n")
	headroom := writeExecutable(t, root, "headroom", `#!/bin/sh
if [ "$1" = "--version" ]; then echo "headroom 0.36.0"; exit 0; fi
if [ "$1" = "wrap" ] && [ "$3" = "--help" ]; then exit 0; fi
exit 42
`)
	runtime := Runtime{Runner: platform.ExecRunner{}, AgentPath: agent, HeadroomPath: headroom}
	err := runtime.Launch(context.Background(), "codex", []string{marker}, true)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 42 {
		t.Fatalf("wrapper exit = %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("direct agent was started after a wrapper process had already run")
	}
}

func TestClaudeStartFallbackPreservesManagedUpdateGuard(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "claude-environment")
	agent := writeExecutable(t, root, "claude", "#!/bin/sh\nprintf '%s' \"${DISABLE_AUTOUPDATER:-}\" > \"$1\"\n")
	count := filepath.Join(root, "headroom-count")
	headroom := writeExecutable(t, root, "headroom", `#!/bin/sh
count=0
[ -f "`+count+`" ] && count="$(cat "`+count+`")"
count=$((count + 1))
printf '%s' "$count" > "`+count+`"
if [ "$1" = "--version" ]; then echo "headroom 0.36.0"; exit 0; fi
if [ "$1" = "wrap" ] && [ "$3" = "--help" ]; then
  [ "$count" -eq 3 ] && rm -- "$0"
  exit 0
fi
exit 2
`)
	runtime := Runtime{Runner: platform.ExecRunner{}, AgentPath: agent, HeadroomPath: headroom}
	if err := runtime.Launch(context.Background(), "claude", []string{marker}, true); err != nil {
		t.Fatal(err)
	}
	value, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "1" {
		t.Fatalf("Claude fallback did not preserve DISABLE_AUTOUPDATER: %q", value)
	}
}

func writeExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
