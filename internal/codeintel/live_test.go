package codeintel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/externalmcp"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Explicit canary uses only the installed official Codex subscription session.
// It never reads/copies credentials, invokes Claude, or changes personal config.
func TestLiveCodexStructuralCapability(t *testing.T) {
	codex := os.Getenv("IVOAI_LIVE_CODEX_PATH")
	if os.Getenv("IVOAI_LIVE_GRAFT") != "1" || codex == "" {
		t.Skip("explicit official Codex canary disabled")
	}
	node, dist := os.Getenv("IVOAI_GRAFT_TEST_NODE"), os.Getenv("IVOAI_GRAFT_TEST_DIST")
	if node == "" || dist == "" {
		t.Fatal("reviewed artifact required")
	}
	p, _ := testProvider(t)
	p.Node = node
	p.Distribution = dist
	p.Executable = os.Args[0]
	p.run = p.runProcess
	os.WriteFile(filepath.Join(p.Root, "main.go"), []byte("package fixture\nfunc ComputeInvoiceNet(gross, discount int) int { return gross-discount }\nfunc InvoiceTotal() int { return ComputeInvoiceNet(100,7) }\n"), 0600)
	var calls atomic.Int32
	p.Observe = func(r core.CodeResult, err error) {
		if err == nil {
			calls.Add(1)
		}
	}
	gateway, err := externalmcp.Start([]externalmcp.Target{{Name: "ivoai-code", Local: p.Handler(), Restricted: true, AllowedTools: ToolNames()}}, "full")
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.Close()
	type evidence struct {
		Arm                       string
		DurationMS                int64
		InputTokens, OutputTokens any
		Calls                     int32
		Correct                   bool
	}
	rows := []evidence{}
	for _, arm := range []string{"A", "B"} {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		args := []string{"exec", "--ignore-user-config", "--ephemeral", "--sandbox", "read-only", "--skip-git-repo-check", "--json", "--color", "never", "-m", "gpt-6.1-sol", "-c", `model_reasoning_effort="low"`, "-C", p.Root}
		env := os.Environ()
		if arm == "B" {
			args = append(args, "-c", "mcp_servers.ivoai-code.url="+strconv.Quote(gateway.URL(0)), "-c", `mcp_servers.ivoai-code.bearer_token_env_var="IVOAI_TEST_CODE_TOKEN"`, "-c", `mcp_servers.ivoai-code.default_tools_approval_mode="approve"`)
			env = append(env, "IVOAI_TEST_CODE_TOKEN="+gateway.Token())
		}
		args = append(args, "Identify ComputeInvoiceNet and its direct caller in this authorized synthetic repository. Report the formula and result of InvoiceTotal. Use a repository inspection capability when available; otherwise inspect local files. Do not edit files or contact external targets.")
		command := exec.CommandContext(ctx, codex, args...)
		command.Env = env
		var out, stderr bytes.Buffer
		command.Stdout = &out
		command.Stderr = &stderr
		start := time.Now()
		err = command.Run()
		cancel()
		if err != nil {
			t.Fatalf("official Codex %s failed (no credential output): %v", arm, err)
		}
		row := evidence{Arm: arm, DurationMS: time.Since(start).Milliseconds(), Calls: calls.Load()}
		var answer strings.Builder
		for _, line := range bytes.Split(out.Bytes(), []byte("\n")) {
			var event struct {
				Type  string                      `json:"type"`
				Item  struct{ Type, Text string } `json:"item"`
				Usage map[string]any              `json:"usage"`
			}
			if json.Unmarshal(line, &event) != nil {
				continue
			}
			if event.Type == "turn.completed" {
				row.InputTokens = event.Usage["input_tokens"]
				row.OutputTokens = event.Usage["output_tokens"]
			}
			if event.Item.Type == "agent_message" {
				answer.WriteString(event.Item.Text)
			}
		}
		row.Correct = strings.Contains(answer.String(), "93") && strings.Contains(answer.String(), "ComputeInvoiceNet") && strings.Contains(answer.String(), "InvoiceTotal")
		if !row.Correct {
			t.Error("answer lacks expected synthetic result and identity")
		}
		if arm == "B" && row.Calls == 0 {
			t.Error("Codex did not consume managed native capability")
		}
		rows = append(rows, row)
	}
	body, _ := json.MarshalIndent(rows, "", "  ")
	if path := os.Getenv("IVOAI_GRAFT_LIVE_OUTPUT"); path != "" {
		if e := os.WriteFile(path, body, 0600); e != nil {
			t.Fatal(e)
		}
	}
	fmt.Fprintln(os.Stdout, string(body))
}
