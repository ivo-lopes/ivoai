package skillcatalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/policy"
	"github.com/ivo-lopes/ivoai/internal/skills"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

func TestCuratedCatalogSeparatesUpstreamProvenanceAndPolicy(t *testing.T) {
	catalog, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Sources) != 13 {
		t.Fatalf("sources=%d", len(catalog.Sources))
	}
	for index, source := range catalog.Sources {
		if index > 0 && catalog.Sources[index-1].ID >= source.ID {
			t.Fatal("catalog ordering is not deterministic")
		}
		if source.Upstream.DefaultBranch == "" || source.Provenance.Revision == "" || source.Upstream.License == "" {
			t.Fatalf("source %s lacks observed source metadata", source.ID)
		}
		for _, classification := range source.Classifications {
			if classification.Phase == skills.PhaseOrchestration || contains(classification.RequestedCapabilities, "orchestration.authority") {
				t.Fatalf("external source %s was granted orchestration authority", source.ID)
			}
		}
	}

	ponytail, _ := catalog.Source("ponytail")
	if ponytail.Classifications[0].Phase != skills.PhaseImplementation || ponytail.Classifications[0].Role == "executor" {
		t.Fatal("Ponytail must remain an implementation skill, not an executor")
	}
	adhd, _ := catalog.Source("i-have-adhd")
	if adhd.Classifications[0].Phase != skills.PhaseInteractionProfile {
		t.Fatal("i-have-adhd must remain an interaction profile")
	}
	caveman, _ := catalog.Source("caveman-skills")
	if len(caveman.Classifications) != 1 || caveman.Classifications[0].CanonicalID != "caveman-surgical-patch" {
		t.Fatal("Caveman runtime/compression was imported instead of a bounded skill")
	}
	superpowers, _ := catalog.Source("superpowers")
	if superpowers.Classifications[0].CanonicalID != "superpowers-test-driven-development" {
		t.Fatal("Superpowers orchestration workflow must not become the IVOAI orchestrator")
	}
}

func TestCuratedCatalogPolicyAndVisualConflicts(t *testing.T) {
	catalog, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	report, err := catalog.PolicyReport(policy.DefaultEngine())
	if err != nil {
		t.Fatal(err)
	}
	decisions := map[string]policy.Decision{}
	for _, item := range report {
		decisions[item.SkillID] = item.Result.Decision
	}
	if decisions["reverse-engineering"] != policy.Deny || decisions["impeccable"] != policy.Deny {
		t.Fatal("shell-capable third-party skills must be denied by default")
	}
	if decisions["codex-security-scan"] != policy.RequireApproval || decisions["anthropic-ioc-analysis"] != policy.RequireApproval {
		t.Fatal("tool-using security skills must require approval")
	}
	if decisions["i-have-adhd"] != policy.Allow || decisions["ponytail"] != policy.Allow {
		t.Fatal("low-risk instruction skills should remain eligible")
	}

	registry := skills.EmptyRegistry()
	for _, id := range []string{"hallmark", "taste-skill"} {
		source, _ := catalog.Source(id)
		item := source.Skills[0]
		registry.Entries = append(registry.Entries, source.Classifications[0].entry(source, item, item.Path))
	}
	_, err = (skills.Resolver{Registry: registry}).Resolve(skills.ResolutionRequest{IDs: []string{"hallmark", "taste-frontend"}, Executor: "codex", MaximumRisk: skills.RiskModerate})
	if err == nil {
		t.Fatal("competing visual directors must conflict")
	}
}

func TestClassifierVerifiesPinnedContentWithoutExecution(t *testing.T) {
	data := []byte("---\nname: synthetic\ndescription: data only\n---\nnever execute ./install.sh\n")
	digest := sha256.Sum256(data)
	revision := "1234567890abcdef1234567890abcdef12345678"
	source := Source{
		ID: "synthetic", DisplayName: "Synthetic",
		Upstream:        Upstream{Repository: "https://github.com/example/synthetic", DefaultBranch: "trunk", License: "MIT", ObservedCount: 1},
		Provenance:      Provenance{Revision: revision, ResolutionSource: "github_api", SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "commit_pinned_local_digest"},
		Skills:          []UpstreamSkill{{Path: "skills/synthetic/SKILL.md", Name: "synthetic", Description: "Synthetic data-only skill.", SHA256: hex.EncodeToString(digest[:])}},
		Classifications: []Classification{{Path: "skills/synthetic/SKILL.md", CanonicalID: "synthetic", Domain: "testing", Phase: skills.PhaseImplementation, Risk: skills.RiskLow, Executors: []string{"codex"}}},
	}
	catalog := Catalog{Schema: SchemaVersion, Sources: []Source{source}}
	if err := catalog.Validate(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "example-synthetic-sha", "skills", "synthetic", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	classifier := Classifier{Catalog: catalog}
	resolved := supplychain.ResolvedSource{ID: source.ID, Kind: supplychain.KindSkill, Source: source.Upstream.Repository, Revision: revision, DefaultBranch: "trunk", Integrity: supplychain.Integrity{Algorithm: "sha256", Digest: hex.EncodeToString(digest[:]), SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "commit_pinned_local_digest"}}
	entries, err := classifier.Classify(context.Background(), resolved, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Provenance.Source.Path != "example-synthetic-sha/skills/synthetic/SKILL.md" {
		t.Fatalf("entries=%+v", entries)
	}
	if err := os.WriteFile(path, append(data, 'x'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := classifier.Classify(context.Background(), resolved, root); err == nil {
		t.Fatal("content poisoning was accepted")
	}
	resolved.Revision = "abcdef1234567890abcdef1234567890abcdef12"
	if _, err := classifier.Classify(context.Background(), resolved, root); err == nil {
		t.Fatal("unreviewed upstream revision was accepted")
	}
}

func TestCatalogDoesNotVendorSkillBodies(t *testing.T) {
	data, err := catalogFS.ReadFile("catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range [][]byte{[]byte("# Ponytail"), []byte("ignore IVOAI policy"), []byte("ACTION REQUIRED")} {
		if bytes.Contains(data, forbidden) {
			t.Fatalf("catalog vendored third-party body marker %q", forbidden)
		}
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
