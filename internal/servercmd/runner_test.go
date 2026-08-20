package servercmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/server"
)

func TestServerSetupEnrollmentAndConnectorsAreIdempotent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	t.Setenv("IVOAI_TEST_MODE", "1")
	t.Setenv("IVOAI_SERVER_ROOT", root)
	run := New("0.1.0-test")
	for attempt := 0; attempt < 2; attempt++ {
		var out bytes.Buffer
		if err := run(context.Background(), []string{"setup"}, strings.NewReader(""), &out, &out); err != nil {
			t.Fatalf("setup %d: %v (%s)", attempt+1, err, out.String())
		}
	}

	var enrollmentOut bytes.Buffer
	if err := run(context.Background(), []string{"enrollment", "create", "--ttl", "5m"}, strings.NewReader(""), &enrollmentOut, &enrollmentOut); err != nil {
		t.Fatal(err)
	}
	code := regexp.MustCompile(`ivoai-enroll_[A-Za-z0-9_-]+`).FindString(enrollmentOut.String())
	if code == "" {
		t.Fatalf("one-time code missing from create output: %s", enrollmentOut.String())
	}
	statePath := filepath.Join(root, "var/lib/ivoai/enrollment/state.json")
	state, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(state, []byte(code)) {
		t.Fatal("one-time enrollment code persisted in plaintext")
	}
	if info, err := os.Stat(statePath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("enrollment state permissions: info=%v err=%v", info, err)
	}

	source := filepath.Join(t.TempDir(), "documents")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"connector", "add", "--name", "docs", "--type", "filesystem", "--path", source}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var list bytes.Buffer
	if err := run(context.Background(), []string{"connector", "list"}, strings.NewReader(""), &list, &list); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(list.String(), "docs\tfilesystem\t"+source) {
		t.Fatalf("connector missing: %s", list.String())
	}
	if err := run(context.Background(), []string{"connector", "remove", "docs"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
}

func TestServerRootOverrideRequiresTestMode(t *testing.T) {
	t.Setenv("IVOAI_SERVER_ROOT", filepath.Join(t.TempDir(), "root"))
	t.Setenv("IVOAI_TEST_MODE", "")
	err := New("test")(context.Background(), []string{"status"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "only in test mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSupportedServerOSUsesNumericVersions(t *testing.T) {
	tests := []struct {
		id, version string
		want        bool
	}{
		{"ubuntu", "22.04", true},
		{"ubuntu", "24.04", true},
		{"ubuntu", "26.10", true},
		{"ubuntu", "22.03", false},
		{"ubuntu", "20.04", false},
		{"debian", "12", true},
		{"debian", "13.1", true},
		{"debian", "9", false},
		{"fedora", "42", false},
		{"ubuntu", "rolling", false},
	}
	for _, tt := range tests {
		if got := supportedServerOS(tt.id, tt.version); got != tt.want {
			t.Errorf("supportedServerOS(%q, %q) = %t, want %t", tt.id, tt.version, got, tt.want)
		}
	}
}

func TestSupportedServerArchitectures(t *testing.T) {
	for architecture, expected := range map[string]bool{"amd64": true, "arm64": true, "386": false, "riscv64": false} {
		if actual := supportedServerArchitecture(architecture); actual != expected {
			t.Errorf("supportedServerArchitecture(%q) = %t, want %t", architecture, actual, expected)
		}
	}
}

func TestSecureChownTreeRefusesSymlinkEntries(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := secureChownTree(root, os.Getuid(), os.Getgid()); err == nil {
		t.Fatal("descriptor traversal accepted a symlink entry")
	}
	content, err := os.ReadFile(outside)
	if err != nil || string(content) != "safe" {
		t.Fatalf("outside file changed: %q %v", content, err)
	}
}

func TestManagedDataRootOwnershipPreservesApplicationSymlinks(t *testing.T) {
	root := t.TempDir()
	blobs := filepath.Join(root, "blobs")
	if err := os.Mkdir(blobs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blobs, "model-config"), []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("blobs/model-config", filepath.Join(root, "config.json")); err != nil {
		t.Fatal(err)
	}
	if err := chownMode(root, os.Getuid(), os.Getgid(), 0o700); err != nil {
		t.Fatalf("managed data root rejected an application cache symlink: %v", err)
	}
	info, err := os.Lstat(filepath.Join(root, "config.json"))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("application cache symlink was not preserved: info=%v err=%v", info, err)
	}
}

func TestWaitForServerStartReportsProgress(t *testing.T) {
	var output bytes.Buffer
	release := make(chan struct{})
	start := func(context.Context) error {
		<-release
		return nil
	}
	go func() {
		time.Sleep(5 * time.Millisecond)
		close(release)
	}()
	if err := waitForServerStart(context.Background(), &output, time.Millisecond, start); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"waiting for container health checks", "still initializing", "docker compose"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("server start output omitted %q: %s", expected, output.String())
		}
	}
}

