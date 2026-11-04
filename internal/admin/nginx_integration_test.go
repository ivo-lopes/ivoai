package admin

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
)

// Opt-in because nginx is a separately installed executable, not a Go dependency.
// An explicit unavailable/invalid executable is a failure, never a certified skip.
func TestRealNginxAdministrativeTLSOAuthMutationAndOutput(t *testing.T) {
	binary := os.Getenv("IVOAI_TEST_NGINX")
	if binary == "" {
		t.Skip("set IVOAI_TEST_NGINX to an audited nginx executable for the real TLS proxy gate")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("nginx executable must be an explicit absolute path")
	}
	root := t.TempDir()
	a := fixtureApp(t)
	if err := a.Store.Save(config.Default()); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewUnstartedServer(nil)
	frontPort, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	frontAddress := frontPort.Addr().String()
	frontPort.Close()
	publicOrigin := "https://" + frontAddress
	public := publicOrigin + "/ivoai/admin"
	var calls atomic.Int32
	s, err := New(Options{App: a, Origin: "http://" + upstream.Listener.Addr().String(), PublicURL: public, TrustedProxies: []string{"127.0.0.1/32"}, Dispatch: func(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) error {
		calls.Add(1)
		if len(args) == 4 && args[0] == "config" && args[1] == "set" {
			operation := *a
			operation.In, operation.Out, operation.Err = in, out, errOut
			if err := operation.ConfigSet(args[2], args[3]); err != nil {
				return err
			}
			_, err := io.WriteString(out, strings.Repeat("Nginx-proxied administrative evidence\n", 10000)+"FINAL-NGINX-EVIDENCE\n")
			return err
		}
		return fmt.Errorf("outside nginx fixture scope")
	}})
	if err != nil {
		t.Fatal(err)
	}
	upstream.Config.Handler = s.Handler()
	upstream.Start()
	defer upstream.Close()
	certificate, pool := nginxCertificate(t, root)
	configuration := fmt.Sprintf(`pid %q;
error_log %q crit;
events { worker_connections 64; }
http {
 access_log off;
 client_body_temp_path %q;
 proxy_temp_path %q;
 fastcgi_temp_path %q;
 uwsgi_temp_path %q;
 scgi_temp_path %q;
 server {
  listen %s ssl;
  server_name 127.0.0.1;
  ssl_certificate %q;
  ssl_certificate_key %q;
  ssl_protocols TLSv1.2 TLSv1.3;
  location = /ivoai/admin { return 308 /ivoai/admin/; }
  location /ivoai/admin/ {
   proxy_pass http://%s;
   proxy_set_header Host $http_host;
   proxy_set_header Forwarded "";
   proxy_set_header X-Forwarded-For "";
   proxy_set_header X-Forwarded-Host "";
   proxy_set_header X-Forwarded-Proto "";
   proxy_http_version 1.1;
   proxy_read_timeout 310s;
   proxy_buffering off;
   proxy_cache off;
   proxy_next_upstream off;
   client_max_body_size 64k;
  }
  location / { return 404; }
 }
}
`, filepath.Join(root, "nginx.pid"), filepath.Join(root, "error.log"), filepath.Join(root, "body"), filepath.Join(root, "proxy"), filepath.Join(root, "fastcgi"), filepath.Join(root, "uwsgi"), filepath.Join(root, "scgi"), frontAddress, certificate, filepath.Join(root, "key.pem"), upstream.Listener.Addr().String())
	conf := filepath.Join(root, "nginx.conf")
	if err := os.WriteFile(conf, []byte(configuration), 0600); err != nil {
		t.Fatal(err)
	}
	cleanEnv := []string{"PATH=/usr/bin:/bin", "HOME=" + root, "TMPDIR=" + root, "LANG=C.UTF-8"}
	validate := exec.Command(binary, "-t", "-p", root+"/", "-c", conf)
	validate.Env = cleanEnv
	if result, err := validate.CombinedOutput(); err != nil {
		t.Fatalf("isolated nginx -t failed: %v %s", err, result)
	}
	process := exec.Command(binary, "-p", root+"/", "-c", conf, "-g", "daemon off; master_process off;")
	process.Env = cleanEnv
	var diagnostics bytes.Buffer
	process.Stdout = &diagnostics
	process.Stderr = &diagnostics
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	t.Cleanup(func() {
		_ = process.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = process.Process.Kill()
			<-done
		}
	})
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	client.Jar, _ = cookiejar.New(nil)
	defer client.CloseIdleConnections()
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, e := client.Get(public + "/api/admin/v1/meta")
		if e == nil {
			resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatal("nginx boundary not mounted", resp.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("nginx did not start: %v", e)
		}
		time.Sleep(20 * time.Millisecond)
	}
	status, _, headers := request(t, client, "GET", public, "", "")
	if status != 308 || (headers.Get("Location") != "/ivoai/admin/" && headers.Get("Location") != public+"/") {
		t.Fatal("nginx prefix redirect lost", status, headers.Get("Location"))
	}
	for _, path := range []string{"/", "/assets/admin.js", "/assets/admin.css"} {
		status, _, h := request(t, client, "GET", public+path, "", "")
		if status != 200 || h.Get("Strict-Transport-Security") == "" {
			t.Fatal("nginx assets/TLS mount failed", path, status)
		}
	}
	activation, err := s.auth.CreateActivation(time.Minute, []string{ScopeRead, ScopeManage})
	if err != nil {
		t.Fatal(err)
	}
	status, _, headers = request(t, client, "GET", public+"/auth/start", "", "")
	if status != 303 {
		t.Fatal("nginx auth start failed", status)
	}
	authorize := headers.Get("Location")
	status, page, _ := request(t, client, "GET", authorize, "", "")
	match := csrfPattern.FindSubmatch(page)
	if status != 200 || len(match) != 2 {
		t.Fatal("nginx PKCE authorization form unavailable", status)
	}
	form := url.Values{"csrf": {string(match[1])}, "activation_code": {activation.Code}}.Encode()
	status, _, headers = request(t, client, "POST", authorize, form, publicOrigin)
	if status != 303 {
		t.Fatal("nginx OAuth authorize failed", status)
	}
	callback := headers.Get("Location")
	if !strings.HasPrefix(callback, public+"/auth/callback?") {
		t.Fatal("nginx callback escaped prefix")
	}
	status, _, headers = request(t, client, "GET", callback, "", "")
	if status != 303 || headers.Get("Location") != "/ivoai/admin/" {
		t.Fatal("nginx callback failed", status)
	}
	cookieFound := false
	for _, raw := range headers.Values("Set-Cookie") {
		c, err := http.ParseSetCookie(raw)
		if err != nil {
			t.Fatal(err)
		}
		if c.Name == s.cookie {
			cookieFound = true
			if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/ivoai/admin/" {
				t.Fatal("nginx session cookie boundary changed")
			}
		}
	}
	if !cookieFound {
		t.Fatal("missing nginx session cookie")
	}
	status, data, _ := request(t, client, "GET", public+"/api/admin/v1/csrf", "", "")
	var csrf struct {
		Token  string `json:"csrf_token"`
		Access string `json:"access"`
	}
	_ = json.Unmarshal(data, &csrf)
	if status != 200 || csrf.Token == "" || csrf.Access != "manage" {
		t.Fatal("nginx management scope unavailable")
	}
	post := func(body any, csrfToken, origin string) *http.Response {
		encoded, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", public+"/api/admin/v1/actions/config.auto", bytes.NewReader(encoded))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		req.Header.Set("X-IVOAI-CSRF", csrfToken)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	input := map[string]string{"value": "false"}
	previewBody := map[string]any{"phase": "preview", "input": input, "request_id": "nginx-real-confirmed-change"}
	for _, denial := range []struct{ csrf, origin string }{{"", publicOrigin}, {csrf.Token, "https://evil.example"}} {
		resp := post(previewBody, denial.csrf, denial.origin)
		resp.Body.Close()
		if resp.StatusCode != 403 || calls.Load() != 0 {
			t.Fatal("nginx CSRF/origin denial failed", resp.StatusCode)
		}
	}
	preview := post(previewBody, csrf.Token, publicOrigin)
	var receipt struct {
		Confirmation string `json:"confirmation"`
	}
	_ = json.NewDecoder(preview.Body).Decode(&receipt)
	preview.Body.Close()
	if preview.StatusCode != 200 || receipt.Confirmation == "" {
		t.Fatal("nginx preview unavailable")
	}
	execute := map[string]any{"phase": "execute", "input": input, "request_id": "nginx-real-confirmed-change", "confirmation": receipt.Confirmation}
	response := post(execute, csrf.Token, publicOrigin)
	var result struct {
		Status string                  `json:"status"`
		Ref    administrativeOutputRef `json:"output_ref"`
	}
	_ = json.NewDecoder(response.Body).Decode(&result)
	response.Body.Close()
	if response.StatusCode != 200 || result.Status != "completed" || result.Ref.DownloadURL == "" || calls.Load() != 1 {
		t.Fatal("nginx governed mutation/output failed", response.StatusCode, result.Status, calls.Load())
	}
	cfg, err := a.Store.Load()
	if err != nil || cfg.Orchestration.Auto.Enabled {
		t.Fatal("actual App change was not persisted", err)
	}
	download, err := client.Get(publicOrigin + result.Ref.DownloadURL)
	if err != nil {
		t.Fatal(err)
	}
	complete, err := io.ReadAll(download.Body)
	download.Body.Close()
	if err != nil || download.StatusCode != 200 || !bytes.Contains(complete, []byte("FINAL-NGINX-EVIDENCE")) {
		t.Fatal("nginx full output download failed")
	}
	replay := post(execute, csrf.Token, publicOrigin)
	replay.Body.Close()
	if replay.StatusCode != 409 || calls.Load() != 1 {
		t.Fatal("nginx replay invoked mutation")
	}
	status, _, _ = request(t, client, "GET", publicOrigin+"/api/admin/v1/meta", "", "")
	if status != 404 {
		t.Fatal("nginx leaked routes outside mount")
	}
	status, _, _ = request(t, client, "GET", public+"/api/admin/v1/overview", "", "https://evil.example")
	if status != 403 {
		t.Fatal("nginx cross-origin read admitted")
	}
	status, _, _ = request(t, client, "POST", public+"/auth/logout", "", publicOrigin)
	if status != 200 {
		t.Fatal("nginx logout failed", status)
	}
	revoked, err := client.Get(publicOrigin + result.Ref.DownloadURL)
	if err != nil {
		t.Fatal(err)
	}
	revoked.Body.Close()
	if revoked.StatusCode != 401 || calls.Load() != 1 {
		t.Fatal("nginx logout retained output authority")
	}
	if data, err := os.ReadFile(filepath.Join(root, "error.log")); err == nil && len(data) != 0 {
		t.Fatal("nginx emitted critical diagnostics")
	}
	version, err := exec.Command(binary, "-v").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("actual_proxy=%s TLS_verified=true prefix=/ivoai/admin OAuth_PKCE=true CSRF_origin_denial=true actual_App_config_mutation=true complete_sanitized_output_download=true replay_denied=true logout_revoked=true provider_inference=false", strings.TrimSpace(string(version)))
}

func nginxCertificate(t *testing.T, root string) (string, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "IVOAI isolated Nginx fixture"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	encoded := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	path := filepath.Join(root, "certificate.pem")
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "key.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(encoded) {
		t.Fatal("fixture certificate trust failed")
	}
	return path, pool
}
