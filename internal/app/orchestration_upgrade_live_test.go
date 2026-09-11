package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/connections"
	"github.com/ivo-lopes/ivoai/internal/secrets"
)

// Runs both actual binaries against isolated configuration, never operator auth.
// The previous binary should be downloaded and checksum-verified by the caller.
func TestOrchestrationPublishedBinaryConfigUpgradeRollback(t *testing.T) {
	previous, candidate := os.Getenv("IVOAI_UPGRADE_PREVIOUS_BINARY"), os.Getenv("IVOAI_UPGRADE_TARGET_BINARY")
	if previous == "" || candidate == "" {
		t.Skip("provide verified previous and target artifact paths")
	}
	root := t.TempDir()
	for _, kind := range []string{"CONFIG", "DATA", "STATE", "CACHE"} {
		t.Setenv("XDG_"+kind+"_HOME", filepath.Join(root, kind))
	}
	run := func(binary string, args ...string) string {
		t.Helper()
		cmd := exec.Command(binary, args...)
		body, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("artifact command %s failed: %v", args[0], err)
		}
		return string(body)
	}
	run(previous, "config", "set", "opencode.permission_mode", "full")
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	store := config.NewStore(paths)
	cfg, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"company-a", "company-b"} {
		id := "srv_fixture_" + alias
		cfg.Connections.Servers[alias] = config.ServerProfile{ID: id, Alias: alias, Purpose: alias, URL: "https://" + alias + ".example.invalid", Enabled: true, Status: "connected"}
		if err := (secrets.Store{Path: paths.Secrets}).Set(id, secrets.ClientCredential{Token: "fixture-only-" + alias}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	registry := connections.Registry{Store: store}
	if err := registry.Add("fixture-plane", config.MCPServer{URL: "https://mcp.example.invalid/mcp", Enabled: true, Kind: "external"}); err != nil {
		t.Fatal(err)
	}
	if err := registry.SetBearer("fixture-plane", "fixture-only-mcp-credential"); err != nil {
		t.Fatal(err)
	}
	before, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	secretBefore, err := os.ReadFile(paths.Secrets)
	if err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		current, err := store.Load()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(current.Connections.Servers, before.Connections.Servers) || !reflect.DeepEqual(current.MCP, before.MCP) || current.OpenCode.PermissionMode != "full" {
			t.Fatal("profile/MCP/permission compatibility changed")
		}
		secretAfter, err := os.ReadFile(paths.Secrets)
		if err != nil || !bytes.Equal(secretBefore, secretAfter) {
			t.Fatal("credential ownership changed")
		}
		info, err := os.Stat(paths.Secrets)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("secret store permissions changed")
		}
	}
	run(candidate, "config", "set", "orchestration.auto.plan_execution", "immediate")
	run(candidate, "config", "set", "orchestration.auto.worker_cap", "2")
	run(candidate, "skills", "update")
	run(candidate, "config", "set", "skills.ponytail", "off")
	run(candidate, "skills", "pin", "ponytail")
	check()
	// Rollback must still read and update familiar settings while preserving
	// unknown new policy keys for a later reapply.
	run(previous, "config", "set", "headroom.enabled", "false")
	check()
	run(candidate, "config", "set", "orchestration.auto.knowledge_routing", "purpose-auto")
	run(candidate, "skills", "update")
	run(candidate, "skills", "doctor")
	check()
	current, _ := store.Load()
	if current.Orchestration.Auto.ResolvedPlanExecution() != "immediate" || current.Orchestration.Auto.WorkerCap != 2 {
		t.Fatal("rollback/reapply dropped new policy")
	}
	if current.Skills.ResolvedPonytail() != "off" || !current.Skills.Sources["ponytail"].Pinned {
		t.Fatal("rollback/reapply dropped native capability policy")
	}
	t.Log("CONFIG_UPGRADE=PASS CONFIG_ROLLBACK_REAPPLY=PASS PROFILES=2 SECRET_ISOLATION=PASS PERMISSION_FULL_PERSISTED=true")
}
