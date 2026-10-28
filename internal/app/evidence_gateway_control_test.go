package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/codeintel"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

type exactCodeEnvelope struct {
	Text      string                     `json:"text"`
	Artifact  workingcontext.ArtifactRef `json:"artifact"`
	Fidelity  core.CompressionFidelity   `json:"fidelity"`
	Untrusted bool                       `json:"untrusted"`
}

// The shared assertion verifies the App's established wire contract against the
// stored bytes. ArtifactStore ownership is distinct from Gateway sealed recovery.
func assertExactCodeEnvelope(t *testing.T, a *App, body []byte, owner workingcontext.Ownership) exactCodeEnvelope {
	t.Helper()
	var value exactCodeEnvelope
	if len(body) > 4096 || json.Unmarshal(body, &value) != nil || value.Artifact.Validate() != nil || value.Artifact.Owner != owner || value.Artifact.Truncated || !value.Untrusted || value.Fidelity != core.CompressionExactRequired {
		t.Fatal("invalid bounded exact evidence envelope")
	}
	artifacts, err := workingcontext.NewLocalStore(filepath.Join(a.Store.Paths.CacheDir, "working-context"), workingcontext.LocalOptions{})
	if err != nil {
		t.Fatal("artifact store unavailable")
	}
	reader, ref, err := artifacts.Read(context.Background(), owner, value.Artifact.ID)
	if err != nil {
		t.Fatal("exact artifact unavailable to owner")
	}
	exact, readErr := io.ReadAll(io.LimitReader(reader, 4097))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || len(exact) > 4096 || string(exact) != value.Text || ref.ID != value.Artifact.ID {
		t.Fatal("exact artifact differs from wire evidence")
	}
	return value
}

func assertCodeOwnerIsolation(t *testing.T, a *App, wrong workingcontext.Ownership, id string) {
	t.Helper()
	artifacts, err := workingcontext.NewLocalStore(filepath.Join(a.Store.Paths.CacheDir, "working-context"), workingcontext.LocalOptions{})
	if err != nil {
		t.Fatal("artifact store unavailable")
	}
	if _, err := artifacts.Stat(context.Background(), wrong, id); err == nil {
		t.Fatal("cross-owner artifact metadata accessible")
	}
	if r, _, err := artifacts.Read(context.Background(), wrong, id); err == nil {
		r.Close()
		t.Fatal("cross-owner artifact bytes accessible")
	}
}

type gatewayControlCounts struct {
	CodeInspections    int `json:"code_inspections"`
	CodeIntelCacheHits int `json:"codeintel_cache_hits"`
	DirectInspections  int `json:"direct_inspections"`
	GatewayRetrievals  int `json:"gateway_retrievals"`
	GatewayCacheHits   int `json:"gateway_cache_hits"`
}

func gatewayControlSnapshot(t *testing.T, store session.Store, id string) gatewayControlCounts {
	t.Helper()
	value, err := store.Get(id)
	if err != nil {
		t.Fatal("control session unavailable")
	}
	var c gatewayControlCounts
	for _, e := range value.Observability {
		if e.Operation == observability.OperationCodeInspect {
			if e.CacheHit == nil || e.State != observability.StateCompleted {
				t.Fatal("incomplete CodeIntel observation")
			}
			c.CodeInspections++
			if e.Provider == "direct" {
				c.DirectInspections++
			}
			if e.CacheHit != nil && *e.CacheHit {
				c.CodeIntelCacheHits++
			}
		}
		if e.Operation == observability.OperationCapabilityResolve && e.Capability == "evidence.code" {
			if e.CacheHit == nil || e.State != observability.StateCompleted {
				t.Fatal("incomplete Gateway observation")
			}
			c.GatewayRetrievals++
			if e.CacheHit != nil && *e.CacheHit {
				c.GatewayCacheHits++
			}
		}
	}
	return c
}
func (c gatewayControlCounts) subtract(before gatewayControlCounts) gatewayControlCounts {
	return gatewayControlCounts{c.CodeInspections - before.CodeInspections, c.CodeIntelCacheHits - before.CodeIntelCacheHits, c.DirectInspections - before.DirectInspections, c.GatewayRetrievals - before.GatewayRetrievals, c.GatewayCacheHits - before.GatewayCacheHits}
}

