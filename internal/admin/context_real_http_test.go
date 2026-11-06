package admin_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/cli"
	contextsvc "github.com/ivo-lopes/ivoai/internal/context"
	"github.com/ivo-lopes/ivoai/internal/server"
	"github.com/ivo-lopes/ivoai/internal/servercmd"
)

// This opt-in proof requires the real, pinned TEI/E5 and Qdrant in a disposable
// network-none Docker mesh. The HTTP helper dispatches cli.Run and keeps the
// actual ExecRunner; neither successful operations nor embeddings are mocked.
func TestAdministrativeContextRealHTTP(t *testing.T) {
	if os.Getenv("IVOAI_ADMIN_CONTEXT_REAL_STACK") != "1" {
		t.Skip("explicit private real TEI/Qdrant mesh required; absent is not certification")
	}
	requireAdministrativeContextMesh(t)
	if os.Geteuid() != 0 || os.Getenv("IVOAI_TEST_MODE") != "" || os.Getenv("IVOAI_SERVER_ROOT") != "" {
		t.Fatal("requires isolated mapped-root operator without test-mode or root override")
	}
	embeddingKey, qdrantKey := os.Getenv("API_KEY"), os.Getenv("QDRANT__SERVICE__API_KEY")
	if embeddingKey == "" || qdrantKey == "" {
		t.Fatal("private generated backend keys required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	started := time.Now()
	embedder := contextsvc.HTTPEmbedder{BaseURL: "http://127.0.0.1:8080", DimensionsN: 384, APIKey: embeddingKey}
	ready, cancelReady := context.WithTimeout(ctx, 90*time.Second)
	for {
		vectors, err := embedder.Embed(ready, []string{"query: private administrative Context readiness"})
		if err == nil && len(vectors) == 1 && len(vectors[0]) == 384 {
			break
		}
		select {
		case <-ready.Done():
			cancelReady()
			t.Fatal("real TEI embeddings unavailable")
		case <-time.After(500 * time.Millisecond):
		}
	}
	cancelReady()
	for _, probe := range []struct{ url, header, value string }{{"http://127.0.0.1:8080/embed", "Authorization", ""}, {"http://127.0.0.1:6333/collections", "api-key", ""}} {
		method, body := "GET", ""
		if strings.HasSuffix(probe.url, "/embed") {
			method, body = "POST", `{"inputs":["unauthorized"]}`
		}
		req, err := http.NewRequestWithContext(ctx, method, probe.url, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 && resp.StatusCode != 403 {
			t.Fatal("real backend admitted missing key", resp.StatusCode)
		}
	}
	// Fixed production paths exist only inside our separately mounted private mesh.
	layout := server.DefaultLayout("")
	if err := (server.Manager{Layout: layout, Architecture: "amd64"}).Setup(ctx); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"qdrant.env":     "QDRANT__SERVICE__API_KEY=" + qdrantKey + "\n",
		"embeddings.env": "API_KEY=" + embeddingKey + "\n",
		"memory.env":     "AI_MEMORY_AUTH_TOKEN=unused-private-memory-marker\n",
	} {
		if err := os.WriteFile(filepath.Join(layout.SecretsDir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join(layout.CorpusDir, "approved-admin-http")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	content := "# Administrative Context proof\n\nThe recovery codeword is ORQUIDEA-ADMIN-CONTEXT-175. Validate the backup before replacing a database. UTF-8: ação e recuperação.\n"
	if err := os.WriteFile(filepath.Join(source, "runbook.md"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".env"), []byte("EXCLUDED_PRIVATE_CANARY=never-index-this\n"), 0600); err != nil {
		t.Fatal(err)
	}
	connector, _ := json.Marshal([]map[string]string{{"name": "approved-admin-http", "kind": "filesystem", "path": source}})
	if err := os.WriteFile(filepath.Join(layout.ConfigDir, "connectors.json"), connector, 0600); err != nil {
		t.Fatal(err)
	}
	a := positiveAdministrativeApp(t) // isolates user paths and clears all API environment variables.
	cli.RegisterServerRunner(servercmd.New("0.17.0"))
	t.Cleanup(func() { cli.RegisterServerRunner(nil) })
	h := newPositiveAdministrativeHTTP(t, a) // actual OAuth/PKCE, cookie, Origin, CSRF and CLI dispatch.
	// Missing browser authentication and invalid CSRF must fail before CLI dispatch.
	request, err := http.NewRequestWithContext(ctx, "POST", h.server.URL+"/api/admin/v1/actions/server.context.ingest", strings.NewReader(`{"phase":"preview","input":{"name":"approved-admin-http"}}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", h.server.URL)
	response, err := h.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 || h.calls.Load() != 0 {
		t.Fatal("unauthenticated browser operation dispatched", response.StatusCode)
	}
	csrf := h.csrf
	h.csrf = "invalid"
	denied, _ := h.request("POST", "/api/admin/v1/actions/server.context.ingest", map[string]any{"phase": "preview", "input": map[string]string{"name": "approved-admin-http"}})
	h.csrf = csrf
	if denied != 403 || h.calls.Load() != 0 {
		t.Fatal("invalid CSRF operation dispatched", denied)
	}
	ingest := h.execute("server.context.ingest", map[string]string{"name": "approved-admin-http"}, "context-real-ingest")
	var receipt struct {
		Status    string                        `json:"status"`
		Documents int                           `json:"documents"`
		Exact     bool                          `json:"exact_recovery_verified"`
		Sync      string                        `json:"context_sync"`
		Verified  []struct{ ID, SHA256 string } `json:"verified_documents"`
	}
	if err := json.Unmarshal(administrativeContextOutput(t, ingest), &receipt); err != nil {
		t.Fatal("invalid CLI ingestion JSON", err)
	}
	sum := sha256.Sum256([]byte(content))
	digest := hex.EncodeToString(sum[:])
	if receipt.Status != "completed" || receipt.Documents != 1 || len(receipt.Verified) != 1 || !receipt.Exact || receipt.Sync != "PARTIAL" || receipt.Verified[0].SHA256 != digest {
		t.Fatal("ingestion receipt does not verify exact curated source")
	}
	docID := receipt.Verified[0].ID
	search := administrativeContextRead(t, h, "server.context.search", map[string]string{"query": "What is the recovery codeword and what must happen before replacing a database?"}, "context-real-search")
	var results struct {
		Results   []contextsvc.SearchResult `json:"results"`
		Untrusted bool                      `json:"untrusted"`
	}
	if err := json.Unmarshal(administrativeContextOutput(t, search), &results); err != nil {
		t.Fatal(err)
	}
	if !results.Untrusted || len(results.Results) != 1 || results.Results[0].Chunk.DocumentID != docID || !strings.Contains(results.Results[0].Chunk.Text, "ORQUIDEA-ADMIN-CONTEXT-175") || strings.Contains(results.Results[0].Chunk.Text, "CANARY") {
		t.Fatal("HTTP semantic search did not recover only the curated document")
	}
	document := administrativeContextRead(t, h, "server.context.document", map[string]string{"id": docID}, "context-real-document")
	var exact struct {
		Document  contextsvc.Document `json:"document"`
		SHA       string              `json:"content_sha256"`
		Untrusted bool                `json:"untrusted"`
	}
	if err := json.Unmarshal(administrativeContextOutput(t, document), &exact); err != nil {
		t.Fatal(err)
	}
	if exact.Document.Content != content || exact.SHA != digest || !exact.Untrusted {
		t.Fatal("HTTP exact retrieval changed source bytes")
	}
	if h.calls.Load() != 5 {
		t.Fatal("unexpected CLI dispatch count; possible replay", h.calls.Load())
	}
	report := map[string]any{"pass": true, "actions": []string{"server.context.ingest", "server.context.search", "server.context.document"}, "http_cli_core_dispatches": h.calls.Load(), "ingest_duplicate_request_status": 409, "readonly_repeat_status": 200, "unauthenticated_status": 401, "invalid_csrf_status": 403, "real_tei_embeddings": true, "embedding_dimensions": 384, "real_qdrant": true, "fake_runner": false, "deterministic_embedder": false, "documents": 1, "document_sha256": digest, "semantic_score": results.Results[0].Score, "exact_recovery": true, "receipt_context_sync": receipt.Sync, "mindsite_context_sync": "NOT_ASSERTED", "shared_context_accessed": false, "operator": "private mapped-root OS identity plus admin OAuth/browser controls", "seconds": time.Since(started).Seconds()}
	raw, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(filepath.Join(layout.DataDir, "admin-context-http-proof.json"), append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("real HTTP→CLI→Core→TEI/Qdrant completed3 actions with5 dispatches; mutation duplicate409, both authorized read repeats200; exactSHA%s", digest)
}

// Reads use the existing catalog contract: preview has no confirmation and an
// authorized repeat is allowed. Only mutable ingestion claims a NoReplay ID.
func administrativeContextRead(t *testing.T, h *positiveAdministrativeHTTP, id string, input map[string]string, requestID string) map[string]any {
	t.Helper()
	path := "/api/admin/v1/actions/" + id
	calls := h.calls.Load()
	status, preview := h.request("POST", path, map[string]any{"phase": "preview", "input": input})
	if status != 200 || preview["confirmation"] != nil || h.calls.Load() != calls {
		t.Fatal("read preview unexpectedly dispatched or required mutation approval", id, status)
	}
	body := map[string]any{"phase": "execute", "input": input, "request_id": requestID}
	status, result := h.request("POST", path, body)
	if status != 200 || result["status"] != "completed" {
		t.Fatalf("real read %s failed: HTTP%d status=%v error=%v dispatch=%v", id, status, result["status"], result["error"], h.lastError())
	}
	status, repeated := h.request("POST", path, body)
	if status != 200 || repeated["status"] != "completed" || h.calls.Load() != calls+2 {
		t.Fatal("authorized repeated read violated existing dispatch contract", id, status)
	}
	if string(administrativeContextOutput(t, result)) != string(administrativeContextOutput(t, repeated)) {
		t.Fatal("unchanged private corpus produced inconsistent repeated read", id)
	}
	return result
}

func administrativeContextOutput(t *testing.T, result map[string]any) []byte {
	t.Helper()
	out, ok := result["output"].(string)
	if !ok || out == "" {
		t.Fatal("administrative CLI output unavailable")
	}
	return []byte(out)
}

func requireAdministrativeContextMesh(t *testing.T) {
	t.Helper()
	const path = "/ivoai-private-context-boundary.json"
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatal("private boundary marker required before any server writes")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var marker struct {
		Nonce   string `json:"nonce"`
		Parent  string `json:"parent_network_namespace"`
		Private bool   `json:"private_disposable_fixture"`
	}
	if json.Unmarshal(raw, &marker) != nil || !marker.Private || len(marker.Nonce) != 24 || marker.Nonce != os.Getenv("IVOAI_PRIVATE_CONTEXT_NONCE") {
		t.Fatal("private mesh identity mismatch")
	}
	netns, err := os.Readlink("/proc/self/ns/net")
	if err != nil || marker.Parent == "" || netns == marker.Parent {
		t.Fatal("refuse host/parent network namespace")
	}
	// Absence of a complete private mount is fail-closed before fixed paths are touched.
	mounts, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/etc/ivoai", "/etc/systemd/system", "/var/lib/ivoai", "/opt/ivoai"} {
		found := false
		for _, line := range strings.Split(string(mounts), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 4 && fields[4] == path {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("dedicated disposable server mount missing", path)
		}
	}
}
