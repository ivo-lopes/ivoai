package context

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type mutableConnector struct {
	name string
	docs []Document
}

func (c *mutableConnector) Name() string { return c.name }
func (c *mutableConnector) Documents(context.Context) ([]Document, error) {
	return append([]Document(nil), c.docs...), nil
}

func TestFilesystemConnectorFiltersSensitiveBinaryAndSymlink(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "readme.md"), []byte("safe knowledge"))
	mustWrite(t, filepath.Join(root, ".env"), []byte("TOKEN=secret"))
	mustWrite(t, filepath.Join(root, "private.pem"), []byte("secret"))
	mustWrite(t, filepath.Join(root, "binary.bin"), []byte{0, 1, 2})
	outside := filepath.Join(t.TempDir(), "outside.txt")
	mustWrite(t, outside, []byte("must not ingest"))
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil && runtime.GOOS != "windows" {
		t.Fatal(err)
	}
	docs, err := (FilesystemConnector{Root: root}).Documents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Path != "readme.md" || docs[0].Content != "safe knowledge" {
		t.Fatalf("unexpected documents: %#v", docs)
	}
}

func TestFilesystemConnectorRejectsSymlinkRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	realRoot := t.TempDir()
	mustWrite(t, filepath.Join(realRoot, "private.txt"), []byte("must not ingest"))
	link := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatal(err)
	}
	if _, err := (FilesystemConnector{Root: link}).Documents(context.Background()); err == nil {
		t.Fatal("symlink connector root was accepted")
	}
}

func TestFilesystemConnectorEnforcesAggregateQuota(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "one.txt"), []byte("1234"))
	mustWrite(t, filepath.Join(root, "two.txt"), []byte("5678"))
	_, err := (FilesystemConnector{Root: root, MaxTotalBytes: 7}).Documents(context.Background())
	if err == nil || !strings.Contains(err.Error(), "quota") {
		t.Fatalf("aggregate corpus quota not enforced: %v", err)
	}
}

func TestGitConnectorOnlyTrackedFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	repo := t.TempDir()
	commands := [][]string{{"init", "-q"}, {"config", "user.email", "test@example.invalid"}, {"config", "user.name", "Test"}}
	for _, args := range commands {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	mustWrite(t, filepath.Join(repo, "tracked.txt"), []byte("tracked"))
	mustWrite(t, filepath.Join(repo, "untracked.txt"), []byte("untracked"))
	mustWrite(t, filepath.Join(repo, ".env"), []byte("secret"))
	if out, err := exec.Command("git", "-C", repo, "add", "tracked.txt", ".env").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	docs, err := (GitConnector{Repository: repo}).Documents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Path != "tracked.txt" {
		t.Fatalf("unexpected git documents: %#v", docs)
	}
}

func TestGitConnectorDisablesRepositoryFSMonitor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable script semantics differ on Windows")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "test@example.invalid"}, {"config", "user.name", "Test"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	mustWrite(t, filepath.Join(repo, "tracked.txt"), []byte("tracked"))
	if out, err := exec.Command("git", "-C", repo, "add", "tracked.txt").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	marker := filepath.Join(t.TempDir(), "fsmonitor-executed")
	hook := filepath.Join(t.TempDir(), "fsmonitor")
	mustWrite(t, hook, []byte("#!/bin/sh\n: > \""+marker+"\"\nprintf '{}\\n'\n"))
	if err := os.Chmod(hook, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "config", "core.fsmonitor", hook).CombinedOutput(); err != nil {
		t.Fatalf("configure fsmonitor: %v %s", err, out)
	}
	docs, err := (GitConnector{Repository: repo}).Documents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Path != "tracked.txt" {
		t.Fatalf("unexpected git documents: %#v", docs)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("untrusted core.fsmonitor executed: %v", err)
	}
}

func TestSensitiveCredentialPathsAreRejected(t *testing.T) {
	for _, path := range []string{
		".git-credentials", ".docker/config.json", ".kube/config", ".aws/credentials",
		".config/gcloud/application_default_credentials.json", "terraform.tfstate",
		"prod.tfvars", ".vault-token", "qdrant.env",
	} {
		if SafeDocumentPath(path) {
			t.Errorf("sensitive path accepted: %s", path)
		}
	}
	if !SafeDocumentPath("docs/config.json") {
		t.Error("generic documentation config.json should not be rejected")
	}
}

