package components

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestManagedOpenCodeBuildIdentity(t *testing.T) {
	metadata := managedOpenCodeMetadata{
		Version: "1.18.25-ivoai.1", Platform: "linux/amd64",
		SHA256: strings.Repeat("a", 64), SourceSHA256: strings.Repeat("b", 64), PatchSHA256: strings.Repeat("c", 64),
		UpstreamRevision: "cb7d8b2f5e44876ef98b661dc10590c915af3a9f",
		Archive:          "ivoai-opencode_linux_amd64.tar.gz",
		URL:              "https://github.com/ivo-lopes/ivoai/releases/download/v0.10.1/ivoai-opencode_linux_amd64.tar.gz",
	}
	identity := sha256.Sum256([]byte(metadata.SourceSHA256 + "\n" + metadata.PatchSHA256 + "\n"))
	metadata.Revision = hex.EncodeToString(identity[:])
	encode := func(value managedOpenCodeMetadata) string {
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return base64.StdEncoding.EncodeToString(body)
	}
	original := Spec{Name: "opencode", Version: "1.18.25", Revision: metadata.UpstreamRevision,
		Strategy: StrategySupplyChain, PayloadPath: "opencode", License: "MIT",
		Assets: map[string]Asset{"linux/amd64": {URL: "https://example.invalid/upstream", SHA256: strings.Repeat("d", 64)}},
	}
	projected, err := managedOpenCodeSpec(original, encode(metadata), "linux/amd64")
	if err != nil {
		t.Fatal(err)
	}
	if projected.Version != metadata.Version || projected.Revision == original.Revision || projected.Assets["linux/amd64"].SHA256 != metadata.SHA256 || projected.Strategy != StrategySupplyChain || projected.PayloadPath != "opencode" {
		t.Fatal("patched build did not retain isolated, verified supply-chain identity")
	}
	if original.Version != "1.18.25" || original.Assets["linux/amd64"].URL != "https://example.invalid/upstream" {
		t.Fatal("upstream catalog was mutated")
	}
	preview := metadata
	preview.URL = strings.Replace(preview.URL, "v0.10.1", "v0.10.1-rc.1", 1)
	if _, err := managedOpenCodeSpec(original, encode(preview), "linux/amd64"); err != nil {
		t.Fatal("official prerelease tag contract rejected", err)
	}
	for name, mutate := range map[string]func(*managedOpenCodeMetadata){
		"different-platform": func(m *managedOpenCodeMetadata) { m.Platform = "linux/arm64" },
		"different-upstream": func(m *managedOpenCodeMetadata) { m.UpstreamRevision = strings.Repeat("f", 40) },
		"wrong-digest":       func(m *managedOpenCodeMetadata) { m.SHA256 = "invalid" },
		"wrong-identity":     func(m *managedOpenCodeMetadata) { m.Revision = strings.Repeat("e", 64) },
		"foreign-publisher":  func(m *managedOpenCodeMetadata) { m.URL = strings.Replace(m.URL, "ivo-lopes/ivoai", "other/repo", 1) },
		"credentials-in-url": func(m *managedOpenCodeMetadata) {
			m.URL = strings.Replace(m.URL, "https://", "https://user:fixture@", 1)
		},
		"mutable-branch-url": func(m *managedOpenCodeMetadata) { m.URL = strings.Replace(m.URL, "v0.10.1", "main", 1) },
		"wrong-archive":      func(m *managedOpenCodeMetadata) { m.Archive = "other.tar.gz" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := metadata
			mutate(&changed)
			if _, err := managedOpenCodeSpec(original, encode(changed), "linux/amd64"); err == nil {
				t.Fatal("invalid release metadata accepted")
			}
		})
	}
}
