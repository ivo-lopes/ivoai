package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeController struct {
	calls  []string
	states []ServiceState
}

func (f *fakeController) record(operation string, names []string) {
	f.calls = append(f.calls, operation+":"+strings.Join(names, ","))
}
func (f *fakeController) DaemonReload(context.Context) error { f.record("reload", nil); return nil }
func (f *fakeController) Enable(_ context.Context, names []string) error {
	f.record("enable", names)
	return nil
}
func (f *fakeController) Start(_ context.Context, names []string) error {
	f.record("start", names)
	return nil
}
func (f *fakeController) Stop(_ context.Context, names []string) error {
	f.record("stop", names)
	return nil
}
func (f *fakeController) Restart(_ context.Context, names []string) error {
	f.record("restart", names)
	return nil
}
func (f *fakeController) Status(_ context.Context, names []string) ([]ServiceState, error) {
	f.record("status", names)
	return f.states, nil
}
func (f *fakeController) Logs(_ context.Context, name string, lines int) (string, error) {
	f.record("logs", []string{name})
	return "safe logs", nil
}

func TestSetupIsIdempotentAndLifecycleUsesManagedServices(t *testing.T) {
	root := t.TempDir()
	layout := DefaultLayout(root)
	controller := &fakeController{}
	manager := Manager{Layout: layout, Controller: controller, Architecture: "amd64", ContainerUser: "4242:4242"}
	if err := manager.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{layout.QdrantSnapshotsDir, layout.QdrantInitDir} {
		info, err := os.Stat(directory)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("Qdrant writable directory is not private: %s info=%v err=%v", directory, info, err)
		}
	}
	first, err := os.ReadFile(filepath.Join(layout.ConfigDir, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(first, []byte(`user: "4242:4242"`)) || bytes.Contains(first, []byte(`user: "0:0"`)) {
		t.Fatalf("ai-memory container is not assigned the non-root service identity: %s", first)
	}
	if bytes.Count(first, []byte(`user: "4242:4242"`)) != 3 {
		t.Fatalf("every dependency container must use the service identity: %s", first)
	}
	if !bytes.Contains(first, []byte("/readyz")) || bytes.Contains(first, []byte(`"</dev/tcp/127.0.0.1/6333"`)) {
		t.Fatalf("Qdrant must use its HTTP readiness endpoint: %s", first)
	}
	for _, expected := range []string{"QDRANT_INIT_FILE_PATH: /qdrant/init/.qdrant-initialized", "/var/lib/ivoai/qdrant-snapshots:/qdrant/snapshots", "/var/lib/ivoai/qdrant-init:/qdrant/init"} {
		if !bytes.Contains(first, []byte(expected)) {
			t.Errorf("Qdrant writable runtime mount missing %q: %s", expected, first)
		}
	}
	if err := manager.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(layout.ConfigDir, "compose.yaml"))
	if !bytes.Equal(first, second) || strings.Contains(string(second), ":latest") {
		t.Fatal("setup changed assets or used latest")
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(controller.calls, "\n"), "start:ivoai-dependencies.service") {
		t.Fatalf("unexpected controller calls: %#v", controller.calls)
	}
	unit, _ := os.ReadFile(filepath.Join(layout.SystemdDir, "ivoai-gateway.service"))
	for _, hardening := range []string{"NoNewPrivileges=yes", "ProtectSystem=strict", "User=ivoai-gateway", "Group=ivoai", "ProtectProc=invisible", "ProcSubset=pid", "Restart=on-failure"} {
		if !bytes.Contains(unit, []byte(hardening)) {
			t.Fatalf("unit missing %s", hardening)
		}
	}
	contextUnit, _ := os.ReadFile(filepath.Join(layout.SystemdDir, "ivoai-context.service"))
	for _, hardening := range []string{"User=ivoai-context", "Group=ivoai", "ProtectProc=invisible", "ProcSubset=pid", "InaccessiblePaths=/etc/ivoai/secrets"} {
		if !bytes.Contains(contextUnit, []byte(hardening)) {
			t.Fatalf("context unit missing %s", hardening)
		}
	}
}

func TestSetupRejectsRootContainerIdentity(t *testing.T) {
	manager := Manager{Layout: DefaultLayout(t.TempDir()), Architecture: "amd64", ContainerUser: "0:0"}
	if err := manager.Setup(context.Background()); err == nil {
		t.Fatal("root container identity was accepted")
	}
}

