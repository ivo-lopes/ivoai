//go:build linux

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

// This protocol peer is deliberately synthetic. Official inference is a
// separate opt-in test, and cannot be inferred from these adversarial cases.
const peer = `
import json, os, pathlib, sys, time
scenario = SCENARIO
pathlib.Path('started').write_text('yes')
def send(value):
 print(json.dumps(value), flush=True)
session='fixture-owned-session'
for line in sys.stdin:
 request=json.loads(line)
 method=request.get('method')
 if method=='initialize':
  assert not any(k in os.environ for k in ['GEMINI_API_KEY','GOOGLE_API_KEY','ANTHROPIC_API_KEY','NODE_OPTIONS'])
  if scenario=='duplicate_json':
   print('{"jsonrpc":"2.0","id":1,"result":{},"result":{}}',flush=True)
   continue
  result={'protocolVersion':1,'agentInfo':{'name':'Antigravity ACP Server','version':'1.5.0'},'agentCapabilities':{'loadSession':True}}
 elif method in ['session/new','session/load']:
  if scenario in ['announcement','foreign_announcement']:
   send({'jsonrpc':'2.0','method':'session/update','params':{'sessionId':'foreign' if scenario=='foreign_announcement' else session,'update':{'sessionUpdate':'config_option_update'}}})
  result={'sessionId':request['params'].get('sessionId',session),'models':{'currentModelId':'gemini-4-argon-low','availableModels':[{'modelId':'gemini-4-argon-low'}]}}
 elif method=='session/set_model':
  result={}
 elif method=='session/prompt':
  if scenario=='hang':
   time.sleep(30)
  if scenario in ['deny','replay','allow']:
   permission={'jsonrpc':'2.0','id':'permission-1','method':'session/request_permission','params':{'sessionId':session,'toolCall':{'name':'write_file','rawInput':{'Path':'canary'}},'options':[{'optionId':'once','kind':'allow_once'},{'optionId':'always','kind':'allow_always'}]}}
   send(permission)
   approval=json.loads(sys.stdin.readline())
   expected='selected' if scenario in ['allow','replay'] else 'cancelled'
   assert approval['result']['outcome']['outcome']==expected
   if scenario=='allow': assert approval['result']['outcome']['optionId']=='once'
   if scenario=='replay': send(permission)
  send({'jsonrpc':'2.0','method':'session/update','params':{'sessionId':'foreign' if scenario=='foreign' else session,'update':{'sessionUpdate':'agent_message_chunk','content':{'type':'text','text':'answer'}}}})
  result={'stopReason':'end_turn','usage':{'inputTokens':5,'outputTokens':3,'cachedReadTokens':2}}
 else:
  raise RuntimeError('unexpected method')
 send({'jsonrpc':'2.0','id':request['id'],'result':result})
if scenario=='exit_fail': sys.exit(7)
`

func fixture(t *testing.T, scenario string) (Options, core.ExecutorRequest) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("python3 is required for the adversarial protocol fixture")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	body := "#!" + python + "\n" + strings.Replace(peer, "SCENARIO", "'"+scenario+"'", 1)
	binary := filepath.Join(root, "native-peer")
	if err := os.WriteFile(binary, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(body))
	owned := ""
	o := Options{Enabled: true, SessionID: "core-session", Binary: binary, SHA256: hex.EncodeToString(sum[:]), Directory: root, RuntimeDir: filepath.Join(root, "runtime"), ProfileDir: filepath.Join(root, "profile"), Environment: []string{"HOME=" + root, "PATH=/usr/bin:/bin", "GOOGLE_API_KEY=never-forward", "GEMINI_API_KEY=never-forward", "ANTHROPIC_API_KEY=never-forward", "NODE_OPTIONS=--require=untrusted"}, TurnTimeout: 2 * time.Second}
	o.Authorize = func(context.Context, core.ExecutorRequest, Receipt) error { return nil }
	o.Validate = o.Authorize
	o.BindSession = func(_ context.Context, id string, resumed bool) error {
		if resumed && owned != id || !resumed && owned != "" {
			return errors.New("ownership mismatch")
		}
		owned = id
		return nil
	}
	return o, core.ExecutorRequest{Executor: "antigravity", FrontendSessionID: "frontend-session", Prompt: "Explique este projeto."}
}

func TestBoundedTurnAndOwnedContinuation(t *testing.T) {
	o, req := fixture(t, "normal")
	r := New(o)
	for i := 0; i < 2; i++ {
		answer := ""
		result, err := r.Run(context.Background(), req, func(s string) error { answer += s; return nil })
		if err != nil {
			t.Fatal(err)
		}
		if answer != "answer" || result.Trace.ExitCode != 0 || result.Trace.Signal != "" || !result.Trace.FinalResponse || !result.Policy.NoReplay || !result.Policy.NoAutomaticFailover {
			t.Fatalf("bad result: %#v answer=%q", result, answer)
		}
		if result.Trace.Usage == nil || result.Trace.Usage.InputTokens != 5 || *result.Trace.Usage.CachedInputTokens != 2 {
			t.Fatal("official-report provenance lost")
		}
		req.ExecutorSessionID = result.ExecutorSessionID
	}
}

