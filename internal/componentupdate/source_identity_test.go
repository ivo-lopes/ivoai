package componentupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

// Mirrors the reviewed managed Graft component contract, without importing
// components (which itself depends on this package) or executing a runtime.
func identityGraftSource(arch, tag string, archive []byte) supplychain.ResolvedSource {
	digest := sha256.Sum256(archive)
	archiveSHA := hex.EncodeToString(digest[:])
	revision := sha256.Sum256([]byte("d39598390324ad0076a88ab593b63ad2ba352370ea07c073e31f11327979f8e1\n" + strings.Repeat("c", 64) + "\n" + archiveSHA + "\n"))
	return supplychain.ResolvedSource{
		ID: "graft", Kind: supplychain.KindComponent,
		Source:   "https://github.com/ivo-lopes/ivoai/releases/download/" + tag + "/ivoai-graft_linux_" + arch + ".tar.gz",
		Revision: hex.EncodeToString(revision[:]), LogicalVersion: "0.21.1-ivoai.1", DefaultBranch: "main",
		PayloadFormat: "tar_gzip", PayloadPath: "graft-runtime", License: "MIT", Executables: []string{"graft-runtime"},
		Integrity: supplychain.Integrity{Algorithm: "sha256", Digest: archiveSHA, SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "checksum_only"},
	}
}

func TestSameResolvedSourceGraftReleaseMirrors(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		for _, tag := range []string{"v0.16.0", "v0.16.0-rc.1", "v1.2.3-beta-2"} {
			t.Run(arch+"/"+tag, func(t *testing.T) {
				active := identityGraftSource(arch, "v0.15.0", []byte("same reviewed archive"))
				expected := identityGraftSource(arch, tag, []byte("same reviewed archive"))
				beforeA, _ := json.Marshal(active)
				beforeE, _ := json.Marshal(expected)
				if !SameResolvedSource(active, expected) || !SameResolvedSource(expected, active) || !SameResolvedSource(active, active) {
					t.Fatal("identical Graft artifact was not recognized across canonical release mirrors")
				}
				afterA, _ := json.Marshal(active)
				afterE, _ := json.Marshal(expected)
				if !bytes.Equal(beforeA, afterA) || !bytes.Equal(beforeE, afterE) {
					t.Fatal("source comparison mutated authoritative provenance")
				}
			})
		}
	}
}

func TestSameResolvedSourceRejectsNoncanonicalGraftMirrors(t *testing.T) {
	active := identityGraftSource("amd64", "v0.15.0", []byte("archive"))
	canonical := identityGraftSource("amd64", "v0.16.0", []byte("archive"))
	for name, url := range map[string]string{
		"publisher":          strings.Replace(canonical.Source, "ivo-lopes/ivoai", "other/ivoai", 1),
		"repository":         strings.Replace(canonical.Source, "ivo-lopes/ivoai", "ivo-lopes/other", 1),
		"publisher_case":     strings.Replace(canonical.Source, "ivo-lopes", "Ivo-Lopes", 1),
		"host":               strings.Replace(canonical.Source, "github.com", "github.com.example.invalid", 1),
		"host_case":          strings.Replace(canonical.Source, "github.com", "GitHub.com", 1),
		"protocol":           strings.Replace(canonical.Source, "https:", "http:", 1),
		"scheme_case":        strings.Replace(canonical.Source, "https:", "HTTPS:", 1),
		"port":               strings.Replace(canonical.Source, "github.com", "github.com:443", 1),
		"userinfo":           strings.Replace(canonical.Source, "https://", "https://user@", 1),
		"password":           strings.Replace(canonical.Source, "https://", "https://user:pass@", 1),
		"query":              canonical.Source + "?token=synthetic",
		"empty_query":        canonical.Source + "?",
		"fragment":           canonical.Source + "#synthetic",
		"empty_fragment":     canonical.Source + "#",
		"encoded_version":    strings.Replace(canonical.Source, "v0.16.0", "%760.16.0", 1),
		"encoded_path":       strings.Replace(canonical.Source, "graft_linux", "%67raft_linux", 1),
		"encoded_separator":  strings.Replace(canonical.Source, "ivo-lopes/ivoai", "ivo-lopes%2Fivoai", 1),
		"latest":             strings.Replace(canonical.Source, "v0.16.0", "latest", 1),
		"main":               strings.Replace(canonical.Source, "v0.16.0", "main", 1),
		"missing_v":          strings.Replace(canonical.Source, "v0.16.0", "0.16.0", 1),
		"incomplete_version": strings.Replace(canonical.Source, "v0.16.0", "v0.16", 1),
		"build_metadata":     strings.Replace(canonical.Source, "v0.16.0", "v0.16.0+build", 1),
		"empty_prerelease":   strings.Replace(canonical.Source, "v0.16.0", "v0.16.0-", 1),
		"architecture":       strings.Replace(canonical.Source, "amd64", "arm64", 1),
		"unknown_arch":       strings.Replace(canonical.Source, "amd64", "riscv64", 1),
		"payload":            strings.Replace(canonical.Source, "ivoai-graft_", "graft_", 1),
		"other_archive":      strings.Replace(canonical.Source, ".tar.gz", ".zip", 1),
		"trailing_slash":     canonical.Source + "/",
		"double_slash":       strings.Replace(canonical.Source, "/releases/", "//releases/", 1),
		"dot_segment":        strings.Replace(canonical.Source, "/releases/", "/./releases/", 1),
		"trailing_newline":   canonical.Source + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			changed := canonical
			changed.Source = url
			if SameResolvedSource(active, changed) || SameResolvedSource(changed, active) {
				t.Fatal("noncanonical source accepted as the same Graft artifact")
			}
		})
	}
}

