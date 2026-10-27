package evidence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/rtkview"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

// Generated command snapshots contain synthetic data only. Commands execute once
// before the paired measurements; ViewOutput can never rerun them.
type generatedSnapshot struct {
	name, filter string
	raw          []byte
	facts        []string
	captureUS    int64
}
type generatedObservation struct {
	Scenario          string `json:"scenario"`
	Repetition        int    `json:"repetition"`
	Arm               int    `json:"arm"`
	Order             int    `json:"order"`
	DurationUS        int64  `json:"duration_us"`
	RawBytes          int    `json:"raw_bytes"`
	ViewBytes         int    `json:"view_bytes"`
	Derived           bool   `json:"derived"`
	Diagnostic        string `json:"diagnostic"`
	ExactRecovery     bool   `json:"exact_recovery"`
	FactsTotal        int    `json:"facts_total"`
	FactsPresent      int    `json:"facts_present"`
	FactsMissing      int    `json:"facts_missing"`
	BaselineCorrect   bool   `json:"baseline_correct"`
	ProviderFailure   bool   `json:"provider_failure"`
	ViewError         bool   `json:"view_error"`
	ArtifactPersisted bool   `json:"artifact_persisted"`
	ArtifactID        string `json:"artifact_id"`
	SnapshotSHA256    string `json:"snapshot_sha256"`
	CaptureUS         int64  `json:"capture_us"`
}

type generatedBuffer struct{ bytes.Buffer }

func (b *generatedBuffer) Write(p []byte) (int, error) {
	if len(p) > MaxPayloadBytes-b.Len() {
		return 0, errors.New("synthetic command output exceeded limit")
	}
	return b.Buffer.Write(p)
}

