package opencodebridge

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Uses the official client's existing auth unchanged. Opt-in only; normal CI
// uses the deterministic invocation/bridge contracts without personal login.
func TestLiveCodexGitAndNonGitResume(t *testing.T) {
	path := os.Getenv("IVOAI_LIVE_CODEX_NON_GIT")
	if path == "" {
		t.Skip("official Codex live test not requested")
	}
	for _, git := range []bool{false, true} {
		name := "administrative_non_git"
		if git {
			name = "git"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if git {
				cmd := exec.Command("git", "init", "--quiet", dir)
				if err := cmd.Run(); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			runner := CLIRunner{Codex: ExecutorSpec{Path: path, Dir: dir, Env: os.Environ(), Args: []string{"--sandbox", "read-only", "-a", "never"}}}
			resume := ""
			for turn := 0; turn < 2; turn++ {
				var output strings.Builder
				result, err := runner.Run(ctx, ExecutorRequest{Executor: "codex", Prompt: "Responda apenas OK.", ExecutorSessionID: resume}, func(s string) error { output.WriteString(s); return nil })
				if err != nil {
					t.Fatalf("turn %d: %v trace=%+v", turn, err, result.Trace)
				}
				if result.Trace.ExitCode != 0 || !result.Trace.FinalResponse || !strings.Contains(output.String(), "OK") {
					t.Fatalf("turn %d incomplete", turn)
				}
				resume = result.ExecutorSessionID
			}
			if !git {
				if _, err := os.Stat(dir + "/.git"); !os.IsNotExist(err) {
					t.Fatal("non-Git workspace was mutated into a repository")
				}
			}
		})
	}
}