func TestSameResolvedSourcePreservesEveryNonURLField(t *testing.T) {
	mutations := map[string]func(*supplychain.ResolvedSource){
		"ID":                          func(s *supplychain.ResolvedSource) { s.ID = "other" },
		"Kind":                        func(s *supplychain.ResolvedSource) { s.Kind = supplychain.KindHelper },
		"Revision":                    func(s *supplychain.ResolvedSource) { s.Revision = strings.Repeat("a", 64) },
		"LogicalVersion":              func(s *supplychain.ResolvedSource) { s.LogicalVersion = "0.22.0-ivoai.1" },
		"DefaultBranch":               func(s *supplychain.ResolvedSource) { s.DefaultBranch = "release" },
		"PayloadFormat":               func(s *supplychain.ResolvedSource) { s.PayloadFormat = "raw" },
		"PayloadPath":                 func(s *supplychain.ResolvedSource) { s.PayloadPath = "bin/graft-runtime" },
		"License":                     func(s *supplychain.ResolvedSource) { s.License = "GPL-3.0" },
		"Executables":                 func(s *supplychain.ResolvedSource) { s.Executables = []string{"graft-runtime", "other"} },
		"Integrity.Algorithm":         func(s *supplychain.ResolvedSource) { s.Integrity.Algorithm = "sha512" },
		"Integrity.Digest":            func(s *supplychain.ResolvedSource) { s.Integrity.Digest = strings.Repeat("b", 64) },
		"Integrity.SignatureStatus":   func(s *supplychain.ResolvedSource) { s.Integrity.SignatureStatus = "verified" },
		"Integrity.AttestationStatus": func(s *supplychain.ResolvedSource) { s.Integrity.AttestationStatus = "verified" },
		"Integrity.TrustLevel":        func(s *supplychain.ResolvedSource) { s.Integrity.TrustLevel = "independently_attested" },
	}
	// A new provenance field must receive a concrete adverse case as well.
	for _, typ := range []reflect.Type{reflect.TypeFor[supplychain.ResolvedSource](), reflect.TypeFor[supplychain.Integrity]()} {
		for index := 0; index < typ.NumField(); index++ {
			field := typ.Field(index).Name
			if typ == reflect.TypeFor[supplychain.Integrity]() {
				field = "Integrity." + field
			}
			if field != "Source" && field != "Integrity" && mutations[field] == nil {
				t.Fatalf("missing negative identity case for %s", field)
			}
		}
	}
	for field, mutate := range mutations {
		t.Run(field, func(t *testing.T) {
			active := identityGraftSource("amd64", "v0.15.0", []byte("archive"))
			expected := identityGraftSource("amd64", "v0.16.0", []byte("archive"))
			mutate(&expected)
			if SameResolvedSource(active, expected) || SameResolvedSource(expected, active) {
				t.Fatal("release-mirror comparison discarded non-URL identity metadata")
			}
		})
	}
}

func TestSameResolvedSourceExceptionCannotBeUsedForOtherContracts(t *testing.T) {
	for name, mutate := range map[string]func(*supplychain.ResolvedSource){
		"other_component": func(s *supplychain.ResolvedSource) { s.ID = "opencode" },
		"helper":          func(s *supplychain.ResolvedSource) { s.Kind = supplychain.KindHelper },
		"skill": func(s *supplychain.ResolvedSource) {
			s.Kind, s.PayloadPath, s.Executables = supplychain.KindSkill, "", nil
		},
		"raw":             func(s *supplychain.ResolvedSource) { s.PayloadFormat = "raw" },
		"implicit_format": func(s *supplychain.ResolvedSource) { s.PayloadFormat = "" },
		"other_payload":   func(s *supplychain.ResolvedSource) { s.PayloadPath = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			active := identityGraftSource("amd64", "v0.15.0", []byte("archive"))
			expected := identityGraftSource("amd64", "v0.16.0", []byte("archive"))
			mutate(&active)
			mutate(&expected)
			if !SameResolvedSource(active, active) {
				t.Fatal("strict equality regressed for a source outside the Graft exception")
			}
			if SameResolvedSource(active, expected) || SameResolvedSource(expected, active) {
				t.Fatal("Graft mirror exception widened to another component contract")
			}
		})
	}
	ordinary := componentSource(strings.Repeat("a", 40), "1.0.0", []byte("ordinary"))
	if !SameResolvedSource(ordinary, ordinary) {
		t.Fatal("ordinary exact source equality regressed")
	}
	changed := ordinary
	changed.Source += "?mirror=other"
	if SameResolvedSource(ordinary, changed) {
		t.Fatal("ordinary components gained URL normalization")
	}
}

