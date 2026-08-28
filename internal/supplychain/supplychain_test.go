package supplychain

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/observability"
)

type archiveEntry struct {
	name     string
	body     string
	mode     int64
	typeflag byte
	link     string
}

var acceptingValidator = ValidatorFunc(func(context.Context, ResolvedSource, string) error { return nil })

func testManager(root string) Manager {
	return Manager{Root: root, Structural: acceptingValidator, Policy: acceptingValidator, Health: acceptingValidator}
}

func testArchive(t *testing.T, entries ...archiveEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		if entry.typeflag == 0 {
			entry.typeflag = tar.TypeReg
		}
		if entry.mode == 0 {
			entry.mode = 0o600
		}
		header := &tar.Header{Name: entry.name, Mode: entry.mode, Typeflag: entry.typeflag, Linkname: entry.link}
		if entry.typeflag == tar.TypeReg {
			header.Size = int64(len(entry.body))
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if entry.typeflag == tar.TypeReg {
			if _, err := tarWriter.Write([]byte(entry.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func resolvedFor(archive []byte, revision string) ResolvedSource {
	sum := sha256.Sum256(archive)
	return ResolvedSource{ID: "synthetic", Kind: KindSkill, Source: "https://example.invalid/skills", Revision: revision, LogicalVersion: "1.0.0", DefaultBranch: "trunk", Integrity: Integrity{Algorithm: "sha256", Digest: fmt.Sprintf("%x", sum[:]), SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "checksum_only"}}
}

func stageFixture(t *testing.T, manager Manager, archive []byte, revision string) Staged {
	t.Helper()
	staged, err := manager.StageArchive(context.Background(), resolvedFor(archive, revision), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	return staged
}

func TestPipelineRequiresImmutableResolutionAndChecksum(t *testing.T) {
	archive := testArchive(t, archiveEntry{name: "SKILL.md", body: "metadata"})
	bad := resolvedFor(archive, "main")
	if _, err := testManager(t.TempDir()).StageArchive(context.Background(), bad, bytes.NewReader(archive)); err == nil {
		t.Fatal("floating revision accepted")
	}
	bad = resolvedFor(archive, strings.Repeat("a", 40))
	bad.Integrity.Digest = strings.Repeat("0", 64)
	if _, err := testManager(t.TempDir()).StageArchive(context.Background(), bad, bytes.NewReader(archive)); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
	bad = resolvedFor(archive, strings.Repeat("a", 40))
	bad.Source = "https://"
	if _, err := testManager(t.TempDir()).StageArchive(context.Background(), bad, bytes.NewReader(archive)); err == nil {
		t.Fatal("invalid source accepted")
	}
}

func TestArchiveSafetyRejectsTraversalLinksDuplicatesExecutablesAndLimits(t *testing.T) {
	tests := map[string][]archiveEntry{
		"traversal":    {{name: "../escape", body: "x"}},
		"absolute":     {{name: "/escape", body: "x"}},
		"backslash":    {{name: `dir\escape`, body: "x"}},
		"reserved":     {{name: ".ivoai-provenance.json", body: "forged"}},
		"symlink":      {{name: "link", typeflag: tar.TypeSymlink, link: "target"}},
		"hardlink":     {{name: "link", typeflag: tar.TypeLink, link: "target"}},
		"special-file": {{name: "pipe", typeflag: tar.TypeFifo}},
		"duplicate":    {{name: "same", body: "a"}, {name: "same", body: "b"}},
		"executable":   {{name: "install.sh", body: "exit 0", mode: 0o700}},
	}
	for name, entries := range tests {
		t.Run(name, func(t *testing.T) {
			archive := testArchive(t, entries...)
			if _, err := testManager(t.TempDir()).StageArchive(context.Background(), resolvedFor(archive, strings.Repeat("a", 40)), bytes.NewReader(archive)); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
	archive := testArchive(t, archiveEntry{name: "large", body: strings.Repeat("x", 1024)})
	manager := testManager(t.TempDir())
	manager.Limits = Limits{ArchiveBytes: 1 << 20, ExpandedBytes: 512, FileBytes: 512, Files: 10}
	if _, err := manager.StageArchive(context.Background(), resolvedFor(archive, strings.Repeat("a", 40)), bytes.NewReader(archive)); err == nil {
		t.Fatal("oversized expanded archive accepted")
	}
	compressedManager := testManager(t.TempDir())
	compressedManager.Limits = Limits{ArchiveBytes: 10, ExpandedBytes: 1 << 20, FileBytes: 1 << 20, Files: 10}
	if _, err := compressedManager.StageArchive(context.Background(), resolvedFor(archive, strings.Repeat("a", 40)), bytes.NewReader(archive)); err == nil {
		t.Fatal("oversized compressed archive accepted")
	}
	countArchive := testArchive(t, archiveEntry{name: "one", body: "1"}, archiveEntry{name: "two", body: "2"})
	countManager := testManager(t.TempDir())
	countManager.Limits = Limits{ArchiveBytes: 1 << 20, ExpandedBytes: 1 << 20, FileBytes: 1 << 20, Files: 1}
	if _, err := countManager.StageArchive(context.Background(), resolvedFor(countArchive, strings.Repeat("a", 40)), bytes.NewReader(countArchive)); err == nil {
		t.Fatal("file-count overflow accepted")
	}
	fileManager := testManager(t.TempDir())
	fileManager.Limits = Limits{ArchiveBytes: 1 << 20, ExpandedBytes: 1 << 20, FileBytes: 4, Files: 10}
	if _, err := fileManager.StageArchive(context.Background(), resolvedFor(archive, strings.Repeat("a", 40)), bytes.NewReader(archive)); err == nil {
		t.Fatal("per-file overflow accepted")
	}
}

func TestValidationFailurePreservesActiveAndStagingNeverExecutes(t *testing.T) {
	root := t.TempDir()
	archive := testArchive(t, archiveEntry{name: "SKILL.md", body: "do not execute: rm -rf /"}, archiveEntry{name: "hook.sh", body: "touch forbidden"})
	manager := testManager(root)
	first := stageFixture(t, manager, archive, strings.Repeat("a", 40))
	if err := manager.Promote(first); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "forbidden")
	manager.Structural = ValidatorFunc(func(_ context.Context, _ ResolvedSource, staged string) error {
		if _, err := os.Stat(filepath.Join(staged, "hook.sh")); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatal("staging executed external content")
		}
		return fmt.Errorf("invalid staged content")
	})
	secondArchive := testArchive(t, archiveEntry{name: "SKILL.md", body: "candidate"}, archiveEntry{name: "hook.sh", body: "touch forbidden"})
	if _, err := manager.StageArchive(context.Background(), resolvedFor(secondArchive, strings.Repeat("b", 40)), bytes.NewReader(secondArchive)); err == nil {
		t.Fatal("invalid staged content accepted")
	}
	active, _, err := manager.Active("synthetic")
	if err != nil || active.Revision != strings.Repeat("a", 40) {
		t.Fatalf("active=%+v err=%v", active, err)
	}
}

func TestAtomicPointerPromotionRollbackAndIdempotency(t *testing.T) {
	root := t.TempDir()
	manager := testManager(root)
	manager.Now = func() time.Time { return time.Unix(100, 0) }
	firstArchive := testArchive(t, archiveEntry{name: "SKILL.md", body: "first"})
	secondArchive := testArchive(t, archiveEntry{name: "SKILL.md", body: "second"})
	first := stageFixture(t, manager, firstArchive, strings.Repeat("a", 40))
	if err := manager.Promote(first); err != nil || manager.Promote(first) != nil {
		t.Fatalf("first promotion err=%v", err)
	}
	second := stageFixture(t, manager, secondArchive, strings.Repeat("b", 40))
	if err := manager.Promote(second); err != nil {
		t.Fatal(err)
	}
	active, _, _ := manager.Active("synthetic")
	if active.Revision != strings.Repeat("b", 40) {
		t.Fatalf("active=%s", active.Revision)
	}
	rolled, err := manager.Rollback("synthetic")
	if err != nil || !rolled {
		t.Fatalf("rolled=%t err=%v", rolled, err)
	}
	rolled, err = manager.Rollback("synthetic")
	if err != nil || rolled {
		t.Fatalf("repeated rollback=%t err=%v", rolled, err)
	}
	active, _, _ = manager.Active("synthetic")
	if active.Revision != strings.Repeat("a", 40) {
		t.Fatalf("rollback active=%s", active.Revision)
	}
	pointerInfo, _ := os.Stat(filepath.Join(root, "state", "synthetic.json"))
	if pointerInfo.Mode().Perm() != 0o600 {
		t.Fatalf("pointer mode=%o", pointerInfo.Mode().Perm())
	}
}

func TestInterruptedStagingRecoveryAndUnmanagedContentPreserved(t *testing.T) {
	root := t.TempDir()
	unmanaged := filepath.Join(filepath.Dir(root), "unmanaged-content")
	if err := os.WriteFile(unmanaged, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := testManager(root)
	if err := manager.ensureRoot(); err != nil {
		t.Fatal(err)
	}
	id := "sc_" + strings.Repeat("a", 32)
	staging := filepath.Join(root, "staging", id)
	if err := os.MkdirAll(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	archive := testArchive(t, archiveEntry{name: "SKILL.md", body: "content"})
	tx := transaction{Schema: SchemaVersion, ID: id, Source: resolvedFor(archive, strings.Repeat("b", 40)), State: "staging", Staging: staging, UpdatedAt: time.Now().UTC()}
	if err := manager.saveTransaction(tx); err != nil {
		t.Fatal(err)
	}
	recovered, err := manager.Recover()
	if err != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatal("interrupted staging remains")
	}
	data, err := os.ReadFile(unmanaged)
	if err != nil || string(data) != "preserve" {
		t.Fatalf("unmanaged changed: %q %v", data, err)
	}
}

func TestInterruptedPromotionRecoveryRestoresPreviousPointer(t *testing.T) {
	root := t.TempDir()
	manager := testManager(root)
	first := stageFixture(t, manager, testArchive(t, archiveEntry{name: "SKILL.md", body: "first"}), strings.Repeat("a", 40))
	if err := manager.Promote(first); err != nil {
		t.Fatal(err)
	}
	second := stageFixture(t, manager, testArchive(t, archiveEntry{name: "SKILL.md", body: "second"}), strings.Repeat("b", 40))
	tx, err := manager.loadTransaction(second.TransactionID)
	if err != nil {
		t.Fatal(err)
	}
	tx.State, tx.Previous, tx.UpdatedAt = "promoting", strings.Repeat("a", 40), time.Now().UTC()
	if err := manager.saveTransaction(tx); err != nil {
		t.Fatal(err)
	}
	if err := manager.savePointer(Pointer{Schema: SchemaVersion, ID: "synthetic", Active: strings.Repeat("b", 40), Previous: strings.Repeat("a", 40), UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	var reconciled []string
	recovered, err := manager.RecoverWithActivation(func(id string) error {
		reconciled = append(reconciled, id)
		return nil
	})
	if err != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	if !reflect.DeepEqual(reconciled, []string{"synthetic"}) {
		t.Fatalf("reconciled artifacts = %v", reconciled)
	}
	active, _, err := manager.Active("synthetic")
	if err != nil || active.Revision != strings.Repeat("a", 40) {
		t.Fatalf("active=%+v err=%v", active, err)
	}
}

type fakeDiscoverer struct{ source ResolvedSource }

func (f fakeDiscoverer) Resolve(context.Context, Reference) (ResolvedSource, error) {
	return f.source, nil
}

type fakeFetcher struct{ archive []byte }

func (f fakeFetcher) Fetch(context.Context, ResolvedSource) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.archive)), nil
}

func TestGenericPipelineStagesComponentsAndSkillsWithoutExecuting(t *testing.T) {
	archive := testArchive(t, archiveEntry{name: "payload.txt", body: "data"})
	source := resolvedFor(archive, strings.Repeat("c", 40))
	pipeline := Pipeline{Manager: testManager(t.TempDir()), Discoverer: fakeDiscoverer{source: source}, Fetcher: fakeFetcher{archive: archive}}
	staged, err := pipeline.Prepare(context.Background(), Reference{ID: source.ID, Kind: source.Kind, Source: source.Source})
	if err != nil || staged.Source.Revision != source.Revision {
		t.Fatalf("staged=%+v err=%v", staged, err)
	}
}

func TestComponentMayDeclareBoundedExecutableWithoutRunningIt(t *testing.T) {
	archive := testArchive(t, archiveEntry{name: "bin/helper", body: "binary fixture", mode: 0o755})
	source := resolvedFor(archive, strings.Repeat("d", 40))
	source.Kind = KindComponent
	source.Executables = []string{"bin/helper"}
	root := t.TempDir()
	staged, err := testManager(root).StageArchive(context.Background(), source, bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(staged.ObjectPath, "bin", "helper"))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("mode=%v err=%v", info.Mode(), err)
	}
}

func TestRawComponentStagesVerifiedExecutableWithoutRunningIt(t *testing.T) {
	payload := []byte("raw binary fixture")
	sum := sha256.Sum256(payload)
	source := ResolvedSource{
		ID: "raw-component", Kind: KindComponent, Source: "https://example.invalid/component",
		Revision: strings.Repeat("d", 40), LogicalVersion: "1.0.0", PayloadFormat: "raw", PayloadPath: "bin/component",
		License: "BSL-1.1", Executables: []string{"bin/component"},
		Integrity: Integrity{Algorithm: "sha256", Digest: fmt.Sprintf("%x", sum[:]), SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "checksum_only"},
	}
	staged, err := testManager(t.TempDir()).StageArchive(context.Background(), source, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(staged.ObjectPath, "bin", "component"))
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("payload=%q err=%v", data, err)
	}
	info, err := os.Stat(filepath.Join(staged.ObjectPath, "bin", "component"))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("mode=%v err=%v", info.Mode(), err)
	}
}

func TestStagingSanitizesRegularFileModeAndHonorsCancellation(t *testing.T) {
	archive := testArchive(t, archiveEntry{name: "payload.txt", body: "data", mode: 0o666})
	manager := testManager(t.TempDir())
	staged, err := manager.StageArchive(context.Background(), resolvedFor(archive, strings.Repeat("d", 40)), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(staged.ObjectPath, "payload.txt"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v err=%v", info.Mode(), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := testManager(t.TempDir()).StageArchive(ctx, resolvedFor(archive, strings.Repeat("e", 40)), bytes.NewReader(archive)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err=%v", err)
	}
}

func TestPipelineRejectsMissingValidatorsAndMismatchedResolution(t *testing.T) {
	archive := testArchive(t, archiveEntry{name: "SKILL.md", body: "metadata"})
	source := resolvedFor(archive, strings.Repeat("e", 40))
	if _, err := (Manager{Root: t.TempDir()}).StageArchive(context.Background(), source, bytes.NewReader(archive)); err == nil {
		t.Fatal("staging without structural, policy, and health validators succeeded")
	}
	pipeline := Pipeline{Manager: testManager(t.TempDir()), Discoverer: fakeDiscoverer{source: source}, Fetcher: fakeFetcher{archive: archive}}
	if _, err := pipeline.Prepare(context.Background(), Reference{ID: "another", Kind: KindSkill, Source: source.Source}); err == nil {
		t.Fatal("discoverer substituted a different artifact")
	}
}

func TestPromotionRequiresMatchingTransactionAndUntamperedObject(t *testing.T) {
	root := t.TempDir()
	manager := testManager(root)
	archive := testArchive(t, archiveEntry{name: "SKILL.md", body: "original"})
	staged := stageFixture(t, manager, archive, strings.Repeat("e", 40))
	forged := staged
	forged.TransactionID = "sc_" + strings.Repeat("f", 32)
	if err := manager.Promote(forged); err == nil {
		t.Fatal("promotion without matching transaction succeeded")
	}
	if _, _, err := manager.Active("synthetic"); !os.IsNotExist(err) {
		t.Fatalf("forged promotion changed active pointer: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staged.ObjectPath, "SKILL.md"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.Promote(staged); err == nil {
		t.Fatal("tampered staged object was promoted")
	}
	if _, _, err := manager.Active("synthetic"); !os.IsNotExist(err) {
		t.Fatalf("tampered promotion changed active pointer: %v", err)
	}
}

func TestPostPromotionHealthFailureRestoresPrevious(t *testing.T) {
	root := t.TempDir()
	manager := testManager(root)
	first := stageFixture(t, manager, testArchive(t, archiveEntry{name: "SKILL.md", body: "first"}), strings.Repeat("a", 40))
	if err := manager.Promote(first); err != nil {
		t.Fatal(err)
	}
	second := stageFixture(t, manager, testArchive(t, archiveEntry{name: "SKILL.md", body: "second"}), strings.Repeat("b", 40))
	manager.Health = ValidatorFunc(func(context.Context, ResolvedSource, string) error { return errors.New("health failed") })
	if err := manager.Promote(second); err == nil {
		t.Fatal("unhealthy candidate committed")
	}
	active, _, err := manager.Active("synthetic")
	if err != nil || active.Revision != strings.Repeat("a", 40) {
		t.Fatalf("active=%+v err=%v", active, err)
	}
}

func TestRollbackRejectsCorruptPreviousAndPreservesActive(t *testing.T) {
	root := t.TempDir()
	manager := testManager(root)
	first := stageFixture(t, manager, testArchive(t, archiveEntry{name: "SKILL.md", body: "first"}), strings.Repeat("a", 40))
	second := stageFixture(t, manager, testArchive(t, archiveEntry{name: "SKILL.md", body: "second"}), strings.Repeat("b", 40))
	if err := manager.Promote(first); err != nil {
		t.Fatal(err)
	}
	if err := manager.Promote(second); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first.ObjectPath, ".ivoai-provenance.json"), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if rolled, err := manager.Rollback("synthetic"); err == nil || rolled {
		t.Fatalf("rolled=%t err=%v", rolled, err)
	}
	active, _, err := manager.Active("synthetic")
	if err != nil || active.Revision != strings.Repeat("b", 40) {
		t.Fatalf("active=%+v err=%v", active, err)
	}
}

func TestSupplyChainEmitsBoundedMetadataOnlyEvents(t *testing.T) {
	root := t.TempDir()
	manager := testManager(root)
	var events []observability.Event
	manager.Observe = func(event observability.Event) { events = append(events, event) }
	staged := stageFixture(t, manager, testArchive(t, archiveEntry{name: "SKILL.md", body: "private body"}), strings.Repeat("a", 40))
	if err := manager.Promote(staged); err != nil {
		t.Fatal(err)
	}
	if len(events) < 2 || events[0].Operation != observability.OperationSupplyStage || events[len(events)-1].Operation != observability.OperationSupplyPromote {
		t.Fatalf("events=%+v", events)
	}
	data, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private body") || strings.Contains(string(data), root) {
		t.Fatalf("event leaked content or path: %s", data)
	}
}

func TestReadOperationsRejectSymlinkedManagedRoot(t *testing.T) {
	realRoot := t.TempDir()
	link := filepath.Join(t.TempDir(), "supply-chain")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ListPointers(link); err == nil {
		t.Fatal("pointer listing followed a symlinked supply-chain root")
	}
	if _, _, err := (Manager{Root: link}).Active("synthetic"); err == nil {
		t.Fatal("active lookup followed a symlinked supply-chain root")
	}
}

func TestReadOperationsRejectSymlinkedStateRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	stateTarget := t.TempDir()
	if err := os.Symlink(stateTarget, filepath.Join(root, "state")); err != nil {
		t.Fatal(err)
	}
	if _, err := ListPointers(root); err == nil {
		t.Fatal("pointer listing followed a symlinked state root")
	}
}

func TestValidateRootRejectsUnsafeStagingSubdirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "staging")); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRoot(root); err == nil {
		t.Fatal("health check accepted symlinked staging root")
	}
}
