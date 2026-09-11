package skillcatalog

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/policy"
	"github.com/ivo-lopes/ivoai/internal/skillgate"
	"github.com/ivo-lopes/ivoai/internal/skills"
	"github.com/ivo-lopes/ivoai/internal/skillupdate"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

func TestNativeHeterogeneousWorkerLazySelection(t *testing.T) {
	root := t.TempDir()
	supply := supplychain.Manager{Root: filepath.Join(root, "supply")}
	store := skills.Store{Path: filepath.Join(root, "registry.json")}
	for _, id := range NativeIDs() {
		local, err := NativeSource(id)
		if err != nil {
			t.Fatal(err)
		}
		manager := skillupdate.Manager{Supply: supply, Registry: store, Discoverer: local, Fetcher: local, Classifier: NativeClassifier{}, Policy: policy.DefaultEngine(), CatalogOnly: true}
		if _, err := manager.Update(context.Background(), supplychain.Reference{ID: id, Kind: supplychain.KindSkill, Source: local.Source.Upstream.Repository}); err != nil {
			t.Fatal(err)
		}
	}
	registry, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, executor := range []string{"codex", "claude"} {
		for _, tc := range []struct {
			role, intent, required string
			zero                   bool
		}{
			{"research", "read repository inventory", "", true},
			{"implementation", "bounded backend fixture change", "ponytail", false},
			{"implementation", "polish frontend and design user interface", "ponytail", false},
			{"security", "reverse engineering and security scan", "", true},
			{"documentation", "plan content strategy", "marketing-content-strategy", false},
			{"documentation", "write ordinary usage text", "", true},
		} {
			t.Run(executor+"/"+tc.role+"/"+tc.intent, func(t *testing.T) {
				candidates := WorkerCandidates(registry, tc.role, tc.intent, executor, "auto")
				result, err := (skillgate.Gate{Registry: store, Supply: supply, Policy: policy.DefaultEngine()}).Evaluate(context.Background(), skillgate.Input{ExplicitOnly: true, Executor: executor, Candidates: candidates})
				if err != nil {
					t.Fatal(err)
				}
				if tc.zero && len(result.Selected) != 0 {
					t.Fatalf("unexpected selection: %v", result.Selected)
				}
				if tc.required != "" && !contains(result.Selected, tc.required) {
					t.Fatalf("missing %s: %v", tc.required, result.Selected)
				}
				directors := 0
				for _, id := range result.Selected {
					for _, e := range registry.Entries {
						if e.ID == id && e.Role == "visual_director" {
							directors++
						}
					}
				}
				if directors > 1 || result.LoadedBodyCount != len(result.Selected) || len(result.Selected) > 3 {
					t.Fatalf("non-lazy or conflicting projection: %+v", result.Selected)
				}
			})
		}
	}
}

func TestNativePackMaterializesOfflineThroughExistingSupplyChain(t *testing.T) {
	root := t.TempDir()
	if len(NativeIDs()) != 13 {
		t.Fatal("approved source count changed")
	}
	for _, id := range NativeIDs() {
		t.Run(id, func(t *testing.T) {
			local, err := NativeSource(id)
			if err != nil {
				t.Fatal(err)
			}
			manager := skillupdate.Manager{Supply: supplychain.Manager{Root: filepath.Join(root, "supply")}, Registry: skills.Store{Path: filepath.Join(root, "skills", "registry.json")}, Discoverer: local, Fetcher: local, Classifier: NativeClassifier{}, Policy: policy.DefaultEngine(), CatalogOnly: true}
			ref := supplychain.Reference{ID: id, Kind: supplychain.KindSkill, Source: local.Source.Upstream.Repository}
			result, err := manager.Update(context.Background(), ref)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Changed || len(result.Skills) == 0 {
				t.Fatal("native capability not materialized")
			}
			if err := manager.ValidateConsistency(context.Background(), id); err != nil {
				t.Fatal(err)
			}
			result, err = manager.Update(context.Background(), ref)
			if err != nil || result.Changed {
				t.Fatal("native materialization not idempotent", err)
			}
		})
	}
	if _, err := NativeSource("awesome-gpt-image-2"); err == nil {
		t.Fatal("historical intake entered native pack")
	}
}

func TestNativeRollbackUsesPriorImmutableOverlayAndReapplyPreservesPersonal(t *testing.T) {
	root := t.TempDir()
	local, err := NativeSource("ponytail")
	if err != nil {
		t.Fatal(err)
	}
	manager := skillupdate.Manager{Supply: supplychain.Manager{Root: filepath.Join(root, "supply")}, Registry: skills.Store{Path: filepath.Join(root, "skills", "registry.json")}, Discoverer: local, Fetcher: local, Classifier: NativeClassifier{}, Policy: policy.DefaultEngine(), CatalogOnly: true, ReconcileMissingIndex: true}
	ref := supplychain.Reference{ID: local.Source.ID, Kind: supplychain.KindSkill, Source: local.Source.Upstream.Repository}
	a := local.Source.Provenance.Revision
	if _, err := manager.Update(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	registry, _ := manager.Registry.Load()
	personal := registry.Entries[0]
	personal.ID = "personal-capability"
	personal.ArtifactID = ""
	registry.Entries = append(registry.Entries, personal)
	if err := manager.Registry.Save(registry); err != nil {
		t.Fatal(err)
	}
	local.Source.Provenance.Revision = strings.Repeat("b", 40)
	manager.Discoverer, manager.Fetcher = local, local
	if _, err := manager.Update(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if changed, err := manager.Rollback(context.Background(), ref.ID); err != nil || !changed {
		t.Fatal("rollback failed", err)
	}
	active, _, err := manager.Supply.Active(ref.ID)
	if err != nil || active.Revision != a {
		t.Fatal("wrong rollback identity", err)
	}
	if _, err := manager.Update(context.Background(), ref); err != nil {
		t.Fatal("reapply failed", err)
	}
	registry, _ = manager.Registry.Load()
	if len(registry.Entries) != 2 {
		t.Fatal("personal capability removed")
	}
	// Binary rollback can restore the old index while immutable caches remain.
	if err := manager.Registry.Save(skills.Registry{Schema: skills.RegistrySchemaVersion, Entries: []skills.Entry{personal}}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Update(context.Background(), ref); err != nil {
		t.Fatal("missing index reapply failed", err)
	}
	registry, _ = manager.Registry.Load()
	if len(registry.Entries) != 2 {
		t.Fatal("reapply did not preserve personal entry")
	}
}