func identityGraftArchive(t *testing.T) []byte {
	t.Helper()
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	payload := []byte("reviewed unit fixture Graft 0.21.1-ivoai.1\n")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "graft-runtime", Mode: 0700, Size: int64(len(payload)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func identityGraftManager(t *testing.T) (Manager, *fakeDiscoverer, *fakeFetcher, *fakeRunner, Result) {
	t.Helper()
	archive := identityGraftArchive(t)
	discoverer := &fakeDiscoverer{source: identityGraftSource("amd64", "v0.15.0", archive)}
	fetcher := &fakeFetcher{payloads: map[string][]byte{discoverer.source.Revision: archive}}
	runner := &fakeRunner{}
	manager := testComponentManager(t, t.TempDir(), discoverer, fetcher, runner)
	manager.Executable = "graft-runtime"
	first, err := manager.Update(context.Background(), supplychain.Reference{ID: "graft", Kind: supplychain.KindComponent, Source: discoverer.source.Source, Version: discoverer.source.LogicalVersion})
	if err != nil || !first.Changed || fetcher.calls != 1 {
		t.Fatalf("initial controlled Graft promotion failed: changed=%t fetches=%d err=%v", first.Changed, fetcher.calls, err)
	}
	discoverer.source.Source = strings.Replace(discoverer.source.Source, "v0.15.0", "v0.16.0", 1)
	return manager, discoverer, fetcher, runner, first
}

func identityObjectSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		if entry.IsDir() {
			result[relative] = fmt.Sprintf("directory:%04o", info.Mode().Perm())
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsafe fixture object entry")
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		result[relative] = fmt.Sprintf("%04o:%x", info.Mode().Perm(), sum)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestGraftMirrorUpdatePreservesObjectAndProvenanceWithoutFetch(t *testing.T) {
	manager, discoverer, fetcher, runner, first := identityGraftManager(t)
	root := filepath.Dir(first.State.Path)
	before := identityObjectSnapshot(t, root)
	probes := runner.runs
	result, err := manager.Update(context.Background(), supplychain.Reference{ID: "graft", Kind: supplychain.KindComponent, Source: discoverer.source.Source, Version: discoverer.source.LogicalVersion})
	if err != nil || result.Changed || fetcher.calls != 1 || runner.runs <= probes || result.State.Path != first.State.Path {
		t.Fatalf("mirror must reuse validated object and still probe health: changed=%t fetches=%d health=%d err=%v", result.Changed, fetcher.calls, runner.runs-probes, err)
	}
	active, actualRoot, err := manager.Supply.Active("graft")
	if err != nil || actualRoot != root || !strings.Contains(active.Source, "/v0.15.0/") || active.Source == discoverer.source.Source {
		t.Fatal("mirror update rewrote original provenance or changed object location")
	}
	if !reflect.DeepEqual(before, identityObjectSnapshot(t, root)) {
		t.Fatal("mirror update changed immutable object bytes, modes, manifest or provenance")
	}
}

func TestGraftMirrorDoesNotBypassObjectIntegrity(t *testing.T) {
	for _, target := range []string{"graft-runtime", ".ivoai-manifest.json", ".ivoai-provenance.json"} {
		t.Run(target, func(t *testing.T) {
			manager, discoverer, fetcher, _, first := identityGraftManager(t)
			path := filepath.Join(filepath.Dir(first.State.Path), target)
			if err := os.WriteFile(path, []byte("corrupt unit fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := manager.Update(context.Background(), supplychain.Reference{ID: "graft", Kind: supplychain.KindComponent, Source: discoverer.source.Source}); err == nil {
				t.Fatal("mirror comparison accepted a corrupt immutable object")
			}
			if fetcher.calls != 1 {
				t.Fatal("corrupt active object triggered automatic refetch/replacement")
			}
		})
	}
}

func TestGraftMirrorDoesNotBypassHealth(t *testing.T) {
	manager, discoverer, fetcher, runner, first := identityGraftManager(t)
	before := identityObjectSnapshot(t, filepath.Dir(first.State.Path))
	runner.fail = true
	if _, err := manager.Update(context.Background(), supplychain.Reference{ID: "graft", Kind: supplychain.KindComponent, Source: discoverer.source.Source}); err == nil {
		t.Fatal("mirror comparison bypassed failing component health")
	}
	if fetcher.calls != 1 || !reflect.DeepEqual(before, identityObjectSnapshot(t, filepath.Dir(first.State.Path))) {
		t.Fatal("failed mirror health altered or refetched the immutable object")
	}
}
