//go:build linux && amd64

package nativeacp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
)

// This proof uses the official CLI and its existing cached authentication.
// It is opt-in because it consumes real subscription inference. No credential
// is read, supplied, copied or printed; provider-key environment is stripped.
func TestOfficialAntigravityACP(t *testing.T) {
	binary := os.Getenv("IVOAI_ANTIGRAVITY_OFFICIAL_BINARY")
	if binary == "" {
		t.Skip("set IVOAI_ANTIGRAVITY_OFFICIAL_BINARY for real authenticated integration")
	}
	if os.Getenv("IVOAI_ANTIGRAVITY_OFFICIAL_SHA256") != "da1751e7658469fa80bf36eda2c60ee61216d8e42b735a184382ba31d1f6b88e" {
		t.Fatal("proof requires the reviewed official Antigravity 1.5.2 Linux amd64 digest")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "fixture.txt")
	if err := os.WriteFile(fixture, []byte("ORIGINAL\n"), 0600); err != nil {
		t.Fatal(err)
	}
	owned := ""
	o := Options{Enabled: true, SessionID: "official-core-session", Binary: binary, SHA256: os.Getenv("IVOAI_ANTIGRAVITY_OFFICIAL_SHA256"), Directory: root, RuntimeDir: filepath.Join(root, "runtime"), ProfileDir: filepath.Join(root, "profile"), Environment: os.Environ(), TurnTimeout: 2 * time.Minute}
	o.Authorize = func(context.Context, core.ExecutorRequest, Receipt) error { return nil }
	o.Validate = o.Authorize
	o.BindSession = func(_ context.Context, id string, resumed bool) error {
		if resumed && owned != id || !resumed && owned != "" {
			return errors.New("foreign native session")
		}
		owned = id
		return nil
	}
	reads := 0
	o.Approve = func(_ context.Context, _ core.ExecutorRequest, _ Receipt, raw json.RawMessage) (bool, error) {
		var request struct {
			Tool struct {
				Name string `json:"name"`
				Args struct {
					Path string `json:"Path"`
				} `json:"args"`
			} `json:"toolCall"`
		}
		if json.Unmarshal(raw, &request) != nil {
			return false, nil
		}
		if request.Tool.Name == "read_file" && filepath.Clean(request.Tool.Args.Path) == fixture {
			reads++
			return true, nil
		}
		return false, nil
	}
	type record struct {
		Case    string               `json:"case"`
		Pass    bool                 `json:"pass"`
		Failure string               `json:"failure,omitempty"`
		Trace   *core.ExecutionTrace `json:"trace,omitempty"`
	}
	records := []record{}
	defer func() {
		output := os.Getenv("IVOAI_ANTIGRAVITY_PROOF_DIR")
		if output == "" {
			return
		}
		if err := privateDirectory(output); err != nil {
			t.Error(err)
			return
		}
		body, err := json.MarshalIndent(map[string]any{"runtime": "Antigravity ACP 1.5.2", "platform": "linux/amd64", "real_inference": true, "credential_copied": false, "paid_api_fallback": false, "tui_parity": false, "cases": records, "pass": !t.Failed()}, "", "  ")
		if err != nil {
			t.Error(err)
			return
		}
		if err = os.WriteFile(filepath.Join(output, "summary.json"), append(body, '\n'), 0600); err != nil {
			t.Error(err)
		}
	}()
	r := New(o)
	req := core.ExecutorRequest{Executor: "antigravity", FrontendSessionID: "official-frontend", Prompt: "Remember the codeword AZUL-IVOAI-739. Answer exactly AZUL-IVOAI-739. Do not call any tools."}
	for i, prompt := range []string{req.Prompt, "What codeword did I ask you to remember? Answer only that codeword. Do not call any tools."} {
		req.Prompt = prompt
		answer := ""
		result, err := r.Run(context.Background(), req, func(s string) error { answer += s; return nil })
		passed := err == nil && strings.Contains(answer, "AZUL-IVOAI-739") && result.Trace != nil && result.Trace.ExitCode == 0 && result.Trace.Signal == ""
		name := []string{"natural_prompt_normal_exit", "owned_continuation_normal_exit"}[i]
		records = append(records, record{name, passed, errorClass(err), sanitizedTrace(result.Trace)})
		if !passed {
			t.Fatalf("%s: err=%v expected codeword=%v trace=%#v", name, err, strings.Contains(answer, "AZUL-IVOAI-739"), result.Trace)
		}
		req.ExecutorSessionID = result.ExecutorSessionID
	}
	req.Prompt = "Read fixture.txt using your read_file tool. Reply only with the exact file contents. Do not edit or run commands."
	answer := ""
	readResult, readRunErr := r.Run(context.Background(), req, func(s string) error { answer += s; return nil })
	readPassed := readRunErr == nil && strings.TrimSpace(answer) == "ORIGINAL" && reads > 0
	records = append(records, record{"scoped_code_recovery", readPassed, errorClass(readRunErr), sanitizedTrace(readResult.Trace)})
	if !readPassed {
		t.Fatalf("scoped read err=%v exactAnswer=%v readApprovals=%d", readRunErr, strings.TrimSpace(answer) == "ORIGINAL", reads)
	}
	// The actual client may report end_turn and exit zero after a soft denial.
	// The adapter must still reject completion and withhold its answer.
	req.Prompt = "Read fixture.txt and replace ORIGINAL with MODIFIED using your file tools. Do not merely describe a change."
	emitted := false
	result, err := r.Run(context.Background(), req, func(string) error { emitted = true; return nil })
	content, readErr := os.ReadFile(fixture)
	passed := core.FailureClass(err) == "ANTIGRAVITY_PERMISSION_DENIED" && !emitted && readErr == nil && string(content) == "ORIGINAL\n"
	records = append(records, record{"denied_operation_no_replay", passed, errorClass(err), sanitizedTrace(result.Trace)})
	if !passed {
		t.Fatalf("denial err=%v emitted=%v canaryIntact=%v", err, emitted, string(content) == "ORIGINAL\n")
	}
	before, _ := os.ReadFile(fixture)
	sum := sha256.Sum256(before)
	t.Logf("official authenticated ACP: two owned turns, normal EOF shutdown, denied operation; fixture sha256=%s", hex.EncodeToString(sum[:]))
}

