package servercmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contextsvc "github.com/ivo-lopes/ivoai/internal/context"
	"github.com/ivo-lopes/ivoai/internal/enrollment"
	"github.com/ivo-lopes/ivoai/internal/gateway"
	"github.com/ivo-lopes/ivoai/internal/server"
)

// Opt-in actual stack proof. Run only inside the unique disposable network-none
// Docker mesh, with pinned real TEI/E5 and Qdrant. No deterministic embedder is
// used here, and the shared Mindsite Context is never addressed.
func TestContextPrivateRealStack(t *testing.T) {
	if os.Getenv("IVOAI_CONTEXT_PRIVATE_REAL_STACK") != "1" {
		t.Skip("explicit private real TEI/Qdrant mesh required")
	}
	requirePrivateContextMesh(t)
	if os.Geteuid() != 0 || os.Getenv("IVOAI_TEST_MODE") != "" || os.Getenv("IVOAI_SERVER_ROOT") != "" {
		t.Fatal("requires mapped-root operator and production CLI contracts, without test-mode/root overrides")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	started := time.Now()
	report := map[string]any{"pass": false, "real_tei_embeddings": true, "deterministic_embedder": false, "real_qdrant": true, "production_or_shared_context_accessed": false, "mindsite_context_sync": "NOT_ASSERTED", "operator_authority": "existing mapped-root OS identity", "gateway_auth": "existing enrollment bearer/context:read", "cgroup_limits_applied": false}
	defer func() {
		report["seconds"] = time.Since(started).Seconds()
		report["pass"] = !t.Failed()
		body, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile("/var/lib/ivoai/private-context-proof.json", append(body, '\n'), 0600); err != nil {
			t.Error(err)
		}
	}()
	layout := server.DefaultLayout("")
	if err := (server.Manager{Layout: layout, Architecture: "amd64"}).Setup(ctx); err != nil {
		t.Fatal("private managed asset setup", err)
	}
	for name, body := range map[string]string{
		"qdrant.env":     "QDRANT__SERVICE__API_KEY=" + os.Getenv("QDRANT__SERVICE__API_KEY") + "\n",
		"embeddings.env": "API_KEY=" + os.Getenv("API_KEY") + "\n",
		"memory.env":     "AI_MEMORY_AUTH_TOKEN=private-unused-memory-service-fixture-marker\n",
	} {
		if err := os.WriteFile(filepath.Join(layout.SecretsDir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join(layout.CorpusDir, "approved")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	content := "# Private Context navigation\n\nThe release recovery codeword is ORQUIDEA-CONTEXT-175. During an incident, validate the backup before replacing a database. UTF-8 verification: ação e recuperação.\n"
	if err := os.WriteFile(filepath.Join(source, "runbook.md"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".env"), []byte("PRIVATE_CANARY_EXCLUDED=never-index-this\n"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(layout.CorpusDir, "outside-canary.md")
	if err := os.WriteFile(outside, []byte("FOREIGN_PATH_CANARY_NEVER_INDEX"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(source, "escape.md")
	if existing, err := os.Readlink(link); err == nil {
		if existing != outside {
			t.Fatal("unexpected fixture symlink target")
		}
	} else if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := saveConnectors(layout, []connectorRecord{{Name: "approved", Kind: "filesystem", Path: source}}); err != nil {
		t.Fatal(err)
	}
	cli := func(args ...string) ([]byte, error) {
		c := exec.CommandContext(ctx, "/proof/ivoai-context-real", append([]string{"server", "context"}, args...)...)
		c.Env = os.Environ()
		return c.CombinedOutput()
	}
	raw, err := cli("ingest", "--connector", "approved")
	if err != nil {
		t.Fatalf("real administrative ingest failed: %v %s", err, raw)
	}
	var receipt ingestionReceipt
	if json.Unmarshal(raw, &receipt) != nil || receipt.Status != "completed" || receipt.Documents != 1 || len(receipt.Verified) != 1 || !receipt.ExactRecoveryVerified || receipt.ContextSync != "PARTIAL" {
		t.Fatalf("invalid real ingestion receipt %s", raw)
	}
	docID := receipt.Verified[0].ID
	sum := sha256.Sum256([]byte(content))
	expectedSHA := hex.EncodeToString(sum[:])
	if receipt.Verified[0].SHA256 != expectedSHA {
		t.Fatal("receipt lost authoritative bytes")
	}
	report["documents"] = receipt.Documents
	report["document_sha256"] = expectedSHA
	report["receipt_context_sync"] = receipt.ContextSync
	raw, err = cli("search", "--query", "What is the release recovery codeword and the first action before replacing a database?")
	if err != nil {
		t.Fatalf("real semantic search failed: %v %s", err, raw)
	}
	var searched struct {
		Results   []contextsvc.SearchResult `json:"results"`
		Untrusted bool                      `json:"untrusted"`
	}
	if json.Unmarshal(raw, &searched) != nil || !searched.Untrusted || len(searched.Results) == 0 || searched.Results[0].Chunk.DocumentID != docID || !strings.Contains(searched.Results[0].Chunk.Text, "ORQUIDEA-CONTEXT-175") {
		t.Fatalf("semantic search did not recover the ingested source: %s", raw)
	}
	report["semantic_search_verified"] = true
	report["semantic_top_score"] = searched.Results[0].Score
	raw, err = cli("get", "--id", docID)
	if err != nil {
		t.Fatalf("real exact get failed: %v %s", err, raw)
	}
	var got struct {
		Document  contextsvc.Document `json:"document"`
		SHA       string              `json:"content_sha256"`
		Untrusted bool                `json:"untrusted"`
	}
	if json.Unmarshal(raw, &got) != nil || got.Document.Content != content || got.SHA != expectedSHA || !got.Untrusted {
		t.Fatal("exact CLI get changed source bytes")
	}
	report["exact_cli_recovery"] = true
	svc, err := contextService(layout)
	if err != nil {
		t.Fatal(err)
	}
	if err = addConfiguredConnectors(svc, layout); err != nil {
		t.Fatal(err)
	}
	// Separate actual collection/catalog proves that main retrieval cannot adopt
	// another backend's document simply because the shared vector engine has it.
	foreignDir := filepath.Join(layout.CorpusDir, "foreign")
	if err = os.MkdirAll(foreignDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(foreignDir, "foreign.md"), []byte("FOREIGN_COLLECTION_CANARY_BROMELIA"), 0600); err != nil {
		t.Fatal(err)
	}
	foreign, err := contextsvc.NewService(svc.Service.Embedder, contextsvc.QdrantStore{BaseURL: qdrantURL, Collection: "private-foreign-context", APIKey: os.Getenv("QDRANT__SERVICE__API_KEY")}, &contextsvc.FileCatalog{Path: filepath.Join(layout.ContextDir, "foreign-catalog.json")})
	if err != nil {
		t.Fatal(err)
	}
	if err = foreign.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err = foreign.AddConnector(contextsvc.FilesystemConnector{ConnectorName: "foreign", Root: foreignDir}); err != nil {
		t.Fatal(err)
	}
	if _, err = foreign.Ingest(ctx, "foreign"); err != nil {
		t.Fatal(err)
	}
	docs, err := foreign.Recent(1)
	if err != nil || len(docs) != 1 {
		t.Fatal("foreign control unavailable", err)
	}
	if _, found, err := svc.GetDocument(docs[0].ID); err != nil || found {
		t.Fatal("foreign document entered main catalog")
	}
	local, err := svc.Search(ctx, "FOREIGN_COLLECTION_CANARY_BROMELIA", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range local {
		if strings.Contains(r.Chunk.Text, "CANARY") || strings.Contains(r.Chunk.Text, "never-index-this") {
			t.Fatal("excluded/foreign content leaked through main search")
		}
	}
	report["source_path_filter_and_collection_isolation"] = true
	store := enrollment.NewStore(filepath.Join(layout.DataDir, "enrollment", "private-proof.json"))
	createClient := func(scopes []enrollment.Scope) enrollment.ClientCredential {
		e, err := store.Create(10*time.Minute, scopes)
		if err != nil {
			t.Fatal(err)
		}
		c, err := store.ConsumeScoped(e.Code, "private-context-proof", scopes)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	reader := createClient([]enrollment.Scope{enrollment.ScopeContextRead})
	wrong := createClient([]enrollment.Scope{enrollment.ScopeStatusRead})
	g, err := gateway.New(gateway.Config{ServerVersion: "private-context-proof", Context: svc, Enrollments: store})
	if err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(g.Handler())
	defer endpoint.Close()
	call := func(token, name string, args any) (int, []byte) {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
		req, err := http.NewRequestWithContext(ctx, "POST", endpoint.URL+"/v1/mcp/context", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, raw
	}
	if status, _ := call("", "context_search", map[string]any{"query": "recovery"}); status != 401 {
		t.Fatal("unauthenticated Context request admitted", status)
	}
	if status, _ := call(wrong.Token, "context_search", map[string]any{"query": "recovery"}); status != 403 {
		t.Fatal("wrong scope admitted", status)
	}
	status, body := call(reader.Token, "context_get_document", map[string]any{"id": docID})
	if status != 200 || !bytes.Contains(body, []byte("ORQUIDEA-CONTEXT-175")) {
		t.Fatal("authorized exact MCP recovery failed", status)
	}
	if status, body = call(reader.Token, "context_ingest", map[string]any{"connector": "approved"}); status != 200 || !bytes.Contains(body, []byte("unknown read-only tool")) {
		t.Fatal("agent-facing MCP exposed mutation", status)
	}
	if err = store.RevokeClient(reader.ClientID); err != nil {
		t.Fatal(err)
	}
	if status, _ := call(reader.Token, "context_get_document", map[string]any{"id": docID}); status != 401 {
		t.Fatal("revoked credential accepted", status)
	}
	report["gateway_unauthenticated_status"] = 401
	report["gateway_wrong_scope_status"] = 403
	report["gateway_revoked_status"] = 401
	report["mcp_mutation_denied"] = true
	report["cpu_features_observed"] = privateContextCPUFeatures()
}

func privateContextCPUFeatures() []string {
	raw, _ := os.ReadFile("/proc/cpuinfo")
	observed := []string{}
	for _, feature := range []string{"avx", "avx2", "sse4_2"} {
		if strings.Contains(" "+string(raw)+" ", " "+feature+" ") {
			observed = append(observed, feature)
		}
	}
	return observed
}

func TestContextPrivateStackReady(t *testing.T) {
	if os.Getenv("IVOAI_CONTEXT_PRIVATE_REAL_STACK") != "1" {
		t.Skip("explicit isolated real stack required")
	}
	requirePrivateContextMesh(t)
	if len(os.Getenv("API_KEY")) < 32 || len(os.Getenv("QDRANT__SERVICE__API_KEY")) < 32 {
		t.Fatal("private transient backend credentials required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	embedder := contextsvc.HTTPEmbedder{BaseURL: embeddingURL, DimensionsN: 384, APIKey: os.Getenv("API_KEY"), Client: &http.Client{Timeout: 3 * time.Second}}
	for {
		vectors, err := embedder.EmbedDocuments(ctx, []string{"Private real-model startup readiness."})
		if err == nil && len(vectors) == 1 && len(vectors[0]) == 384 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("real TEI did not become ready before the deadline")
		case <-time.After(500 * time.Millisecond):
		}
	}
	request, err := http.NewRequestWithContext(ctx, "POST", embeddingURL+"/embed", strings.NewReader(`{"inputs":["unauthorized probe"]}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 && response.StatusCode != 403 {
		t.Fatal("TEI did not reject missing backend credentials", response.StatusCode)
	}
	if err := internalQdrantUpgradeProbe(ctx); err != nil {
		t.Fatal("real Qdrant authentication/readiness", err)
	}
	infoReq, err := http.NewRequestWithContext(ctx, "GET", embeddingURL+"/info", nil)
	if err != nil {
		t.Fatal(err)
	}
	infoReq.Header.Set("Authorization", "Bearer "+os.Getenv("API_KEY"))
	infoResponse, err := http.DefaultClient.Do(infoReq)
	if err != nil {
		t.Fatal(err)
	}
	var info map[string]any
	if err = json.NewDecoder(io.LimitReader(infoResponse.Body, 65536)).Decode(&info); err != nil || infoResponse.StatusCode != 200 {
		infoResponse.Body.Close()
		t.Fatal("TEI runtime info unavailable", err)
	}
	infoResponse.Body.Close()
	selected := map[string]any{}
	for _, key := range []string{"version", "sha", "docker_label", "model_id", "model_sha", "model_dtype"} {
		selected[key] = info[key]
	}
	encoded, _ := json.MarshalIndent(selected, "", "  ")
	if err = os.WriteFile("/var/lib/ivoai/tei-runtime-info.json", append(encoded, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{embeddingURL + "/health", embeddingURL + "/metrics"} {
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+os.Getenv("API_KEY"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal("TEI health/metrics unavailable", err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatal("TEI health/metrics status", resp.StatusCode)
		}
	}
}

// Require a host-created read-only marker AND a distinct network namespace
// before any fixed production path can be touched by this explicit opt-in test.
func requirePrivateContextMesh(t *testing.T) {
	t.Helper()
	info, err := os.Lstat("/ivoai-private-context-boundary.json")
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatal("private Docker boundary marker required before server writes")
	}
	raw, err := os.ReadFile("/ivoai-private-context-boundary.json")
	if err != nil {
		t.Fatal(err)
	}
	var marker struct {
		Nonce         string `json:"nonce"`
		ParentNetwork string `json:"parent_network_namespace"`
		Private       bool   `json:"private_disposable_fixture"`
	}
	if json.Unmarshal(raw, &marker) != nil || !marker.Private || len(marker.Nonce) != 24 || marker.Nonce != os.Getenv("IVOAI_PRIVATE_CONTEXT_NONCE") {
		t.Fatal("private Docker fixture identity mismatch")
	}
	network, err := os.Readlink("/proc/self/ns/net")
	if err != nil || marker.ParentNetwork == "" || network == marker.ParentNetwork {
		t.Fatal("private test cannot use the parent/host network namespace")
	}
}

// Explicit operator reset after a preserved failed epoch; never called by ingest
// or the normal proof. It only addresses the fixed private mesh collection, and
// refuses to delete it if the authoritative catalog already has documents.
func TestContextPrivateResetFailedCollection(t *testing.T) {
	if os.Getenv("IVOAI_CONTEXT_PRIVATE_RESET_FAILED") != "1" {
		t.Skip("explicit failed private epoch reset required")
	}
	requirePrivateContextMesh(t)
	svc, err := contextService(server.DefaultLayout(""))
	if err != nil {
		t.Fatal(err)
	}
	docs, err := svc.Recent(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) > 0 {
		t.Fatal("refuse reset of populated authoritative catalog")
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, qdrantURL+"/collections/ivoai-context-v1-d384", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("api-key", os.Getenv("QDRANT__SERVICE__API_KEY"))
	client := &http.Client{Timeout: time.Minute}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("reset rejected", response.StatusCode)
	}
}
