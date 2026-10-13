package codeintel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/codexresolver"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/externalmcp"
)

// One paired sample per scenario: reproducible correctness evidence, not a
// statistically powered savings claim. Provider caching is reported, not hidden.
func TestExtendedLiveCodexBenchmark(t *testing.T) {
	if os.Getenv("IVOAI_GRAFT_EXTENDED_LIVE") != "1" {
		t.Skip("explicit subscription benchmark disabled")
	}
	codex, node, dist := os.Getenv("IVOAI_LIVE_CODEX_PATH"), os.Getenv("IVOAI_GRAFT_TEST_NODE"), os.Getenv("IVOAI_GRAFT_TEST_DIST")
	if codex == "" || node == "" || dist == "" {
		t.Fatal("official Codex and reviewed artifact required")
	}
	projectionExperiment := os.Getenv("IVOAI_ECONOMIC_PROJECTION") == "1"
	repetitions := 1
	if projectionExperiment {
		repetitions = 3
		guardExtendedCodexConfig(t)
	}
	type scenario struct {
		name, prompt   string
		expected       []string
		edit           bool
		file, contains string
		real           bool
	}
	cases := []scenario{
		{name: "implementation", prompt: "Locate Scale, explain its operation and report Entry's numeric result.", expected: []string{"Scale", "Entry", "6"}},
		{name: "dependencies", prompt: "Identify the function that directly calls Scale and the files defining caller and callee.", expected: []string{"Entry", "app/entry.go", "mathpkg/scale.go"}},
		{name: "shared_function_change", prompt: "Change Scale's multiplier from 2 to 4. Preserve its name and Entry's call. Update the test expectation from 6 to 12. Report Entry's new result.", expected: []string{"12"}, edit: true, file: "mathpkg/scale.go", contains: "* 4"},
		{name: "cross_file_refactor", prompt: "Rename Scale to Double in its declaration and every source caller, preserving behavior. Do not alter the test assertion. Report the renamed function.", expected: []string{"Double"}, edit: true, file: "mathpkg/scale.go", contains: "func Double"},
		{name: "bug_localization", prompt: "Fix BrokenAdd so it adds its two inputs instead of subtracting them. Preserve its name and signature. Report BrokenAdd(7, 2).", expected: []string{"9"}, edit: true, file: "mathpkg/scale.go", contains: "a + b"},
		{name: "api_signatures", prompt: "Report the public signature of Report and which package defines it. Do not enumerate unrelated implementations.", expected: []string{"Report", "string", "mathpkg"}},
		{name: "real_ivoai_navigation", prompt: "Orient me in this IVOAI repository: locate the code intelligence Provider and its Inspect and ForTask methods. Report their repository-relative files. Read only, do not build or modify this real repository.", expected: []string{"internal/codeintel", "Inspect", "ForTask"}, real: true},
		{name: "documentation_only", prompt: "Read README.md and report the documented retention marker. This is a documentation lookup; do not inspect code dependencies.", expected: []string{"LOCAL_RETENTION_17"}},
		{name: "exact_string", prompt: "Locate the exact literal FIXTURE_GATE_73 and report its file. No dependency analysis is needed.", expected: []string{"app/entry.go"}},
		{name: "known_small_file", prompt: "In the already identified README.md replace the heading '# Fixture' with '# Fixture Updated'. Preserve all other text. Report completion.", edit: true, file: "README.md", contains: "# Fixture Updated"},
		{name: "multiple_languages", prompt: "Compare Scale in Go and py_double in Python: report their files and the numeric value each returns for input 3.", expected: []string{"mathpkg/scale.go", "helpers.py", "6"}},
		{name: "worktree_uncommitted", prompt: "Read the current worktree's UncommittedValue implementation, not just HEAD. Report its numeric return value and file.", expected: []string{"78", "dirty.go"}},
	}
	type row struct {
		Scenario, Arm, Model, Effort                                          string
		Repetition                                                            int
		ToolsPresented                                                        int
		InventoryRequests, ToolSchemaBytes                                    int
		ToolSequence                                                          []string
		ProjectionExperiment                                                  bool
		DurationMS                                                            int64
		InputTokens, CachedInputTokens, OutputTokens                          any
		ToolDescriptionTokens, ReasoningTokens                                any
		ToolCalls, NativeCalls, StructuralCalls, CacheHits, NativeResultBytes int
		CPUTimeMS, PeakMemoryBytes                                            *int64
		CodexProcessCPUMS, CodexPeakMemoryBytes                               *int64
		Correct                                                               bool
		Error                                                                 string `json:",omitempty"`
	}
	var rows []row
	persist := func() {
		if path := os.Getenv("IVOAI_GRAFT_EXTENDED_OUTPUT"); path != "" {
			body, e := json.MarshalIndent(rows, "", "  ")
			if e == nil {
				e = os.WriteFile(path, body, 0600)
			}
			if e != nil {
				t.Errorf("sanitized evidence write: %v", e)
			}
		}
	}
	defer persist()
	for repetition := range repetitions {
		for scenarioIndex, s := range cases {
			arms := []string{"A", "B"}
			if projectionExperiment && (repetition+scenarioIndex)%2 == 1 {
				arms = []string{"B", "A"}
			}
			for _, arm := range arms {
				name := s.name + "/" + arm
				if projectionExperiment {
					name = fmt.Sprintf("%s/repeat%d/%s", s.name, repetition, arm)
				}
				t.Run(name, func(t *testing.T) {
					p, _ := testProvider(t)
					p.Node = node
					p.Distribution = dist
					p.Executable = os.Args[0]
					p.run = p.runProcess
					if s.real {
						root := os.Getenv("IVOAI_GRAFT_BENCHMARK_REPO")
						if root == "" {
							t.Fatal("explicit read-only IVOAI repository required")
						}
						p.Root = root
					} else {
						extendedFixture(t, p.Root)
						if s.name == "worktree_uncommitted" {
							p.Root = extendedWorktree(t, p.Root)
						}
					}
					before := extendedTreeDigest(t, p.Root)
					baseline := map[string][]byte{}
					if s.edit {
						files, err := Inventory(context.Background(), p.Root)
						if err != nil {
							t.Fatal(err)
						}
						snapshot, err := Snapshot(p.Root, files)
						if err != nil {
							t.Fatal(err)
						}
						for _, f := range snapshot.Files {
							baseline[f.Path] = f.Body
						}
						baseline["README.md"], err = os.ReadFile(filepath.Join(p.Root, "README.md"))
						if err != nil {
							t.Fatal(err)
						}
					}
					r := row{Scenario: s.name, Arm: arm, Model: "gpt-6.1-sol", Effort: "low", Repetition: repetition, ProjectionExperiment: projectionExperiment}
					var mu sync.Mutex
					p.Observe = func(result core.CodeResult, err error) {
						if err != nil {
							return
						}
						mu.Lock()
						defer mu.Unlock()
						r.NativeCalls++
						r.NativeResultBytes += len(result.Text)
						if result.ProviderVersion != "direct" {
							r.StructuralCalls++
						}
						if result.CacheHit {
							r.CacheHits++
						}
						if result.CPUMS != nil {
							if r.CPUTimeMS == nil {
								r.CPUTimeMS = new(int64)
							}
							*r.CPUTimeMS += *result.CPUMS
						}
						if result.PeakMemoryBytes != nil && (r.PeakMemoryBytes == nil || *result.PeakMemoryBytes > *r.PeakMemoryBytes) {
							v := *result.PeakMemoryBytes
							r.PeakMemoryBytes = &v
						}
					}
					projected := ToolNames()
					if projectionExperiment && arm == "B" {
						projected = ProjectTools(s.prompt)
					}
					if projectionExperiment || arm == "B" {
						r.ToolsPresented = len(projected)
					}
					gateway, e := externalmcp.Start([]externalmcp.Target{{Name: "ivoai-code", Local: p.Handler(), Restricted: true, AllowedTools: projected}}, "full")
					if e != nil {
						t.Fatal(e)
					}
					defer gateway.Close()
					gateway.SetInventoryObserver(func(_ string, m externalmcp.InventoryMetrics) {
						mu.Lock()
						defer mu.Unlock()
						r.InventoryRequests++
						r.ToolSchemaBytes = m.SchemaBytesAfter
						r.ToolsPresented = m.Presented
					})
					sandbox := "read-only"
					if s.edit {
						sandbox = "workspace-write"
					}
					args := []string{"exec", "--ignore-user-config", "--ephemeral", "--sandbox", sandbox, "--skip-git-repo-check", "--json", "--color", "never", "-m", r.Model, "-c", `model_reasoning_effort="low"`, "-C", p.Root}
					// Reuse the managed executor's explicit untrusted override. Codex
					// otherwise persists inferred trust for workspace-write exec.
					args = append(args, codexresolver.ProjectTrustArgs(p.Root)...)
					env := []string{}
					for _, entry := range os.Environ() {
						name, _, _ := strings.Cut(entry, "=")
						if name != "OPENAI_API_KEY" && name != "CODEX_API_KEY" {
							env = append(env, entry)
						}
					}
					if arm == "B" || projectionExperiment {
						args = append(args, "-c", "mcp_servers.ivoai-code.url="+strconv.Quote(gateway.URL(0)), "-c", `mcp_servers.ivoai-code.bearer_token_env_var="IVOAI_TEST_CODE_TOKEN"`, "-c", `mcp_servers.ivoai-code.default_tools_approval_mode="approve"`)
						env = append(env, "IVOAI_TEST_CODE_TOKEN="+gateway.Token())
					}
					args = append(args, "Authorized local benchmark. Use an approved repository inspection capability when available and appropriate; avoid redundant exploration. Do not access external targets, accounts, credentials or external network services. Do not run installation commands. Host-side validation runs offline after your turn. "+s.prompt+" Respond concisely with the requested facts.")
					ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, codex, args...)
					cmd.Env = env
					out := &extendedBoundedBuffer{max: 2 << 20}
					cmd.Stdout = out
					cmd.Stderr = &extendedBoundedBuffer{max: 64 << 10}
					start := time.Now()
					e = cmd.Run()
					r.DurationMS = time.Since(start).Milliseconds()
					if cmd.ProcessState != nil {
						if usage, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
							cpu := usage.Utime.Sec*1000 + usage.Utime.Usec/1000 + usage.Stime.Sec*1000 + usage.Stime.Usec/1000
							rss := usage.Maxrss * 1024
							r.CodexProcessCPUMS = &cpu
							r.CodexPeakMemoryBytes = &rss
						}
					}
					if e != nil {
						r.Error = "provider_execution_failed"
						t.Error(r.Error)
					}
					var answer strings.Builder
					for _, line := range bytes.Split(out.Bytes(), []byte("\n")) {
						var event struct {
							Type  string
							Item  struct{ Type, Text, Tool string }
							Usage map[string]any
						}
						if json.Unmarshal(line, &event) != nil {
							continue
						}
						if event.Type == "turn.completed" {
							r.InputTokens = event.Usage["input_tokens"]
							r.CachedInputTokens = event.Usage["cached_input_tokens"]
							r.OutputTokens = event.Usage["output_tokens"]
						}
						if event.Type == "item.completed" && (event.Item.Type == "command_execution" || event.Item.Type == "mcp_tool_call") {
							r.ToolCalls++
							label := event.Item.Type
							if event.Item.Type == "mcp_tool_call" {
								for _, tool := range ToolNames() {
									if event.Item.Tool == tool {
										label += ":" + tool
										break
									}
								}
							}
							if len(r.ToolSequence) < 64 {
								r.ToolSequence = append(r.ToolSequence, label)
							}
						}
						if event.Item.Type == "agent_message" {
							answer.WriteString(event.Item.Text)
						}
					}
					r.Correct = e == nil && r.InputTokens != nil
					for _, expected := range s.expected {
						if !strings.Contains(answer.String(), expected) {
							r.Correct = false
						}
					}
					if s.edit {
						body, err := os.ReadFile(filepath.Join(p.Root, s.file))
						if err != nil || !strings.Contains(string(body), s.contains) {
							r.Correct = false
						}
						if !extendedValidateEdit(p.Root, s.name, baseline) || !extendedCompile(t, p.Root, s.name) {
							r.Correct = false
						}
					} else if extendedTreeDigest(t, p.Root) != before {
						r.Correct = false
						t.Error("read-only scenario modified source")
					}
					if !r.Correct {
						t.Error("scenario correctness failed; private model output not persisted")
					}
					mu.Lock()
					rows = append(rows, r)
					mu.Unlock()
					persist()
				})
			}
		}
	}
}

