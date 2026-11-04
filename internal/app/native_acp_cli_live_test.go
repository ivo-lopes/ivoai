package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/session"
)

// Opt-in public entrypoint proof. This consumes actual cached subscription
// inference through the reviewed official AGY binary; it uses no fake provider.
func TestAntigravityPublicManagedCLI(t *testing.T) {
	if os.Getenv("IVOAI_ANTIGRAVITY_CLI_LIVE") != "1" {
		t.Skip("set IVOAI_ANTIGRAVITY_CLI_LIVE=1 for official authenticated CLI proof")
	}
	if _, _, err := reviewedAntigravity(context.Background()); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	binary := os.Getenv("IVOAI_ANTIGRAVITY_CLI_BINARY")
	if binary == "" {
		binary = filepath.Join(root, "ivoai")
		build := exec.Command("go", "build", "-o", binary, "./cmd/ivoai")
		build.Dir = "../.."
		build.Env = os.Environ()
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build: %v %s", err, out)
		}
	}
	for _, scenario := range []string{"simple_continuation", "code_evidence", "read_denial"} {
		t.Run(scenario, func(t *testing.T) {
			work := filepath.Join(root, scenario)
			if err := os.Mkdir(work, 0700); err != nil {
				t.Fatal(err)
			}
			repo := filepath.Join(work, "repo")
			if err := os.Mkdir(repo, 0700); err != nil {
				t.Fatal(err)
			}
			files := map[string]string{"go.mod": "module fixture.local/calc\n\ngo 1.24.0\n", "calc.go": "package calc\n\n// Add sums two integers.\nfunc Add(a,b int)int{return a+b}\n", "canary.txt": "PRIVATE_FIXTURE_CANARY\n"}
			for name, body := range files {
				if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, args := range [][]string{{"init", "-q"}, {"add", "."}} {
				cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git: %v %s", err, out)
				}
			}
			env := os.Environ()
			for _, pair := range []struct{ name, dir string }{{"XDG_CONFIG_HOME", "config"}, {"XDG_DATA_HOME", "data"}, {"XDG_STATE_HOME", "state"}, {"XDG_CACHE_HOME", "cache"}} {
				path := filepath.Join(work, pair.dir)
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				env = setAppEnvironment(env, pair.name, path)
			}
			configDir := filepath.Join(work, "config", "ivoai")
			if err := os.Mkdir(configDir, 0700); err != nil {
				t.Fatal(err)
			}
			// Explicit read-auto policy for this controlled fixture. Production
			// retains its configured policy and terminal approval boundary.
			if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("[ivoai]\nversion=1\n[opencode]\npermission_mode=\"full\"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			env = setAppEnvironment(env, antigravityFlag, "1")
			prompt := "Remember IVOAI-NATIVE-845. Answer only IVOAI-NATIVE-845. Do not call tools."
			args := []string{"antigravity", "run", "--directory", repo, "--prompt", prompt}
			if scenario == "simple_continuation" {
				args = append(args, "--follow-up", "What codeword did I ask you to remember? Answer only that codeword. Do not call tools.")
			}
			if scenario == "code_evidence" {
				args[len(args)-1] = "Where is Add implemented? Use the projected Code Intelligence code_inspect tool and cite its file and line. Do not change files or create a DAG."
			}
			if scenario == "read_denial" {
				args[len(args)-1] = "Read canary.txt using view_file. Do not call any MCP, create a plan or delegate work."
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			command := exec.CommandContext(ctx, binary, args...)
			command.Env = env
			started := time.Now()
			out, err := command.CombinedOutput()
			duration := time.Since(started)
			values, listErr := (session.Store{Root: filepath.Join(work, "state", "ivoai", "sessions")}).List()
			if listErr != nil || len(values) != 1 {
				t.Fatalf("sessions=%d err=%v output=%s", len(values), listErr, out)
			}
			value := values[0]
			if len(value.Tasks) != 0 || len(value.Workers) != 0 || value.PlanID != "" {
				t.Fatal("simple/read-only invocation created a DAG")
			}
			if scenario == "read_denial" {
				if strings.Contains(string(out), "PRIVATE_FIXTURE_CANARY") {
					t.Fatalf("denied read escaped: err=%v", err)
				}
			} else {
				if err != nil || value.State != session.StateCompleted {
					t.Fatalf("real native invocation: %v %s", err, out)
				}
				if scenario == "simple_continuation" && strings.Count(string(out), "IVOAI-NATIVE-845") != 2 {
					t.Fatal("owned native conversation did not continue")
				}
				if scenario == "code_evidence" && (!strings.Contains(string(out), "calc.go") || !strings.Contains(string(out), "4")) {
					t.Fatal("exact repository evidence absent")
				}
				if scenario == "code_evidence" {
					observed := false
					for _, event := range value.Observability {
						if string(event.Operation) == "code.inspect" && event.Provider == "direct" && string(event.State) == "completed" {
							observed = true
						}
					}
					if !observed {
						t.Fatal("answer did not use existing Code Intelligence Direct")
					}
				}

				for _, raw := range value.TurnAttempts {
					var attempt struct {
						State  string `json:"turn_state"`
						Native string `json:"native_session_id"`
						Trace  struct {
							Exit   int    `json:"exit_code"`
							Signal string `json:"signal"`
						} `json:"trace"`
					}
					if json.Unmarshal(raw, &attempt) != nil || attempt.State != "completed" || attempt.Native != value.ExecutorSessionID || attempt.Trace.Exit != 0 || attempt.Trace.Signal != "" {
						t.Fatalf("invalid native completion %s", raw)
					}
				}
			}
			for name, body := range files {
				current, e := os.ReadFile(filepath.Join(repo, name))
				if e != nil || string(current) != body {
					t.Fatalf("isolation changed %s", name)
				}
			}
			t.Logf("real official CLI: scenario=%s duration=%s state=%s turns=%d no_dag=true", scenario, duration, value.State, len(value.TurnAttempts))
		})
	}
}