type gatewayControlRow struct {
	Arm         string               `json:"arm"`
	Operation   string               `json:"operation"`
	Outcome     string               `json:"outcome"`
	Counts      gatewayControlCounts `json:"observed_event_deltas"`
	RetrievalUS int64                `json:"retrieval_us"`
	ExactReadUS int64                `json:"envelope_and_exact_read_us"`
	TextBytes   int                  `json:"text_bytes"`
	TextSHA256  string               `json:"text_sha256,omitempty"`
}
type gatewayControlReceipt struct {
	Schema           int                 `json:"schema"`
	Condition        string              `json:"condition"`
	Provider         string              `json:"provider"`
	CodeIntelCache   string              `json:"codeintel_cache"`
	WorkerBasis      string              `json:"worker_basis"`
	RecoveryBasis    string              `json:"recovery_basis"`
	TimingBasis      string              `json:"timing_basis"`
	ModelCalls       int                 `json:"model_calls"`
	Economy          string              `json:"economy"`
	CatalogPromotion bool                `json:"catalog_promotion"`
	Rows             []gatewayControlRow `json:"rows"`
}

// This is an offline mechanism control, not the 16-scenario provider benchmark.
// Both arms execute identical host calls and retain CodeIntel's normal cache.
func runGatewayOfflineControls(t *testing.T) gatewayControlReceipt {
	t.Helper()
	// Keep this established host-store control separate from the opt-in public
	// protected reader. G0 has dedicated admission/rollback gates.
	t.Setenv(protectedEvidenceFlag, "0")
	// Refuse inherited Git redirection before creating a fixture; never operate
	// on a caller's index/object store or run a caller's init template hooks.
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG", "GIT_EXEC_PATH"} {
		if os.Getenv(key) != "" {
			t.Fatal("inherited Git redirection is outside the offline control")
		}
	}
	home, templates := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_TEMPLATE_DIR", templates)
	t.Setenv("GIT_CONFIG_COUNT", "0")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	// All fixture Git operations (including the shared corpus initialization)
	// use an absolute executable and a small explicit environment. Editor,
	// pager, SSH/askpass, external-diff and trace variables are never inherited;
	// no network, interactive, diff or editor operation is requested here.
	runGit := func(args ...string) error {
		fixed := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-c", "commit.gpgsign=false"}
		command := exec.Command("/usr/bin/git", append(fixed, args...)...)
		command.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + home, "GIT_TEMPLATE_DIR=" + templates, "GIT_CONFIG_COUNT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
		return command.Run()
	}
	receipt := gatewayControlReceipt{Schema: 1, Condition: "offline-app-gateway-v2", Provider: "actual CodeIntel direct small-file read; no Graft or model provider", CodeIntelCache: "normal CacheRoot enabled in both arms; direct fixture does not exercise structural result cache", WorkerBasis: "host metadata fixture; NOT_ACTUAL_DISPATCH; real workers UNKNOWN", RecoveryBasis: "ArtifactStore exact read under issued owner; not native-client or sealed Gateway recovery", TimingBasis: "single sequential OFF/ON diagnostic; retrieval includes host work; no equivalent-load or causal timing claim", Economy: "NOT_EVALUATED"}
	for _, arm := range []string{"OFF", "ON"} {
		t.Run(arm, func(t *testing.T) {
			flag := "0"
			if arm == "ON" {
				flag = "1"
			}
			t.Setenv("IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY", flag)
			root := prefetchLiveRepositoryWithGit(t, runGit)
			for _, args := range [][]string{{"-C", root, "add", "."}, {"-C", root, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "offline control fixture"}} {
				if runGit(args...) != nil {
					t.Fatal("fixture commit failed")
				}
			}
			other := filepath.Join(t.TempDir(), "owned")
			if runGit("-C", root, "worktree", "add", "--detach", "-q", other, "HEAD") != nil {
				t.Fatal("fixture worktree failed")
			}
			a, store, id, task := prefetchLiveHost(t, root, "Inspect API mathpkg/scale.go")
			cache := filepath.Join(a.Store.Paths.CacheDir, "code-intelligence")
			factory := func(_ context.Context, root, scope string) (*codeintel.Provider, error) {
				return &codeintel.Provider{Root: root, Scope: scope, Permissions: "filesystem.read", CacheRoot: cache, Allowed: true}, nil
			}
			workerID := "worker_" + strings.Repeat("c", 32)
			primaryOwner := workingcontext.Ownership{SessionID: id, TaskID: task.ID}
			workerOwner := workingcontext.Ownership{SessionID: id, TaskID: "worker_read", WorkerID: workerID}
			var first exactCodeEnvelope
			primary := func() ([]byte, error) {
				return a.preparePrimaryCodeEvidence(context.Background(), store, id, task, 3840, factory)
			}
			run := func(name string, owner workingcontext.Ownership, fetch func() ([]byte, error), hit, deny bool) exactCodeEnvelope {
				t.Helper()
				before := gatewayControlSnapshot(t, store, id)
				start := time.Now()
				body, err := fetch()
				elapsed := time.Since(start).Microseconds()
				delta := gatewayControlSnapshot(t, store, id).subtract(before)
				want := gatewayControlCounts{}
				if !deny {
					want.CodeInspections = 1
					want.DirectInspections = 1
					if arm == "ON" {
						want.GatewayRetrievals = 1
						if hit {
							want.CodeInspections = 0
							want.DirectInspections = 0
							want.GatewayCacheHits = 1
						}
					}
				}
				if delta != want {
					t.Fatalf("%s: unexpected allowlisted event deltas: got %+v want %+v", name, delta, want)
				}
				row := gatewayControlRow{Arm: arm, Operation: name, Outcome: "PASS", Counts: delta, RetrievalUS: elapsed}
				if deny {
					if err == nil || len(body) != 0 {
						t.Fatal("revoked primary reached retrieval")
					}
					row.Outcome = "DENIED_AS_EXPECTED"
					receipt.Rows = append(receipt.Rows, row)
					return exactCodeEnvelope{}
				}
				if err != nil {
					t.Fatalf("%s: retrieval failed", name)
				}
				exactStart := time.Now()
				value := assertExactCodeEnvelope(t, a, body, owner)
				row.ExactReadUS = time.Since(exactStart).Microseconds()
				expected := "mathpkg/scale.go (complete small file; untrusted repository data)\npackage mathpkg\nfunc Scale(x int) int { return x * 2 }\n"
				if name == "generation_changed" {
					expected = strings.Replace(expected, "x * 2", "x * 3", 1)
				}
				if value.Text != expected {
					t.Fatalf("%s: unexpected complete source evidence", name)
				}
				row.TextBytes = len(value.Text)
				sum := sha256.Sum256([]byte(value.Text))
				row.TextSHA256 = hex.EncodeToString(sum[:])
				receipt.Rows = append(receipt.Rows, row)
				return value
			}
			first = run("primary_cold", primaryOwner, primary, false, false)
			if workingcontext.ProtectedArtifactID(first.Artifact.ID) {
				t.Fatal("host-store control unexpectedly enabled protected public recovery")
			}
			warm := run("primary_warm", primaryOwner, primary, true, false)
			if warm.Artifact.ID == first.Artifact.ID {
				t.Fatal("warm retrieval did not reissue artifact")
			}
			if _, err := store.Update(id, func(v *session.Session) error {
				v.Tasks = append(v.Tasks, session.TaskMetadata{ID: "worker_read", Role: "research", Tier: "LIGHT", Model: session.UnknownModel(), State: session.StateStarting, ExecutionMode: "worker"})
				v.Workers = append(v.Workers, session.Worker{ID: workerID, TaskID: "worker_read", WorktreePath: root, Executor: "codex", Role: "research", Model: session.UnknownModel(), State: session.StateStarting, StartedAt: time.Now().UTC()})
				return nil
			}); err != nil {
				t.Fatal("fixture worker admission failed")
			}
			worker := func(directory string) func() ([]byte, error) {
				return func() ([]byte, error) {
					p, _ := factory(context.Background(), directory, "cs_prefetch_fixture")
					p.Observe = func(r core.CodeResult, err error) {
						event := codeintel.Observation(r, err)
						event.SessionID = id
						event.TaskID = workerOwner.TaskID
						event.WorkerID = workerID
						if _, e := store.Update(id, func(v *session.Session) error { return session.AppendObservation(v, event) }); e != nil {
							t.Error("fixture observation persistence failed")
						}
					}
					return a.prefetchCodeEvidence(context.Background(), p, id, workerOwner.TaskID, workerID, core.CodeQuery{Operation: "skeleton", File: "mathpkg/scale.go", BudgetBytes: 3072})
				}
			}
			shared := run("worker_same_worktree", workerOwner, worker(root), true, false)
			if shared.Artifact.ID == first.Artifact.ID {
				t.Fatal("worker inherited primary artifact")
			}
			assertCodeOwnerIsolation(t, a, workerOwner, first.Artifact.ID)
			assertCodeOwnerIsolation(t, a, primaryOwner, shared.Artifact.ID)
			if _, err := store.Update(id, func(v *session.Session) error { v.Workers[0].WorktreePath = other; return nil }); err != nil {
				t.Fatal("fixture worktree admission failed")
			}
			distinct := run("worker_distinct_worktree", workerOwner, worker(other), false, false)
			if distinct.Artifact.ID == shared.Artifact.ID {
				t.Fatal("distinct worktree inherited issued artifact")
			}
			if os.WriteFile(filepath.Join(root, "mathpkg/scale.go"), []byte("package mathpkg\nfunc Scale(x int) int { return x * 3 }\n"), 0600) != nil {
				t.Fatal("fixture generation edit failed")
			}
			changed := run("generation_changed", primaryOwner, primary, false, false)
			if changed.Text == first.Text {
				t.Fatal("generation retained stale evidence")
			}
			if _, err := store.Update(id, func(v *session.Session) error {
				v.Tasks[0].AllowedMCPTools = map[string][]string{nativeCodeMCP: {}}
				return nil
			}); err != nil {
				t.Fatal("fixture revoke failed")
			}
			run("primary_revoked", primaryOwner, primary, false, true)
		})
	}
	return receipt
}

