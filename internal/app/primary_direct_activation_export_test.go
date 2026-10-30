//go:build linux

package app

// Offline qualification only: no client, Node, Graft process, model or launcher.

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/benchmarkfixtures"
	"github.com/ivo-lopes/ivoai/internal/codeintel"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

//go:embed primary_direct_activation_export_test.go
var primaryDirectActivationSource []byte

const primaryDirectActivationMaxBytes = 16384

type primaryDirectActivationIdentity struct {
	Executable string `json:"executable_sha256"`
	Harness    string `json:"harness_sha256"`
	Exporter   string `json:"exporter_sha256"`
	Catalog    string `json:"catalog_sha256"`
	Fixture    string `json:"fixture_sha256"`
	Task       string `json:"task_sha256"`
	Prompt     string `json:"prompt_sha256"`
	Expected   string `json:"expected_sha256"`
	Challenge  string `json:"challenge_sha256"`
}

type primaryDirectActivationArm struct {
	Arm                  string `json:"arm"`
	GatewayEnabled       bool   `json:"gateway_enabled"`
	ActualFactoryChecked bool   `json:"actual_factory_checked"`
	ExecutionMode        string `json:"execution_mode"`
	Classified           bool   `json:"classified"`
	CodeInspections      int    `json:"code_inspections"`
	DirectInspections    int    `json:"direct_inspections"`
	GraftInspections     int    `json:"graft_inspections"`
	CodeIntelHits        int    `json:"codeintel_cache_hits"`
	Resolves             int    `json:"gateway_resolves"`
	Hits                 int    `json:"gateway_cache_hits"`
	Misses               int    `json:"gateway_cache_misses"`
	Unknown              int    `json:"gateway_cache_unknown"`
	TextBytes            int    `json:"text_bytes"`
	TextSHA              string `json:"text_sha256"`
	ExactFidelity        bool   `json:"exact_fidelity"`
	Untrusted            bool   `json:"untrusted"`
	OwnerExact           bool   `json:"artifact_owner_exact"`
	ExactReadEqual       bool   `json:"exact_read_equal"`
	WrongOwnerDenied     bool   `json:"wrong_owner_denied"`
	FixtureUnchanged     bool   `json:"fixture_unchanged"`
}

type primaryDirectActivationReceipt struct {
	Schema                     int                             `json:"schema_version"`
	Contract                   string                          `json:"contract"`
	Scenario                   string                          `json:"scenario"`
	Fixture                    string                          `json:"fixture"`
	Status                     string                          `json:"status"`
	ActualFactoryQualification bool                            `json:"supplies_actual_factory_qualification"`
	ColdOnly                   bool                            `json:"cold_only"`
	Warmup                     bool                            `json:"warmup_performed"`
	ClientStarted              bool                            `json:"model_client_started"`
	Authenticated              bool                            `json:"authenticated"`
	ProtectedRecovery          bool                            `json:"protected_recovery_enabled"`
	Identity                   primaryDirectActivationIdentity `json:"identity"`
	Arms                       []primaryDirectActivationArm    `json:"arms"`
}

func primaryDirectActivationSHA(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func primaryDirectActivationDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(value) == 64 && len(decoded) == 32 && strings.ToLower(value) == value
}

func primaryDirectActivationOutput(output string) error {
	if !filepath.IsAbs(output) || filepath.Clean(output) != output {
		return errors.New("activation output path refused")
	}
	parent := filepath.Dir(output)
	for current := parent; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("activation output directory refused")
		}
		if current == parent {
			owner, ok := info.Sys().(*syscall.Stat_t)
			if !ok || owner.Uid != uint32(os.Getuid()) || info.Mode().Perm()&0077 != 0 {
				return errors.New("activation output directory must be private and owned")
			}
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		return errors.New("activation output must be new")
	}
	return nil
}

