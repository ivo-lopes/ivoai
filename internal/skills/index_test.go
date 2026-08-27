package skills

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func metadataDocument(id string, extra string) string {
	return fmt.Sprintf(`---
schema: 1
id: %s
name: %s
description: Synthetic metadata for deterministic indexing
source_kind: git
source_url: https://example.invalid/skills
repository: https://example.invalid/skills
path: %s/SKILL.md
default_branch: trunk
commit: %s
version: 1.0.0
checksum: sha256:%s
domain: engineering
triggers: [review, plan]
keywords: [secure, code]
requires: []
optional_dependencies: []
conflicts: []
phase: planning
role: advisor
role_mode: composable
capabilities: [filesystem.read]
risk: low
executors: [codex, claude]
operating_systems: [linux]
architectures: [amd64, arm64]
minimum_ivoai: 0.5.0
%s---
THIS BODY MUST NOT BE READ DURING DISCOVERY
`, id, strings.ToUpper(id), id, strings.Repeat("a", 40), strings.Repeat("b", 64), extra)
}

func writeSkill(t *testing.T, root, id, document string) string {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

type countingReadCloser struct {
	value string
	read  int
}

func (r *countingReadCloser) Read(buffer []byte) (int, error) {
	if r.read >= len(r.value) {
		return 0, io.EOF
	}
	buffer[0] = r.value[r.read]
	r.read++
	return 1, nil
}
func (r *countingReadCloser) Close() error { return nil }

func TestIndexerReadsOnlyFrontmatterAndUsesNoLLM(t *testing.T) {
	root := t.TempDir()
	document := metadataDocument("alpha", "") + strings.Repeat("body", 100000)
	path := writeSkill(t, root, "alpha", document)
	reader := &countingReadCloser{value: document}
	index, err := (Indexer{Open: func(opened string) (io.ReadCloser, error) {
		if opened != path {
			t.Fatalf("opened=%s", opened)
		}
		return reader, nil
	}}).Discover(root)
	if err != nil || len(index.Entries) != 1 {
		t.Fatalf("index=%+v err=%v", index, err)
	}
	bodyStart := strings.Index(document, "THIS BODY MUST NOT BE READ")
	if bodyStart < 0 || reader.read > bodyStart {
		t.Fatalf("discovery read body bytes: %d, body starts at %d", reader.read, bodyStart)
	}
}

func TestIndexerQuarantinesMalformedDuplicateMismatchSymlinkAndInvalidMetadata(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "alpha", metadataDocument("alpha", ""))
	writeSkill(t, root, "wrong-dir", metadataDocument("wrong-id", ""))
	writeSkill(t, root, "malformed", "---\nid: malformed\n")
	writeSkill(t, root, "unknown", metadataDocument("unknown", "unexpected: value\n"))
	duplicateDir := filepath.Join(root, "nested", "alpha")
	if err := os.MkdirAll(duplicateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	duplicate := strings.Replace(metadataDocument("alpha", ""), "path: alpha/SKILL.md", "path: nested/alpha/SKILL.md", 1)
	if err := os.WriteFile(filepath.Join(duplicateDir, "SKILL.md"), []byte(duplicate), 0o600); err != nil {
		t.Fatal(err)
	}
	thirdDir := filepath.Join(root, "third", "alpha")
	if err := os.MkdirAll(thirdDir, 0o700); err != nil {
		t.Fatal(err)
	}
	third := strings.Replace(metadataDocument("alpha", ""), "path: alpha/SKILL.md", "path: third/alpha/SKILL.md", 1)
	if err := os.WriteFile(filepath.Join(thirdDir, "SKILL.md"), []byte(third), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "alpha", "SKILL.md"), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	index, err := (Indexer{}).Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Entries) != 0 {
		t.Fatalf("duplicate candidate remained active: %+v", index.Entries)
	}
	reasons := map[string]bool{}
	for _, item := range index.Quarantined {
		reasons[item.Reason] = true
	}
	for _, wanted := range []string{"duplicate_id", "id_path_mismatch", "malformed_frontmatter", "unexpected_symlink"} {
		if !reasons[wanted] {
			t.Fatalf("missing quarantine %s: %+v", wanted, index.Quarantined)
		}
	}
}

func TestIndexerRejectsSymlinkedDiscoveryRoot(t *testing.T) {
	realRoot := t.TempDir()
	link := filepath.Join(t.TempDir(), "skills")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatal(err)
	}
	if _, err := (Indexer{}).Discover(link); err == nil {
		t.Fatal("indexer followed symlinked discovery root")
	}
}

