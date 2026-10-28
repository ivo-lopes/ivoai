package workers

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Opt-in protocol proof: an official pinned client talks only to a synthetic
// Anthropic endpoint in its own network namespace. No subscription is used.
func TestClaudeWorkerOfficialDelegationBoundary(t *testing.T) {
	binary := os.Getenv("IVOAI_CLAUDE_WORKER_OFFICIAL_BINARY")
	if binary == "" {
		t.Skip("set IVOAI_CLAUDE_WORKER_OFFICIAL_BINARY for the pinned offline protocol proof")
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Fatal("this proof pins the official Linux amd64 2.1.296 binary only")
	}
	root := t.TempDir()
	access, err := NewAccess(root, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Executor: "claude", Directory: root, Runtime: root, Task: "synthetic native delegation fixture", Access: access}
	args, _, err := workerArgs(request)
	if err != nil {
		t.Fatal(err)
	}
	args, err = (Adapter{}).isolateScopedMCPs(context.Background(), binary, request, args)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(root, "factory-args.json")
	if err := os.WriteFile(argsFile, body, 0600); err != nil {
		t.Fatal(err)
	}
	output := os.Getenv("IVOAI_CLAUDE_WORKER_PROOF_DIR")
	if output == "" {
		output = filepath.Join(root, "proof")
	}
	script, err := filepath.Abs("../../scripts/test_claude_worker_tools.py")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "python3", script, "--binary", binary, "--factory-args", argsFile, "--output", output)
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + root}
	result, err := command.CombinedOutput()
	t.Logf("%s", result)
	if err != nil {
		t.Fatalf("official Claude boundary proof failed: %v", err)
	}
}
