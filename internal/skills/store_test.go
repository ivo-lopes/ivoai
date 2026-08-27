package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/core"
)

func fixtureEntry(id string) Entry {
	return Entry{
		ID: id, Name: strings.ToUpper(id), Description: "bounded synthetic skill metadata", Domain: "engineering",
		Triggers: []string{"review", "plan"}, Phase: PhasePlanning, Risk: RiskLow, Lifecycle: LifecycleStaged,
		Provenance: Provenance{
			Source:    Source{Kind: "git", URL: "https://example.invalid/skills", Repository: "https://example.invalid/skills", Path: id + "/SKILL.md", DefaultBranch: "trunk"},
			Revision:  Revision{Commit: strings.Repeat("a", 40), LogicalVersion: "1.0.0"},
			Integrity: Integrity{Algorithm: "sha256", Digest: strings.Repeat("b", 64), Verified: true, SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "checksum_only"},
		},
	}
}

func TestRegistryRoundTripIsDeterministicAndPrivate(t *testing.T) {
	path := RegistryPath(t.TempDir())
	store := Store{Path: path}
	registry := EmptyRegistry()
	registry.Entries = []Entry{fixtureEntry("zeta"), fixtureEntry("alpha")}
	if err := store.Save(registry); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{loaded.Entries[0].ID, loaded.Entries[1].ID}; !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
		t.Fatalf("order=%v", got)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	first, _ := os.ReadFile(path)
	if err := store.Save(loaded); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("registry serialization is not deterministic")
	}
}

func TestAbsentRegistryIsV050CompatibleAndEmpty(t *testing.T) {
	store := Store{Path: RegistryPath(t.TempDir())}
	registry, err := store.Load()
	if err != nil || registry.Schema != RegistrySchemaVersion || len(registry.Entries) != 0 {
		t.Fatalf("registry=%+v err=%v", registry, err)
	}
	status := store.Probe(context.Background())
	if !status.Available || status.Health != core.HealthHealthy {
		t.Fatalf("status=%+v", status)
	}
}

func TestRegistryRejectsDuplicateInvalidSourcePathAndFloatingRevision(t *testing.T) {
	for name, mutate := range map[string]func(*Registry){
		"duplicate": func(r *Registry) { r.Entries = append(r.Entries, r.Entries[0]) },
		"source":    func(r *Registry) { r.Entries[0].Provenance.Source.URL = "http://insecure.invalid" },
		"path":      func(r *Registry) { r.Entries[0].Provenance.Source.Path = "../escape/SKILL.md" },
		"floating":  func(r *Registry) { r.Entries[0].Provenance.Revision.Commit = "main" },
	} {
		t.Run(name, func(t *testing.T) {
			registry := EmptyRegistry()
			registry.Entries = []Entry{fixtureEntry("alpha")}
			mutate(&registry)
			if err := (Store{Path: RegistryPath(t.TempDir())}).Save(registry); err == nil {
				t.Fatal("unsafe registry accepted")
			}
		})
	}
}

func TestRegistryBoundsMetadataAndRejectsSymlink(t *testing.T) {
	registry := EmptyRegistry()
	entry := fixtureEntry("alpha")
	entry.Description = strings.Repeat("x", MaxDescriptionBytes+1)
	registry.Entries = []Entry{entry}
	if err := (Store{Path: RegistryPath(t.TempDir())}).Save(registry); err == nil {
		t.Fatal("oversized metadata accepted")
	}
	root := t.TempDir()
	target := filepath.Join(root, "target.json")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "registry.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	_, err := (Store{Path: link}).Load()
	if err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink err=%v", err)
	}
}

func TestRegistryImplementsCoreBoundary(t *testing.T) {
	var _ core.SkillRegistry = Store{}
	store := Store{Path: RegistryPath(t.TempDir())}
	registry := EmptyRegistry()
	registry.Entries = []Entry{fixtureEntry("alpha")}
	if err := store.Save(registry); err != nil {
		t.Fatal(err)
	}
	descriptor, err := store.Resolve(context.Background(), "alpha")
	if err != nil || descriptor.Source.Version != strings.Repeat("a", 40) {
		t.Fatalf("descriptor=%+v err=%v", descriptor, err)
	}
}