func TestIndexerQuarantinesMissingAndSelfDependency(t *testing.T) {
	root := t.TempDir()
	missing := strings.Replace(metadataDocument("missing", ""), "requires: []", "requires: [absent]", 1)
	self := strings.Replace(metadataDocument("self", ""), "requires: []", "requires: [self]", 1)
	writeSkill(t, root, "missing", missing)
	writeSkill(t, root, "self", self)
	index, err := (Indexer{}).Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Entries) != 0 || len(index.Quarantined) != 2 {
		t.Fatalf("index=%+v", index)
	}
}

func TestIndexerPropagatesQuarantineThroughRequiredDependencies(t *testing.T) {
	root := t.TempDir()
	broken := strings.Replace(metadataDocument("broken", ""), "requires: []", "requires: [absent]", 1)
	dependent := strings.Replace(metadataDocument("dependent", ""), "requires: []", "requires: [broken]", 1)
	writeSkill(t, root, "broken", broken)
	writeSkill(t, root, "dependent", dependent)
	index, err := (Indexer{}).Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Entries) != 0 || len(index.Quarantined) != 2 {
		t.Fatalf("index=%+v", index)
	}
}

func TestIndexerQuarantinesInvalidUTF8Metadata(t *testing.T) {
	root := t.TempDir()
	document := []byte(metadataDocument("invalid-utf8", ""))
	marker := []byte("description: Synthetic metadata for deterministic indexing")
	position := strings.Index(string(document), string(marker))
	if position < 0 {
		t.Fatal("description marker missing")
	}
	document[position+len("description: ")] = 0xff
	writeSkill(t, root, "invalid-utf8", string(document))
	index, err := (Indexer{}).Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Entries) != 0 || len(index.Quarantined) != 1 || index.Quarantined[0].Reason != "invalid_utf8" {
		t.Fatalf("index=%+v", index)
	}
}

func TestIndexerQuarantinesUnsupportedSchemaTraversalAndOversizedMetadata(t *testing.T) {
	root := t.TempDir()
	unsupported := strings.Replace(metadataDocument("unsupported", ""), "schema: 1", "schema: 2", 1)
	traversal := strings.Replace(metadataDocument("traversal", ""), "path: traversal/SKILL.md", "path: ../escape/SKILL.md", 1)
	oversized := strings.Replace(metadataDocument("oversized", ""), "description: Synthetic metadata for deterministic indexing", "description: "+strings.Repeat("x", maxFrontmatterBytes), 1)
	writeSkill(t, root, "unsupported", unsupported)
	writeSkill(t, root, "traversal", traversal)
	writeSkill(t, root, "oversized", oversized)
	index, err := (Indexer{}).Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Entries) != 0 || len(index.Quarantined) != 3 {
		t.Fatalf("index=%+v", index)
	}
	reasons := map[string]bool{}
	for _, item := range index.Quarantined {
		reasons[item.Reason] = true
	}
	for _, wanted := range []string{"unsupported_schema", "invalid_skill_source", "metadata_too_large"} {
		if !reasons[wanted] {
			t.Fatalf("missing %s in %+v", wanted, index.Quarantined)
		}
	}
}

func TestIndexerHandlesThousandEntriesWithinBound(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 1000; index++ {
		id := fmt.Sprintf("skill-%04d", index)
		writeSkill(t, root, id, metadataDocument(id, ""))
	}
	result, err := (Indexer{}).Discover(root)
	if err != nil || len(result.Entries) != 1000 || len(result.Quarantined) != 0 {
		t.Fatalf("entries=%d quarantine=%d err=%v", len(result.Entries), len(result.Quarantined), err)
	}
}

func TestMetadataRankingIsDeterministic(t *testing.T) {
	alpha, beta, gamma := fixtureEntry("alpha"), fixtureEntry("beta"), fixtureEntry("gamma")
	alpha.Triggers, alpha.Keywords = []string{"review"}, []string{"secure"}
	beta.Triggers, beta.Keywords = []string{"plan"}, []string{"review"}
	gamma.Triggers, gamma.Keywords, gamma.Risk = []string{"review"}, []string{"secure"}, RiskCritical
	index := Index{Entries: []Entry{beta, gamma, alpha}}
	first := index.Search(SearchQuery{Text: "review secure", Domain: "engineering", Executor: "codex", MaximumRisk: RiskHigh})
	second := index.Search(SearchQuery{Text: "review secure", Domain: "engineering", Executor: "codex", MaximumRisk: RiskHigh})
	if !reflect.DeepEqual(first, second) || len(first) != 2 || first[0].Entry.ID != "alpha" || first[1].Entry.ID != "beta" {
		t.Fatalf("ranking=%+v", first)
	}
}
