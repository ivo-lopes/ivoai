package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/migration"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
	"github.com/ivo-lopes/ivoai/internal/update"
)

func TestTransactionalUpdateCommitRollbackAndUpdateAgain(t *testing.T) {
	a, checker, executable := transactionUpdateFixture(t, false)
	original, _ := os.ReadFile(executable)
	if err := a.transactionalUpdate(context.Background(), checker); err != nil {
		t.Fatal(err)
	}
	updated, _ := os.ReadFile(executable)
	if bytes.Equal(updated, original) || !bytes.Contains(updated, []byte("0.6.0")) {
		t.Fatal("candidate binary was not promoted")
	}
	if err := a.transactionalRollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(executable)
	if !bytes.Equal(restored, original) {
		t.Fatal("rollback did not restore v0.5.0 executable")
	}
	if err := a.transactionalRollback(context.Background()); err != nil {
		t.Fatalf("repeated rollback failed: %v", err)
	}
	if err := a.transactionalUpdate(context.Background(), checker); err != nil {
		t.Fatalf("update after rollback failed: %v", err)
	}
}

func TestTransactionalUpdateAutoRollsBackFailedDoctor(t *testing.T) {
	a, checker, executable := transactionUpdateFixture(t, true)
	original, _ := os.ReadFile(executable)
	originalConfig, _ := os.ReadFile(a.Store.Paths.Config)
	err := a.transactionalUpdate(context.Background(), checker)
	if err == nil || !strings.Contains(err.Error(), "previous binary and managed state were restored") {
		t.Fatalf("failed doctor did not trigger explicit rollback: %v", err)
	}
	restored, _ := os.ReadFile(executable)
	restoredConfig, _ := os.ReadFile(a.Store.Paths.Config)
	if !bytes.Equal(restored, original) || !bytes.Equal(restoredConfig, originalConfig) {
		t.Fatal("automatic rollback did not restore the pre-update installation")
	}
}

func TestTransactionalUpdateRejectsIncompatibleCandidateBeforeSnapshot(t *testing.T) {
	a, checker, executable := transactionUpdateFixture(t, false)
	candidate := candidateScript("0.6.0", false)
	candidate = bytes.Replace(candidate, []byte(`"rollback_safe":true`), []byte(`"rollback_safe":false`), 1)
	checker = checkerForCandidate(t, candidate)
	original, _ := os.ReadFile(executable)
	if err := a.transactionalUpdate(context.Background(), checker); err == nil || !strings.Contains(err.Error(), "rollback-safe") {
		t.Fatalf("incompatible candidate accepted: %v", err)
	}
	after, _ := os.ReadFile(executable)
	if !bytes.Equal(after, original) {
		t.Fatal("incompatible candidate changed the executable")
	}
	if _, err := os.Stat(filepath.Join(a.Store.Paths.StateDir, "updates", "current.json")); !os.IsNotExist(err) {
		t.Fatalf("snapshot was created before compatibility preflight: %v", err)
	}
}

func TestTransactionalUpdateRejectsCandidateMetadataWithTrailingData(t *testing.T) {
	a, checker, _ := transactionUpdateFixture(t, false)
	candidate := bytes.Replace(candidateScript("0.6.0", false), []byte("}' ;;\n  _update-migrate"), []byte("} trailing' ;;\n  _update-migrate"), 1)
	checker = checkerForCandidate(t, candidate)
	if err := a.transactionalUpdate(context.Background(), checker); err == nil || !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("candidate metadata with trailing data was accepted: %v", err)
	}
}

func TestTransactionalUpdateDryRunDoesNotChangeManagedFiles(t *testing.T) {
	a, checker, executable := transactionUpdateFixture(t, false)
	originalBinary, _ := os.ReadFile(executable)
	originalConfig, _ := os.ReadFile(a.Store.Paths.Config)
	if err := a.transactionalUpdateDryRun(context.Background(), checker); err != nil {
		t.Fatal(err)
	}
	afterBinary, _ := os.ReadFile(executable)
	afterConfig, _ := os.ReadFile(a.Store.Paths.Config)
	if !bytes.Equal(originalBinary, afterBinary) || !bytes.Equal(originalConfig, afterConfig) {
		t.Fatal("dry-run changed managed files")
	}
	if _, err := os.Stat(filepath.Join(a.Store.Paths.StateDir, "updates", "current.json")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created a transaction journal: %v", err)
	}
}

