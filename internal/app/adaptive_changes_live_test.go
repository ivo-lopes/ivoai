package app

// Explicit real subscription integration only. Approval uses the production
// decision boundary; neither a plan nor a worker result is inserted by this test.
import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/codeintel"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"golang.org/x/sys/unix"
)

type adaptiveChangeObservation struct {
	Scenario               string                   `json:"scenario"`
	Outcome                string                   `json:"outcome"`
	Failure                string                   `json:"failure,omitempty"`
	WallMS                 int64                    `json:"wall_ms"`
	Approvals              int                      `json:"explicit_host_plan_approvals"`
	ReadApprovals          int                      `json:"explicit_host_code_read_approvals"`
	RejectedPlans          int                      `json:"out_of_scope_plans_rejected"`
	DAG                    bool                     `json:"dag"`
	ReceiptClosed          bool                     `json:"receipt_closed"`
	Attempts               int                      `json:"primary_attempts"`
	CompletedWorkers       int                      `json:"completed_real_workers"`
	WorkerWorktrees        int                      `json:"worker_worktrees"`
	WorkerExecutorScope    bool                     `json:"all_workers_codex_without_fallback"`
	ParallelWriteDegraded  bool                     `json:"parallel_write_degraded"`
	RootScopePreserved     bool                     `json:"root_scope_preserved"`
	ChangedPaths           []string                 `json:"changed_paths,omitempty"`
	PureCodeOracle         bool                     `json:"pure_code_oracle"`
	BaselineTestsFailed    bool                     `json:"baseline_fixed_tests_failed"`
	FinalTestsPassed       bool                     `json:"final_fixed_tests_passed"`
	NativeExitZero         bool                     `json:"primary_native_exit_zero"`
	PrimaryUsage           *core.ReportedTokenUsage `json:"primary_official_client_usage,omitempty"`
	WorkerUsageObservation string                   `json:"worker_usage_observation"`
}

type adaptiveChangeFile struct {
	Hash [32]byte
	Mode fs.FileMode
}

// Git's own index/commit/worktree bookkeeping is expected to change during real
// integration. Every repository data file, including fixed tests, is compared.
func adaptiveChangeSnapshot(root string) (map[string]adaptiveChangeFile, error) {
	out := map[string]adaptiveChangeFile{}
	total := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil || len(out) >= 64 {
				return fs.ErrInvalid
			}
			out[filepath.ToSlash(rel)+"/"] = adaptiveChangeFile{Mode: info.Mode().Perm()}
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<10 || len(out) >= 64 {
			return fs.ErrInvalid
		}
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err != nil {
			return fs.ErrInvalid
		}
		file := os.NewFile(uintptr(fd), path)
		opened, statErr := file.Stat()
		if statErr != nil || !opened.Mode().IsRegular() || opened.Size() > 64<<10 {
			file.Close()
			return fs.ErrInvalid
		}
		body, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
		closeErr := file.Close()
		total += len(body)
		if err != nil || closeErr != nil || len(body) > 64<<10 || total > 256<<10 {
			return fs.ErrInvalid
		}
		out[filepath.ToSlash(rel)] = adaptiveChangeFile{sha256.Sum256(body), info.Mode().Perm()}
		return nil
	})
	return out, err
}

