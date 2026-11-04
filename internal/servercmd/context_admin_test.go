package servercmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	contextsvc "github.com/ivo-lopes/ivoai/internal/context"
	"github.com/ivo-lopes/ivoai/internal/server"
)

type blockedIngest struct {
	started, release chan struct{}
	calls            int
}

func (b *blockedIngest) Ingest(ctx context.Context, name string) (int, error) {
	b.calls++
	close(b.started)
	select {
	case <-b.release:
		return 1, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}
func TestContextAdminAndDaemonShareNonblockingIngestionLock(t *testing.T) {
	layout := server.DefaultLayout(t.TempDir())
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	b := &blockedIngest{started: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := ingestContextLocked(context.Background(), layout, b, "docs"); done <- err }()
	<-b.started
	if _, err := ingestContextLocked(context.Background(), layout, b, "docs"); err == nil {
		t.Fatal("concurrent ingestion admitted")
	}
	close(b.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if b.calls != 1 {
		t.Fatal("duplicate ingestion")
	}
	if err := os.Remove(filepath.Join(layout.ContextDir, "ingest.lock")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(target, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(layout.ContextDir, "ingest.lock")); err != nil {
		t.Fatal(err)
	}
	if _, err := ingestContextLocked(context.Background(), layout, b, "docs"); err == nil {
		t.Fatal("symlink lock admitted")
	}
	actual, _ := os.ReadFile(target)
	if string(actual) != "untouched" {
		t.Fatal("symlink target modified")
	}
}
func TestContextAdministrativeIngestionVerifiesExactCatalogRecovery(t *testing.T) {
	layout := server.DefaultLayout(t.TempDir())
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	content := "IVOAI source-specific administrative ingestion proof. Exact UTF-8: ação.\n"
	if err := os.WriteFile(filepath.Join(source, "approved.md"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".env"), []byte("SECRET=must-not-index"), 0600); err != nil {
		t.Fatal(err)
	}
	svc, err := contextsvc.NewService(contextsvc.DeterministicEmbedder{DimensionsN: 8}, contextsvc.NewMemoryStore(), contextsvc.NewMemoryCatalog())
	if err != nil {
		t.Fatal(err)
	}
	backend := &contextsvc.LegacyQdrantContextBackend{Service: svc, Version: "test", Managed: true}
	if err := backend.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := backend.AddConnector(contextsvc.FilesystemConnector{ConnectorName: "approved", Root: source}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	r := &runner{out: &out}
	if err := r.ingestAndVerify(context.Background(), layout, backend, "approved"); err != nil {
		t.Fatal(err)
	}
	var receipt ingestionReceipt
	if json.Unmarshal(out.Bytes(), &receipt) != nil {
		t.Fatal("invalid receipt")
	}
	if receipt.Documents != 1 || len(receipt.Verified) != 1 || !receipt.ExactRecoveryVerified || receipt.ContextSync != "PARTIAL" {
		t.Fatalf("bad receipt %#v", receipt)
	}
	doc, found, err := backend.GetDocument(receipt.Verified[0].ID)
	if err != nil || !found || doc.Content != content {
		t.Fatal("exact retrieval failed")
	}
	results, err := backend.Search(context.Background(), "administrative ingestion proof", 5)
	if err != nil || len(results) == 0 {
		t.Fatal("query retrieval failed")
	}
	if strings.Contains(out.String(), "SECRET") || strings.Contains(out.String(), content) {
		t.Fatal("content leaked into ingestion receipt")
	}
	out.Reset()
	if err := r.ingestAndVerify(context.Background(), layout, backend, "missing"); err == nil {
		t.Fatal("missing connector successful")
	}
	if json.Unmarshal(out.Bytes(), &receipt) != nil || receipt.Status != "failed" || receipt.ContextSync != "BLOCKED" {
		t.Fatal("failed ingestion claimed sync")
	}
}
func TestContextAdminRejectsSelectorsBeforeConstructingPrivateBackend(t *testing.T) {
	layout := server.DefaultLayout(t.TempDir())
	r := &runner{out: &bytes.Buffer{}, errOut: &bytes.Buffer{}}
	for _, args := range [][]string{{"ingest"}, {"search", "--query", "a", "unexpected"}, {"get", "--id", "bad\nselector"}, {"ingest", "--connector", "x", "--arbitrary", "value"}} {
		if err := r.contextAdministration(context.Background(), layout, args); err == nil || errors.Is(err, os.ErrNotExist) {
			t.Fatal("selector accepted or private backend touched", args, err)
		}
	}
}