func TestEvidenceGatewayOfflineControls(t *testing.T) { runGatewayOfflineControls(t) }

// Run the real entry point in a bounded child so refusal must precede the OFF
// subtest and every Git operation. Values refer only to nonexistent private
// fixture paths; no caller hook or configuration can be executed by this test.
func TestGatewayControlRejectsInheritedGitExecution(t *testing.T) {
	for _, key := range []string{"GIT_CONFIG_PARAMETERS", "GIT_CONFIG", "GIT_EXEC_PATH"} {
		t.Run(key, func(t *testing.T) {
			home := t.TempDir()
			value := filepath.Join(home, "must-not-use")
			if key == "GIT_CONFIG_PARAMETERS" {
				value = "'core.hooksPath=" + value + "'"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestEvidenceGatewayOfflineControls$", "-test.count=1", "-test.v")
			command.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + home, "TMPDIR=" + home, key + "=" + value}
			output, err := command.CombinedOutput()
			exit, exited := err.(*exec.ExitError)
			if !exited || exit.ExitCode() != 1 || ctx.Err() != nil || len(output) > 4096 || !strings.Contains(string(output), "inherited Git redirection is outside the offline control") || strings.Contains(string(output), "/OFF") {
				t.Fatal("inherited Git execution setting was not refused before the first arm")
			}
		})
	}
}

// Export requires an explicit new file in a private directory. Never overwrite
// historical evidence; no dynamic paths, owners, source bodies or errors escape.
func TestExportEvidenceGatewayOfflineControls(t *testing.T) {
	output := os.Getenv("IVOAI_GATEWAY_CONTROL_OUTPUT")
	if output == "" {
		t.Skip("NOT_RUN: explicit receipt output required")
	}
	parent, err := os.Stat(filepath.Dir(output))
	if !filepath.IsAbs(output) || err != nil || !parent.IsDir() || parent.Mode().Perm()&0077 != 0 {
		t.Fatal("receipt parent must be an absolute private directory")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("receipt output must not already exist")
	}
	receipt := runGatewayOfflineControls(t)
	if t.Failed() {
		return
	}
	body, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil || len(body) > 32768 || len(receipt.Rows) != 12 {
		t.Fatal("invalid bounded control receipt")
	}
	f, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("new receipt unavailable")
	}
	if _, err = f.Write(append(body, '\n')); err != nil {
		f.Close()
		t.Fatal("receipt write failed")
	}
	if f.Close() != nil {
		t.Fatal("receipt close failed")
	}
}
