package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/benchmarkfixtures"
	"github.com/ivo-lopes/ivoai/internal/codeintel"
	"github.com/ivo-lopes/ivoai/internal/codexresolver"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

type prefetchLiveRow struct {
	CatalogSHA256, HarnessSHA256, FixtureSHA256                                                                    string
	Scenario, Arm, Model, Effort, DistributionInventorySHA256                                                      string
	TotalMS, ProviderMS, PrefetchMS                                                                                int64
	PromptBytes, EvidenceBytes, SourceBytes, ToolCalls, ToolResultBytes, PrefetchCalls, StructuralCalls, CacheHits int
	InputTokens, CachedInputTokens, OutputTokens, ReasoningTokens, ToolDescriptionTokens                           any
	NativeCPUMS, NativePeakMemoryBytes                                                                             *int64
	JSONDecoded, Correct                                                                                           bool
	Error                                                                                                          string `json:",omitempty"`
}

// This paired benchmark exercises the actual host prefetch/ArtifactStore
// contract. It does not claim a full frontend lifecycle or statistically powered
// savings. No MCP is exposed in either arm: B receives production host evidence.
func TestLivePrimaryCodePrefetchBenchmark(t *testing.T) {
	if os.Getenv("IVOAI_GRAFT_PREFETCH_LIVE") != "1" {
		t.Skip("explicit official subscription benchmark opt-in required")
	}
	codex, node, dist, binary := os.Getenv("IVOAI_LIVE_CODEX_PATH"), os.Getenv("IVOAI_GRAFT_TEST_NODE"), os.Getenv("IVOAI_GRAFT_TEST_DIST"), os.Getenv("IVOAI_GRAFT_SMOKE_BINARY")
	for name, path := range map[string]string{"Codex": codex, "Node": node, "reviewed distribution": dist, "managed sandbox helper": binary} {
		if !filepath.IsAbs(path) {
			t.Fatalf("absolute %s input required", name)
		}
		info, err := os.Stat(path)
		if err != nil || name == "reviewed distribution" && !info.IsDir() || name != "reviewed distribution" && (!info.Mode().IsRegular() || info.Mode()&0111 == 0) {
			t.Fatalf("invalid %s input", name)
		}
	}
	inventory, err := os.ReadFile(filepath.Join(dist, "files.json"))
	if err != nil {
		t.Fatal("reviewed distribution file inventory required")
	}
	inventoryHash := sha256.Sum256(inventory)
	guardPrefetchCodexConfig(t)
	runtimeArm := os.Getenv("IVOAI_ECONOMIC_RUNTIME_ARM")
	if runtimeArm != "" && runtimeArm != "A" && runtimeArm != "B" {
		t.Fatal("invalid comparison arm")
	}
	cases := []struct{ name, task, prompt string }{
		{"api", "Inspect API mathpkg/scale.go", "Report Scale's unqualified declaration identifier as symbol, source file, ordered parameter_types, and return_type as JSON."},
		{"implementation", "Locate function Scale", "Report Scale's unqualified declaration identifier as symbol, source file, input 3 and computed result as JSON."},
		{"multi_module_callers", "Identify callers of Scale", "Report every direct caller of Scale as a JSON callers array, each with unqualified declaration identifier as symbol and repository-relative file. Include only direct callers."},
	}
	if runtimeArm != "" {
		cases[0].task = "Report the public signature of Scale"
		cases[1].task = "Locate Scale and explain its operation"
		cases[2].task = "Identify the function that directly calls Scale"
	}

	for _, name := range []string{"dependencies", "monorepo", "unsupported_parser", "non_code"} {
		definition, ok := benchmarkfixtures.Lookup(name)
		if !ok || !definition.RunnerSupported {
			t.Fatal("invalid frozen benchmark scenario")
		}
		cases = append(cases, struct{ name, task, prompt string }{definition.ID, definition.Task, definition.Prompt})
	}
	var rows []prefetchLiveRow
	persist := func() {
		if path := os.Getenv("IVOAI_GRAFT_PREFETCH_OUTPUT"); path != "" {
			body, err := json.MarshalIndent(rows, "", "  ")
			if err == nil {
				err = os.WriteFile(path, body, 0600)
			}
			if err != nil {
				t.Error("cannot save sanitized prefetch measurements")
			}
		}
	}
	defer persist()
	for _, scenario := range cases {
		for _, arm := range []string{"A", "B"} {
			if runtimeArm != "" && arm != runtimeArm {
				continue
			}
			t.Run(scenario.name+"/"+arm, func(t *testing.T) {
				root := prefetchScenarioRepository(t, scenario.name)
				before := prefetchLiveSourceHash(t, root)
				fixtureHash, hashErr := benchmarkfixtures.DirectorySHA256(root)
				definition, supported := benchmarkfixtures.Lookup(scenario.name)
				expectedHash, _ := benchmarkfixtures.ProjectSHA256(definition.Fixture)
				if hashErr != nil || !supported || fixtureHash != expectedHash {
					t.Fatal("fixture identity mismatch")
				}
				a, store, id, task := prefetchLiveHost(t, root, scenario.task)
				row := prefetchLiveRow{Scenario: scenario.name, Arm: arm, Model: "gpt-6.1-sol", Effort: "low", DistributionInventorySHA256: hex.EncodeToString(inventoryHash[:])}
				row.CatalogSHA256, row.HarnessSHA256, row.FixtureSHA256 = benchmarkfixtures.CatalogSHA256(), prefetchHarnessSHA256(), fixtureHash
				defer func() {
					if !row.Correct && row.Error == "" {
						row.Error = "host_prefetch_or_fixture_failed"
					}
					rows = append(rows, row)
					persist()
				}()
				start := time.Now()
				var evidence json.RawMessage
				if arm == "B" || runtimeArm != "" {
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					evidence, err = a.preparePrimaryCodeEvidence(ctx, store, id, task, 4094, func(_ context.Context, directory, scope string) (*codeintel.Provider, error) {
						if directory != root || scope != "cs_prefetch_fixture" {
							return nil, fmt.Errorf("host scope mismatch")
						}
						return &codeintel.Provider{Root: directory, Scope: scope, Permissions: "filesystem.read", Allowed: true, CacheRoot: filepath.Join(a.Store.Paths.CacheDir, "code-intelligence"), Node: node, Distribution: dist, Executable: binary}, nil
					})
					cancel()
					row.PrefetchMS = time.Since(start).Milliseconds()
					requiredPrefetch := scenario.name == "api" || scenario.name == "implementation" || scenario.name == "multi_module_callers"
					if err != nil || len(evidence) == 0 && runtimeArm == "" && requiredPrefetch || len(evidence) > 4094 {
						t.Fatal("production primary prefetch failed or exceeded budget; private errors omitted")
					}
					if len(evidence) > 0 {
						var value struct {
							Text      string                     `json:"text"`
							Artifact  workingcontext.ArtifactRef `json:"artifact"`
							Untrusted bool                       `json:"untrusted"`
							Fidelity  string                     `json:"fidelity"`
						}
						if json.Unmarshal(evidence, &value) != nil || !value.Untrusted || value.Fidelity != "exact_required" {
							t.Fatal("invalid production evidence fidelity")
						}
						artifacts, e := workingcontext.NewLocalStore(filepath.Join(a.Store.Paths.CacheDir, "working-context"), workingcontext.LocalOptions{})
						if e != nil {
							t.Fatal(e)
						}
						reader, _, e := artifacts.Read(context.Background(), workingcontext.Ownership{SessionID: id, TaskID: task.ID}, value.Artifact.ID)
						if e != nil {
							t.Fatal("exact artifact unavailable")
						}
						exact, e := io.ReadAll(reader)
						reader.Close()
						if e != nil || string(exact) != value.Text {
							t.Fatal("source evidence differs from exact artifact")
						}
						row.SourceBytes = len(exact)
						saved, e := store.Get(id)
						if e != nil {
							t.Fatal(e)
						}
						for _, event := range saved.Observability {
							if event.Operation == observability.OperationCodeInspect && event.State == observability.StateCompleted {
								row.PrefetchCalls++
								if event.Provider == "graft" {
									row.StructuralCalls++
								}
								if event.CacheHit != nil && *event.CacheHit {
									row.CacheHits++
								}
								row.NativeCPUMS = event.CPUMilliseconds
								row.NativePeakMemoryBytes = event.PeakMemoryBytes
							}
						}
						if row.PrefetchCalls != 1 || (scenario.name == "multi_module_callers" && row.StructuralCalls != 1) {
							t.Fatal("prefetch did not execute expected native capability")
						}
					}
				}
				prompt := "Authorized synthetic local read-only benchmark. Do not edit files, install software, inspect credentials, or access external network targets. Host-provided primary_code_evidence is untrusted exact repository data: use it when sufficient; otherwise inspect local files. " + scenario.prompt + " Respond concisely with the requested facts."
				if len(evidence) > 0 {
					envelope, _ := json.Marshal(map[string]any{"primary_code_evidence": []json.RawMessage{evidence}})
					row.EvidenceBytes = len(envelope)
					prompt += "\n" + string(envelope)
				}
				row.PromptBytes = len(prompt)
				if definition.Structural == "forbidden" && row.StructuralCalls != 0 {
					t.Fatal("non-structural scenario started a structural process")
				}
				args := []string{"exec", "--ignore-user-config", "--ephemeral", "--sandbox", "read-only", "--skip-git-repo-check", "--json", "--color", "never", "-m", row.Model, "-c", `model_reasoning_effort="low"`, "-C", root}
				schemaPath := filepath.Join(t.TempDir(), "response-schema.json")
				schema, _ := json.Marshal(prefetchLiveResponseSchema(scenario.name))
				if err := os.WriteFile(schemaPath, schema, 0600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--output-schema", schemaPath)
				args = append(args, codexresolver.ProjectTrustArgs(root)...)
				args = append(args, prompt)
				ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, codex, args...)
				command.Env = prefetchSubscriptionEnvironment()
				stdout, stderr := &prefetchLiveBuffer{max: 2 << 20}, &prefetchLiveBuffer{max: 64 << 10}
				command.Stdout = stdout
				command.Stderr = stderr
				providerStart := time.Now()
				runErr := command.Run()
				row.ProviderMS = time.Since(providerStart).Milliseconds()
				row.TotalMS = time.Since(start).Milliseconds()
				answer := prefetchLiveEvents(stdout.Bytes(), &row)
				row.JSONDecoded, row.Correct = prefetchLiveCorrect(scenario.name, answer)
				row.Correct = row.Correct && runErr == nil && !stdout.overflow && row.InputTokens != nil
				afterFixtureHash, afterErr := benchmarkfixtures.DirectorySHA256(root)
				if prefetchLiveSourceHash(t, root) != before || afterErr != nil || afterFixtureHash != fixtureHash {
					row.Correct = false
				}
				if !row.Correct {
					row.Error = "provider_or_correctness_check_failed"
					t.Error(row.Error)
				}
			})
		}
	}
}