func adaptiveChangePaths(before, after map[string]adaptiveChangeFile, allowed map[string]bool) ([]string, bool) {
	changed := []string{}
	if len(before) != len(after) {
		return changed, false
	}
	for path, prior := range before {
		current, exists := after[path]
		if !exists {
			return changed, false
		}
		if current != prior {
			if !allowed[path] || current.Mode&0111 != 0 {
				return changed, false
			}
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return changed, len(changed) == len(allowed)
}

// Generated code is never executed until these tiny pure functions have been
// checked. Imports/init/globals, loops, arbitrary calls and filesystem/network
// access are outside this fixture's oracle, irrespective of the model's claims.
func adaptiveChangePureFile(path, function, parameterType, resultType string, count int) bool {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil || f.Name.Name != "adaptivefixture" {
		return false
	}
	var declaration *ast.FuncDecl
	for _, item := range f.Decls {
		switch value := item.(type) {
		case *ast.GenDecl:
			if value.Tok != token.IMPORT {
				return false
			}
			for _, spec := range value.Specs {
				imported, ok := spec.(*ast.ImportSpec)
				if !ok || imported.Name != nil || imported.Path.Value != `"fmt"` && imported.Path.Value != `"strconv"` || function == "Add" {
					return false
				}
			}
		case *ast.FuncDecl:
			if declaration != nil || value.Recv != nil || value.Name.Name != function || value.Type.TypeParams != nil {
				return false
			}
			declaration = value
		default:
			return false
		}
	}
	if declaration == nil || declaration.Body == nil || declaration.Type.Params == nil || declaration.Type.Results == nil || len(declaration.Type.Results.List) != 1 || len(declaration.Body.List) != 1 {
		return false
	}
	result, ok := declaration.Type.Results.List[0].Type.(*ast.Ident)
	if !ok || result.Name != resultType {
		return false
	}
	parameters := map[string]bool{}
	for _, field := range declaration.Type.Params.List {
		kind, ok := field.Type.(*ast.Ident)
		if !ok || kind.Name != parameterType || len(field.Names) == 0 {
			return false
		}
		for _, name := range field.Names {
			if name.Name == "_" || parameters[name.Name] {
				return false
			}
			parameters[name.Name] = true
		}
	}
	returned, ok := declaration.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(parameters) != count || len(returned.Results) != 1 {
		return false
	}
	if function == "Add" {
		var expression ast.Expr = returned.Results[0]
		for {
			parenthesized, ok := expression.(*ast.ParenExpr)
			if !ok {
				break
			}
			expression = parenthesized.X
		}
		addition, ok := expression.(*ast.BinaryExpr)
		if !ok || addition.Op != token.ADD {
			return false
		}
		left, leftOK := addition.X.(*ast.Ident)
		right, rightOK := addition.Y.(*ast.Ident)
		return leftOK && rightOK && parameters[left.Name] && parameters[right.Name] && left.Name != right.Name
	}
	var safeExpression func(ast.Expr, int) bool
	safeExpression = func(expression ast.Expr, depth int) bool {
		if depth > 8 {
			return false
		}
		switch value := expression.(type) {
		case *ast.Ident:
			return parameters[value.Name]
		case *ast.BasicLit:
			return len(value.Value) <= 128 && (value.Kind == token.STRING || value.Kind == token.INT)
		case *ast.BinaryExpr:
			return value.Op == token.ADD && safeExpression(value.X, depth+1) && safeExpression(value.Y, depth+1)
		case *ast.ParenExpr:
			return safeExpression(value.X, depth+1)
		case *ast.CallExpr:
			if function == "Add" || value.Ellipsis.IsValid() {
				return false
			}
			selector, ok := value.Fun.(*ast.SelectorExpr)
			if !ok {
				return false
			}
			library, ok := selector.X.(*ast.Ident)
			if !ok {
				return false
			}
			if library.Name == "strconv" && selector.Sel.Name == "Itoa" && len(value.Args) == 1 {
				return safeExpression(value.Args[0], depth+1)
			}
			if library.Name != "fmt" || selector.Sel.Name != "Sprintf" || len(value.Args) != 2 {
				return false
			}
			format, ok := value.Args[0].(*ast.BasicLit)
			if !ok || format.Kind != token.STRING {
				return false
			}
			literal, err := strconv.Unquote(format.Value)
			return err == nil && (literal == "Hello, %s" || literal == "item-%03d" || literal == "%03d" || literal == "%s") && safeExpression(value.Args[1], depth+1)
		}
		return false
	}
	return safeExpression(returned.Results[0], 0)
}

func adaptiveChangeGoTest(ctx context.Context, goBinary, root, private string) bool {
	command := exec.CommandContext(ctx, goBinary, "test", "-count=1", "-timeout=20s", "./...")
	command.Dir, command.Stdout, command.Stderr, command.WaitDelay = root, io.Discard, io.Discard, time.Second
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + private, "TMPDIR=" + private, "GOTMPDIR=" + private, "GOCACHE=" + filepath.Join(private, "go-cache"), "GOPATH=" + filepath.Join(private, "go-path"), "GOENV=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "CGO_ENABLED=0", "GOMAXPROCS=2", "LANG=C.UTF-8"}
	return command.Run() == nil
}

func adaptiveChangeApprove(f *localMetadataFixture, allowed map[string]bool, writers int, decision session.Decision) bool {
	v, err := f.store.Get(f.id)
	checkpoint, checkpointErr := f.store.LoadCheckpoint(f.id)
	if err != nil || checkpointErr != nil || checkpoint.Recovery == nil || checkpoint.Recovery.Directory != f.root || v.PrimaryTurn == nil || v.PrimaryTurn.Mode != core.PrimaryDAG || v.PlanID != decision.ID || v.PrimaryTurn.PlanID != decision.ID || checkpoint.Recovery.Plan.ID != decision.ID || len(v.Workers) != 0 {
		return false
	}
	covered, writerCount := map[string]bool{}, 0
	for _, task := range checkpoint.Recovery.Plan.Tasks {
		profile, profileErr := orchestration.CapabilityProfile(task.Role)
		if profileErr != nil || task.Profile.Provider != "codex" || len(task.AllowedMCPs) != 0 || len(task.AllowedMCPTools) != 0 || len(task.Skills) != 0 {
			return false
		}
		if len(task.WritePaths) == 0 {
			if profile.Write {
				return false
			}
			continue
		}
		if !profile.Write || task.ExecutionMode != "worker" || len(task.WritePaths) != 1 {
			return false
		}
		writerCount++
		for _, path := range task.WritePaths {
			if !allowed[path] || covered[path] {
				return false
			}
			covered[path] = true
		}
	}
	return writerCount >= writers && len(covered) == len(allowed)
}

func TestLiveAdaptivePrimaryManagedChanges(t *testing.T) {
	if os.Getenv("IVOAI_ADAPTIVE_CHANGES_LIVE") != "1" {
		t.Skip("NOT_RUN: explicit real official subscription changes required")
	}
	binary, goBinary, report := os.Getenv("IVOAI_LIVE_CODEX_PATH"), os.Getenv("IVOAI_ADAPTIVE_ORACLE_GO"), os.Getenv("IVOAI_ADAPTIVE_CHANGES_REPORT")
	if !filepath.IsAbs(binary) || !filepath.IsAbs(goBinary) || primaryDirectActivationOutput(report) != nil {
		t.Fatal("explicit reviewed executables and new private report required")
	}
	rows := []adaptiveChangeObservation{}
	defer func() {
		file, err := os.OpenFile(report, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Error("bounded changes receipt unavailable")
			return
		}
		err = json.NewEncoder(file).Encode(map[string]any{"schema_version": 1, "scope": "real_official_managed_adaptive_changes", "global_economic_gate": "INCOMPLETE", "observations": rows})
		if file.Close() != nil || err != nil {
			t.Error("changes receipt write failed")
		}
	}()
	for _, scenario := range []struct {
		id, prompt string
		allowed    map[string]bool
		workers    int
	}{
		{"C", "Corrija o bug de soma em math.go usando um worker aprovado do IVOAI e integre o resultado. Add(2, 3) deveria retornar 5. Preserve os outros arquivos e os testes existentes.", map[string]bool{"math.go": true}, 1},
		{"D", "Implemente duas melhorias independentes com dois workers do IVOAI, um para cada arquivo, e integre os resultados: Greeting em greeting.go deve retornar Hello, seguido de espaço e do nome; FormatID em format.go deve retornar item- seguido do número preenchido com zeros até três dígitos. Preserve a API e os testes existentes, valide o conjunto e apresente a conclusão.", map[string]bool{"greeting.go": true, "format.go": true}, 2},
	} {
		if selected := os.Getenv("IVOAI_ADAPTIVE_CHANGES_SCENARIOS"); selected != "" && !strings.Contains(","+selected+",", ","+scenario.id+",") {
			continue
		}
		t.Run(scenario.id, func(t *testing.T) {
			start := time.Now()
			row := adaptiveChangeObservation{Scenario: scenario.id, Outcome: "FAIL", WorkerUsageObservation: "UNAVAILABLE_NOT_SUMMED_WITH_PRIMARY"}
			defer func() { row.WallMS = time.Since(start).Milliseconds(); rows = append(rows, row) }()
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
			defer cancel()
			f := adaptiveInferenceFixture(t, binary)
			f.cfg.Orchestration.Auto.PlanExecution, f.cfg.Orchestration.Auto.ProviderPreference = "approve", "codex"
			f.cfg.Orchestration.Auto.AutomaticFailover = false
			if err := f.a.Store.Save(f.cfg); err != nil {
				row.Failure = "PRIVATE_CONFIG_FAILED"
				t.Fatal("private configuration unavailable")
			}
			f.cfg, _ = f.a.Store.Load()
			if scenario.id == "D" {
				for name, body := range map[string]string{
					"math.go":          "package adaptivefixture\nfunc Add(a,b int) int { return a+b }\n",
					"greeting.go":      "package adaptivefixture\nfunc Greeting(name string) string { return name }\n",
					"format.go":        "package adaptivefixture\nfunc FormatID(id int) string { return \"unset\" }\n",
					"features_test.go": "package adaptivefixture\nimport \"testing\"\nfunc TestGreeting(t *testing.T) { for _, n := range []string{\"Ada\",\"\",\"Ivo\"} { if Greeting(n)!=\"Hello, \"+n { t.Fatal(\"greeting\") } } }\nfunc TestFormatID(t *testing.T) { for n,w := range map[int]string{0:\"item-000\",7:\"item-007\",42:\"item-042\",1000:\"item-1000\"} { if FormatID(n)!=w { t.Fatal(\"format\") } } }\n",
				} {
					if os.WriteFile(filepath.Join(f.root, name), []byte(body), 0600) != nil {
						row.Failure = "FIXTURE_FAILED"
						t.Fatal("disposable fixture unavailable")
					}
				}
				for _, args := range [][]string{{"add", "."}, {"-c", "user.name=IVOAI fixture", "-c", "user.email=fixture@example.invalid", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "independent fixtures"}} {
					command := exec.Command("/usr/bin/git", args...)
					command.Dir, command.Stdout, command.Stderr = f.root, io.Discard, io.Discard
					command.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + f.a.Store.Paths.StateDir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}
					if command.Run() != nil {
						row.Failure = "FIXTURE_GIT_FAILED"
						t.Fatal("disposable Git baseline unavailable")
					}
				}
			}
			// Fixed host tests exercise actual addition rather than accepting a
			// constant that happens to satisfy the original Add(2,3) example.
			mathTests := "package adaptivefixture\nimport \"testing\"\nfunc TestAdd(t *testing.T) { for _, c := range [][3]int{{2,3,5},{-2,3,1},{0,0,0},{1,-1,0},{7,0,7},{0,-9,-9},{100,23,123}} { if Add(c[0],c[1])!=c[2] { t.Fatal(\"addition\") } } }\n"
			if os.WriteFile(filepath.Join(f.root, "math_test.go"), []byte(mathTests), 0600) != nil {
				row.Failure = "FIXED_MATH_ORACLE_UNAVAILABLE"
				t.Fatal("fixed arithmetic oracle unavailable")
			}
			for _, args := range [][]string{{"add", "math_test.go"}, {"-c", "user.name=IVOAI fixture", "-c", "user.email=fixture@example.invalid", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "fixed arithmetic oracle"}} {
				command := exec.Command("/usr/bin/git", args...)
				command.Dir, command.Stdout, command.Stderr = f.root, io.Discard, io.Discard
				command.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + f.a.Store.Paths.StateDir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}
				if command.Run() != nil {
					row.Failure = "FIXED_ORACLE_BASELINE_UNAVAILABLE"
					t.Fatal("fixed oracle baseline unavailable")
				}
			}
			before, err := adaptiveChangeSnapshot(f.root)
			if err != nil {
				row.Failure = "INITIAL_SNAPSHOT_FAILED"
				t.Fatal("initial source snapshot unavailable")
			}
			private := t.TempDir()
			row.BaselineTestsFailed = !adaptiveChangeGoTest(ctx, goBinary, f.root, private)
			if !row.BaselineTestsFailed {
				row.Failure = "BASELINE_ORACLE_DID_NOT_FAIL"
				t.Fatal("fixture did not reproduce the requested defect")
			}
			environment := prefetchSubscriptionEnvironment()
			scope, err := prefetchNewDirectScope(binary, f.root, "gpt-6.1-sol", "read-only", environment)
			if err != nil {
				row.Failure = "SCOPE_UNAVAILABLE"
				t.Fatal("official scope unavailable")
			}
			_, admission := prefetchCommonLiveAdmission(ctx, scope, prefetchSubscriptionHostPolicy{PermitFilteredEnvironment: true})
			if admission.State != "ADMITTED" {
				row.Outcome, row.Failure = "NOT_RUN", admission.Reason
				t.Skip("NOT_RUN: existing official admission refused")
			}
			catalog := opencodebridge.CatalogFromRegistry((routing.Discoverer{CodexPath: binary}).Discover(ctx)).RestrictedTo("codex")
			strong, ok := catalog.StrongPrimary("codex")
			for _, entry := range catalog.Entries() {
				if entry.Executor == "codex" && entry.UpstreamModel == "gpt-6.1-sol" && entry.ModelSource == string(routing.SourceRuntimeVerified) {
					for _, effort := range entry.SupportedEfforts {
						if effort == "low" {
							strong, ok = entry, true
							strong.DefaultEffort = "low"
						}
					}
				}
			}
			if !ok || strong.ModelSource != string(routing.SourceRuntimeVerified) {
				row.Outcome, row.Failure = "NOT_RUN", "OBSERVED_STRONG_MODEL_UNAVAILABLE"
				t.Skip("NOT_RUN: observed official model unavailable")
			}
			bridge, results := adaptiveInferenceBridge(t, f, environment, catalog)
			type finishedTurn struct{ err error }
			finished := make(chan finishedTurn, 1)
			go func() {
				_, err := bridge.SubmitTurn(ctx, "fresh_frontend", "change_"+scenario.id, scenario.prompt, strong.ID, strong.DefaultEffort)
				finished <- finishedTurn{err}
			}()
			ticker := time.NewTicker(25 * time.Millisecond)
			defer ticker.Stop()
			var runErr error
			readTools, readApproved := map[string]bool{}, map[string]bool{}
			for _, projected := range codeintel.ProjectTools(scenario.prompt) {
				for _, native := range codeintel.ToolNames() {
					if projected == native {
						readTools[projected] = true
					}
				}
			}
		waiting:
			for {
				select {
				case outcome := <-finished:
					runErr = outcome.err
					break waiting
				case <-ctx.Done():
					row.Failure = "MANAGED_CHANGE_TIMEOUT"
					t.Fatal("managed change exceeded its fixed bound")
				case <-ticker.C:
					// Native read consent and persisted plan consent are separate
					// production boundaries. Authorize only the exact projected
					// Code Intelligence read; never reinterpret an ext_ request as a plan.
					approvalCtx, approvalCancel := context.WithTimeout(ctx, 2*time.Second)
					pending, permissionErr := bridge.PendingDecisions(approvalCtx)
					if permissionErr != nil {
						approvalCancel()
						row.Failure = "NATIVE_READ_APPROVAL_QUERY_FAILED"
						cancel()
						t.Fatal("native read approval query failed")
					}
					for _, permission := range pending {
						if readApproved[permission.ID] {
							continue
						}
						server, tool, valid := strings.Cut(permission.Description, " · ")
						allow := strings.HasPrefix(permission.ID, "ext_") && valid && server == nativeCodeMCP && readTools[tool]
						if bridge.ReplyDecision(approvalCtx, permission.ID, allow) != nil {
							approvalCancel()
							row.Failure = "NATIVE_READ_APPROVAL_REPLY_FAILED"
							cancel()
							t.Fatal("native read approval reply failed")
						}
						if !allow {
							approvalCancel()
							row.Failure = "UNEXPECTED_NATIVE_PERMISSION_DENIED"
							cancel()
							t.Fatal("native permission exceeded the projected read scope")
						}
						readApproved[permission.ID] = true
						row.ReadApprovals++
					}
					approvalCancel()
					v, err := f.store.Get(f.id)
					if err != nil {
						row.Failure = "SESSION_UNAVAILABLE"
						cancel()
						t.Fatal("session unavailable during approval")
					}
					for _, decision := range v.Decisions {
						if decision.State != "pending" {
							continue
						}
						allow := decision.Kind == "plan" && adaptiveChangeApprove(f, scenario.allowed, scenario.workers, decision)
						if f.store.ResolveDecision(f.id, decision.ID, allow) != nil {
							row.Failure = "APPROVAL_PERSISTENCE_FAILED"
							cancel()
							t.Fatal("host decision persistence failed")
						}
						if !allow {
							row.RejectedPlans++
							row.Failure = "PLAN_SCOPE_DENIED"
							cancel()
							t.Fatal("plan exceeded the explicitly approved fixture scope")
						}
						row.Approvals++
					}
				}
			}
			var result core.ExecutorResult
			select {
			case result = <-results:
			case <-ctx.Done():
				row.Failure = "MANAGED_RESULT_UNAVAILABLE"
				t.Fatal("managed result unavailable")
			}
			v, err := f.store.Get(f.id)
			if err != nil {
				row.Failure = "FINAL_SESSION_UNAVAILABLE"
				t.Fatal("final managed session unavailable")
			}
			row.DAG, row.ReceiptClosed, row.Attempts = v.PlanID != "", v.PrimaryTurn != nil && v.PrimaryTurn.Mode == core.PrimaryClosed, len(v.TurnAttempts)
			row.ParallelWriteDegraded = v.ParallelWriteDegraded
			if result.Trace != nil {
				row.NativeExitZero, row.PrimaryUsage = result.Trace.ExitCode == 0 && result.Trace.Signal == "", result.Trace.Usage
			}
			row.WorkerExecutorScope = true
			for _, worker := range v.Workers {
				if worker.Executor != "codex" || worker.FallbackReason != "" || worker.RequestedExecutor != "" && worker.RequestedExecutor != "codex" {
					row.WorkerExecutorScope = false
				}
				if worker.Executor == "codex" && worker.State == session.StateCompleted && worker.ExitCode != nil && *worker.ExitCode == 0 && worker.EndedAt != nil && worker.PID > 0 {
					row.CompletedWorkers++
				}
				if worker.WorktreePath != "" && worker.WorktreePath != f.root {
					row.WorkerWorktrees++
				}
			}
			if runErr != nil {
				row.Failure = core.FailureClass(runErr)
				t.Fatal("real managed change failed: " + row.Failure)
			}
			after, snapshotErr := adaptiveChangeSnapshot(f.root)
			if snapshotErr == nil {
				row.ChangedPaths, row.RootScopePreserved = adaptiveChangePaths(before, after, scenario.allowed)
			}
			if !row.DAG || !row.ReceiptClosed || !row.NativeExitZero || !result.Policy.NoReplay || !result.Policy.NoAutomaticFailover || row.Approvals != 1 || row.Attempts != 1 || row.CompletedWorkers < scenario.workers || !row.WorkerExecutorScope || !row.RootScopePreserved {
				row.Failure = "DAG_WORKER_OR_ISOLATION_FAILED"
				t.Fatal("real DAG/worker/ownership contract failed")
			}
			row.PureCodeOracle = adaptiveChangePureFile(filepath.Join(f.root, "math.go"), "Add", "int", "int", 2)
			if scenario.id == "D" {
				row.PureCodeOracle = row.PureCodeOracle && adaptiveChangePureFile(filepath.Join(f.root, "greeting.go"), "Greeting", "string", "string", 1) && adaptiveChangePureFile(filepath.Join(f.root, "format.go"), "FormatID", "int", "string", 1)
			}
			if !row.PureCodeOracle {
				row.Failure = "PURE_CODE_ORACLE_FAILED"
				t.Fatal("generated code is outside the fixed pure oracle")
			}
			row.FinalTestsPassed = adaptiveChangeGoTest(ctx, goBinary, f.root, private)
			if !row.FinalTestsPassed {
				row.Failure = "EXECUTABLE_ORACLE_FAILED"
				t.Fatal("fixed executable tests failed")
			}
			row.Outcome = "PASS"
		})
	}
}