func errorClass(err error) string {
	if err == nil {
		return ""
	}
	return core.FailureClass(err)
}
func sanitizedTrace(trace *core.ExecutionTrace) *core.ExecutionTrace {
	if trace == nil {
		return nil
	}
	v := *trace
	v.Executable = "reviewed-official-agy"
	v.Directory = "isolated-fixture"
	v.PID = 0
	return &v
}

// An official-inference transport check, with a deliberately synthetic MCP
// tool. It proves the client's MCP/permission wire shape, not Core scheduling.
func TestOfficialAntigravityACPMCPTransport(t *testing.T) {
	binary := os.Getenv("IVOAI_ANTIGRAVITY_OFFICIAL_BINARY")
	if binary == "" {
		t.Skip("set IVOAI_ANTIGRAVITY_OFFICIAL_BINARY for real authenticated integration")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "mcp.py")
	body := `import json,sys,pathlib
pathlib.Path(__file__).with_suffix('.started').write_text('started')
for line in sys.stdin:
 r=json.loads(line)
 with pathlib.Path(__file__).with_suffix('.audit').open('a') as audit: audit.write(json.dumps({'method':r.get('method'),'protocol':r.get('params',{}).get('protocolVersion')})+'\n')
 if 'id' not in r: continue
 m=r['method']
 if m=='initialize': v={'protocolVersion':'2024-11-05','capabilities':{'tools':{}},'serverInfo':{'name':'ivoai-wire-fixture','version':'1'}}
 elif m=='tools/list': v={'tools':[{'name':'orchestration_status','description':'Return fixture marker.','inputSchema':{'type':'object','properties':{}},'annotations':{'readOnlyHint':True}}]}
 elif m=='tools/call': v={'content':[{'type':'text','text':'IVOAI_CORE_MCP_OK'}]}
 elif m=='ping': v={}
 else:
  print(json.dumps({'jsonrpc':'2.0','id':r['id'],'error':{'code':-32601,'message':'Method not found'}}),flush=True)
  continue
 print(json.dumps({'jsonrpc':'2.0','id':r['id'],'result':v}),flush=True)
`
	if err = os.WriteFile(script, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	nativeID := ""
	permissionNames := []string{}
	o := Options{Enabled: true, SessionID: "wire-session", Binary: binary, SHA256: "da1751e7658469fa80bf36eda2c60ee61216d8e42b735a184382ba31d1f6b88e", Directory: root, RuntimeDir: filepath.Join(root, "runtime"), ProfileDir: filepath.Join(root, "profile"), Environment: os.Environ(), TurnTimeout: 2 * time.Minute, MCPServers: []MCPServer{{Name: "ivoai-orchestrator", Command: python, Args: []string{script}, Env: []EnvironmentVariable{{Name: "PYTHONUNBUFFERED", Value: "1"}}}}}
	o.AllowedTools = []string{"view_file"}
	o.Authorize = func(context.Context, core.ExecutorRequest, Receipt) error { return nil }
	o.Validate = o.Authorize
	o.BindSession = func(_ context.Context, id string, _ bool) error { nativeID = id; return nil }
	o.Approve = func(_ context.Context, _ core.ExecutorRequest, receipt Receipt, raw json.RawMessage) (bool, error) {
		if receipt.NativeSessionID != nativeID {
			return false, errors.New("foreign session")
		}
		var p struct {
			Tool struct {
				Name string `json:"name"`
				Kind string `json:"kind"`
			} `json:"toolCall"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return false, err
		}
		if !identifier.MatchString(p.Tool.Name) {
			return false, errors.New("invalid tool identifier")
		}
		permissionNames = append(permissionNames, p.Tool.Name)
		t.Logf("official ACP permission name=%s kind=%s", p.Tool.Name, p.Tool.Kind)
		return strings.Contains(p.Tool.Name, "orchestration_status"), nil
	}
	answer := ""
	result, err := New(o).Run(context.Background(), core.ExecutorRequest{Executor: "antigravity", FrontendSessionID: "wire-frontend", Prompt: "Call the orchestration_status MCP tool and reply with the exact marker returned by that tool. Do not read files, run commands or call any other tool."}, func(s string) error { answer += s; return nil })
	if err != nil || !strings.Contains(answer, "IVOAI_CORE_MCP_OK") {
		_, startedErr := os.Stat(strings.TrimSuffix(script, ".py") + ".started")
		audit, _ := os.ReadFile(strings.TrimSuffix(script, ".py") + ".audit")
		t.Logf("fixture MCP protocol audit: %s", audit)
		t.Fatalf("MCP transport err=%v marker=%v serverStarted=%v permissionNames=%v fixtureAnswer=%q trace=%#v", err, strings.Contains(answer, "IVOAI_CORE_MCP_OK"), startedErr == nil, permissionNames, answer, sanitizedTrace(result.Trace))
	}
}