type extendedBoundedBuffer struct {
	bytes.Buffer
	max int
}

func (b *extendedBoundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	left := b.max - b.Len()
	if left > 0 {
		if len(p) > left {
			p = p[:left]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}
func extendedWrite(t *testing.T, root, name, body string) {
	t.Helper()
	p := filepath.Join(root, name)
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
}
func extendedFixture(t *testing.T, root string) {
	extendedWrite(t, root, "main.go", "package fixture\n")
	extendedWrite(t, root, "go.mod", "module example.test/fixture\n\ngo 1.23\n")
	extendedWrite(t, root, "mathpkg/scale.go", "package mathpkg\nfunc Scale(v int) int { return v * 2 }\nfunc BrokenAdd(a,b int) int { return a - b }\nfunc Report(name string) string { return name }\n")
	extendedWrite(t, root, "app/entry.go", "package app\nimport \"example.test/fixture/mathpkg\"\n// FIXTURE_GATE_73\nfunc Entry() int { return mathpkg.Scale(3) }\n")
	extendedWrite(t, root, "app/entry_test.go", "package app\nimport \"testing\"\nfunc TestEntry(t *testing.T) { if Entry() != 6 {t.Fatal(Entry())} }\n")
	extendedWrite(t, root, "helpers.py", "def py_double(value):\n    return value * 2\n")
	extendedWrite(t, root, "README.md", "# Fixture\nRetention marker: LOCAL_RETENTION_17\n")
	extendedWrite(t, root, "dirty.go", "package fixture\nfunc UncommittedValue() int { return 77 }\n")
	for i := 0; i < 40; i++ {
		extendedWrite(t, root, fmt.Sprintf("noise/file%02d.go", i), fmt.Sprintf("package noise\nfunc Independent%02d() int {return %d}\n", i, i))
	}
	extendedGit(t, root, "add", ".")
	extendedGit(t, root, "commit", "-qm", "fixture baseline")
}
func extendedGit(t *testing.T, root string, args ...string) {
	t.Helper()
	argv := append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "commit.gpgsign=false", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-C", root}, args...)
	cmd := exec.Command("git", argv...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("fixture git: %v %s", e, out)
	}
}
func extendedWorktree(t *testing.T, root string) string {
	path := filepath.Join(t.TempDir(), "worktree")
	extendedGit(t, root, "worktree", "add", "--detach", path, "HEAD")
	extendedWrite(t, path, "dirty.go", "package fixture\nfunc UncommittedValue() int { return 78 }\n")
	return path
}
func extendedTreeDigest(t *testing.T, root string) string {
	t.Helper()
	files, e := Inventory(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := Snapshot(root, files)
	if e != nil {
		t.Fatal(e)
	}
	return snapshot.Digest
}
func extendedCompile(t *testing.T, root, scenario string, verificationCaches ...string) bool {
	expected := 6
	if scenario == "shared_function_change" {
		expected = 12
	}
	contract := fmt.Sprintf("package app\nimport \"testing\"\nfunc TestIVOAIIndependentAcceptance(t *testing.T){if Entry()!=%d {t.Fatal(\"incorrect Entry behavior\")}}\n", expected)
	if scenario == "bug_localization" {
		contract = "package app\nimport (\"testing\"; \"example.test/fixture/mathpkg\")\nfunc TestIVOAIIndependentAcceptance(t *testing.T){if mathpkg.BrokenAdd(7,2)!=9 || Entry()!=6 {t.Fatal(\"incorrect arithmetic behavior\")}}\n"
	}
	if scenario == "known_small_file" {
		body, e := os.ReadFile(filepath.Join(root, "README.md"))
		if e != nil || string(body) != "# Fixture Updated\nRetention marker: LOCAL_RETENTION_17\n" {
			return false
		}
	}
	extendedWrite(t, root, "app/ivoai_acceptance_test.go", contract)
	defer os.Remove(filepath.Join(root, "app/ivoai_acceptance_test.go"))

	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	home := t.TempDir()
	if len(verificationCaches) > 0 {
		home = verificationCaches[0]
	}
	cmd := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), "test", "-p=1", "./...")
	cmd.Dir = root
	cmd.Env = []string{"HOME=" + home, "GOCACHE=" + filepath.Join(home, "cache"), "GOPATH=" + home, "GOENV=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "GOWORK=off", "GOMAXPROCS=2", "CGO_ENABLED=0", "PATH=/usr/bin:/bin"}
	return cmd.Run() == nil
}

func TestExtendedBenchmarkFixtureAcceptance(t *testing.T) {
	p, _ := testProvider(t)
	extendedFixture(t, p.Root)
	cache := t.TempDir()
	if !extendedCompile(t, p.Root, "baseline", cache) {
		t.Fatal("valid baseline failed")
	}
	if extendedCompile(t, p.Root, "bug_localization", cache) {
		t.Fatal("known arithmetic defect incorrectly accepted")
	}
	body, e := os.ReadFile(filepath.Join(p.Root, "mathpkg/scale.go"))
	if e != nil {
		t.Fatal(e)
	}
	extendedWrite(t, p.Root, "mathpkg/scale.go", strings.Replace(string(body), "a - b", "a + b", 1))
	if !extendedCompile(t, p.Root, "bug_localization", cache) {
		t.Fatal("corrected arithmetic rejected")
	}
	other := extendedWorktree(t, p.Root)
	if extendedTreeDigest(t, p.Root) == extendedTreeDigest(t, other) {
		t.Fatal("worktree working change ignored")
	}
	b := &extendedBoundedBuffer{max: 3}
	if n, e := b.Write([]byte("private")); n != 7 || e != nil || b.String() != "pri" {
		t.Fatal("capture bound failed")
	}
}

// Host verification executes only the closed expected fixture program. Arbitrary
// model-authored code, extra files and links fail before Go test can execute it.
func extendedValidateEdit(root, scenario string, baseline map[string][]byte) bool {
	wanted := map[string][]byte{}
	for path, body := range baseline {
		text := string(body)
		switch scenario {
		case "shared_function_change":
			if path == "mathpkg/scale.go" {
				text = strings.Replace(text, "v * 2", "v * 4", 1)
			}
			if path == "app/entry_test.go" {
				text = strings.Replace(text, "!= 6", "!= 12", 1)
			}
		case "cross_file_refactor":
			if path == "mathpkg/scale.go" || path == "app/entry.go" {
				text = strings.ReplaceAll(text, "Scale", "Double")
			}
		case "bug_localization":
			if path == "mathpkg/scale.go" {
				text = strings.Replace(text, "a - b", "a + b", 1)
			}
		case "known_small_file":
			if path == "README.md" {
				text = strings.Replace(text, "# Fixture", "# Fixture Updated", 1)
			}
		}
		wanted[path] = []byte(text)
	}
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if rel == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		rel = filepath.ToSlash(rel)
		want, ok := wanted[rel]
		if !ok || entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("unexpected fixture file")
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(rel, ".go") {
			body, err = format.Source(body)
			if err != nil {
				return err
			}
			want, err = format.Source(want)
			if err != nil {
				return err
			}
		}
		if !bytes.Equal(body, want) {
			return fmt.Errorf("unexpected fixture program")
		}
		seen[rel] = true
		return nil
	})
	return err == nil && len(seen) == len(wanted)
}

