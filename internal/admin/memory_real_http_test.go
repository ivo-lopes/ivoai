package admin_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/cli"
	"github.com/ivo-lopes/ivoai/internal/server"
	"github.com/ivo-lopes/ivoai/internal/servercmd"
)

// This opt-in uses the real administrative HTTP handler, CLI adapter and an
// official ai-memory process. Its fixed backend port must never be opened in
// the caller's network namespace. The outer fixture runs the compiled test
// under bubblewrap with a private network namespace and no external route.
func TestAdministrativePositiveMemoryStatusOfficialServer(t *testing.T) {
	binary := reviewedAdminBinary(t, "IVOAI_ADMIN_REAL_MEMORY_BINARY", "5a8e2c51cb09dadfc157601772b32c79ee63a6cbf9f05caf59c67da385bae6c1")
	parentNamespace := os.Getenv("IVOAI_ADMIN_MEMORY_PARENT_NETNS")
	currentNamespace, err := os.Readlink("/proc/self/ns/net")
	if err != nil || parentNamespace == "" || parentNamespace == currentNamespace {
		t.Fatal("real Memory status fixture requires an explicitly isolated network namespace")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:49374")
	if err != nil {
		t.Fatal("private fixed backend port is already owned")
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	a := positiveAdministrativeApp(t)
	root := t.TempDir()
	// This existing guard selects private filesystem layout and bypasses the
	// production installation/systemd precondition only. Neither HTTP dispatch,
	// credentials, MCP network probing nor the backend process is substituted.
	t.Setenv("IVOAI_TEST_MODE", "1")
	t.Setenv("IVOAI_SERVER_ROOT", root)
	layout := server.DefaultLayout(root)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := server.EnsureBackendSecrets(layout); err != nil {
		t.Fatal(err)
	}
	token, err := server.LoadBackendSecret(layout, "memory.env", "AI_MEMORY_AUTH_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, binary, "serve", "--transport", "http", "--bind", "127.0.0.1:49374", "--no-watcher", "--workspace", "admin-fixture", "--project", "memory-status")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + filepath.Join(root, "backend-home"), "AI_MEMORY_DATA_DIR=" + layout.MemoryDir, "AI_MEMORY_EMBEDDING_PROVIDER=none", "AI_MEMORY_AUTH_TOKEN=" + token, "RUST_LOG=error"}
	// Do not retain child configuration/log output: backend credentials are
	// ephemeral fixture secrets and must not enter test or evidence logs.
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal("official ai-memory did not start", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
			t.Error("official child required forced cleanup after termination deadline")
		}
	}
	t.Cleanup(stop)
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	t.Cleanup(client.CloseIdleConnections)
	ready := false
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
		select {
		case <-exited:
			stopped = true
			t.Fatal("official backend exited before readiness")
		default:
		}
		status, _ := administrativeMemoryCatalog(t, client, "")
		if status == http.StatusUnauthorized {
			ready = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !ready {
		t.Fatal("official authenticated backend did not become ready")
	}
	status, catalog := administrativeMemoryCatalog(t, client, token)
	if status != http.StatusOK || !bytes.Contains(catalog, []byte(`"memory_status"`)) {
		t.Fatal("official authenticated MCP tools catalog is unavailable")
	}
	status, _ = administrativeMemoryCatalog(t, client, "invalid-private-fixture-token")
	if status != http.StatusUnauthorized {
		t.Fatal("official backend accepted the wrong credential")
	}

	cli.RegisterServerRunner(servercmd.New("0.17.0"))
	t.Cleanup(func() { cli.RegisterServerRunner(nil) })
	h := newPositiveAdministrativeHTTP(t, a)
	path := "/api/admin/v1/actions/server.memory.status"
	body := map[string]any{"phase": "execute", "input": map[string]string{}, "request_id": "real-memory-status-read"}
	cookie := h.cookie
	h.cookie = ""
	status, _ = h.request("POST", path, body)
	h.cookie = cookie
	if status != http.StatusUnauthorized || h.calls.Load() != 0 {
		t.Fatal("unauthorized administrative read was dispatched")
	}
	csrf := h.csrf
	h.csrf = ""
	status, _ = h.request("POST", path, body)
	h.csrf = csrf
	if status != http.StatusForbidden || h.calls.Load() != 0 {
		t.Fatal("administrative read without CSRF was dispatched")
	}
	secretPath := filepath.Join(layout.SecretsDir, "memory.env")
	secretBefore, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	configBefore, err := os.ReadFile(a.Store.Paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		status, result := h.request("POST", path, body)
		output, _ := result["output"].(string)
		if status != http.StatusOK || result["status"] != "completed" || strings.TrimSpace(output) != "ai-memory: healthy" || h.lastError() != nil {
			t.Fatal("actual administrative Memory status did not report healthy")
		}
		encoded, err := json.Marshal(result)
		if err != nil || bytes.Contains(encoded, []byte(token)) {
			t.Fatal("private backend credential leaked into administrative output")
		}
	}
	// This action is a read. Repeating an authorized read is intentional; the
	// mutation-only confirmation/NoReplay contract does not invent a 409 here.
	if h.calls.Load() != 2 {
		t.Fatal("unexpected authorized read dispatch count")
	}
	configAfter, err := os.ReadFile(a.Store.Paths.Config)
	if err != nil || !bytes.Equal(configBefore, configAfter) {
		t.Fatal("status read changed managed configuration")
	}
	secretAfter, err := os.ReadFile(secretPath)
	if err != nil || !bytes.Equal(secretBefore, secretAfter) {
		t.Fatal("status read changed backend credential")
	}
	wrong := make([]byte, 32)
	if _, err := rand.Read(wrong); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretPath, []byte("AI_MEMORY_AUTH_TOKEN="+hex.EncodeToString(wrong)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	status, result := h.request("POST", path, body)
	if status != http.StatusOK || result["status"] != "failed" || h.lastError() == nil || h.calls.Load() != 3 {
		t.Fatal("wrong private backend credential was not reported as failure")
	}
	if err := os.WriteFile(secretPath, secretBefore, 0600); err != nil {
		t.Fatal(err)
	}
	stop()
	status, result = h.request("POST", path, body)
	if status != http.StatusOK || result["status"] != "failed" || h.lastError() == nil || h.calls.Load() != 4 {
		t.Fatal("stopped backend did not fail without a hidden success/retry")
	}
	t.Log("real HTTP+CLI+Core server.memory.status: official ai-memory3.4.2 MCP catalog, two healthy reads; missing auth/CSRF refused before dispatch; wrong backend credential/stopped process failed; private config/secret unchanged by reads; no inference/systemd")
}

func administrativeMemoryCatalog(t *testing.T, client *http.Client, token string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:49374/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		t.Fatal("MCP response could not be read")
	}
	return resp.StatusCode, body
}
