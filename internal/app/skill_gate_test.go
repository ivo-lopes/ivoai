package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/skills"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

func TestManagedDirectLaunchesApplySkillGateToAllDirectExecutors(t *testing.T) {
	for _, executor := range []string{"codex", "claude", "opencode"} {
		t.Run(executor, func(t *testing.T) {
			root := t.TempDir()
			arguments := filepath.Join(root, executor+"-args")
			codex := appExecutable(t, root, "codex", "#!/bin/sh\nprintf '%s\\n' \"$@\" > '"+arguments+"'\n")
			claude := appExecutable(t, root, "claude", "#!/bin/sh\nprintf '%s\\n' \"$@\" > '"+arguments+"'\n")
			opencode := appExecutable(t, root, "opencode", "#!/bin/sh\npath=$(printf '%s' \"$OPENCODE_CONFIG_CONTENT\" | sed 's/.*\\[\"\\([^\"]*\\)\"\\].*/\\1/')\ncp \"$path\" '"+arguments+"'\n")
			a := sessionTestApp(t, root, codex, claude, appExecutable(t, root, "ruflo", "#!/bin/sh\nexit 0\n"))
			state, _ := a.Store.LoadState()
			state.Components["opencode"] = config.ComponentState{Installed: true, Path: opencode, Version: "fixture"}
			if err := a.Store.SaveState(state); err != nil {
				t.Fatal(err)
			}
			installAppTestSkill(t, a, filepath.Base(root), "VALIDATED SKILL BODY")
			previous, _ := os.Getwd()
			t.Cleanup(func() { _ = os.Chdir(previous) })
			if err := os.Chdir(root); err != nil {
				t.Fatal(err)
			}
			if err := a.Launch(context.Background(), executor, nil); err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(arguments)
			if err != nil || !strings.Contains(string(body), "VALIDATED SKILL BODY") || !strings.Contains(string(body), "IVOAI policy") {
				t.Fatalf("%s args=%q err=%v", executor, body, err)
			}
			if executor == "codex" && !strings.Contains(string(body), "developer_instructions=") {
				t.Fatalf("Codex did not use its official instruction channel: %q", body)
			}
			if executor == "claude" && !strings.Contains(string(body), "--append-system-prompt") {
				t.Fatalf("Claude did not use its official instruction channel: %q", body)
			}
			if executor == "opencode" && strings.Contains(string(body), "--append-system-prompt") {
				t.Fatalf("OpenCode received a foreign instruction flag: %q", body)
			}
		})
	}
}

func TestAutomaticSessionAppliesLocalSkillGateWithoutChangingQuotaRouting(t *testing.T) {
	root := t.TempDir()
	arguments := filepath.Join(root, "codex-args")
	a := autoTestApp(t, root, "#!/bin/sh\nprintf '%s\\n' \"$@\" > '"+arguments+"'\n", "#!/bin/sh\nexit 0\n")
	installAppTestSkill(t, a, filepath.Base(root), "AUTO VALIDATED SKILL")
	a.QuotaManager = &quota.Manager{Store: quota.Store{Root: a.Store.Paths.QuotaDir}, Probes: map[quota.Provider]quota.Probe{
		quota.ProviderCodex:  probeFunc(func(context.Context) (quota.ProviderQuota, error) { return available(quota.ProviderCodex), nil }),
		quota.ProviderClaude: probeFunc(func(context.Context) (quota.ProviderQuota, error) { return available(quota.ProviderClaude), nil }),
	}}
	if err := a.Auto(context.Background(), "codex", nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(arguments)
	if err != nil || !strings.Contains(string(body), "AUTO VALIDATED SKILL") || !strings.Contains(string(body), "developer_instructions=") {
		t.Fatalf("automatic args=%q err=%v", body, err)
	}
	values, err := a.SessionList()
	if err != nil || len(values) != 1 || values[0].PrimaryExecutor != "codex" {
		t.Fatalf("quota routing changed: %+v err=%v", values, err)
	}
	foundGate := false
	for _, event := range values[0].Observability {
		foundGate = foundGate || event.Operation == "skill.gate"
	}
	if !foundGate {
		t.Fatal("automatic session did not persist bounded Skill Gate observability")
	}
}

func installAppTestSkill(t *testing.T, a *App, trigger, content string) {
	t.Helper()
	archive := appSkillArchive(t, "skills/demo/SKILL.md", content)
	sum := sha256.Sum256(archive)
	revision := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digest := hex.EncodeToString(sum[:])
	source := supplychain.ResolvedSource{ID: "test-pack", Kind: supplychain.KindSkill, Source: "https://example.com/test-pack", Revision: revision, LogicalVersion: revision, DefaultBranch: "develop", Integrity: supplychain.Integrity{Algorithm: "sha256", Digest: digest, SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "commit_pinned_local_digest"}}
	accept := supplychain.ValidatorFunc(func(context.Context, supplychain.ResolvedSource, string) error { return nil })
	manager := supplychain.Manager{Root: filepath.Join(a.Store.Paths.DataDir, "supply-chain"), Structural: accept, Policy: accept, Health: accept}
	pipeline := supplychain.Pipeline{Manager: manager, Discoverer: appSkillDiscoverer{source}, Fetcher: appSkillFetcher{archive}}
	staged, err := pipeline.Prepare(context.Background(), supplychain.Reference{ID: source.ID, Kind: source.Kind, Source: source.Source})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Promote(staged); err != nil {
		t.Fatal(err)
	}
	entry := skills.Entry{ID: "demo", ArtifactID: source.ID, Name: "demo", Description: "Synthetic managed skill", Domain: "testing", Triggers: []string{trigger}, Phase: skills.PhaseImplementation, Role: "helper", RoleMode: skills.RoleComposable, Risk: skills.RiskLow, Lifecycle: skills.LifecycleActive, Compatibility: skills.Compatibility{Executors: []string{"codex", "claude", "opencode"}}, Provenance: skills.Provenance{Source: skills.Source{Kind: "git", URL: source.Source, Repository: source.Source, Path: "skills/demo/SKILL.md", DefaultBranch: source.DefaultBranch}, Revision: skills.Revision{Commit: revision, LogicalVersion: revision}, Integrity: skills.Integrity{Algorithm: "sha256", Digest: digest, Verified: true, SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "commit_pinned_local_digest"}}}
	if err := (skills.Store{Path: skills.RegistryPath(a.Store.Paths.StateDir)}).Save(skills.Registry{Schema: skills.RegistrySchemaVersion, Entries: []skills.Entry{entry}}); err != nil {
		t.Fatal(err)
	}
}

type appSkillDiscoverer struct{ source supplychain.ResolvedSource }

func (d appSkillDiscoverer) Resolve(context.Context, supplychain.Reference) (supplychain.ResolvedSource, error) {
	return d.source, nil
}

type appSkillFetcher struct{ archive []byte }

func (f appSkillFetcher) Fetch(context.Context, supplychain.ResolvedSource) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.archive)), nil
}

func appSkillArchive(t *testing.T, path, content string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: path, Mode: 0o600, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