func TestTransactionalUpdateDryRunReportsInterruptedJournalBeforeNetwork(t *testing.T) {
	a, _, executable := transactionUpdateFixture(t, false)
	updateCtx, err := a.resolveUpdateContext(executable)
	if err != nil {
		t.Fatal(err)
	}
	manager := migration.Manager{Root: updateCtx.root, Files: updateCtx.files, AllowedRoots: updateCtx.allowedRoots, Registry: updateMigrationRegistry()}
	tx, err := manager.Begin(context.Background(), "v0.5.0", "v0.6.0", updateCtx.schemas, updateCtx.schemas)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(updateCtx.root, "current.json")
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	journal = bytes.Replace(journal, []byte(`"state": "rolled_back"`), []byte(`"state": "promoted"`), 1)
	if err := os.WriteFile(journalPath, journal, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.transactionalUpdateDryRun(context.Background(), update.Checker{}); !errors.Is(err, migration.ErrRecoveryRequired) {
		t.Fatalf("dry-run did not report interrupted update before network access: %v", err)
	}
}

func TestServerUpdateUsesServerSetupAndDoctor(t *testing.T) {
	a, checker, executable := transactionUpdateFixture(t, false)
	serverRoot := filepath.Join(t.TempDir(), "server-root")
	t.Setenv("IVOAI_SERVER_ROOT", serverRoot)
	for _, dir := range []string{filepath.Join(serverRoot, "etc", "ivoai"), filepath.Join(serverRoot, "var", "lib", "ivoai"), filepath.Join(serverRoot, "etc", "systemd", "system")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(serverRoot, "etc", "ivoai", "server.toml"), []byte("protocol_version=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "calls.log")
	t.Setenv("IVOAI_UPDATE_TEST_LOG", logPath)
	checker = checkerForCandidate(t, candidateScriptWithLog("0.6.0", logPath))
	if err := a.transactionalUpdate(context.Background(), checker); err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(logPath)
	if !strings.Contains(string(calls), "setup --mode server") || !strings.Contains(string(calls), "server doctor") {
		t.Fatalf("server update used the wrong post-promotion commands:\n%s", calls)
	}
	if _, err := os.Stat(filepath.Join(serverRoot, "var", "lib", "ivoai", "updates", "current.json")); err != nil {
		t.Fatalf("server transaction journal is not in server-owned state: %v", err)
	}
	_ = executable
}

func TestServerRollbackConsumesV050ClientRootBinaryBridge(t *testing.T) {
	a, _, executable := transactionUpdateFixture(t, false)
	if err := os.WriteFile(executable, candidateScript("0.6.0", false), 0o755); err != nil {
		t.Fatal(err)
	}
	serverRoot := filepath.Join(t.TempDir(), "server-root")
	t.Setenv("IVOAI_SERVER_ROOT", serverRoot)
	for _, dir := range []string{filepath.Join(serverRoot, "etc", "ivoai"), filepath.Join(serverRoot, "var", "lib", "ivoai"), filepath.Join(serverRoot, "etc", "systemd", "system")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(serverRoot, "etc", "ivoai", "server.toml"), []byte("protocol_version=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacyRollback := filepath.Join(a.Store.Paths.StateDir, "updates", "ivoai.previous")
	if err := os.MkdirAll(filepath.Dir(legacyRollback), 0o700); err != nil {
		t.Fatal(err)
	}
	old := []byte("#!/bin/sh\ncase \"$1\" in version) echo 0.5.0;; *) exit 0;; esac\n")
	if err := os.WriteFile(legacyRollback, old, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := a.transactionalRollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(executable)
	if err != nil || !bytes.Equal(restored, old) {
		t.Fatalf("server legacy bridge did not restore v0.5.0: err=%v", err)
	}
}

func TestServerUpdateContextDoesNotCreateClientXDGState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg-config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "xdg-data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "xdg-state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "xdg-cache"))
	t.Setenv("IVOAI_TEST_MODE", "1")
	serverRoot := filepath.Join(root, "server-root")
	t.Setenv("IVOAI_SERVER_ROOT", serverRoot)
	for _, dir := range []string{filepath.Join(serverRoot, "etc", "ivoai"), filepath.Join(serverRoot, "var", "lib", "ivoai"), filepath.Join(serverRoot, "etc", "systemd", "system"), filepath.Join(root, "bin")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(serverRoot, "etc", "ivoai", "server.toml"), []byte("protocol_version=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "bin", "ivoai")
	if err := os.WriteFile(executable, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	a, err := New("dev", strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	a.ExecutablePath = executable
	ctx, err := a.resolveUpdateContext(executable)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.mode != "server" {
		t.Fatalf("mode=%q", ctx.mode)
	}
	if _, err := os.Stat(a.Store.Paths.ConfigDir); !os.IsNotExist(err) {
		t.Fatalf("server-only resolution created client XDG state: %v", err)
	}
}

func TestUpdateContextSnapshotsOnlyIVOAIManagedComponents(t *testing.T) {
	a, _, executable := transactionUpdateFixture(t, false)
	managed := filepath.Join(a.Store.Paths.DataDir, "bin", "managed")
	external := filepath.Join(t.TempDir(), "external")
	if err := os.MkdirAll(filepath.Dir(managed), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{managed, external} {
		if err := os.WriteFile(path, []byte("component"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	state := config.State{Schema: 1, Components: map[string]config.ComponentState{
		"managed":  {Installed: true, Managed: true, Path: managed},
		"external": {Installed: true, Managed: false, Path: external},
		"mismatch": {Installed: true, Managed: true, Path: managed + "-other"},
	}}
	ownership := config.Ownership{Schema: 1, Components: map[string]config.OwnedItem{
		"managed":  {Managed: true, Path: managed},
		"external": {Managed: false, Path: external},
		"mismatch": {Managed: true, Path: managed},
	}}
	if err := a.Store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.SaveOwnership(ownership); err != nil {
		t.Fatal(err)
	}
	if _, err := a.resolveUpdateContext(executable); err == nil || !strings.Contains(err.Error(), "inconsistent") {
		t.Fatal("inconsistent managed component metadata was accepted")
	}
	delete(state.Components, "mismatch")
	delete(ownership.Components, "mismatch")
	if err := a.Store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.SaveOwnership(ownership); err != nil {
		t.Fatal(err)
	}
	ctx, err := a.resolveUpdateContext(executable)
	if err != nil {
		t.Fatal(err)
	}
	var componentPaths []string
	for _, spec := range ctx.files {
		if spec.Artifact == "components" {
			componentPaths = append(componentPaths, spec.Path)
		}
	}
	if len(componentPaths) != 1 || componentPaths[0] != managed {
		t.Fatalf("component snapshots=%v", componentPaths)
	}
}

func TestUpdateContextIncludesIndependentSkillRegistryParticipant(t *testing.T) {
	a, _, executable := transactionUpdateFixture(t, false)
	ctx, err := a.resolveUpdateContext(executable)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(a.Store.Paths.StateDir, "skills", "registry.json")
	for _, spec := range ctx.files {
		if spec.Artifact == migration.ArtifactSkillRegistry {
			if spec.Name != "skill-registry" || spec.Path != want || !spec.Optional || spec.Root != a.Store.Paths.StateDir {
				t.Fatalf("registry participant=%+v", spec)
			}
			return
		}
	}
	t.Fatal("skill registry is missing from transactional update snapshots")
}

func TestUpdateContextIncludesSupplyChainActivePointersOnly(t *testing.T) {
	a, _, executable := transactionUpdateFixture(t, false)
	root := filepath.Join(a.Store.Paths.DataDir, "supply-chain")
	manager := supplychain.Manager{Root: root}
	if err := os.MkdirAll(filepath.Join(root, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	pointer := []byte(`{"schema":1,"id":"component","active":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","updated_at":"2026-08-27T00:00:00Z"}`)
	if err := os.WriteFile(filepath.Join(root, "state", "component.json"), pointer, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := supplychain.ListPointers(manager.Root); err != nil {
		t.Fatal(err)
	}
	ctx, err := a.resolveUpdateContext(executable)
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range ctx.files {
		if spec.Artifact == migration.ArtifactSupplyChain {
			if spec.Path != filepath.Join(root, "state", "component.json") || spec.Root != a.Store.Paths.DataDir {
				t.Fatalf("supply-chain participant=%+v", spec)
			}
			return
		}
	}
	t.Fatal("supply-chain pointer missing from update snapshot")
}

func TestUpdateContextInspectsOlderOrNewerSourceSchemaBeforeMigration(t *testing.T) {
	a, _, executable := transactionUpdateFixture(t, false)
	for _, path := range []string{a.Store.Paths.State, a.Store.Paths.Ownership} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data = bytes.Replace(data, []byte("schema = 1"), []byte("schema = 2"), 1)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	updateCtx, err := a.resolveUpdateContext(executable)
	if err != nil {
		t.Fatalf("source schema was rejected before the migration registry could run: %v", err)
	}
	if updateCtx.schemas[migration.ArtifactState] != 2 || updateCtx.schemas[migration.ArtifactOwnership] != 2 {
		t.Fatalf("source schemas were not inspected from raw envelopes: %v", updateCtx.schemas)
	}
}

func transactionUpdateFixture(t *testing.T, failDoctor bool) (*App, update.Checker, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("IVOAI_TEST_MODE", "1")
	executable := filepath.Join(root, "bin", "ivoai")
	if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
		t.Fatal(err)
	}
	old := []byte("#!/bin/sh\ncase \"$1\" in version) echo 0.5.0;; doctor) exit 0;; server) exit 0;; *) exit 0;; esac\n")
	if err := os.WriteFile(executable, old, 0o755); err != nil {
		t.Fatal(err)
	}
	a, err := New("0.5.0", strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	a.ExecutablePath = executable
	if err := a.Store.Ensure(); err != nil {
		t.Fatal(err)
	}
	configValue := config.Default()
	if err := a.Store.Save(configValue); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.SaveState(config.State{Schema: config.StateSchemaVersion, Components: map[string]config.ComponentState{}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.SaveOwnership(config.Ownership{Schema: config.OwnershipSchemaVersion, Components: map[string]config.OwnedItem{}}); err != nil {
		t.Fatal(err)
	}
	return a, checkerForCandidate(t, candidateScript("0.6.0", failDoctor)), executable
}

func checkerForCandidate(t *testing.T, candidate []byte) update.Checker {
	t.Helper()
	archive := candidateArchive(t, candidate)
	sum := sha256.Sum256(archive)
	asset := "ivoai_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch filepath.Base(r.URL.Path) {
		case "latest":
			fmt.Fprint(w, `{"tag_name":"v0.6.0","html_url":"https://example.invalid/v0.6.0"}`)
		case "checksums.txt":
			fmt.Fprintf(w, "%x  %s\n", sum, asset)
		default:
			_, _ = w.Write(archive)
		}
	}))
	t.Cleanup(server.Close)
	return update.Checker{Client: server.Client(), Endpoint: server.URL + "/latest", ReleaseBase: server.URL}
}

func candidateScript(version string, failDoctor bool) []byte {
	doctor := "exit 0"
	if failDoctor {
		doctor = "exit 1"
	}
	return []byte(fmt.Sprintf(`#!/bin/sh
case "$1" in
  version) echo %s ;;
  _update-metadata) echo '{"protocol_version":1,"version":"%s","target_schemas":{"config":1,"state":1,"ownership":1,"components":1,"server":1},"supported_source_schemas":{"config":[1],"state":[1],"ownership":[1],"components":[1],"server":[1]},"rollback_safe":true}' ;;
  _update-migrate) test -n "$IVOAI_UPDATE_TRANSACTION" -a -n "$IVOAI_UPDATE_ROOT" -a -n "$IVOAI_UPDATE_PARENT_PID" ;;
  setup) exit 0 ;;
  doctor) %s ;;
  server) %s ;;
  *) exit 1 ;;
esac
`, version, version, doctor, doctor))
}

func candidateScriptWithLog(version, logPath string) []byte {
	base := candidateScript(version, false)
	needle := []byte("#!/bin/sh\n")
	logging := []byte(fmt.Sprintf("printf '%%s\\n' \"$*\" >> %q\n", logPath))
	return bytes.Replace(base, needle, append(needle, logging...), 1)
}

func candidateArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "ivoai", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