func generatedSnapshots(t *testing.T) []generatedSnapshot {
	t.Helper()
	root := t.TempDir()
	home := t.TempDir()
	template := t.TempDir()
	env := []string{"HOME=" + home, "XDG_CONFIG_HOME=" + home, "PATH=/nonexistent", "LC_ALL=C", "LANG=C", "TZ=UTC", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_ATTR_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_AUTHOR_NAME=Synthetic Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Synthetic Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid", "GIT_AUTHOR_DATE=2020-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2020-01-01T00:00:00Z"}
	gitArgs := []string{"-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false", "-c", "protocol.allow=never", "-c", "gc.auto=0", "-c", "maintenance.auto=false", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-c", "core.attributesFile=/dev/null", "-c", "core.excludesFile=/dev/null", "-c", "color.ui=false", "-c", "core.pager=cat"}
	run := func(binary string, args ...string) ([]byte, int64) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir, cmd.Env = root, env
		var out generatedBuffer
		cmd.Stdout, cmd.Stderr = &out, io.Discard
		start := time.Now()
		err := cmd.Run()
		duration := time.Since(start).Microseconds()
		if err != nil {
			t.Fatal("synthetic command failed; raw stdout/stderr withheld")
		}
		return bytes.Clone(out.Bytes()), duration
	}
	git := func(args ...string) ([]byte, int64) {
		return run("/usr/bin/git", append(append([]string{}, gitArgs...), args...)...)
	}
	write := func(name, text string) {
		t.Helper()
		path := filepath.Join(root, name)
		if os.MkdirAll(filepath.Dir(path), 0700) != nil || os.WriteFile(path, []byte(text), 0600) != nil {
			t.Fatal("synthetic project creation failed")
		}
	}
	git("init", "--quiet", "--template="+template)
	var grepFacts []string
	for file := 0; file < 24; file++ {
		name := fmt.Sprintf("src/component_%02d.go", file)
		var body strings.Builder
		body.WriteString("package synthetic\n")
		grepFacts = append(grepFacts, name)
		for phase := 0; phase < 3; phase++ {
			marker := fmt.Sprintf("evidence_probe_component_%02d_phase_%02d", file, phase)
			fmt.Fprintf(&body, "// %s: deterministic independent synthetic evidence\n", marker)
			grepFacts = append(grepFacts, marker)
		}
		write(name, body.String())
	}
	git("add", "--all")
	git("commit", "--quiet", "--no-verify", "--no-gpg-sign", "-m", "fixture: synthetic initial inventory")
	logFacts := []string{"fixture: synthetic initial inventory"}
	for commit := 1; commit < 18; commit++ {
		write("history.txt", fmt.Sprintf("independent history state %02d\n", commit))
		git("add", "--", "history.txt")
		subject := fmt.Sprintf("feat(component_%02d): independent change %02d", commit, commit)
		git("commit", "--quiet", "--no-verify", "--no-gpg-sign", "-m", subject)
		logFacts = append(logFacts, subject)
	}
	logRaw, logUS := git("log", "--no-show-signature", "--no-decorate", "--format=medium", "--date=iso-strict", "--max-count=18")
	var statusFacts []string
	for file := 0; file < 12; file++ {
		name := fmt.Sprintf("src/component_%02d.go", file)
		write(name, fmt.Sprintf("package synthetic\n// changed component %02d\n", file))
		statusFacts = append(statusFacts, name)
	}
	git("add", "--", "src/component_00.go", "src/component_01.go", "src/component_02.go")
	for file := 12; file < 14; file++ {
		name := fmt.Sprintf("src/component_%02d.go", file)
		if os.Remove(filepath.Join(root, name)) != nil {
			t.Fatal("synthetic deletion failed")
		}
		statusFacts = append(statusFacts, name)
	}
	for file := 0; file < 8; file++ {
		name := fmt.Sprintf("notes/untracked_%02d.txt", file)
		write(name, fmt.Sprintf("independent note %02d\n", file))
		statusFacts = append(statusFacts, name)
	}
	statusRaw, statusUS := git("status", "--untracked-files=all")
	// Search committed source, not the modified working tree, by creating a
	// separate deterministic tree from its generated content (no Git reexecution).
	search := filepath.Join(root, "search")
	if os.Mkdir(search, 0700) != nil {
		t.Fatal("synthetic search root failed")
	}
	for file := 0; file < 24; file++ {
		name := fmt.Sprintf("search/src/component_%02d.go", file)
		var body strings.Builder
		body.WriteString("package synthetic\n")
		for phase := 0; phase < 3; phase++ {
			fmt.Fprintf(&body, "// evidence_probe_component_%02d_phase_%02d: deterministic independent synthetic evidence\n", file, phase)
		}
		write(name, body.String())
	}
	grepRaw, grepUS := run("/usr/bin/grep", "-R", "-n", "-F", "--include=*.go", "--", "evidence_probe", "search/src")
	// grep's literal paths include the deliberate search/ prefix.
	for i, fact := range grepFacts {
		if strings.HasPrefix(fact, "src/") {
			grepFacts[i] = "search/" + fact
		}
	}
	snapshots := []generatedSnapshot{{"generated_git_status", "git-status", statusRaw, statusFacts, statusUS}, {"generated_git_log", "git-log", logRaw, logFacts, logUS}, {"generated_grep", "grep", grepRaw, grepFacts, grepUS}}
	for _, s := range snapshots {
		if len(s.raw) == 0 || len(s.raw) > MaxPayloadBytes || countGeneratedFacts(string(s.raw), s.facts) != len(s.facts) {
			t.Fatal("synthetic snapshot lacks declared facts")
		}
	}
	return snapshots
}

func countGeneratedFacts(text string, facts []string) int {
	n := 0
	for _, fact := range facts {
		if strings.Contains(text, fact) {
			n++
		}
	}
	return n
}

func TestGeneratedRTKSnapshotsUseRealCommandsAndIndependentFacts(t *testing.T) {
	snapshots := generatedSnapshots(t)
	if len(snapshots) != 3 {
		t.Fatal("scenario count changed")
	}
	for _, s := range snapshots {
		if len(s.facts) < 18 || countGeneratedFacts(string(s.raw), s.facts) != len(s.facts) {
			t.Fatal("incomplete synthetic command fixture")
		}
	}
}

// Opt-in real-OSS observations do not require a reducing view: declined views,
// provider failures, omitted literal facts and all latency costs are retained.
func TestRealRTKGeneratedGatewayABBA(t *testing.T) {
	binary, helper := os.Getenv("IVOAI_TEST_RTK_OSS"), os.Getenv("IVOAI_TEST_RTK_HELPER")
	if binary == "" || helper == "" {
		t.Skip("NOT_RUN: explicit pinned OSS binary and static helper required")
	}
	provider := rtkview.New(binary, helper)
	for _, snapshot := range generatedSnapshots(t) {
		t.Run(snapshot.name, func(t *testing.T) {
			storeRoot := filepath.Join(t.TempDir(), "artifacts")
			local, err := workingcontext.NewLocalStore(storeRoot, workingcontext.LocalOptions{})
			if err != nil {
				t.Fatal("local ArtifactStore creation failed")
			}
			gateway, err := New(local)
			if err != nil {
				t.Fatal("gateway creation failed")
			}
			request := Request{Owner: workingcontext.Ownership{SessionID: "sess_" + strings.Repeat("a", 32), TaskID: "primary"}, Scope: Scope{Organization: Digest("synthetic-org"), Repository: Digest("synthetic-repository"), Worktree: Digest("synthetic-worktree"), Permissions: Digest("synthetic-read")}, Source: "tool_read", QueryDigest: Digest(snapshot.name), ReadOnly: true, BudgetBytes: MaxPayloadBytes, Authorize: func(ctx context.Context) error { return ctx.Err() }, CurrentGeneration: func(ctx context.Context) (string, error) { return Digest(snapshot.raw), ctx.Err() }}
			fetches := 0
			packet, err := gateway.Retrieve(context.Background(), request, func(context.Context) (Document, error) {
				fetches++
				return Document{Raw: bytes.Clone(snapshot.raw), MediaType: "text/plain", Basis: Extracted, SourceComplete: true, Sensitivity: workingcontext.SensitivityInternal}, nil
			})
			if err != nil {
				t.Fatal("snapshot persistence failed")
			}
			// A freshly opened store must recover the disk-persisted reference exactly.
			reopened, err := workingcontext.NewLocalStore(storeRoot, workingcontext.LocalOptions{})
			if err != nil {
				t.Fatal("ArtifactStore reopen failed")
			}
			reader, stored, err := reopened.Read(context.Background(), request.Owner, packet.Ref.Result.Artifact.ID)
			if err != nil {
				t.Fatal("persisted artifact reference unreadable")
			}
			persisted, err := io.ReadAll(reader)
			reader.Close()
			persistedOK := err == nil && bytes.Equal(persisted, snapshot.raw) && stored.Complete && !stored.Truncated
			if !persistedOK {
				t.Fatal("disk artifact exactness failed")
			}
			for repetition := 0; repetition < 6; repetition++ {
				for step := 0; step < 2; step++ {
					arm := (repetition + step) % 2
					start := time.Now()
					view, viewErr := gateway.ViewOutput(context.Background(), request, packet.Ref, provider, OutputViewOptions{Enabled: arm == 1, AllowDerived: true, Kind: "inventory", Filter: snapshot.filter})
					duration := time.Since(start).Microseconds()
					raw, recoverErr := gateway.Recover(context.Background(), request, packet.Ref)
					exact := recoverErr == nil && bytes.Equal(raw, snapshot.raw)
					facts := countGeneratedFacts(view.Text, snapshot.facts)
					diagnostic := view.Diagnostic
					if viewErr != nil {
						diagnostic = "gateway_error"
					}
					row := generatedObservation{Scenario: snapshot.name, Repetition: repetition, Arm: arm, Order: step, DurationUS: duration, RawBytes: len(snapshot.raw), ViewBytes: view.ViewBytes, Derived: view.Derived, Diagnostic: diagnostic, ExactRecovery: exact, FactsTotal: len(snapshot.facts), FactsPresent: facts, FactsMissing: len(snapshot.facts) - facts, BaselineCorrect: arm == 0 && viewErr == nil && view.Text == string(snapshot.raw) && facts == len(snapshot.facts), ProviderFailure: diagnostic == "provider_failed" || diagnostic == "provider_timeout" || diagnostic == "provider_unavailable", ViewError: viewErr != nil, ArtifactPersisted: persistedOK, ArtifactID: packet.Ref.Result.Artifact.ID, SnapshotSHA256: packet.Ref.Result.Artifact.SHA256, CaptureUS: snapshot.captureUS}
					encoded, _ := json.Marshal(row)
					t.Log("GENERATED_RTK_JSON=" + string(encoded))
					if !exact || viewErr != nil || arm == 0 && !row.BaselineCorrect {
						t.Error("benchmark arm failed exactness/gateway/baseline check; observation retained")
					}
				}
			}
			if fetches != 1 {
				t.Error("snapshot was fetched more than once")
			}
		})
	}
}