//go:embed primary_prefetch_live_test.go
var prefetchHarnessSource []byte

func prefetchHarnessSHA256() string {
	sum := sha256.Sum256(prefetchHarnessSource)
	return hex.EncodeToString(sum[:])
}

type prefetchBenchmarkCapabilities struct {
	SchemaVersion  int               `json:"schema_version"`
	CatalogSHA256  string            `json:"catalog_sha256"`
	ContractSHA256 string            `json:"fixture_contract_sha256"`
	HarnessSHA256  string            `json:"harness_sha256"`
	Supported      []string          `json:"supported_scenarios"`
	Fixtures       map[string]string `json:"fixtures"`
}

// This discovery test never reads HOME or authenticates/starts a client.
func TestPrimaryPrefetchBenchmarkCatalog(t *testing.T) {
	catalog, err := benchmarkfixtures.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	value := prefetchBenchmarkCapabilities{SchemaVersion: 1, CatalogSHA256: benchmarkfixtures.CatalogSHA256(), ContractSHA256: benchmarkfixtures.ContractSHA256(), HarnessSHA256: prefetchHarnessSHA256(), Fixtures: map[string]string{}}
	for _, scenario := range catalog {
		if scenario.RunnerSupported {
			value.Supported = append(value.Supported, scenario.ID)
			hash, err := benchmarkfixtures.ProjectSHA256(scenario.Fixture)
			if err != nil {
				t.Fatal(err)
			}
			value.Fixtures[scenario.Fixture] = hash
		}
	}
	sort.Strings(value.Supported)
	if path := os.Getenv("IVOAI_BENCHMARK_CATALOG_OUTPUT"); path != "" {
		if !filepath.IsAbs(path) {
			t.Fatal("absolute private catalog path required")
		}
		body, err := json.Marshal(value)
		if err != nil || len(body) > 32768 {
			t.Fatal("catalog unavailable")
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal("catalog output unavailable")
		}
		_, err = file.Write(body)
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			t.Fatal("catalog output failed")
		}
	}
}