func TestExtendedBenchmarkRejectsUnexpectedPrograms(t *testing.T) {
	p, _ := testProvider(t)
	extendedFixture(t, p.Root)
	files, e := Inventory(context.Background(), p.Root)
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := Snapshot(p.Root, files)
	if e != nil {
		t.Fatal(e)
	}
	baseline := map[string][]byte{}
	for _, f := range snapshot.Files {
		baseline[f.Path] = f.Body
	}
	baseline["README.md"], e = os.ReadFile(filepath.Join(p.Root, "README.md"))
	if e != nil {
		t.Fatal(e)
	}
	extendedWrite(t, p.Root, "mathpkg/scale.go", strings.Replace(string(baseline["mathpkg/scale.go"]), "a - b", "a + b", 1))
	if !extendedValidateEdit(p.Root, "bug_localization", baseline) {
		t.Fatal("closed expected edit rejected")
	}
	extendedWrite(t, p.Root, "extra.go", "package fixture\nfunc init(){panic(\"unapproved program\")}\n")
	if extendedValidateEdit(p.Root, "bug_localization", baseline) {
		t.Fatal("extra executable code accepted")
	}
	if e := os.Remove(filepath.Join(p.Root, "extra.go")); e != nil {
		t.Fatal(e)
	}
	extendedWrite(t, p.Root, "mathpkg/scale.go", string(baseline["mathpkg/scale.go"])+"\nfunc init(){}\n")
	if extendedValidateEdit(p.Root, "bug_localization", baseline) {
		t.Fatal("unexpected executable content accepted")
	}
}

// A subscription test must leave personal configuration byte-identical.
func guardExtendedCodexConfig(t *testing.T) {
	t.Helper()
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		user, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		home = filepath.Join(user, ".codex")
	}
	path := filepath.Join(home, "config.toml")
	before, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal("cannot guard personal configuration")
	}
	exists, hash := err == nil, sha256.Sum256(before)
	t.Cleanup(func() {
		after, err := os.ReadFile(path)
		if (err == nil) != exists || sha256.Sum256(after) != hash {
			t.Error("personal configuration changed")
		}
	})
}