func TestServicePipelineAndReadOnlyMCP(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "guide.txt"), []byte("ivoai keeps operational memory available across agent sessions."))
	store := NewMemoryStore()
	service, err := NewService(DeterministicEmbedder{DimensionsN: 16}, store, NewMemoryCatalog())
	if err != nil {
		t.Fatal(err)
	}
	service.Chunker = Chunker{Size: 24, Overlap: 4}
	if err := service.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := service.AddConnector(FilesystemConnector{ConnectorName: "docs", Root: root}); err != nil {
		t.Fatal(err)
	}
	if count, err := service.Ingest(context.Background(), "docs"); err != nil || count != 1 {
		t.Fatalf("ingest = %d, %v", count, err)
	}
	status := service.Status(context.Background())
	if !status.Healthy || status.Documents != 1 || status.Chunks < 2 || status.Connectors != 1 {
		t.Fatalf("unexpected status: %#v", status)
	}
	request := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"context_search","arguments":{"query":"operational memory","limit":2}}}`
	recorder := httptest.NewRecorder()
	MCPHandler{Service: service}.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(request)))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `\"untrusted\":true`) {
		t.Fatalf("MCP response: %d %s", recorder.Code, recorder.Body.String())
	}
	mutating := strings.Replace(request, "context_search", "connector_add", 1)
	recorder = httptest.NewRecorder()
	MCPHandler{Service: service}.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(mutating)))
	if !strings.Contains(recorder.Body.String(), "unknown read-only tool") {
		t.Fatalf("mutation was not rejected: %s", recorder.Body.String())
	}
}

func TestIngestReconcilesDeletedDocumentsAndPurgeRemovesSource(t *testing.T) {
	store := NewMemoryStore()
	catalog := &FileCatalog{Path: filepath.Join(t.TempDir(), "catalog.json")}
	service, err := NewService(DeterministicEmbedder{DimensionsN: 8}, store, catalog)
	if err != nil {
		t.Fatal(err)
	}
	service.Chunker = Chunker{Size: 1024}
	if err := service.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	connector := &mutableConnector{name: "docs", docs: []Document{
		{ID: "one", Source: "spoofed", Path: "one.txt", Content: "first retained knowledge", IngestedAt: now},
		{ID: "two", Source: "spoofed", Path: "two.txt", Content: "second deleted knowledge", IngestedAt: now},
	}}
	if err := service.AddConnector(connector); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Ingest(context.Background(), "docs"); err != nil {
		t.Fatal(err)
	}
	connector.docs = connector.docs[:1]
	if _, err := service.Ingest(context.Background(), "docs"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := service.GetDocument("two"); err != nil || found {
		t.Fatalf("deleted source document remained in catalog: found=%t err=%v", found, err)
	}
	status := service.Status(context.Background())
	if status.Documents != 1 || status.Chunks != 1 {
		t.Fatalf("reconciliation left stale data: %#v", status)
	}
	if err := service.PurgeSource(context.Background(), "docs"); err != nil {
		t.Fatal(err)
	}
	status = service.Status(context.Background())
	if status.Documents != 0 || status.Chunks != 0 {
		t.Fatalf("connector purge left context readable: %#v", status)
	}
}

func TestHTTPEmbedderValidatesDimensions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embed" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([][]float32{{1, 0, 0}})
	}))
	defer server.Close()
	embedder := HTTPEmbedder{BaseURL: server.URL, DimensionsN: 3}
	vectors, err := embedder.Embed(context.Background(), []string{"text"})
	if err != nil || len(vectors) != 1 || len(vectors[0]) != 3 {
		t.Fatalf("embed = %#v, %v", vectors, err)
	}
	embedder.DimensionsN = 4
	if _, err := embedder.Embed(context.Background(), []string{"text"}); err == nil {
		t.Fatal("expected dimension mismatch")
	}
}

func TestQdrantAdapterUsesVersionedCollection(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-key") != "qdrant-internal-token" {
			t.Errorf("missing private Qdrant authorization")
		}
		paths = append(paths, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/collections/"):
			w.WriteHeader(http.StatusNotFound)
		case strings.HasSuffix(r.URL.Path, "/points/count"):
			_, _ = w.Write([]byte(`{"result":{"count":3}}`))
		case strings.HasSuffix(r.URL.Path, "/points/search"):
			_, _ = w.Write([]byte(`{"result":[]}`))
		default:
			_, _ = w.Write([]byte(`{"result":true}`))
		}
	}))
	defer server.Close()
	store := QdrantStore{BaseURL: server.URL, Collection: "ivoai_context_v1", APIKey: "qdrant-internal-token"}
	if err := store.Ensure(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	count, err := store.Count(context.Background())
	if err != nil || count != 3 {
		t.Fatalf("count = %d, %v", count, err)
	}
	joined := strings.Join(paths, "\n")
	if !strings.Contains(joined, "/collections/ivoai_context_v1") {
		t.Fatalf("collection was not versioned: %s", joined)
	}
}

func TestQdrantEnsureAcceptsExistingCollection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("existing collection should not be recreated: %s", r.Method)
		}
		_, _ = w.Write([]byte(`{"result":{"config":{"params":{"vectors":{"size":384}}}}}`))
	}))
	defer server.Close()
	if err := (QdrantStore{BaseURL: server.URL, Collection: "existing_d384"}).Ensure(context.Background(), 384); err != nil {
		t.Fatalf("existing collection should be idempotent: %v", err)
	}
}

func TestChunkerHandlesUnicodeAndOverlap(t *testing.T) {
	chunks := (Chunker{Size: 8, Overlap: 2}).Split("áéíóú alpha beta gamma")
	if len(chunks) < 2 || strings.Contains(strings.Join(chunks, ""), "�") {
		t.Fatalf("unexpected chunks: %#v", chunks)
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