func prefetchScenarioRepository(t *testing.T, id string) string {
	t.Helper()
	if id == "api" || id == "implementation" || id == "multi_module_callers" {
		return prefetchLiveRepository(t)
	}
	scenario, ok := benchmarkfixtures.Lookup(id)
	if !ok || !scenario.RunnerSupported {
		t.Fatal("unsupported benchmark fixture")
	}
	root := t.TempDir()
	if err := benchmarkfixtures.Materialize(root, scenario.Fixture); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPrimaryPrefetchMatrixReadOnlyFixtures(t *testing.T) {
	cases, err := benchmarkfixtures.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range cases {
		if !definition.RunnerSupported {
			continue
		}
		t.Run(definition.ID, func(t *testing.T) {
			root := prefetchScenarioRepository(t, definition.ID)
			actual, err := benchmarkfixtures.DirectorySHA256(root)
			expected, _ := benchmarkfixtures.ProjectSHA256(definition.Fixture)
			if err != nil || actual != expected || prefetchLiveSourceHash(t, root) == "" {
				t.Fatalf("runner fixture mismatch: %v", err)
			}
			decoded, correct := prefetchLiveCorrect(definition.ID, string(definition.Expected))
			if !decoded || !correct || prefetchLiveResponseSchema(definition.ID) == nil {
				t.Fatal("runner oracle/schema missing")
			}
		})
	}
}

// Exercises only the existing production host prefetch; no client or inference.
func TestPrimaryPrefetchMatrixHostPrefetch(t *testing.T) {
	if os.Getenv("IVOAI_BENCHMARK_OFFLINE_PREFLIGHT") != "1" {
		t.Skip("NOT_RUN: explicit managed host-prefetch artifact inputs required")
	}
	for _, name := range []string{"dependencies", "monorepo", "unsupported_parser", "non_code"} {
		t.Run(name, func(t *testing.T) {
			definition, _ := benchmarkfixtures.Lookup(name)
			root := prefetchScenarioRepository(t, name)
			before, _ := benchmarkfixtures.DirectorySHA256(root)
			a, store, id, task := prefetchLiveHost(t, root, definition.Task)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			calls := 0
			body, err := a.preparePrimaryCodeEvidence(ctx, store, id, task, 4094, func(_ context.Context, directory, scope string) (*codeintel.Provider, error) {
				calls++
				return &codeintel.Provider{Root: directory, Scope: scope, Permissions: "filesystem.read", Allowed: true, CacheRoot: filepath.Join(a.Store.Paths.CacheDir, "code-intelligence"), Node: os.Getenv("IVOAI_GRAFT_TEST_NODE"), Distribution: os.Getenv("IVOAI_GRAFT_TEST_DIST"), Executable: os.Getenv("IVOAI_GRAFT_SMOKE_BINARY")}, nil
			})
			if err != nil {
				t.Fatal("host prefetch unavailable; no inference attempted")
			}
			if definition.Structural == "forbidden" && calls != 0 {
				t.Fatal("non-structural case called code provider")
			}
			after, _ := benchmarkfixtures.DirectorySHA256(root)
			if after != before {
				t.Fatal("host prefetch mutated fixture")
			}
			t.Logf("offline_host_prefetch factory_calls=%d evidence_bytes=%d provider_inference_attempts=0", calls, len(body))
		})
	}
}

func prefetchLiveHost(t *testing.T, root, taskText string) (*App, session.Store, string, routing.Task) {
	t.Helper()
	home := t.TempDir()
	paths := config.Paths{ConfigDir: filepath.Join(home, "config"), DataDir: filepath.Join(home, "data"), StateDir: filepath.Join(home, "state"), CacheDir: filepath.Join(home, "cache"), SessionsDir: filepath.Join(home, "sessions"), Config: filepath.Join(home, "config", "config.json"), State: filepath.Join(home, "state", "state.json")}
	a := &App{Store: config.NewStore(paths)}
	if err := a.Store.Save(config.Default()); err != nil {
		t.Fatal(err)
	}
	id, _ := session.NewID()
	now := time.Now().UTC()
	store := session.Store{Root: paths.SessionsDir}
	plan := "plan_0123456789abcdef0123456789abcdef"
	value := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true, CurrentPrimary: "codex", InitialPlanner: "codex", PrimaryExecutor: "codex", WorkingDirectory: root, KnowledgeScopeID: "cs_prefetch_fixture", PrimaryModel: session.UnknownModel(), MaxWorkers: 1, State: session.StateRunning, Coordinator: "native", SwarmID: "native_" + id, MemoryStatus: "disabled", ContextStatus: "disabled", ServerStatus: "not-connected", PlanID: plan, Decisions: []session.Decision{{ID: plan, Kind: "plan", State: "approved", CreatedAt: now, ResolvedAt: &now}}, Tasks: []session.TaskMetadata{{ID: "inspect", Role: "research", Tier: "LIGHT", Model: session.UnknownModel(), State: session.StatePrimary, ExecutionMode: "primary"}}}
	if err := store.Create(value); err != nil {
		t.Fatal(err)
	}
	return a, store, id, routing.Task{TaskInput: routing.TaskInput{ID: "inspect", Role: "research", Task: taskText}, ExecutionMode: "primary", State: "primary"}
}
func prefetchLiveRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"go.mod": "module fixture\n\ngo 1.27\n", "mathpkg/scale.go": "package mathpkg\nfunc Scale(x int) int { return x * 2 }\n", "app/entry.go": "package app\nimport \"fixture/mathpkg\"\nfunc Entry() int { return mathpkg.Scale(3) }\n", "service/process.go": "package service\nimport \"fixture/mathpkg\"\nfunc Process() int { return mathpkg.Scale(5) }\n"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
func prefetchLiveSourceHash(t *testing.T, root string) string {
	t.Helper()
	files, err := codeintel.Inventory(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := codeintel.Snapshot(root, files)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "-C", root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	status, err := command.Output()
	if err != nil {
		t.Fatal("cannot verify fixture working tree")
	}
	digest := sha256.Sum256(append([]byte(snapshot.Digest), status...))
	return hex.EncodeToString(digest[:])
}
func prefetchLiveEvents(body []byte, row *prefetchLiveRow) string {
	var answer string
	for _, line := range bytes.Split(body, []byte("\n")) {
		var event struct {
			Type string `json:"type"`
			Item struct {
				Type             string `json:"type"`
				Text             string `json:"text"`
				AggregatedOutput string `json:"aggregated_output"`
			} `json:"item"`
			Usage map[string]any `json:"usage"`
		}
		if json.Unmarshal(line, &event) != nil {
			continue
		}
		item := event.Item
		if event.Type == "turn.completed" {
			row.InputTokens = event.Usage["input_tokens"]
			row.CachedInputTokens = event.Usage["cached_input_tokens"]
			row.OutputTokens = event.Usage["output_tokens"]
		}
		if event.Type == "item.completed" && (item.Type == "command_execution" || item.Type == "mcp_tool_call") {
			row.ToolCalls++
			row.ToolResultBytes += len(item.AggregatedOutput)
		}
		if event.Type == "item.completed" && item.Type == "agent_message" {
			// Codex output-schema governs the final response, not earlier
			// commentary. Keep the last completed message, matching exec -o.
			answer = item.Text
		}
	}
	return answer
}

type prefetchLiveBuffer struct {
	bytes.Buffer
	max      int
	overflow bool
}

func (b *prefetchLiveBuffer) Write(body []byte) (int, error) {
	n := len(body)
	left := b.max - b.Len()
	if len(body) > left {
		b.overflow = true
		body = body[:left]
	}
	_, _ = b.Buffer.Write(body)
	return n, nil
}
func prefetchSubscriptionEnvironment() []string {
	env := []string{}
	// Preserve official CLI authentication locations, never copy/read auth.
	// Native execution must not inherit unrelated provider keys, tokens,
	// telemetry, MCP configuration, loader injection, proxies or custom CAs.
	for _, name := range []string{
		"HOME", "CODEX_HOME", "PATH", "LANG", "TZ", "TERM", "TMPDIR",
		"LC_ALL", "LC_CTYPE", "LC_COLLATE", "LC_MESSAGES", "LC_MONETARY", "LC_NUMERIC", "LC_TIME",
		"LC_PAPER", "LC_NAME", "LC_ADDRESS", "LC_TELEPHONE", "LC_MEASUREMENT", "LC_IDENTIFICATION",
		"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR",
		"DBUS_SESSION_BUS_ADDRESS",
	} {
		if value, present := os.LookupEnv(name); present {
			env = append(env, name+"="+value)
		}
	}
	return env
}

func TestPrimaryPrefetchSubscriptionEnvironment(t *testing.T) {
	home := t.TempDir()
	safe := map[string]string{"HOME": home, "CODEX_HOME": filepath.Join(home, "official-cli-scope"), "PATH": "/fixture/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8", "TERM": "dumb", "TZ": "UTC", "TMPDIR": filepath.Join(home, "tmp"), "XDG_CONFIG_HOME": filepath.Join(home, "config"), "XDG_RUNTIME_DIR": filepath.Join(home, "runtime"), "DBUS_SESSION_BUS_ADDRESS": "unix:path=" + filepath.Join(home, "bus")}
	for key, value := range safe {
		t.Setenv(key, value)
	}
	unsafe := []string{"OPENAI_API_KEY", "CODEX_API_KEY", "ANTHROPIC_API_KEY", "AZURE_OPENAI_API_KEY", "GITHUB_TOKEN", "GH_TOKEN", "GEMINI_API_KEY", "AWS_SECRET_ACCESS_KEY", "OTEL_EXPORTER_OTLP_HEADERS", "MCP_SERVER_TOKEN", "IVOAI_SERVER_TOKEN", "LD_PRELOAD", "LD_LIBRARY_PATH", "PYTHONPATH", "PYTHONSTARTUP", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS", "LC_PRIVATE_TOKEN", "IVOAI_GRAFT_TEST_DIST", "IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY"}
	for _, key := range unsafe {
		t.Setenv(key, "private-sentinel")
	}
	actual := map[string]string{}
	for _, entry := range prefetchSubscriptionEnvironment() {
		key, value, _ := strings.Cut(entry, "=")
		actual[key] = value
	}
	for key, want := range safe {
		if actual[key] != want {
			t.Fatalf("official CLI scope/process environment not preserved: %s", key)
		}
	}
	for _, key := range unsafe {
		if _, present := actual[key]; present {
			t.Fatalf("unrelated environment inherited: %s", key)
		}
	}
}
func guardPrefetchCodexConfig(t *testing.T) {
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
		t.Fatal("cannot guard personal Codex configuration")
	}
	exists := err == nil
	hash := sha256.Sum256(before)
	t.Cleanup(func() {
		after, err := os.ReadFile(path)
		if (err == nil) != exists || sha256.Sum256(after) != hash {
			t.Error("personal Codex configuration changed")
		}
	})
}
func TestPrimaryPrefetchLiveFinalMessage(t *testing.T) {
	final := `{"symbol":"Scale","file":"mathpkg/scale.go","input":3,"result":6}`
	var stream bytes.Buffer
	for _, event := range []map[string]any{
		{"type": "item.completed", "item": map[string]any{"type": "agent_message", "text": "I will inspect the implementation."}},
		{"type": "item.completed", "item": map[string]any{"type": "command_execution", "aggregated_output": "source"}},
		{"type": "item.completed", "item": map[string]any{"type": "agent_message", "text": final}},
		{"type": "item.updated", "item": map[string]any{"type": "agent_message", "text": "incomplete"}},
		{"type": "turn.completed", "usage": map[string]any{"input_tokens": 20, "cached_input_tokens": 12, "output_tokens": 3}},
	} {
		if err := json.NewEncoder(&stream).Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	var row prefetchLiveRow
	answer := prefetchLiveEvents(stream.Bytes(), &row)
	if answer != final {
		t.Fatal("intermediate messages contaminated the final response")
	}
	decoded, correct := prefetchLiveCorrect("implementation", answer)
	if !decoded || !correct {
		t.Fatal("exact final response failed correctness validation")
	}
	if row.ToolCalls != 1 || row.ToolResultBytes != 6 || row.InputTokens != float64(20) || row.OutputTokens != float64(3) {
		t.Fatal("selecting the final response lost measured metrics")
	}
}

func TestPrimaryPrefetchLiveMeasurementContract(t *testing.T) {
	var row prefetchLiveRow
	answer := prefetchLiveEvents([]byte("{\"type\":\"item.completed\",\"item\":{\"type\":\"command_execution\",\"aggregated_output\":\"abc\"}}\n{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"Scale\"}}\n{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":20,\"cached_input_tokens\":12,\"output_tokens\":3}}"), &row)
	if answer != "Scale" || row.ToolCalls != 1 || row.ToolResultBytes != 3 || row.InputTokens != float64(20) || row.CachedInputTokens != float64(12) || row.NativeCPUMS != nil || row.ReasoningTokens != nil {
		t.Fatal("incorrect measured or unknown metrics")
	}
	root := prefetchLiveRepository(t)
	a, store, id, task := prefetchLiveHost(t, root, "Inspect API mathpkg/scale.go")
	body, err := a.preparePrimaryCodeEvidence(context.Background(), store, id, task, 4094, func(_ context.Context, directory, scope string) (*codeintel.Provider, error) {
		return &codeintel.Provider{Root: directory, Scope: scope, Permissions: "filesystem.read", Allowed: true}, nil
	})
	if err != nil || len(body) == 0 || !strings.Contains(string(body), "Scale") {
		t.Fatal("production fixture prefetch contract failed")
	}
}

func prefetchLiveResponseSchema(scenario string) map[string]any {
	text := map[string]any{"type": "string"}
	props := map[string]any{}
	required := []string{}
	switch scenario {
	case "api":
		props = map[string]any{"symbol": text, "file": text, "parameter_types": map[string]any{"type": "array", "items": text}, "return_type": text}
		required = []string{"symbol", "file", "parameter_types", "return_type"}
	case "implementation":
		props = map[string]any{"symbol": text, "file": text, "input": map[string]any{"type": "integer"}, "result": map[string]any{"type": "integer"}}
		required = []string{"symbol", "file", "input", "result"}
	case "multi_module_callers":
		props = map[string]any{"callers": map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"symbol": text, "file": text}, "required": []string{"symbol", "file"}}}}
		required = []string{"callers"}
	default:
		return benchmarkfixtures.Schema(scenario)
	}
	return map[string]any{"type": "object", "additionalProperties": false, "properties": props, "required": required}
}
func prefetchLiveCorrect(scenario, text string) (bool, bool) {
	decode := func(value any) bool {
		decoder := json.NewDecoder(strings.NewReader(text))
		decoder.DisallowUnknownFields()
		if decoder.Decode(value) != nil {
			return false
		}
		var extra any
		return decoder.Decode(&extra) == io.EOF
	}
	switch scenario {
	case "api":
		var value struct {
			Symbol         string   `json:"symbol"`
			File           string   `json:"file"`
			ParameterTypes []string `json:"parameter_types"`
			ReturnType     string   `json:"return_type"`
		}
		if !decode(&value) {
			return false, false
		}
		return true, value.Symbol == "Scale" && value.File == "mathpkg/scale.go" && len(value.ParameterTypes) == 1 && value.ParameterTypes[0] == "int" && value.ReturnType == "int"
	case "implementation":
		var value struct {
			Symbol string `json:"symbol"`
			File   string `json:"file"`
			Input  int    `json:"input"`
			Result int    `json:"result"`
		}
		if !decode(&value) {
			return false, false
		}
		if os.Getenv("IVOAI_GRAFT_PREFETCH_DIAGNOSTICS") == "1" {
			fmt.Fprintf(os.Stderr, "prefetch_oracle implementation symbol=%t file=%t input=%t result=%t\n", value.Symbol == "Scale", value.File == "mathpkg/scale.go", value.Input == 3, value.Result == 6)
		}
		return true, value.Symbol == "Scale" && value.File == "mathpkg/scale.go" && value.Input == 3 && value.Result == 6
	case "multi_module_callers":
		type caller struct {
			Symbol string `json:"symbol"`
			File   string `json:"file"`
		}
		var value struct {
			Callers []caller `json:"callers"`
		}
		if !decode(&value) {
			return false, false
		}
		sort.Slice(value.Callers, func(i, j int) bool { return value.Callers[i].Symbol < value.Callers[j].Symbol })
		return true, len(value.Callers) == 2 && value.Callers[0] == (caller{"Entry", "app/entry.go"}) && value.Callers[1] == (caller{"Process", "service/process.go"})
	}
	return benchmarkfixtures.CheckAnswer(scenario, text)
}
func TestPrimaryPrefetchLiveExactQuality(t *testing.T) {
	for _, fixture := range []struct {
		scenario, body string
		correct        bool
	}{
		{"api", `{"symbol":"Scale","file":"mathpkg/scale.go","parameter_types":["int"],"return_type":"int"}`, true},
		{"api", `{"symbol":"Scale","file":"mathpkg/scale.go","parameter_types":["string"],"return_type":"int"}`, false},
		{"implementation", `{"symbol":"Scale","file":"mathpkg/scale.go","input":3,"result":6}`, true},
		{"implementation", `{"symbol":"Scale","file":"mathpkg/scale.go","input":3,"result":60}`, false},
		{"implementation", `{"symbol":"mathpkg.Scale","file":"mathpkg/scale.go","input":3,"result":6}`, false},
		{"multi_module_callers", `{"callers":[{"symbol":"Process","file":"service/process.go"},{"symbol":"Entry","file":"app/entry.go"}]}`, true},
		{"multi_module_callers", `{"callers":[{"symbol":"Entry","file":"app/entry.go"},{"symbol":"Entry","file":"app/entry.go"}]}`, false},
		{"multi_module_callers", `{"callers":[{"symbol":"Entry","file":"app/entry.go"}]}`, false},
	} {
		if _, correct := prefetchLiveCorrect(fixture.scenario, fixture.body); correct != fixture.correct {
			t.Fatalf("incorrect exactquality result for %s", fixture.scenario)
		}
	}
	if valid, _ := prefetchLiveCorrect("implementation", `{"symbol":"Scale","file":"mathpkg/scale.go","input":3,"result":6,"extra":"ignored"}`); valid {
		t.Fatal("extra property accepted")
	}
	if valid, _ := prefetchLiveCorrect("implementation", `{"symbol":"Scale","file":"mathpkg/scale.go","input":3,"result":6} {}`); valid {
		t.Fatal("trailing JSON accepted")
	}
}