func TestBackendSecretsArePrivateAndIdempotent(t *testing.T) {
	layout := DefaultLayout(t.TempDir())
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(layout.SecretsDir, "tls")); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("managed TLS secret directory is unavailable: info=%v err=%v", info, err)
	}
	if err := EnsureBackendSecrets(layout); err != nil {
		t.Fatal(err)
	}
	first, err := LoadBackendSecret(layout, "qdrant.env", "QDRANT__SERVICE__API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureBackendSecrets(layout); err != nil {
		t.Fatal(err)
	}
	second, err := LoadBackendSecret(layout, "qdrant.env", "QDRANT__SERVICE__API_KEY")
	if err != nil || first != second || len(first) != 64 {
		t.Fatalf("secret changed or malformed: length=%d err=%v", len(second), err)
	}
	for name := range backendSecretFiles {
		info, err := os.Stat(filepath.Join(layout.SecretsDir, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s permissions: %v %v", name, info, err)
		}
	}
}

func TestBackendSecretsRefuseSymlink(t *testing.T) {
	layout := DefaultLayout(t.TempDir())
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("QDRANT__SERVICE__API_KEY="+strings.Repeat("a", 64)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(layout.SecretsDir, "qdrant.env")); err != nil {
		t.Fatal(err)
	}
	if err := EnsureBackendSecrets(layout); err == nil {
		t.Fatal("backend secret symlink was accepted")
	}
}

func TestBackupRestoreAndSecretExclusion(t *testing.T) {
	source := DefaultLayout(filepath.Join(t.TempDir(), "source"))
	if err := source.Ensure(); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(source.ConfigDir, "server.toml"), "safe-config")
	writeTestFile(t, filepath.Join(source.SecretsDir, "enrollment.json"), "secret")
	writeTestFile(t, filepath.Join(source.ContextDir, "catalog.json"), "context")
	writeTestFile(t, filepath.Join(source.MemoryDir, "memory.db"), "memory")
	writeTestFile(t, filepath.Join(source.QdrantDir, "index"), "rebuildable")
	backup := filepath.Join(t.TempDir(), "backup.tar.gz")
	if err := Backup(source, backup, time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	archive, _ := os.ReadFile(backup)
	if bytes.Contains(archive, []byte("secret")) {
		t.Fatal("secret present in backup")
	}
	destination := DefaultLayout(filepath.Join(t.TempDir(), "destination"))
	if err := Restore(destination, backup); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(destination.ConfigDir, "server.toml"), "safe-config")
	assertFile(t, filepath.Join(destination.ContextDir, "catalog.json"), "context")
	assertFile(t, filepath.Join(destination.MemoryDir, "memory.db"), "memory")
	if _, err := os.Stat(filepath.Join(destination.QdrantDir, "index")); !os.IsNotExist(err) {
		t.Fatal("rebuildable Qdrant data was restored")
	}
	if _, err := os.Stat(filepath.Join(destination.SecretsDir, "enrollment.json")); !os.IsNotExist(err) {
		t.Fatal("secret was restored")
	}
}

func TestRestoreRejectsTraversalAndLinks(t *testing.T) {
	for _, test := range []struct {
		name  string
		entry *tar.Header
	}{
		{"traversal", &tar.Header{Name: "data/context/../../escape", Typeflag: tar.TypeReg, Size: 1, Mode: 0o600}},
		{"symlink", &tar.Header{Name: "data/context/link", Typeflag: tar.TypeSymlink, Linkname: "/etc", Mode: 0o777}},
	} {
		t.Run(test.name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "malicious.tar.gz")
			createArchive(t, archive, test.entry)
			if err := Restore(DefaultLayout(filepath.Join(t.TempDir(), "root")), archive); err == nil {
				t.Fatal("malicious archive accepted")
			}
		})
	}
}

func TestRestoreRejectsManagedExecutableConfiguration(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "malicious.tar.gz")
	createArchive(t, archive, &tar.Header{Name: "config/compose.yaml", Typeflag: tar.TypeReg, Size: 1, Mode: 0o600})
	if err := Restore(DefaultLayout(filepath.Join(t.TempDir(), "root")), archive); err == nil || !strings.Contains(err.Error(), "unsupported path") {
		t.Fatalf("managed compose replacement accepted: %v", err)
	}
}

