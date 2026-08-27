package supplychain

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type archiveEntry struct {
	name     string
	body     string
	mode     int64
	typeflag byte
	link     string
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
	if _, err := (Manager{Root: t.TempDir()}).StageArchive(context.Background(), bad, bytes.NewReader(archive)); err == nil {
		t.Fatal("floating revision accepted")
	}
	bad = resolvedFor(archive, strings.Repeat("a", 40))
	bad.Integrity.Digest = strings.Repeat("0", 64)
	if _, err := (Manager{Root: t.TempDir()}).StageArchive(context.Background(), bad, bytes.NewReader(archive)); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
	bad = resolvedFor(archive, strings.Repeat("a", 40))
	bad.Source = "https://"
	if _, err := (Manager{Root: t.TempDir()}).StageArchive(context.Background(), bad, bytes.NewReader(archive)); err == nil {
		t.Fatal("invalid source accepted")
	}
}

func TestArchiveSafetyRejectsTraversalLinksDuplicatesExecutablesAndLimits(t *testing.T) {
	tests := map[string][]archiveEntry{
		"traversal":  {{name: "../escape", body: "x"}},
		"absolute":   {{name: "/escape", body: "x"}},
		"symlink":    {{name: "link", typeflag: tar.TypeSymlink, link: "target"}},
		"hardlink":   {{name: "link", typeflag: tar.TypeLink, link: "target"}},
		"duplicate":  {{name: "same", body: "a"}, {name: "same", body: "b"}},
		"executable": {{name: "install.sh", body: "exit 0", mode: 0o700}},
	}
	for name, entries := range tests {
		t.Run(name, func(t *testing.T) {
			archive := testArchive(t, entries...)
			if _, err := (Manager{Root: t.TempDir()}).StageArchive(context.Background(), resolvedFor(archive, strings.Repeat("a", 40)), bytes.NewReader(archive)); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
	archive := testArchive(t, archiveEntry{name: "large", body: strings.Repeat("x", 1024)})
	manager := Manager{Root: t.TempDir(), Limits: Limits{ArchiveBytes: 1 << 20, ExpandedBytes: 512, FileBytes: 512, Files: 10}}
	if _, err := manager.StageArchive(context.Background(), resolvedFor(archive, strings.Repeat("a", 40)), bytes.NewReader(archive)); err == nil {
		t.Fatal("oversized expanded archive accepted")
	}
	if _, err := (Manager{Root: t.TempDir(), Limits: Limits{ArchiveBytes: 10, ExpandedBytes: 1 << 20, FileBytes: 1 << 20, Files: 10}}).StageArchive(context.Background(), resolvedFor(archive, strings.Repeat("a", 40)), bytes.NewReader(archive)); err == nil {
		t.Fatal("oversized compressed archive accepted")
	}
}

func TestValidationFailurePreservesActiveAndStagingNeverExecutes(t *testing.T) {
	root := t.TempDir()
	archive := testArchive(t, archiveEntry{name: "SKILL.md", body: "do not execute: rm -rf /"}, archiveEntry{name: "hook.sh", body: "touch forbidden"})
	manager := Manager{Root: root}
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
	manager := Manager{Root: root, Now: func() time.Time { return time.Unix(100, 0) }}
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
	manager := Manager{Root: root}
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
	pipeline := Pipeline{Manager: Manager{Root: t.TempDir()}, Discoverer: fakeDiscoverer{source: source}, Fetcher: fakeFetcher{archive: archive}}
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
	staged, err := (Manager{Root: root}).StageArchive(context.Background(), source, bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(staged.ObjectPath, "bin", "helper"))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("mode=%v err=%v", info.Mode(), err)
	}
}
