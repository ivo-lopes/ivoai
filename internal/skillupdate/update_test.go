package skillupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/policy"
	"github.com/ivo-lopes/ivoai/internal/skills"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

const (
	revisionA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	revisionB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type fakeDiscoverer struct {
	source supplychain.ResolvedSource
	err    error
	calls  int
}

func (f *fakeDiscoverer) Resolve(context.Context, supplychain.Reference) (supplychain.ResolvedSource, error) {
	f.calls++
	return f.source, f.err
}

type fakeFetcher struct {
	archives map[string][]byte
	calls    int
}

func (f *fakeFetcher) Fetch(_ context.Context, source supplychain.ResolvedSource) (io.ReadCloser, error) {
	f.calls++
	archive, ok := f.archives[source.Revision]
	if !ok {
		return nil, errors.New("missing fake archive")
	}
	return io.NopCloser(bytes.NewReader(archive)), nil
}

func TestUpdateAtoBNoChangeAndRollbackKeepsRegistryPointerConsistent(t *testing.T) {
	root := t.TempDir()
	archiveA := skillArchive(t, "A", 0o600, 64)
	archiveB := skillArchive(t, "B", 0o600, 64)
	discoverer := &fakeDiscoverer{source: resolved(revisionA, archiveA)}
	fetcher := &fakeFetcher{archives: map[string][]byte{revisionA: archiveA, revisionB: archiveB}}
	manager := testManager(root, discoverer, fetcher)

	first, err := manager.Update(context.Background(), reference())
	if err != nil || !first.Changed || first.Revision != revisionA {
		t.Fatalf("first update = %+v, %v", first, err)
	}
	assertConsistent(t, manager, revisionA)
	if got := discoverer.source.DefaultBranch; got != "develop" {
		t.Fatalf("default branch = %q", got)
	}

	discoverer.source = resolved(revisionB, archiveB)
	second, err := manager.Update(context.Background(), reference())
	if err != nil || !second.Changed || second.Revision != revisionB {
		t.Fatalf("second update = %+v, %v", second, err)
	}
	assertConsistent(t, manager, revisionB)
	fetchCalls := fetcher.calls
	unchanged, err := manager.Update(context.Background(), reference())
	if err != nil || unchanged.Changed || fetcher.calls != fetchCalls {
		t.Fatalf("no-change update = %+v err=%v fetches=%d", unchanged, err, fetcher.calls)
	}

	rolled, err := manager.Rollback(context.Background(), "official-pack")
	if err != nil || !rolled {
		t.Fatalf("rollback = %t, %v", rolled, err)
	}
	assertConsistent(t, manager, revisionA)
}

func TestUpdateRejectsFloatingRevisionChecksumOversizeMalformedPolicyAndHealth(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Manager, *fakeDiscoverer, *fakeFetcher)
	}{
		{"floating revision", func(_ *Manager, d *fakeDiscoverer, _ *fakeFetcher) { d.source.Revision = "develop" }},
		{"checksum mismatch", func(_ *Manager, d *fakeDiscoverer, _ *fakeFetcher) {
			d.source.Integrity.Digest = strings.Repeat("0", 64)
		}},
		{"oversized download", func(m *Manager, _ *fakeDiscoverer, _ *fakeFetcher) { m.Supply.Limits.ArchiveBytes = 16 }},
		{"malformed pack", func(m *Manager, _ *fakeDiscoverer, _ *fakeFetcher) {
			m.Classifier = ClassifierFunc(func(context.Context, supplychain.ResolvedSource, string) ([]skills.Entry, error) {
				return nil, errors.New("malformed pack")
			})
		}},
		{"policy failure", func(m *Manager, _ *fakeDiscoverer, _ *fakeFetcher) {
			m.Classifier = fixedClassifier([]string{"shell.execute"})
		}},
		{"health failure", func(m *Manager, _ *fakeDiscoverer, _ *fakeFetcher) {
			m.Smoke = SmokeFunc(func(context.Context, supplychain.ResolvedSource, string, []skills.Entry) error {
				return errors.New("health failed")
			})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			archive := skillArchive(t, "A", 0o600, 64)
			discoverer := &fakeDiscoverer{source: resolved(revisionA, archive)}
			fetcher := &fakeFetcher{archives: map[string][]byte{revisionA: archive}}
			manager := testManager(root, discoverer, fetcher)
			test.mutate(&manager, discoverer, fetcher)
			if _, err := manager.Update(context.Background(), reference()); err == nil {
				t.Fatal("expected safe update rejection")
			}
			if _, _, err := manager.Supply.Active("official-pack"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed update promoted an active artifact: %v", err)
			}
		})
	}
}

