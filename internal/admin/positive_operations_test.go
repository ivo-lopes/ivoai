package admin_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/admin"
	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/cli"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/memory"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/policy"
	"github.com/ivo-lopes/ivoai/internal/skillcatalog"
	"github.com/ivo-lopes/ivoai/internal/skills"
	"github.com/ivo-lopes/ivoai/internal/skillupdate"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
	"github.com/ivo-lopes/ivoai/internal/webauth"
)

func positiveAdministrativeApp(t *testing.T) *app.App {
	t.Helper()
	root := t.TempDir()
	for _, key := range []string{"HOME", "CODEX_HOME", "CLAUDE_CONFIG_DIR", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, filepath.Join(root, key))
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("AI_MEMORY_DATA_DIR", filepath.Join(root, "memory-data"))
	t.Setenv("IVOAI_TEST_MODE", "")
	for _, key := range []string{"IVOAI_SERVER_ROOT", "AI_MEMORY_SERVER_URL", "AI_MEMORY_AUTH_TOKEN", "API_KEY", "QDRANT__SERVICE__API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "GEMINI_API_KEY"} {
		t.Setenv(key, "")
	}
	a, err := app.New("0.17.0", strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	a.Runner = platform.ExecRunner{}
	cfg := config.Default()
	cfg.Memory.Enabled = false
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	return a
}

type positiveAdministrativeHTTP struct {
	t            *testing.T
	server       *httptest.Server
	cookie, csrf string
	calls        atomic.Int64
	errMu        sync.Mutex
	dispatchErr  error
}

func newPositiveAdministrativeHTTP(t *testing.T, a *app.App) *positiveAdministrativeHTTP {
	t.Helper()
	srv := httptest.NewUnstartedServer(nil)
	origin := "http://" + srv.Listener.Addr().String()
	h := &positiveAdministrativeHTTP{t: t, server: srv}
	svc, err := admin.New(admin.Options{App: a, Origin: origin, Dispatch: func(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) error {
		h.calls.Add(1)
		operation := *a
		operation.In, operation.Out, operation.Err = in, out, errOut
		err := cli.Run(ctx, &operation, args)
		h.errMu.Lock()
		h.dispatchErr = err
		h.errMu.Unlock()
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	srv.Config.Handler = svc.Handler()
	srv.Start()
	t.Cleanup(srv.Close)
	auth, err := admin.AccessStore(a.Store.Paths)
	if err != nil {
		t.Fatal(err)
	}
	activation, err := auth.CreateActivation(time.Minute, []string{admin.ScopeRead, admin.ScopeManage})
	if err != nil {
		t.Fatal(err)
	}
	redirect := origin + "/auth/callback"
	client, err := auth.RegisterClient("isolated positive operations", []string{redirect})
	if err != nil {
		t.Fatal(err)
	}
	verifier := strings.Repeat("v", 43)
	nonce, err := auth.BeginAuthorization(client.ID, redirect, webauth.PKCEChallenge(verifier), "positive", svc.Resource(), []string{admin.ScopeRead, admin.ScopeManage})
	if err != nil {
		t.Fatal(err)
	}
	code, _, _, err := auth.AuthorizeRequest(activation.Code, nonce)
	if err != nil {
		t.Fatal(err)
	}
	token, err := auth.ExchangeCode(code, client.ID, redirect, verifier, svc.Resource())
	if err != nil {
		t.Fatal(err)
	}
	httpClient := srv.Client()
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := httpClient.Get(origin + "/auth/start")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatal("flow cookie missing")
	}
	h.cookie = strings.TrimSuffix(cookies[0].Name, "_flow") + "=" + token.AccessToken
	status, value := h.request("GET", "/api/admin/v1/csrf", nil)
	if status != 200 {
		t.Fatal("CSRF admission failed", status)
	}
	h.csrf, _ = value["csrf_token"].(string)
	if h.csrf == "" {
		t.Fatal("missing CSRF")
	}
	return h
}

func (h *positiveAdministrativeHTTP) request(method, path string, value any) (int, map[string]any) {
	h.t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		h.t.Fatal(err)
	}
	req, err := http.NewRequest(method, h.server.URL+path, bytes.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", h.server.URL)
	req.Header.Set("Cookie", h.cookie)
	req.Header.Set("X-IVOAI-CSRF", h.csrf)
	resp, err := h.server.Client().Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		h.t.Fatal("invalid response", resp.StatusCode, err)
	}
	return resp.StatusCode, result
}
func (h *positiveAdministrativeHTTP) execute(id string, input map[string]string, requestID string) map[string]any {
	return h.executeOutcome(id, input, requestID, "completed")
}
func (h *positiveAdministrativeHTTP) lastError() error {
	h.errMu.Lock()
	defer h.errMu.Unlock()
	return h.dispatchErr
}
func (h *positiveAdministrativeHTTP) executeOutcome(id string, input map[string]string, requestID, expected string) map[string]any {
	h.t.Helper()
	path := "/api/admin/v1/actions/" + id
	status, preview := h.request("POST", path, map[string]any{"phase": "preview", "input": input})
	token, _ := preview["confirmation"].(string)
	if status != 200 || token == "" {
		h.t.Fatal("preview rejected", id, status, preview["error"])
	}
	body := map[string]any{"phase": "execute", "input": input, "request_id": requestID, "confirmation": token}
	status, result := h.request("POST", path, body)
	if status != 200 || result["status"] != expected {
		h.t.Fatalf("actual operation %s failed: HTTP%d status=%v error=%v output=%v", id, status, result["status"], result["error"], fmt.Sprint(result["output"], "; dispatch=", h.lastError()))
	}
	calls := h.calls.Load()
	status, _ = h.request("POST", path, body)
	if status != 409 || h.calls.Load() != calls {
		h.t.Fatal("replay dispatched", id, status, h.calls.Load(), calls)
	}
	return result
}

func TestAdministrativePositiveCapabilityRollbackWithPriorRelease(t *testing.T) {
	a := positiveAdministrativeApp(t)
	raw, err := os.ReadFile("testdata/ponytail-prior-release/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog skillcatalog.Catalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Validate(); err != nil {
		t.Fatal(err)
	}
	old := skillcatalog.LocalSource{Source: catalog.Sources[0], Files: os.DirFS("testdata/ponytail-prior-release/files"), Paths: []string{"LICENSE", "skills/ponytail/SKILL.md"}}
	supply := supplychain.Manager{Root: filepath.Join(a.Store.Paths.DataDir, "supply-chain")}
	manager := skillupdate.Manager{Supply: supply, Registry: skills.Store{Path: skills.RegistryPath(a.Store.Paths.StateDir)}, Discoverer: old, Fetcher: old, Classifier: skillcatalog.NativeClassifier{}, Policy: policy.DefaultEngine(), CatalogOnly: true, ReconcileMissingIndex: true}
	if _, err := manager.Update(context.Background(), supplychain.Reference{ID: "ponytail", Kind: supplychain.KindSkill, Source: old.Source.Upstream.Repository}); err != nil {
		t.Fatal(err)
	}
	h := newPositiveAdministrativeHTTP(t, a)
	h.execute("capability.update", map[string]string{"source": "ponytail"}, "positive-update-ponytail")
	current, _, err := supply.Active("ponytail")
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision == old.Source.Provenance.Revision {
		t.Fatal("HTTP update did not activate current revision")
	}
	h.execute("capability.rollback", map[string]string{"source": "ponytail"}, "positive-rollback-ponytail")
	restored, _, err := supply.Active("ponytail")
	if err != nil || restored.Revision != old.Source.Provenance.Revision {
		t.Fatal("HTTP rollback did not restore authentic prior revision", err)
	}
	if err := manager.ValidateConsistency(context.Background(), "ponytail"); err != nil {
		t.Fatal("rollback registry/active object diverged", err)
	}
	t.Logf("real HTTP+CLI+Core rollback restored reviewed prior Ponytail %s from current %s; no network/provider process; duplicate request=409", restored.Revision, current.Revision)
}

func reviewedAdminBinary(t *testing.T, key, expected string) string {
	t.Helper()
	path := os.Getenv(key)
	if path == "" {
		t.Skip(key + " requires an explicitly reviewed real binary; absent is not execution certification")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != expected {
		t.Fatal("real binary integrity mismatch", key)
	}
	return path
}

func TestAdministrativePositiveRepairHooksOfficialBinary(t *testing.T) {
	binary := reviewedAdminBinary(t, "IVOAI_ADMIN_REAL_MEMORY_BINARY", "5a8e2c51cb09dadfc157601772b32c79ee63a6cbf9f05caf59c67da385bae6c1")
	a := positiveAdministrativeApp(t)
	cfg, err := a.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Memory.Enabled = true
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	state, err := a.Store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Components == nil {
		state.Components = map[string]config.ComponentState{}
	}
	state.Components["ai-memory"] = config.ComponentState{Version: "3.4.2", Path: binary, Installed: true, Managed: true}
	if err := a.Store.SaveState(state); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(filepath.Dir(filepath.Dir(binary)), "ai-memory-linux-x86_64.tar.gz")
	assets := verifiedAdministrativeHookAssets(t, archive)
	if err := memory.InstallHookAssets(assets, a.Store.Paths.HooksDir); err != nil {
		t.Fatal(err)
	}
	registration := memory.HookRegistrationPaths()["codex"]
	if err := os.MkdirAll(filepath.Dir(registration), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registration, []byte(`{"foreign_setting":"preserve","hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"printf foreign-never-executed"}]}]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	manager := memory.Manager{Runner: platform.ExecRunner{}, HooksDir: a.Store.Paths.HooksDir, Binary: binary, Out: io.Discard, Err: io.Discard}
	if err := manager.ConfigureHooks(context.Background(), "http://127.0.0.1:1", ""); err != nil {
		t.Fatal(err)
	}
	registrationBefore, err := os.ReadFile(registration)
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		Files []struct {
			Path string
			Body []byte
			Mode uint32
		}
	}
	receiptBody, err := os.ReadFile(filepath.Join(a.Store.Paths.HooksDir, ".ivoai-hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(receiptBody, &receipt); err != nil || len(receipt.Files) == 0 {
		t.Fatal("actual renderer did not create hook receipt", err)
	}
	target := filepath.Join(a.Store.Paths.HooksDir, receipt.Files[0].Path)
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	h := newPositiveAdministrativeHTTP(t, a)
	h.execute("doctor.repair-hooks", map[string]string{}, "positive-official-hook-repair")
	restored, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(restored, receipt.Files[0].Body) {
		t.Fatal("HTTP did not restore receipt-owned wrapper exactly", err)
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != os.FileMode(receipt.Files[0].Mode) {
		t.Fatal("repaired wrapper permissions mismatch", err)
	}
	registrationAfter, err := os.ReadFile(registration)
	if err != nil || !bytes.Equal(registrationBefore, registrationAfter) || !bytes.Contains(registrationAfter, []byte("foreign-never-executed")) {
		t.Fatal("repair changed foreign registration", err)
	}
	foreign := []byte("#!/bin/sh\nprintf foreign-never-executed\n")
	if err := os.WriteFile(target, foreign, 0700); err != nil {
		t.Fatal(err)
	}
	h.executeOutcome("doctor.repair-hooks", map[string]string{}, "negative-official-hook-foreign-edit", "failed")
	retained, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(retained, foreign) || h.lastError() == nil || !strings.Contains(h.lastError().Error(), "modified") {
		t.Fatal("foreign hook edit was not safely refused", err)
	}
	t.Log(fmt.Sprintf("real HTTP+CLI+Core repair restored missing receipt-owned hook using official ai-memory3.4.2; wrapper=%s; no hook execution or personal settings", filepath.Base(target)))
}

// Serve the already verified private candidate bytes over actual loopback HTTP.
// No provider/command success is mocked: Prepare checks archive integrity and
// executes the real candidate compatibility preflight. This is not GitHub access
// or certification of a final published tag artifact.
type privateReleaseTransport struct {
	origin           string
	allowedLocalHost string
	transport        http.RoundTripper
}

func (p privateReleaseTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	path := ""
	if r.URL.Host == p.allowedLocalHost {
		return p.transport.RoundTrip(r)
	}
	if r.URL.Host == "api.github.com" && r.URL.Path == "/repos/ivo-lopes/ivoai/releases/latest" {
		path = "/latest"
	}
	prefix := "/ivo-lopes/ivoai/releases/download/v0.17.0/"
	if r.URL.Host == "github.com" && strings.HasPrefix(r.URL.Path, prefix) {
		name := strings.TrimPrefix(r.URL.Path, prefix)
		if name == "checksums.txt" || name == "ivoai_linux_amd64.tar.gz" {
			path = "/" + name
		}
	}
	if path == "" {
		return nil, fmt.Errorf("unapproved external request blocked")
	}
	copy := r.Clone(r.Context())
	destination, err := http.NewRequestWithContext(r.Context(), r.Method, p.origin+path, r.Body)
	if err != nil {
		return nil, err
	}
	copy.URL = destination.URL
	copy.Host = destination.Host
	return p.transport.RoundTrip(copy)
}

func TestAdministrativePositiveUpdateDryRunRealCandidate(t *testing.T) {
	previous := reviewedAdminBinary(t, "IVOAI_ADMIN_REAL_V016_BINARY", "a933d4d313930066409c77682a8cc26b574b5c259174e1cb68fac6b890c9d2ef")
	_ = reviewedAdminBinary(t, "IVOAI_ADMIN_REAL_CANDIDATE", "a133ef9439e4fbac0dd55ef744824167c6b506cdd0abba3b4f8766311b5a76f4")
	assets := os.Getenv("IVOAI_ADMIN_REAL_CANDIDATE_ASSETS")
	if assets == "" {
		t.Fatal("explicit reviewed candidate asset directory required")
	}
	archive, err := os.ReadFile(filepath.Join(assets, "ivoai_linux_amd64.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	archiveSum := sha256.Sum256(archive)
	if hex.EncodeToString(archiveSum[:]) != "b80d694e38ba640bc1ae6ba0f6ed6f717cfad8ae4cf3d78a8d25568c7c0a4073" {
		t.Fatal("private reviewed archive identity mismatch")
	}
	// The mirror's checksum is derived from the independently pinned private
	// archive. This is an isolated release-protocol fixture, not a publisher
	// signature or a claim that the private candidate is the published tag.
	checksum := []byte(fmt.Sprintf("%x  ivoai_linux_amd64.tar.gz\n", archiveSum))
	a := positiveAdministrativeApp(t)
	a.Version = "0.16.0"
	installed := filepath.Join(t.TempDir(), "ivoai")
	oldBytes, err := os.ReadFile(previous)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, oldBytes, 0700); err != nil {
		t.Fatal(err)
	}
	// The existing guarded test-only executable selector keeps the Go test
	// harness from treating itself as the installed Core. It does not bypass
	// archive checksum, real candidate execution or compatibility validation.
	t.Setenv("IVOAI_TEST_MODE", "1")
	t.Setenv("IVOAI_SERVER_ROOT", filepath.Join(t.TempDir(), "no-server"))
	a.ExecutablePath = installed
	if err := a.Store.SaveState(config.State{Schema: config.StateSchemaVersion, Components: map[string]config.ComponentState{}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.SaveOwnership(config.Ownership{Schema: config.OwnershipSchemaVersion, Components: map[string]config.OwnedItem{}}); err != nil {
		t.Fatal(err)
	}
	requested := map[string]int{}
	var requestedMu sync.Mutex
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedMu.Lock()
		requested[r.URL.Path]++
		currentChecksum := append([]byte(nil), checksum...)
		requestedMu.Unlock()
		switch r.URL.Path {
		case "/latest":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"tag_name":"v0.17.0","html_url":"https://example.invalid/private-reviewed-candidate"}`)
		case "/checksums.txt":
			_, _ = w.Write(currentChecksum)
		case "/ivoai_linux_amd64.tar.gz":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer mirror.Close()
	h := newPositiveAdministrativeHTTP(t, a)
	oldTransport := http.DefaultTransport
	local := http.DefaultTransport.(*http.Transport).Clone()
	local.Proxy = nil
	http.DefaultTransport = privateReleaseTransport{origin: mirror.URL, allowedLocalHost: strings.TrimPrefix(h.server.URL, "http://"), transport: local}
	t.Cleanup(func() { http.DefaultTransport = oldTransport; local.CloseIdleConnections() })
	paths := []string{a.Store.Paths.Config, a.Store.Paths.State, a.Store.Paths.Ownership, installed}
	before := map[string][]byte{}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = body
	}
	h.execute("update.dry-run", map[string]string{}, "positive-real-candidate-dry-run")
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(body, before[path]) {
			t.Fatal("dry-run changed managed bytes", filepath.Base(path), err)
		}
	}
	requestedMu.Lock()
	for _, path := range []string{"/latest", "/checksums.txt", "/ivoai_linux_amd64.tar.gz"} {
		if requested[path] != 1 {
			t.Fatal("unexpected release HTTP call count", path, requested[path])
		}
	}
	checksum = []byte(strings.Repeat("0", 64) + "  ivoai_linux_amd64.tar.gz\n")
	requestedMu.Unlock()
	h.executeOutcome("update.dry-run", map[string]string{}, "negative-real-candidate-checksum", "failed")
	if h.lastError() == nil || !strings.Contains(h.lastError().Error(), "checksum mismatch") {
		t.Fatal("corrupted checksum was not refused", h.lastError())
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(body, before[path]) {
			t.Fatal("failed dry-run changed managed bytes", filepath.Base(path), err)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(installed))
	if err != nil || len(entries) != 1 || entries[0].Name() != "ivoai" {
		t.Fatal("candidate staging survived dry-run", err)
	}
	t.Log("real HTTP+CLI+Core dry-run verified real private0.17 candidate and executed metadata; originalv0.16binary/config/state/ownership exact; no install, publication or repeated dispatch")
}

func verifiedAdministrativeHookAssets(t *testing.T, archive string) string {
	t.Helper()
	body, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != "15a4f4a346a27e6fd63a36d1b3de23c5cbe142b80b3bce561e21b1f225a1e645" {
		t.Fatal("official ai-memory archive integrity mismatch")
	}
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	root := t.TempDir()
	count := 0
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimPrefix(header.Name, "./")
		if !strings.HasPrefix(name, "hooks/") || header.Typeflag == tar.TypeDir {
			continue
		}
		rel := strings.TrimPrefix(name, "hooks/")
		if !filepath.IsLocal(rel) || header.Typeflag != tar.TypeReg || header.Size > 4<<20 {
			t.Fatal("unsupported verified hook entry")
		}
		target := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(io.LimitReader(reader, header.Size+1))
		if err != nil || int64(len(data)) != header.Size {
			t.Fatal("invalid verified hook size", err)
		}
		if err := os.WriteFile(target, data, os.FileMode(header.Mode)&0777); err != nil {
			t.Fatal(err)
		}
		count++
	}
	if count == 0 {
		t.Fatal("official hook assets absent")
	}
	return root
}