func TestWaitForServicesStableRejectsTransientAndInactiveServices(t *testing.T) {
	active := []server.ServiceState{
		{Name: "ivoai-dependencies.service", Active: true, Detail: "active"},
		{Name: "ivoai-context.service", Active: true, Detail: "active"},
		{Name: "ivoai-gateway.service", Active: true, Detail: "active"},
	}
	if err := waitForServicesStable(context.Background(), 50*time.Millisecond, time.Millisecond, 3*time.Millisecond, func(context.Context) ([]server.ServiceState, error) {
		return active, nil
	}); err != nil {
		t.Fatalf("stable services rejected: %v", err)
	}

	inactive := append([]server.ServiceState(nil), active...)
	inactive[2] = server.ServiceState{Name: "ivoai-gateway.service", Detail: "activating"}
	if err := waitForServicesStable(context.Background(), 8*time.Millisecond, time.Millisecond, 3*time.Millisecond, func(context.Context) ([]server.ServiceState, error) {
		return inactive, nil
	}); err == nil || !strings.Contains(err.Error(), "ivoai-gateway.service=activating") {
		t.Fatalf("inactive service did not fail with a diagnostic: %v", err)
	}
}

func TestSecureRegularOwnershipUsesNoFollowDescriptor(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "managed.env")
	if err := os.WriteFile(regular, []byte("safe\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := secureRegularOwnership(regular, os.Getuid(), os.Getgid(), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(regular)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("regular file permissions: %v / %v", info, err)
	}

	target := filepath.Join(dir, "outside")
	if err := os.WriteFile(target, []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.env")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := secureRegularOwnership(link, os.Getuid(), os.Getgid(), 0o600); err == nil {
		t.Fatal("symlink was accepted as a managed regular file")
	}
	outsideInfo, err := os.Stat(target)
	if err != nil || outsideInfo.Mode().Perm() != 0o644 {
		t.Fatalf("symlink target permissions changed: %v / %v", outsideInfo, err)
	}
}

func TestGatewayConfigurePersistsHTTPSWithoutManualEditing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	t.Setenv("IVOAI_TEST_MODE", "1")
	t.Setenv("IVOAI_SERVER_ROOT", root)
	run := New("test")
	if err := run(context.Background(), []string{"setup"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(context.Background(), []string{"gateway", "configure", "--public-url", "https://ai.example.com"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatal(err)
	}
	configuration, err := os.ReadFile(filepath.Join(root, "etc/ivoai/gateway.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(configuration, []byte(`"public_url": "https://ai.example.com"`)) || !strings.Contains(out.String(), "reverse proxy") {
		t.Fatalf("gateway configuration was not persisted: %s / %s", configuration, out.String())
	}
	if err := run(context.Background(), []string{"gateway", "configure", "--public-url", "https://ai.example.com", "--listen", "0.0.0.0:7744"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("public plaintext gateway configuration accepted")
	}
	var proxyOut bytes.Buffer
	if err := run(context.Background(), []string{"gateway", "configure", "--public-url", "https://ai.example.com", "--listen", "192.0.2.10:7744", "--trusted-proxy", "192.0.2.20/32"}, strings.NewReader(""), &proxyOut, &proxyOut); err != nil {
		t.Fatalf("trusted reverse-proxy configuration rejected: %v (%s)", err, proxyOut.String())
	}
	configuration, err = os.ReadFile(filepath.Join(root, "etc/ivoai/gateway.json"))
	if err != nil || !bytes.Contains(configuration, []byte(`"trusted_proxy_cidrs"`)) || !strings.Contains(proxyOut.String(), "trusted HTTPS reverse proxy") {
		t.Fatalf("trusted reverse-proxy configuration was not persisted: %s / %v / %s", configuration, err, proxyOut.String())
	}
	certificate := filepath.Join(t.TempDir(), "fullchain.pem")
	key := filepath.Join(t.TempDir(), "private-key.pem")
	if err := os.WriteFile(certificate, []byte("certificate fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("private key fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"gateway", "configure", "--public-url", "https://ai.example.com", "--listen", "0.0.0.0:7744", "--tls-cert", certificate, "--tls-key", key}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	configuration, err = os.ReadFile(filepath.Join(root, "etc/ivoai/gateway.json"))
	if err != nil || !bytes.Contains(configuration, []byte("/etc/ivoai/secrets/tls")) {
		t.Fatalf("direct TLS did not use the managed secret tree: %s / %v", configuration, err)
	}
	for _, name := range []string{"certificate.pem", "private-key.pem"} {
		info, err := os.Stat(filepath.Join(root, "etc/ivoai/secrets/tls", name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("managed TLS file %s has unsafe permissions: %v / %v", name, info, err)
		}
	}
}
