package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stoppedUpgradeFixture(t *testing.T) (Manager, *fakeController, string) {
	t.Helper()
	layout := DefaultLayout(t.TempDir())
	controller := &fakeController{}
	m := Manager{Layout: layout, Architecture: "amd64", Controller: controller}
	if err := m.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range ManagedServices {
		controller.states = append(controller.states, ServiceState{Name: name})
	}
	old := strings.ReplaceAll(ComposeYAML, MemoryImageReference(), "akitaonrails/ai-memory@sha256:"+strings.Repeat("1", 64))
	old = strings.ReplaceAll(old, QdrantImageReference(), "qdrant/qdrant@sha256:"+strings.Repeat("2", 64))
	if err := os.WriteFile(filepath.Join(layout.ConfigDir, "compose.yaml"), []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{filepath.Join(layout.MemoryDir, "memory.db"): "original-schema-two", filepath.Join(layout.QdrantDir, "index"): "original-index", filepath.Join(layout.ConfigDir, "server.toml"): "custom = true\n"} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return m, controller, old
}

func TestDependencySetupRefusesMigrationBeforeAnyRuntimeWrite(t *testing.T) {
	m, controller, old := stoppedUpgradeFixture(t)
	before, _ := os.Stat(filepath.Join(m.Layout.ConfigDir, "compose.yaml"))
	calls := len(controller.calls)
	if err := m.Setup(context.Background()); !errors.Is(err, ErrDependencyUpgradeRequired) {
		t.Fatal("ordinary setup admitted a forward runtime", err)
	}
	after, _ := os.Stat(filepath.Join(m.Layout.ConfigDir, "compose.yaml"))
	body, _ := os.ReadFile(filepath.Join(m.Layout.ConfigDir, "compose.yaml"))
	if string(body) != old || !before.ModTime().Equal(after.ModTime()) || len(controller.calls) != calls {
		t.Fatal("refused setup changed runtime files/services")
	}
}

func TestDependencyUpgradeValidatesCopyAndRollbackRestoresDataIndexAndPins(t *testing.T) {
	m, _, old := stoppedUpgradeFixture(t)
	archive := filepath.Join(m.Layout.BackupDir, "before.tar.gz")
	receipt, err := m.UpgradeDependencies(context.Background(), archive, func(ctx context.Context, stage Layout) error {
		if stage.MemoryDir == m.Layout.MemoryDir || stage.QdrantDir == m.Layout.QdrantDir {
			t.Fatal("validator received original data")
		}
		assertFile(t, filepath.Join(stage.MemoryDir, "memory.db"), "original-schema-two")
		assertFile(t, filepath.Join(stage.QdrantDir, "index"), "original-index")
		if err := os.WriteFile(filepath.Join(stage.MemoryDir, "memory.db"), []byte("forward-copy"), 0600); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(stage.QdrantDir, "index"), []byte("forward-index-copy"), 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(m.Layout.MemoryDir, "memory.db"), "original-schema-two")
	assertFile(t, filepath.Join(m.Layout.QdrantDir, "index"), "original-index")
	assertFile(t, filepath.Join(m.Layout.ConfigDir, "server.toml"), "custom = true\n")
	current, _ := os.ReadFile(filepath.Join(m.Layout.ConfigDir, "compose.yaml"))
	if string(current) != ComposeYAML {
		t.Fatal("new pins were not installed after validation")
	}
	if receipt == "" {
		t.Fatal("missing coordinated rollback receipt")
	}
	if err := m.Setup(context.Background()); err != nil {
		t.Fatal("ordinary setup after verified upgrade failed", err)
	}
	assertFile(t, filepath.Join(m.Layout.ConfigDir, "server.toml"), "custom = true\n")
	if err := os.WriteFile(filepath.Join(m.Layout.MemoryDir, "memory.db"), []byte("activated-forward-schema"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.Layout.QdrantDir, "index"), []byte("activated-forward-index"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.RollbackDependencies(context.Background(), receipt); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(m.Layout.MemoryDir, "memory.db"), "original-schema-two")
	assertFile(t, filepath.Join(m.Layout.QdrantDir, "index"), "original-index")
	assertFile(t, filepath.Join(m.Layout.ConfigDir, "compose.yaml"), old)
	preserved, _ := filepath.Glob(filepath.Join(m.Layout.BackupDir, "post-upgrade-index-*", "index"))
	if len(preserved) != 1 {
		t.Fatal("current forward index was not preserved for inspection")
	}
	assertFile(t, preserved[0], "activated-forward-index")
}

func TestDependencyValidationFailureDoesNotInstallOrModifyOriginalStore(t *testing.T) {
	m, _, old := stoppedUpgradeFixture(t)
	receipt, err := m.UpgradeDependencies(context.Background(), filepath.Join(m.Layout.BackupDir, "before.tar.gz"), func(context.Context, Layout) error { return errors.New("candidate unavailable") })
	if err == nil || receipt != "" {
		t.Fatal("failed candidate was activated")
	}
	assertFile(t, filepath.Join(m.Layout.ConfigDir, "compose.yaml"), old)
	assertFile(t, filepath.Join(m.Layout.MemoryDir, "memory.db"), "original-schema-two")
	assertFile(t, filepath.Join(m.Layout.QdrantDir, "index"), "original-index")
}

func TestDependencyRollbackRejectsChangedIndexBeforeRestoringData(t *testing.T) {
	m, _, _ := stoppedUpgradeFixture(t)
	archive := filepath.Join(m.Layout.BackupDir, "before.tar.gz")
	receipt, err := m.UpgradeDependencies(context.Background(), archive, func(context.Context, Layout) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.Layout.MemoryDir, "memory.db"), []byte("current-store"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archive+".qdrant", "index"), []byte("modified-snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.RollbackDependencies(context.Background(), receipt); err == nil {
		t.Fatal("modified snapshot accepted")
	}
	assertFile(t, filepath.Join(m.Layout.MemoryDir, "memory.db"), "current-store")
}

func TestDependencyOperationsRequireStoppedServicesAndSerialize(t *testing.T) {
	m, controller, _ := stoppedUpgradeFixture(t)
	controller.states[0].Active = true
	if _, err := m.UpgradeDependencies(context.Background(), filepath.Join(m.Layout.BackupDir, "before.tar.gz"), func(context.Context, Layout) error { return nil }); err == nil {
		t.Fatal("live store admitted")
	}
	if err := WithDependencyMutationLock(context.Background(), m.Layout, func(held context.Context) error {
		if err := WithDependencyMutationLock(context.Background(), m.Layout, func(context.Context) error { t.Fatal("contended operation admitted"); return nil }); err == nil {
			t.Fatal("missing non-replayable contention")
		}
		return WithDependencyMutationLock(held, m.Layout, func(context.Context) error { return nil })
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDependencyReceiptRejectsUnsupportedAssetsAndPublicPermissions(t *testing.T) {
	m, _, _ := stoppedUpgradeFixture(t)
	receipt, err := m.UpgradeDependencies(context.Background(), filepath.Join(m.Layout.BackupDir, "before.tar.gz"), func(context.Context, Layout) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(receipt, 0644); err != nil {
		t.Fatal(err)
	}
	if err := m.RollbackDependencies(context.Background(), receipt); err == nil {
		t.Fatal("public operational receipt accepted")
	}
	if err := os.Chmod(receipt, 0600); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(receipt)
	var r DependencyUpgradeReceipt
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatal(err)
	}
	delete(r.Assets, "docs.unit")
	r.Assets["../../outside"] = DependencyAsset{Present: true, Mode: 0600, Content: []byte("unsafe")}
	if err := writeDependencyReceipt(receipt, r); err != nil {
		t.Fatal(err)
	}
	if err := m.RollbackDependencies(context.Background(), receipt); err == nil {
		t.Fatal("arbitrary operational path accepted")
	}
}

func TestDependencyLockContextCannotOutliveItsKernelLease(t *testing.T) {
	m, _, _ := stoppedUpgradeFixture(t)
	var escaped context.Context
	if err := WithDependencyMutationLock(context.Background(), m.Layout, func(held context.Context) error { escaped = held; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := WithDependencyMutationLock(context.Background(), m.Layout, func(context.Context) error {
		if err := WithDependencyMutationLock(escaped, m.Layout, func(context.Context) error {
			t.Fatal("expired lock capability bypassed another active transaction")
			return nil
		}); err == nil {
			t.Fatal("expired context still carried authority")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
