package app

import (
	"context"
	"encoding/json"
	"github.com/ivo-lopes/ivoai/internal/nativeacp"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Official client + synthetic endpoint proves protocol compatibility only.
// Production has no API key/endpoint override and never falls back to this fixture.
func TestClaudePrimaryOfficialOfflineProtocol(t *testing.T) {
	binary := os.Getenv("IVOAI_CLAUDE_PRIMARY_OFFICIAL_BINARY")
	if binary == "" {
		t.Skip("opt-in pinned official CLI offline protocol proof; no real inference")
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Fatal("official offline proof pins Linux amd64")
	}
	root := t.TempDir()
	args, err := claudeManagedArgs(root, []nativeacp.MCPServer{{Name: "ivoai-orchestrator", Command: "/usr/bin/python3", Args: []string{"/fixture/probe.py", "--mcp"}}})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(root, "factory-args.json")
	if err = os.WriteFile(argsFile, body, 0600); err != nil {
		t.Fatal(err)
	}
	output := os.Getenv("IVOAI_CLAUDE_PRIMARY_PROOF_DIR")
	if output == "" {
		output = filepath.Join(root, "proof")
	}
	if err = os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("../../scripts/test_claude_primary_protocol.py")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "python3", script, "--binary", binary, "--factory-args", argsFile, "--runtime", root, "--output", output)
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + root}
	result, err := command.CombinedOutput()
	t.Logf("%s", result)
	if err != nil {
		t.Fatalf("official offline protocol: %v", err)
	}
}