func TestGatewayConfigurationSupportsReverseProxyAndDirectTLS(t *testing.T) {
	layout := DefaultLayout(t.TempDir())
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	reverseProxy := GatewayConfig{ListenAddress: "127.0.0.1:7744", PublicURL: "https://ai.example.com"}
	if err := SaveGatewayConfig(layout, reverseProxy); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadGatewayConfig(layout)
	if err != nil || loaded != reverseProxy {
		t.Fatalf("load gateway config = %#v, %v", loaded, err)
	}
	if err := os.Chmod(GatewayConfigPath(layout), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGatewayConfig(layout); err != nil {
		t.Fatalf("root-owned group-readable gateway config was rejected: %v", err)
	}
	if err := (GatewayConfig{ListenAddress: "0.0.0.0:7744", PublicURL: "https://ai.example.com"}).Validate(false); err == nil {
		t.Fatal("public plaintext listener accepted")
	}
	cert := filepath.Join(t.TempDir(), "gateway.crt")
	key := filepath.Join(t.TempDir(), "gateway.key")
	writeTestFile(t, cert, "certificate fixture")
	writeTestFile(t, key, "key fixture")
	direct := GatewayConfig{ListenAddress: "0.0.0.0:7744", PublicURL: "https://ai.example.com", TLSCertFile: cert, TLSKeyFile: key}
	if err := direct.Validate(true); err != nil {
		t.Fatalf("direct TLS config rejected: %v", err)
	}
	managedCert, managedKey, err := InstallGatewayTLS(layout, cert, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, managed := range []string{managedCert, managedKey} {
		if info, err := os.Stat(managed); err != nil || info.Mode().Perm() != 0o600 || !strings.HasPrefix(managed, layout.SecretsDir+string(filepath.Separator)) {
			t.Fatalf("TLS file not managed privately: %s info=%v err=%v", managed, info, err)
		}
	}
}

func TestARMSetupUsesPinnedArchitectureImage(t *testing.T) {
	layout := DefaultLayout(t.TempDir())
	manager := Manager{Layout: layout, Architecture: "arm64"}
	if err := manager.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	override, err := os.ReadFile(filepath.Join(layout.ConfigDir, "compose.arm64.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(override), "sha256:2873ddd3029bf6eed09c0befe737d88123107a0b89274b1552d68d1e3bb2a047") || strings.Contains(string(override), "latest") || strings.Contains(string(override), "build:") {
		t.Fatalf("invalid ARM pin: %s", override)
	}
	unit, _ := os.ReadFile(filepath.Join(layout.SystemdDir, "ivoai-dependencies.service"))
	if !strings.Contains(string(unit), "compose.arm64.yaml") {
		t.Fatalf("ARM override is not active in managed Compose: %s", unit)
	}
}

func TestDeployAssetsMatchEmbeddedServerAssets(t *testing.T) {
	for _, item := range []struct {
		path, embedded string
	}{
		{"../../deploy/server/compose.yaml", ComposeYAML},
		{"../../deploy/server/compose.arm64.yaml", ARM64ComposeOverride},
		{"../../deploy/server/systemd/ivoai-gateway.service", GatewayUnit},
		{"../../deploy/server/systemd/ivoai-context.service", ContextUnit},
		{"../../deploy/server/systemd/ivoai-dependencies.service", DependenciesUnit},
	} {
		deployed, err := os.ReadFile(item.path)
		if err != nil {
			t.Fatal(err)
		}
		if string(deployed) != item.embedded {
			t.Errorf("deployed asset %s differs from embedded setup asset", item.path)
		}
	}
}

func createArchive(t *testing.T, path string, malicious *tar.Header) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	manifest := []byte(`{"format_version":1,"created_at":"2026-08-20T00:00:00Z"}`)
	if err := tw.WriteHeader(&tar.Header{Name: "manifest.json", Typeflag: tar.TypeReg, Size: int64(len(manifest)), Mode: 0o600}); err != nil {
		t.Fatal(err)
	}
	_, _ = tw.Write(manifest)
	if err := tw.WriteHeader(malicious); err != nil {
		t.Fatal(err)
	}
	if malicious.Size > 0 {
		_, _ = tw.Write(bytes.Repeat([]byte("x"), int(malicious.Size)))
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path, expected string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != expected {
		t.Fatalf("%s = %q, %v", path, data, err)
	}
}