func runPrimaryDirectActivation(t *testing.T, challenge string) primaryDirectActivationReceipt {
	t.Helper()
	if !primaryDirectActivationDigest(challenge) {
		t.Fatal("fresh host challenge digest required")
	}
	definition, ok := benchmarkfixtures.Lookup("api")
	if !ok || !definition.RunnerSupported || definition.Execution != "read_only" || definition.Fixture != "go-core" {
		t.Fatal("frozen API scenario unavailable")
	}
	query, classified := codeintel.ForTask(definition.Task)
	if !classified || query.Operation != "ask" || query.Query != "Scale" {
		t.Fatal("catalog task did not admit exact declaration lookup")
	}
	expected := map[string]any{}
	if json.Unmarshal(definition.Expected, &expected) != nil {
		t.Fatal("catalog oracle unavailable")
	}
	canonicalExpected, err := json.Marshal(expected)
	if err != nil {
		t.Fatal("catalog oracle unavailable")
	}
	fixture, err := benchmarkfixtures.ProjectSHA256(definition.Fixture)
	if err != nil {
		t.Fatal("catalog fixture unavailable")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal("test executable identity unavailable")
	}
	file, err := os.Open(executable)
	if err != nil {
		t.Fatal("test executable identity unavailable")
	}
	sum := sha256.New()
	bytes, copyErr := io.Copy(sum, io.LimitReader(file, (512<<20)+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || bytes > 512<<20 {
		t.Fatal("test executable identity outside bound")
	}
	receipt := primaryDirectActivationReceipt{Schema: 1, Contract: "primary_direct_gateway_cold_v1", Scenario: definition.ID, Fixture: definition.Fixture, Status: "QUALIFIED_COLD_DIRECT_ONLY", ColdOnly: true,
		Identity: primaryDirectActivationIdentity{Executable: hex.EncodeToString(sum.Sum(nil)), Harness: prefetchHarnessSHA256(), Exporter: primaryDirectActivationSHA(primaryDirectActivationSource), Catalog: benchmarkfixtures.CatalogSHA256(), Fixture: fixture, Task: primaryDirectActivationSHA([]byte(definition.Task)), Prompt: primaryDirectActivationSHA([]byte(definition.Prompt)), Expected: primaryDirectActivationSHA(canonicalExpected), Challenge: challenge}}
	home, templates := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("IVOAI_EXPERIMENTAL_EVIDENCE_RECOVERY", "0")
	runGit := func(args ...string) error {
		fixed := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false"}
		command := exec.Command("/usr/bin/git", append(fixed, args...)...)
		command.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + home, "GIT_TEMPLATE_DIR=" + templates, "GIT_CONFIG_COUNT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
		return command.Run()
	}
	for _, arm := range []string{"A", "B"} {
		flag := "0"
		if arm == "B" {
			flag = "1"
		}
		t.Setenv("IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY", flag)
		root := prefetchLiveRepositoryWithGit(t, runGit)
		before, err := benchmarkfixtures.DirectorySHA256(root)
		if err != nil || before != fixture {
			t.Fatal("materialized fixture identity changed")
		}
		a, store, id, task := prefetchLiveHost(t, root, definition.Task)
		provider, err := a.CodeIntelligenceProvider(context.Background(), root, "cs_prefetch_fixture")
		if err != nil || provider == nil {
			t.Fatal("actual public Direct factory unavailable")
		}
		mode, supported := any(provider).(interface{ ExecutionMode() string })
		if !supported || mode.ExecutionMode() != "direct_only" || !provider.Allowed || provider.Root != root || provider.Scope != "cs_prefetch_fixture" || provider.Permissions != "filesystem.read" || provider.Node != "" || provider.Distribution != "" || provider.Executable != "" {
			t.Fatal("actual Direct-only factory contract unavailable; legacy baseline is not qualified")
		}
		body, err := a.preparePrimaryCodeEvidence(context.Background(), store, id, task, 4094, a.CodeIntelligenceProvider)
		if err != nil || len(body) == 0 {
			t.Fatal("actual primary prefetch unavailable")
		}
		owner := workingcontext.Ownership{SessionID: id, TaskID: task.ID}
		envelope := assertExactCodeEnvelope(t, a, body, owner)
		assertCodeOwnerIsolation(t, a, workingcontext.Ownership{SessionID: id, TaskID: "other"}, envelope.Artifact.ID)
		saved, err := store.Get(id)
		if err != nil {
			t.Fatal("session observation unavailable")
		}
		counts := gatewayControlSnapshot(t, store, id)
		var observed prefetchLiveRow
		prefetchLiveObserveHost(&observed, saved.Observability)
		want := 0
		if arm == "B" {
			want = 1
		}
		if counts.CodeInspections != 1 || counts.DirectInspections != 1 || counts.CodeIntelCacheHits != 0 || observed.PrefetchCalls != 1 || observed.StructuralCalls != 0 || observed.GatewayCodeResolves != want || observed.GatewayCodeCacheMisses != want || observed.GatewayCodeCacheHits != 0 || observed.GatewayCodeCacheUnknown != 0 {
			t.Fatal("cold actual events did not meet declared treatment")
		}
		after, err := benchmarkfixtures.DirectorySHA256(root)
		if err != nil || after != before {
			t.Fatal("offline qualification changed source fixture")
		}
		receipt.Arms = append(receipt.Arms, primaryDirectActivationArm{Arm: arm, GatewayEnabled: flag == "1", ActualFactoryChecked: true, ExecutionMode: mode.ExecutionMode(), Classified: true, CodeInspections: counts.CodeInspections, DirectInspections: counts.DirectInspections, GraftInspections: observed.StructuralCalls, CodeIntelHits: counts.CodeIntelCacheHits, Resolves: observed.GatewayCodeResolves, Hits: observed.GatewayCodeCacheHits, Misses: observed.GatewayCodeCacheMisses, Unknown: observed.GatewayCodeCacheUnknown, TextBytes: len(envelope.Text), TextSHA: primaryDirectActivationSHA([]byte(envelope.Text)), ExactFidelity: true, Untrusted: envelope.Untrusted, OwnerExact: envelope.Artifact.Owner == owner, ExactReadEqual: true, WrongOwnerDenied: true, FixtureUnchanged: true})
	}
	if len(receipt.Arms) != 2 || receipt.Arms[0].TextSHA != receipt.Arms[1].TextSHA || receipt.Arms[0].TextBytes != receipt.Arms[1].TextBytes {
		t.Fatal("OFF/ON did not preserve identical exact evidence")
	}
	receipt.ActualFactoryQualification = true
	return receipt
}

func TestPrimaryDirectActivationColdContract(t *testing.T) {
	runPrimaryDirectActivation(t, strings.Repeat("1", 64))
}

func TestPrimaryDirectActivationRejectsUnclassifiedTask(t *testing.T) {
	for _, name := range []string{"small_known_file_correction", "cross_module_refactor"} {
		definition, ok := benchmarkfixtures.Lookup(name)
		if !ok {
			t.Fatal("historical edit scenario unavailable")
		}
		if _, admitted := codeintel.ForTask(definition.Task); admitted {
			t.Fatal("unactivated control was silently reclassified")
		}
	}
}

func TestPrimaryDirectActivationOutputRefusesReuseAndSymlinks(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(parent, "receipt.json")
	if err := primaryDirectActivationOutput(output); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("prior proof"), 0600); err != nil {
		t.Fatal(err)
	}
	if primaryDirectActivationOutput(output) == nil {
		t.Fatal("receipt overwrite accepted")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(parent, alias); err != nil {
		t.Fatal(err)
	}
	if primaryDirectActivationOutput(filepath.Join(alias, "new.json")) == nil {
		t.Fatal("symlink output directory accepted")
	}
	for _, challenge := range []string{"", strings.Repeat("A", 64), strings.Repeat("0", 63), strings.Repeat("x", 64)} {
		if primaryDirectActivationDigest(challenge) {
			t.Fatal("invalid host challenge accepted")
		}
	}
}

func TestExportPrimaryDirectActivationCold(t *testing.T) {
	output := os.Getenv("IVOAI_DIRECT_ACTIVATION_OUTPUT")
	if output == "" {
		t.Skip("NOT_RUN: explicit private offline receipt output required")
	}
	if err := primaryDirectActivationOutput(output); err != nil {
		t.Fatal(err)
	}
	receipt := runPrimaryDirectActivation(t, os.Getenv("IVOAI_DIRECT_ACTIVATION_CHALLENGE_SHA256"))
	body, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil || len(body) > primaryDirectActivationMaxBytes {
		t.Fatal("bounded activation receipt unavailable")
	}
	file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("new private activation receipt unavailable")
	}
	_, writeErr := file.Write(append(body, '\n'))
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("activation receipt write failed")
	}
}
