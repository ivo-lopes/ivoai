package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/components"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

// TestLiveGraftManagedOpenCode is explicitly provider-consuming. It requires
// IVOAI_GRAFT_NATIVE_LIVE=1 and absolute IVOAI_GRAFT_SMOKE_{BINARY,ARCHIVE,METADATA,
// NODE,CODEX,OPENCODE} paths. The test binary must receive the same reviewed
// components.managedGraftBuild ldflag as the candidate. Codex owns its existing
// authentication; credentials and personal configuration are never copied.
func TestLiveGraftManagedOpenCode(t *testing.T) {
	if os.Getenv("IVOAI_GRAFT_NATIVE_LIVE") != "1" {
		t.Skip("explicit opt-in required for real managed frontend/provider smoke")
	}
	input := map[string]string{}
	for _, key := range []string{"BINARY", "ARCHIVE", "METADATA", "NODE", "CODEX", "OPENCODE"} {
		path := os.Getenv("IVOAI_GRAFT_SMOKE_" + key)
		if !filepath.IsAbs(path) {
			t.Fatalf("IVOAI_GRAFT_SMOKE_%s must be an absolute reviewed artifact path", key)
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("missing %s artifact", key)
		}
		input[key] = path
	}
	var spec *components.Spec
	for _, candidate := range components.DefaultCatalog() {
		if candidate.Name == "graft" {
			v := candidate
			spec = &v
			break
		}
	}
	if spec == nil {
		t.Fatal("go test requires the same managedGraftBuild ldflag as the candidate")
	}
	source, err := components.ResolvedSource(*spec)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(input["METADATA"])
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]string
	if json.Unmarshal(body, &metadata) != nil || metadata["revision"] != source.Revision || metadata["sha256"] != source.Integrity.Digest || metadata["version"] != source.LogicalVersion || metadata["url"] != source.Source {
		t.Fatal("test catalog differs from reviewed artifact metadata")
	}
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		home = filepath.Join(userHome, ".codex")
	}
	personal := filepath.Join(home, "config.toml")
	before, beforeErr := os.ReadFile(personal)
	if beforeErr != nil && !os.IsNotExist(beforeErr) {
		t.Fatal("cannot guard personal Codex configuration")
	}
	fingerprint := sha256.Sum256(before)
	t.Cleanup(func() {
		after, afterErr := os.ReadFile(personal)
		if (beforeErr == nil) != (afterErr == nil) || sha256.Sum256(after) != fingerprint {
			t.Error("PERSONAL_CODEX_CONFIG_MUTATIONS detected")
		} else {
			t.Log("PERSONAL_CODEX_CONFIG_MUTATIONS=0")
		}
	})
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(filepath.Dir(input["CODEX"]), "codex-code-mode-host")
	if info, err := os.Stat(host); err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		t.Fatal("official Codex artifact requires its matching code-mode-host companion")
	}
	root := t.TempDir()
	for _, kind := range []string{"CONFIG", "DATA", "STATE", "CACHE"} {
		t.Setenv("XDG_"+kind+"_HOME", filepath.Join(root, kind))
	}
	paths, err = config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	store := config.NewStore(paths)
	cfg := config.Default()
	cfg.OpenCode.PermissionMode = "full"
	cfg.Compression.Provider = "direct"
	cfg.Headroom.Enabled = false
	cfg.Orchestration.Auto.ProviderPreference = "codex"
	cfg.Orchestration.Auto.PlanExecution = "approve"
	cfg.Orchestration.Auto.AutomaticFailover = false
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	// StageArchive still verifies the reviewed archive checksum and inventories
	// every file. Validators do not execute payload in this test-only fixture;
	// the actual candidate provider/sandbox invocation below is the health proof.
	passive := supplychain.ValidatorFunc(func(context.Context, supplychain.ResolvedSource, string) error { return nil })
	manager := supplychain.Manager{Root: filepath.Join(paths.DataDir, "supply-chain"), Structural: passive, Policy: passive, Health: passive}
	archive, err := os.Open(input["ARCHIVE"])
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	staged, err := manager.StageArchive(context.Background(), source, archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Promote(staged); err != nil {
		t.Fatal(err)
	}
	_, distribution, err := manager.Active("graft")
	if err != nil {
		t.Fatal(err)
	}
	node := components.ManagedNodePath(paths.DataDir)
	if err := os.MkdirAll(filepath.Dir(node), 0700); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(input["NODE"])
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(node, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatal("private Node copy failed")
	}
	state := config.State{Schema: config.StateSchemaVersion, Components: map[string]config.ComponentState{
		"graft":                {Installed: true, Managed: true, Version: spec.Version, Path: filepath.Join(distribution, spec.PayloadPath)},
		"codex":                {Installed: true, Managed: true, Version: "0.162.1", Path: input["CODEX"]},
		"opencode":             {Installed: true, Managed: true, Version: "1.19.0-ivoai.1", Path: input["OPENCODE"]},
		"codex-code-mode-host": {Installed: true, Managed: true, Version: "0.162.1", Path: host},
	}}
	if err := store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	pack := exec.CommandContext(ctx, input["BINARY"], "skills", "update")
	if err := pack.Run(); err != nil {
		t.Fatal("candidate native skill pack materialization failed")
	}
	command := exec.CommandContext(ctx, "python3", "../../scripts/smoke-graft-native.py", input["BINARY"], root)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		t.Fatal("real Graft/OpenCode fixture failed", err)
	}
}
