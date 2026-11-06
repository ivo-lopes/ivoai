package servercmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	contextsvc "github.com/ivo-lopes/ivoai/internal/context"
	"github.com/ivo-lopes/ivoai/internal/server"
	"golang.org/x/sys/unix"
)

type contextIngester interface {
	Ingest(context.Context, string) (int, error)
}

// A common kernel lock covers both the existing daemon and one-shot admin
// invocation. An overlap is rejected, never silently queued/retried. The lock
// file has no content and respects the context directory's inherited group.
func ingestContextLocked(ctx context.Context, layout server.Layout, service contextIngester, name string) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	dir, err := os.Lstat(layout.ContextDir)
	if err != nil || !dir.IsDir() || dir.Mode()&os.ModeSymlink != 0 {
		return 0, errors.New("context state unavailable")
	}
	parent, ok := dir.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("context directory identity unavailable")
	}
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	fd, err := unix.Open(filepath.Join(layout.ContextDir, "ingest.lock"), flags|unix.O_CREAT|unix.O_EXCL, 0640)
	created := err == nil
	if errors.Is(err, unix.EEXIST) {
		fd, err = unix.Open(filepath.Join(layout.ContextDir, "ingest.lock"), flags, 0)
	}
	if err != nil {
		return 0, errors.New("context ingestion lock unavailable; an administrator may need to repair its ownership")
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 || stat.Size != 0 {
		return 0, errors.New("invalid context ingestion lock")
	}
	// The service directory supplies the sole approved service owner/group. A
	// root-first invocation must transfer its newly created lock to that identity;
	// otherwise umask077 leaves a root:service0600 inode unreadable by the daemon.
	if created {
		if err := unix.Fchown(fd, int(parent.Uid), int(parent.Gid)); err != nil {
			return 0, errors.New("context ingestion lock ownership unavailable")
		}
		if err := unix.Fstat(fd, &stat); err != nil {
			return 0, errors.New("context ingestion lock identity unavailable")
		}
	}
	if (stat.Uid != parent.Uid && stat.Uid != 0) || stat.Gid != parent.Gid || stat.Mode&0777 & ^uint32(0640) != 0 {
		return 0, errors.New("context ingestion lock ownership mismatch")
	}
	if stat.Mode&0777 != 0640 {
		if err := unix.Fchmod(fd, 0640); err != nil {
			return 0, errors.New("context ingestion lock permissions unavailable; administrator repair required")
		}
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return 0, errors.New("context ingestion already in progress")
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	return service.Ingest(ctx, name)
}

type ingestionDocument struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	SHA256 string `json:"sha256"`
}
type ingestionReceipt struct {
	SchemaVersion         int                 `json:"schema_version"`
	Connector             string              `json:"connector"`
	Status                string              `json:"status"`
	Documents             int                 `json:"documents"`
	Verified              []ingestionDocument `json:"verified_documents"`
	StartedAt             time.Time           `json:"started_at"`
	CompletedAt           time.Time           `json:"completed_at"`
	ExactRecoveryVerified bool                `json:"exact_recovery_verified"`
	ContextSync           string              `json:"context_sync"`
}

func (r *runner) contextAdministration(ctx context.Context, layout server.Layout, args []string) error {
	fs := flag.NewFlagSet("server context "+args[0], flag.ContinueOnError)
	fs.SetOutput(r.errOut)
	name := ""
	query := ""
	id := ""
	switch args[0] {
	case "ingest":
		fs.StringVar(&name, "connector", "", "existing approved connector name")
	case "search":
		fs.StringVar(&query, "query", "", "document search query")
	case "get":
		fs.StringVar(&id, "id", "", "exact document ID")
	}
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return errors.New("invalid context administration arguments")
	}
	value := name + query + id
	if strings.TrimSpace(value) == "" || len(value) > 4096 || strings.ContainsAny(value, "\x00\r\n") {
		return errors.New("context selector required")
	}
	if args[0] == "ingest" && os.Geteuid() != 0 && os.Getenv("IVOAI_TEST_MODE") != "1" {
		return errors.New("context ingestion requires an existing server administrator; run sudo ivoai server context ingest")
	}
	if os.Getenv("IVOAI_TEST_MODE") != "1" {
		if err := requireServerSetup(ctx, layout); err != nil {
			return err
		}
	}
	service, err := contextService(layout)
	if err != nil {
		return err
	}
	if err := addConfiguredConnectors(service, layout); err != nil {
		return err
	}
	switch args[0] {
	case "search":
		results, err := service.Search(ctx, query, 10)
		if err != nil {
			return errors.New("context search failed")
		}
		return json.NewEncoder(r.out).Encode(map[string]any{"results": results, "untrusted": true})
	case "get":
		doc, found, err := service.GetDocument(id)
		if err != nil || !found {
			return errors.New("context document unavailable")
		}
		h := sha256.Sum256([]byte(doc.Content))
		return json.NewEncoder(r.out).Encode(map[string]any{"document": doc, "content_sha256": hex.EncodeToString(h[:]), "untrusted": true})
	case "ingest":
		return r.ingestAndVerify(ctx, layout, service, name)
	}
	return errors.New("invalid context action")
}

func (r *runner) ingestAndVerify(ctx context.Context, layout server.Layout, service *contextsvc.LegacyQdrantContextBackend, name string) error {
	receipt := ingestionReceipt{SchemaVersion: 1, Connector: name, Status: "failed", Verified: []ingestionDocument{}, StartedAt: time.Now().UTC(), ContextSync: "BLOCKED"}
	count, err := ingestContextLocked(ctx, layout, initializedContextIngest{service}, name)
	receipt.Documents = count
	receipt.CompletedAt = time.Now().UTC()
	if err != nil {
		_ = json.NewEncoder(r.out).Encode(receipt)
		return errors.New("context ingestion failed or already in progress; inspect service diagnostics, do not replay automatically")
	}
	docs, err := service.Service.Catalog.BySource(name)
	if err != nil {
		_ = json.NewEncoder(r.out).Encode(receipt)
		return errors.New("ingestion completed but exact recovery verification failed")
	}
	for _, d := range docs {
		recovered, found, getErr := service.GetDocument(d.ID)
		if getErr != nil || !found || recovered.Content != d.Content || recovered.Source != name {
			_ = json.NewEncoder(r.out).Encode(receipt)
			return errors.New("ingestion completed but exact recovery verification failed")
		}
		h := sha256.Sum256([]byte(recovered.Content))
		receipt.Verified = append(receipt.Verified, ingestionDocument{d.ID, recovered.Source, hex.EncodeToString(h[:])})
	}
	receipt.Status = "completed"
	receipt.ExactRecoveryVerified = len(docs) > 0 && count == len(docs)
	if count > 0 && len(receipt.Verified) > 0 {
		receipt.ContextSync = "PARTIAL"
	}
	// Catalog recovery is verified here. Search retrieval must be checked with
	// an explicit source-specific query before anyone claims full synchronization.
	return json.NewEncoder(r.out).Encode(receipt)
}

type initializedContextIngest struct {
	*contextsvc.LegacyQdrantContextBackend
}

func (s initializedContextIngest) Ingest(ctx context.Context, name string) (int, error) {
	if err := s.Initialize(ctx); err != nil {
		return 0, err
	}
	return s.LegacyQdrantContextBackend.Ingest(ctx, name)
}