func TestFailedBValidationPreservesAAndUnmanagedRegistryContent(t *testing.T) {
	root := t.TempDir()
	archiveA := skillArchive(t, "A", 0o600, 64)
	archiveB := skillArchive(t, "B", 0o600, 64)
	discoverer := &fakeDiscoverer{source: resolved(revisionA, archiveA)}
	fetcher := &fakeFetcher{archives: map[string][]byte{revisionA: archiveA, revisionB: archiveB}}
	manager := testManager(root, discoverer, fetcher)
	unmanaged := syntheticEntry("unmanaged-skill", "other-pack", revisionA, strings.Repeat("c", 64), nil)
	unmanaged.Provenance.Source.URL = "https://example.com/other"
	if err := manager.Registry.Save(skills.Registry{Schema: skills.RegistrySchemaVersion, Entries: []skills.Entry{unmanaged}}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Update(context.Background(), reference()); err != nil {
		t.Fatal(err)
	}
	discoverer.source = resolved(revisionB, archiveB)
	calls := 0
	manager.Smoke = SmokeFunc(func(context.Context, supplychain.ResolvedSource, string, []skills.Entry) error {
		calls++
		if calls > 1 {
			return errors.New("post-promotion doctor failed")
		}
		return nil
	})
	if _, err := manager.Update(context.Background(), reference()); err == nil {
		t.Fatal("expected post-promotion health failure")
	}
	assertConsistent(t, manager, revisionA)
	registry, err := manager.Registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range registry.Entries {
		found = found || entry.ID == "unmanaged-skill"
	}
	if !found {
		t.Fatal("unmanaged registry content was removed")
	}
}

func TestRollbackRefusesCorruptPreviousAndStagingNeverExecutes(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "executed")
	archiveA := skillArchiveWithScript(t, "A", marker)
	archiveB := skillArchive(t, "B", 0o600, 64)
	discoverer := &fakeDiscoverer{source: resolved(revisionA, archiveA)}
	fetcher := &fakeFetcher{archives: map[string][]byte{revisionA: archiveA, revisionB: archiveB}}
	manager := testManager(root, discoverer, fetcher)
	if _, err := manager.Update(context.Background(), reference()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("staging executed an untrusted script")
	}
	discoverer.source = resolved(revisionB, archiveB)
	if _, err := manager.Update(context.Background(), reference()); err != nil {
		t.Fatal(err)
	}
	previous := filepath.Join(manager.Supply.Root, "objects", "official-pack", revisionA, "skills", "demo", "SKILL.md")
	if err := os.WriteFile(previous, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if rolled, err := manager.Rollback(context.Background(), "official-pack"); err == nil || rolled {
		t.Fatalf("corrupt rollback = %t, %v", rolled, err)
	}
	assertConsistent(t, manager, revisionB)
}

func TestRecoveryReconcilesRegistryAfterInterruptedPromotion(t *testing.T) {
	root := t.TempDir()
	archiveA := skillArchive(t, "A", 0o600, 64)
	discoverer := &fakeDiscoverer{source: resolved(revisionA, archiveA)}
	fetcher := &fakeFetcher{archives: map[string][]byte{revisionA: archiveA}}
	manager := testManager(root, discoverer, fetcher)
	if _, err := manager.Update(context.Background(), reference()); err != nil {
		t.Fatal(err)
	}
	registry, err := manager.Registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	registry.Entries[0].Provenance.Revision.Commit = revisionB
	registry.Entries[0].Provenance.Integrity.Digest = strings.Repeat("b", 64)
	if err := manager.Registry.Save(registry); err != nil {
		t.Fatal(err)
	}
	if err := manager.ValidateConsistency(context.Background(), "official-pack"); err == nil {
		t.Fatal("expected Registry/pointer divergence")
	}
	// A normal no-change update fails closed instead of fetching or hiding the
	// divergence. Restoring the active immutable projection is deterministic.
	entries, err := manager.classify(context.Background(), discoverer.source, filepath.Join(manager.Supply.Root, "objects", "official-pack", revisionA))
	if err != nil {
		t.Fatal(err)
	}
	reconciled, err := replacePack(registry, discoverer.source, entries)
	if err != nil || manager.Registry.Save(reconciled) != nil {
		t.Fatalf("reconcile registry: %v", err)
	}
	assertConsistent(t, manager, revisionA)
}

func testManager(root string, discoverer *fakeDiscoverer, fetcher *fakeFetcher) Manager {
	supplyRoot := filepath.Join(root, "supply")
	return Manager{
		Supply: supplychain.Manager{Root: supplyRoot}, Discoverer: discoverer, Fetcher: fetcher,
		Registry: skills.Store{Path: filepath.Join(root, "state", "registry.json")}, Classifier: fixedClassifier(nil),
		Policy: policy.DefaultEngine(), Executor: "codex", AvailableCapabilities: map[string]bool{"filesystem.read": true}, MaximumRisk: skills.RiskModerate,
	}
}

func fixedClassifier(capabilities []string) Classifier {
	return ClassifierFunc(func(_ context.Context, source supplychain.ResolvedSource, root string) ([]skills.Entry, error) {
		path := filepath.Join(root, "skills", "demo", "SKILL.md")
		if _, err := platform.ReadRegularFile(path, 1<<20); err != nil {
			return nil, err
		}
		return []skills.Entry{syntheticEntry("demo", source.ID, source.Revision, source.Integrity.Digest, capabilities)}, nil
	})
}

func syntheticEntry(id, artifactID, revision, digest string, capabilities []string) skills.Entry {
	return skills.Entry{
		ID: id, ArtifactID: artifactID, Name: id, Description: "Synthetic skill", Domain: "testing", Phase: skills.PhaseImplementation,
		Capabilities: capabilities, Risk: skills.RiskLow, Lifecycle: skills.LifecycleStaged,
		Provenance: skills.Provenance{Source: skills.Source{Kind: "git", URL: "https://example.com/official", Repository: "https://example.com/official", Path: "skills/demo/SKILL.md", DefaultBranch: "develop"}, Revision: skills.Revision{Commit: revision, LogicalVersion: revision}, Integrity: skills.Integrity{Algorithm: "sha256", Digest: digest, Verified: true, SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "commit_pinned_local_digest"}},
	}
}

func reference() supplychain.Reference {
	return supplychain.Reference{ID: "official-pack", Kind: supplychain.KindSkill, Source: "https://example.com/official"}
}

func resolved(revision string, archive []byte) supplychain.ResolvedSource {
	sum := sha256.Sum256(archive)
	return supplychain.ResolvedSource{ID: "official-pack", Kind: supplychain.KindSkill, Source: "https://example.com/official", Revision: revision, LogicalVersion: revision, DefaultBranch: "develop", Integrity: supplychain.Integrity{Algorithm: "sha256", Digest: hex.EncodeToString(sum[:]), SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "commit_pinned_local_digest"}}
}

func skillArchive(t *testing.T, version string, mode int64, padding int) []byte {
	t.Helper()
	return archive(t, map[string]archiveFile{"skills/demo/SKILL.md": {body: "---\nname: demo\n---\n# " + version + strings.Repeat("x", padding), mode: mode}})
}

func skillArchiveWithScript(t *testing.T, version, marker string) []byte {
	t.Helper()
	return archive(t, map[string]archiveFile{
		"skills/demo/SKILL.md": {body: "---\nname: demo\n---\n# " + version, mode: 0o600},
		"install.sh":           {body: "#!/bin/sh\ntouch " + marker + "\n", mode: 0o600},
	})
}

type archiveFile struct {
	body string
	mode int64
}

func archive(t *testing.T, files map[string]archiveFile) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, file := range files {
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: file.mode, Size: int64(len(file.body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write([]byte(file.body)); err != nil {
			t.Fatal(err)
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

func assertConsistent(t *testing.T, manager Manager, revision string) {
	t.Helper()
	source, _, err := manager.Supply.Active("official-pack")
	if err != nil || source.Revision != revision {
		t.Fatalf("active source = %+v, %v", source, err)
	}
	if err := manager.ValidateConsistency(context.Background(), "official-pack"); err != nil {
		t.Fatal(err)
	}
}
