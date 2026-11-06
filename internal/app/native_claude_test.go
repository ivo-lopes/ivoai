package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/nativeacp"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestClaudeHeadlessRequiresExplicitAdmission(t *testing.T) {
	t.Setenv(claudeNativeFlag, "")
	a := &App{Out: io.Discard, Err: io.Discard}
	err := a.ClaudeRun(context.Background(), NativeRunOptions{Prompt: "Explique a autenticação."})
	if err == nil || !strings.Contains(err.Error(), claudeNativeFlag) {
		t.Fatalf("flag admission: %v", err)
	}
}

func TestClaudeHeadlessProjectionExcludesAmbientToolsAndCredentials(t *testing.T) {
	root := t.TempDir()
	args, err := claudeManagedArgs(root, []nativeacp.MCPServer{{Name: "ivoai-orchestrator", Command: "/reviewed/ivoai", Args: []string{"_orchestrator-serve"}}})
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				values[args[i]] = args[i+1]
				i++
			} else {
				values[args[i]] = ""
			}
		}
	}
	if values["--tools"] != "" || values["--setting-sources"] != "" || values["--permission-mode"] != "dontAsk" || values["--allowedTools"] != "mcp__ivoai-orchestrator__*" {
		t.Fatalf("unsafe projection: %#v", values)
	}
	for _, tool := range []string{"Bash", "Write", "Edit", "Agent", "Task", "Skill"} {
		if !strings.Contains(values["--disallowedTools"], tool) {
			t.Fatal(tool)
		}
	}
	var settings map[string]any
	if json.Unmarshal([]byte(values["--settings"]), &settings) != nil || settings["disableAllHooks"] != true || settings["autoMemoryEnabled"] != false {
		t.Fatal("ambient hooks/memory enabled")
	}
	path := values["--mcp-config"]
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private projection: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var projection struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(data, &projection) != nil || len(projection.Servers) != 1 || projection.Servers["ivoai-orchestrator"] == nil {
		t.Fatal(string(data))
	}
	env := claudeManagedEnvironment([]string{"HOME=/owned", "PATH=/usr/bin:/bin", "ANTHROPIC_API_KEY=secret", "ANTHROPIC_BASE_URL=https://foreign.invalid", "CLAUDE_CODE_USE_BEDROCK=1", "OPENAI_API_KEY=secret"})
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "secret") || strings.Contains(joined, "foreign") || strings.Contains(joined, "BEDROCK") || !strings.Contains(joined, "HOME=/owned") {
		t.Fatalf("environment projection escaped: %q", joined)
	}
	for _, servers := range [][]nativeacp.MCPServer{{{Name: "../foreign", Command: "/owned"}}, {{Name: "ivoai", Type: "http", Command: "/owned"}}, {{Name: "ivoai", Command: "/owned"}, {Name: "ivoai", Command: "/foreign"}}} {
		if _, err = claudeManagedArgs(root, servers); err == nil {
			t.Fatal("foreign or duplicate MCP projection accepted")
		}
	}
}

func TestClaudeHeadlessRuntimeRejectsUnmanagedAndArbitraryPins(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "claude")
	if err := os.WriteFile(path, []byte("not official"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, c := range []config.ComponentState{{Installed: true, Managed: false, Version: "2.1.318", Path: path}, {Installed: true, Managed: true, Version: "2.1.319", Path: path}, {Installed: true, Managed: true, Version: "2.1.318", Path: path}} {
		if _, err := reviewedManagedNative(context.Background(), "claude", config.State{Components: map[string]config.ComponentState{"claude-code": c}}); err == nil {
			t.Fatal("unreviewed executable admitted")
		}
	}
}

// Synthetic JSON stream tests exercise the actual production wrapper; they are
// deliberately not counted as official inference or subscription certification.
func TestClaudeManagedRunnerWithholdsOutputOnRevocationAndForeignContinuation(t *testing.T) {
	for _, scenario := range []string{"success", "late_revocation", "foreign_resume", "start_denied", "nonzero"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "claude-fixture")
			script := "#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' '{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"native_owned\",\"model\":\"model_fixture\"}' '{\"type\":\"stream_event\",\"event\":{\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"owned answer\"}}}' '{\"type\":\"result\",\"is_error\":false}'\n"
			if scenario == "nonzero" {
				script += "exit 9\n"
			}
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256([]byte(script))
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			bound, validated, started := 0, 0, 0
			options := nativeacp.Options{SessionID: "core-owned", Directory: root, RuntimeDir: root, Environment: []string{"HOME=" + root, "PATH=/usr/bin:/bin"}, Authorize: func(context.Context, core.ExecutorRequest, nativeacp.Receipt) error { return nil }, BindSession: func(_ context.Context, id string, resumed bool) error {
				bound++
				if scenario == "foreign_resume" && id != "foreign" {
					return errors.New("foreign native id")
				}
				return nil
			}, Validate: func(context.Context, core.ExecutorRequest, nativeacp.Receipt) error {
				validated++
				if scenario == "late_revocation" {
					return errors.New("receipt revoked")
				}
				return nil
			}, Started: func(context.Context, nativeacp.Receipt, int) error {
				started++
				if scenario == "start_denied" {
					return errors.New("owner revoked")
				}
				return nil
			}}
			runner, err := managedClaudeRunner(managedNativeRuntime{path, hex.EncodeToString(sum[:]), "synthetic", info}, options)
			if err != nil {
				t.Fatal(err)
			}
			request := core.ExecutorRequest{Executor: "claude", FrontendSessionID: "frontend-owned", Prompt: "untrusted task"}
			if scenario == "foreign_resume" {
				request.ExecutorSessionID = "foreign"
			}
			var output strings.Builder
			result, err := runner.Run(context.Background(), request, func(s string) error { output.WriteString(s); return nil })
			if !result.Policy.NoReplay || !result.Policy.NoAutomaticFailover {
				t.Fatal("replay policy lost")
			}
			if scenario == "success" {
				if err != nil || output.String() != "owned answer" || bound != 1 || validated != 1 || started != 1 || result.Trace.ExitCode != 0 {
					t.Fatalf("result=%#v output=%q err=%v", result, output.String(), err)
				}
			} else if err == nil || output.Len() != 0 {
				t.Fatalf("failed attempt exposed answer: %q, %v", output.String(), err)
			}
		})
	}
}

func TestClaudeSharesCoreReceiptOwnershipAndRevocation(t *testing.T) {
	g, store, id := nativeGuardFixture(t)
	_, err := store.Update(id, func(v *session.Session) error {
		v.PrimaryTurn.Executor = "claude"
		v.PrimaryTurn.ScopeDigest = nativeManagedScope(g.scope, g.cwd, g.binary, claudeNativeSHA, g.rootInfo)
		v.SetFrontend("claude", "frontend")
		v.CurrentPrimary = "claude"
		v.PrimaryExecutor = "claude"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	g.receipt.Executor = "claude"
	g.receipt.ScopeDigest = nativeManagedScope(g.scope, g.cwd, g.binary, claudeNativeSHA, g.rootInfo)
	if _, err = g.check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = g.consume(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = g.check(context.Background()); err == nil {
		t.Fatal("closed Claude receipt revived")
	}
}
