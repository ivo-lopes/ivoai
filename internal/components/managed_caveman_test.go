package components

import (
	"crypto/sha256"
	"encoding/hex"
	"runtime"
	"strings"
	"testing"
)

func cavemanMetadata(t *testing.T, platform string, body []byte) map[string]string {
	t.Helper()
	sum := sha256.Sum256(body)
	value := map[string]string{"version": managedCavemanVersion, "platform": platform, "sha256": hex.EncodeToString(sum[:]), "upstream_revision": managedCavemanUpstream, "source_sha256": managedCavemanSource, "patch_sha256": managedCavemanPatch, "archive": "ivoai-caveman_" + strings.ReplaceAll(platform, "/", "_") + ".tar.gz"}
	value["url"] = "https://github.com/ivo-lopes/ivoai/releases/download/v0.17.0/" + value["archive"]
	identity := sha256.Sum256([]byte(value["source_sha256"] + "\n" + value["patch_sha256"] + "\n" + value["sha256"] + "\n"))
	value["revision"] = hex.EncodeToString(identity[:])
	return value
}

func TestManagedCavemanReviewedBuildIdentity(t *testing.T) {
	for _, platform := range []string{"linux/amd64", "linux/arm64"} {
		specs, err := managedCavemanSpecs(encodedGraft(t, cavemanMetadata(t, platform, []byte("payload"))), platform)
		if err != nil || len(specs) != 2 {
			t.Fatalf("%s: specs=%+v err=%v", platform, specs, err)
		}
		for i, spec := range specs {
			if spec.Version != managedCavemanVersion || spec.PayloadPath != spec.Executable || spec.Strategy != StrategySupplyChain || spec.PayloadFormat != "tar_gzip" || !spec.NoVersionProbe || spec.License != "Apache-2.0" || spec.TrustLevel != "checksum_only" || spec.SignatureStatus != "not_exposed" || spec.AttestationStatus != "not_exposed" {
				t.Fatalf("invalid reviewed contract: %+v", spec)
			}
			if i == 1 && spec.RequiresManaged != "caveman" {
				t.Fatal("MCP cannot be installed independently of managed Caveman")
			}
		}
	}
	for name, mutate := range map[string]func(map[string]string){
		"platform":  func(m map[string]string) { m["platform"] = "linux/arm64" },
		"version":   func(m map[string]string) { m["version"] = "4.0.2" },
		"upstream":  func(m map[string]string) { m["upstream_revision"] = strings.Repeat("a", 40) },
		"source":    func(m map[string]string) { m["source_sha256"] = strings.Repeat("a", 64) },
		"patch":     func(m map[string]string) { m["patch_sha256"] = strings.Repeat("a", 64) },
		"archive":   func(m map[string]string) { m["archive"] = "caveman.tar.gz" },
		"digest":    func(m map[string]string) { m["sha256"] = strings.Repeat("a", 64) },
		"identity":  func(m map[string]string) { m["revision"] = strings.Repeat("d", 64) },
		"publisher": func(m map[string]string) { m["url"] = strings.Replace(m["url"], "ivo-lopes/ivoai", "attacker/repo", 1) },
		"userinfo":  func(m map[string]string) { m["url"] = strings.Replace(m["url"], "https://", "https://secret@", 1) },
		"mutable":   func(m map[string]string) { m["url"] = strings.Replace(m["url"], "v0.17.0", "main", 1) },
		"query":     func(m map[string]string) { m["url"] += "?token=private" },
		"fragment":  func(m map[string]string) { m["url"] += "#anything" },
	} {
		t.Run(name, func(t *testing.T) {
			m := cavemanMetadata(t, "linux/amd64", []byte("payload"))
			mutate(m)
			if _, err := managedCavemanSpecs(encodedGraft(t, m), "linux/amd64"); err == nil {
				t.Fatal("unreviewed metadata accepted")
			}
		})
	}
	for _, encoded := range []string{"bad", strings.Repeat("a", 8193)} {
		if _, err := managedCavemanSpecs(encoded, "linux/amd64"); err == nil {
			t.Fatal("malformed metadata accepted")
		}
	}
}

func TestManagedCavemanRequiresReleaseMetadata(t *testing.T) {
	old := managedCavemanBuild
	managedCavemanBuild = ""
	t.Cleanup(func() { managedCavemanBuild = old })
	for _, spec := range DefaultCatalog() {
		if spec.Name == "caveman" || spec.Name == "caveman-mcp" {
			t.Fatal("development build adopted an upstream Caveman binary")
		}
	}
	managedCavemanBuild = encodedGraft(t, cavemanMetadata(t, "linux/"+runtime.GOARCH, []byte("payload")))
	if !ManagedCavemanAvailable() {
		t.Fatal("compiled reviewed build unavailable")
	}
	count := 0
	for _, spec := range DefaultCatalog() {
		if spec.Name == "caveman" || spec.Name == "caveman-mcp" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("compiled build projected %d programs", count)
	}
}
