package codebase

import (
	"encoding/json"
	"os"
	"testing"
)

// A runtime pin update must bring the separately audited provenance/license
// inventory with it. This is consistency with that recorded audit, not a new
// review of every vendored or bundled license or build attestation.
func TestPinnedArtifactMatchesRecordedLicenseAndProvenanceAudit(t *testing.T) {
	body, err := os.ReadFile("../../release-notes/evidence/codebase-v0.12.1/source-audit.json")
	if err != nil {
		t.Fatal("recorded source/license audit unavailable")
	}
	var audit struct {
		Commit     string `json:"source_commit"`
		Tag        string `json:"tag_commit"`
		Archive    string `json:"archive"`
		License    string `json:"top_license"`
		LicenseSHA string `json:"license_sha256"`
		Extracted  []struct {
			Name string `json:"name"`
			SHA  string `json:"sha256"`
		} `json:"extracted"`
	}
	if json.Unmarshal(body, &audit) != nil || audit.Commit != SourceCommit || audit.Tag != SourceCommit || audit.Archive != ArchiveSHA256 || audit.License != "MIT" || audit.LicenseSHA != "1f58f9911dc5e3bcb96de28bb28e7b6bb7eb323952d29569c5d7214a152146bb" {
		t.Fatal("runtime pin changed without its source/license audit")
	}
	binary, notices := false, false
	for _, a := range audit.Extracted {
		if a.Name == "codebase-memory-mcp" && a.SHA == BinarySHA256 {
			binary = true
		}
		if a.Name == "THIRD_PARTY_NOTICES.md" && digestPattern.MatchString(a.SHA) {
			notices = true
		}
	}
	if !binary || !notices {
		t.Fatal("binary identity or required third-party notice inventory missing")
	}
}