func TestUntrustedNativeFailuresWithholdAnswer(t *testing.T) {
	for scenario, class := range map[string]string{"deny": "ANTIGRAVITY_PERMISSION_DENIED", "replay": "ANTIGRAVITY_REQUEST_REPLAY", "foreign": "ANTIGRAVITY_SESSION_MISMATCH", "duplicate_json": "ANTIGRAVITY_PROTOCOL_INVALID", "exit_fail": "ANTIGRAVITY_EXIT_FAILED", "foreign_announcement": "ANTIGRAVITY_SESSION_MISMATCH"} {
		t.Run(scenario, func(t *testing.T) {
			o, req := fixture(t, scenario)
			if scenario == "replay" {
				o.Approve = func(context.Context, core.ExecutorRequest, Receipt, json.RawMessage) (bool, error) { return true, nil }
			}
			emitted := false
			result, err := New(o).Run(context.Background(), req, func(string) error { emitted = true; return nil })
			if core.FailureClass(err) != class || emitted || !result.Policy.NoReplay {
				t.Fatalf("class=%s err=%v emitted=%v", core.FailureClass(err), err, emitted)
			}
		})
	}
}

func TestHostApprovalIsOnceAndLateRevocationWithholdsAnswer(t *testing.T) {
	o, req := fixture(t, "allow")
	approved := false
	o.Approve = func(_ context.Context, _ core.ExecutorRequest, receipt Receipt, raw json.RawMessage) (bool, error) {
		if receipt.NativeSessionID != "fixture-owned-session" || !strings.Contains(string(raw), "write_file") {
			t.Fatal("uncorrelated approval")
		}
		approved = true
		return true, nil
	}
	if _, err := New(o).Run(context.Background(), req, func(string) error { return nil }); err != nil || !approved {
		t.Fatalf("approve err=%v", err)
	}
	o, req = fixture(t, "normal")
	o.Validate = func(context.Context, core.ExecutorRequest, Receipt) error { return errors.New("revoked") }
	if _, err := New(o).Run(context.Background(), req, func(string) error { t.Fatal("late denied answer escaped"); return nil }); core.FailureClass(err) != "ANTIGRAVITY_VALIDATION_DENIED" {
		t.Fatal(err)
	}
}

func TestAdmissionAndRuntimeIdentityFailBeforeLaunch(t *testing.T) {
	for _, scenario := range []string{"admission", "digest", "profile"} {
		t.Run(scenario, func(t *testing.T) {
			o, req := fixture(t, "normal")
			switch scenario {
			case "admission":
				o.Authorize = func(context.Context, core.ExecutorRequest, Receipt) error { return errors.New("revoked") }
			case "digest":
				o.SHA256 = strings.Repeat("0", 64)
			case "profile":
				if err := os.MkdirAll(filepath.Dir(settingsPath(o.ProfileDir)), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(settingsPath(o.ProfileDir), []byte(`{"toolPermission":"allow"}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := New(o).Run(context.Background(), req, func(string) error { return nil }); err == nil {
				t.Fatal("unsafe launch accepted")
			}
			if _, err := os.Stat(filepath.Join(o.Directory, "started")); !os.IsNotExist(err) {
				t.Fatal("rejected operation launched")
			}
		})
	}
}

func TestCancellationReapsOwnedProcess(t *testing.T) {
	o, req := fixture(t, "hang")
	o.TurnTimeout = 150 * time.Millisecond
	started := time.Now()
	result, err := New(o).Run(context.Background(), req, func(string) error { t.Fatal("canceled answer escaped"); return nil })
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 2*time.Second || result.Trace == nil || result.Trace.ExitCode == 0 {
		t.Fatalf("err=%v trace=%#v", err, result.Trace)
	}
}

func TestPrivateProfileLockAndUnsafePaths(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, "profile")
	_, unlock, err := prepareProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, _, err := prepareProfile(profile); core.FailureClass(err) != "ANTIGRAVITY_BUSY" {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(profile, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepareProfile(link); err == nil {
		t.Fatal("symlink profile accepted")
	}
}

func TestSessionAnnouncementBindsOnlyAfterNativeConfirmation(t *testing.T) {
	o, req := fixture(t, "announcement")
	if _, err := New(o).Run(context.Background(), req, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestNativeRPCIdentityUsesCanonicalJSONValues(t *testing.T) {
	a, err := requestID(json.RawMessage(`"permission-1"`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := requestID(json.RawMessage(`"permission-\u0031"`))
	if err != nil || a != b {
		t.Fatalf("escaped RPC replay not normalized: %q %q %v", a, b, err)
	}
	for _, raw := range []string{`null`, `1.0`, `-1`, `[]`, `{}`, `"with space"`} {
		if _, err := requestID(json.RawMessage(raw)); err == nil {
			t.Fatalf("invalid RPC identity accepted: %s", raw)
		}
	}
}

func TestPrivateAgentCannotEnableNativeDelegationOrShell(t *testing.T) {
	root := t.TempDir()
	for _, tool := range []string{"invoke_subagent", "run_command", "write_file", "read_file"} {
		if _, _, err := prepareAgent(root, []string{tool}); core.FailureClass(err) != "ANTIGRAVITY_TOOL_PROJECTION_INVALID" {
			t.Fatalf("unsafe native tool admitted %s: %v", tool, err)
		}
	}
	path, body, err := prepareAgent(root, []string{"view_file"})
	if err != nil {
		t.Fatal(err)
	}
	read, err := os.ReadFile(path)
	if err != nil || string(read) != string(body) || !strings.Contains(string(body), "subagent: false") {
		t.Fatal("private agent projection lost")
	}
}
