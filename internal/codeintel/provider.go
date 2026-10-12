package codeintel

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"golang.org/x/sys/unix"
)

var ErrNoSupportedSources = errors.New("repository has no supported source files")

const Version = "0.21.1-ivoai.1"
const MaxResultBytes = 32768

//go:embed assets/adapter.mjs
var adapter []byte

type adapterRequest struct {
	core.CodeQuery
	Distribution string `json:"distribution"`
	Root         string `json:"root"`
	Cache        string `json:"cache"`
	Build        bool   `json:"build"`
}
type adapterResponse struct {
	Text            string `json:"text"`
	Files           int    `json:"files"`
	Nodes           int    `json:"nodes"`
	Truncated       bool   `json:"truncated"`
	PeakMemoryBytes *int64 `json:"peak_memory_bytes,omitempty"`
	CPUMS           *int64 `json:"cpu_ms,omitempty"`
}

type Provider struct {
	Root, Scope, Permissions, CacheRoot string
	Node, Distribution, Executable      string
	Allowed                             bool
	Observe                             func(core.CodeResult, error)
	run                                 func(context.Context, adapterRequest) (adapterResponse, error)
}

type indexReceipt struct {
	Source, Index, Adapter, Runtime string
	Files, Nodes                    int
}
type cachedResult struct {
	Version string
	Result  core.CodeResult
}

func (p *Provider) Query(ctx context.Context, q core.CodeQuery) (result core.CodeResult, err error) {
	start := time.Now()
	defer func() {
		result.DurationMS = time.Since(start).Milliseconds()
		p.recordCost(q, result, err)
		if p.Observe != nil {
			p.Observe(result, err)
		}
	}()
	if !p.Allowed {
		return result, errors.New("code intelligence denied by host policy")
	}
	q, err = Normalize(q)
	if err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	key, err := Identity(p.Root, p.Scope, p.Permissions)
	if err != nil {
		return result, err
	}
	if err = platform.EnsurePrivateDir(p.CacheRoot); err != nil {
		return result, err
	}
	// Cross-process admission bounds waiting requests as well as active Node work.
	releaseQueue, err := queueSlot(p.CacheRoot)
	if err != nil {
		return result, err
	}
	defer releaseQueue()
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	dir := filepath.Join(p.CacheRoot, key)
	if err = platform.EnsurePrivateDir(dir); err != nil {
		return result, err
	}
	unlock, err := lock(ctx, filepath.Join(dir, "lock"))
	if err != nil {
		return result, err
	}
	defer unlock()
	files, err := Inventory(ctx, p.Root)
	if err != nil {
		return result, err
	}
	if len(files) == 0 {
		return result, ErrNoSupportedSources
	}
	snap, err := Snapshot(p.Root, files)
	if err != nil {
		return result, err
	}
	index := filepath.Join(dir, "index")
	receiptPath := filepath.Join(dir, "receipt.json")
	var receipt indexReceipt
	data, _ := readBounded(receiptPath, 4096)
	_ = json.Unmarshal(data, &receipt)
	currentHash, hashErr := indexDigest(index)
	adapterHash := digest(adapter)
	runtimeHash := p.runtimeIdentity()
	validIndex := hashErr == nil && currentHash == receipt.Index && receipt.Adapter == adapterHash && receipt.Runtime == runtimeHash
	if !validIndex {
		if err = os.RemoveAll(index); err != nil {
			return result, err
		}
		receipt = indexReceipt{}
	}
	queryJSON, _ := json.Marshal(q)
	cacheKey := digest(append([]byte(snap.Digest+adapterHash+runtimeHash), queryJSON...))
	resultPath := filepath.Join(dir, "results", cacheKey+".json")
	if validIndex && receipt.Source == snap.Digest && q.Operation != "build" && q.Operation != "viz" {
		if data, e := readBounded(resultPath, MaxResultBytes+8192); e == nil {
			var cached cachedResult
			if json.Unmarshal(data, &cached) == nil && cached.Version == Version && cached.Result.SourceDigest == snap.Digest && len(cached.Result.Text) <= q.BudgetBytes && cached.Result.Untrusted && cached.Result.Fresh {
				result = cached.Result
				result.CacheHit = true
				result.CPUMS, result.PeakMemoryBytes = nil, nil
				return result, nil
			}
		}
	}
	source := filepath.Join(dir, "source")
	// Snapshot files are private copies, never links. Updating only changed bytes
	// preserves mtimes for upstream's incremental extraction cache.
	if err = syncSnapshot(source, snap); err != nil {
		return result, err
	}
	if err = platform.EnsurePrivateDir(index); err != nil {
		return result, err
	}
	unlockProcess, err := lock(ctx, filepath.Join(p.CacheRoot, "process.lock"))
	if err != nil {
		return result, err
	}
	defer unlockProcess()
	request := adapterRequest{CodeQuery: q, Distribution: p.Distribution, Root: source, Cache: index, Build: !validIndex || receipt.Source != snap.Digest || q.Operation == "build"}
	run := p.run
	if run == nil {
		run = p.runProcess
	}
	response, err := run(ctx, request)
	if err != nil {
		_ = os.Remove(receiptPath)
		return result, err
	}
	if len(response.Text) > q.BudgetBytes {
		return result, errors.New("structural result exceeded context budget")
	}
	indexHash, err := indexDigest(index)
	if err != nil {
		return result, errors.New("structural index was not produced")
	}
	if request.Build {
		receipt.Files = response.Files
		receipt.Nodes = response.Nodes
	}
	sourceChanged := receipt.Source != snap.Digest
	receipt.Source = snap.Digest
	receipt.Index = indexHash
	receipt.Adapter = adapterHash
	receipt.Runtime = runtimeHash
	body, _ := json.Marshal(receipt)
	if err = platform.AtomicWritePrivate(body, receiptPath); err != nil {
		return result, err
	}
	result = core.CodeResult{Text: response.Text, SourceDigest: snap.Digest, ProviderVersion: Version, Truncated: response.Truncated, Fresh: true, Untrusted: true, Files: len(files), IndexedFiles: receipt.Files, Nodes: receipt.Nodes, PeakMemoryBytes: response.PeakMemoryBytes, CPUMS: response.CPUMS}
	if q.Operation == "viz" {
		result.ArtifactPath = filepath.Join(index, "visualization", "index.html")
	}
	if q.Operation != "viz" && q.Operation != "build" {
		resultDir := filepath.Dir(resultPath)
		if err = platform.EnsurePrivateDir(resultDir); err != nil {
			return result, err
		}
		// Bounded disk results: at most 64 queries for this exact source generation.
		entries, _ := os.ReadDir(resultDir)
		if sourceChanged || len(entries) >= 64 {
			if err = os.RemoveAll(resultDir); err != nil {
				return result, err
			}
		}
		body, _ = json.Marshal(cachedResult{Version: Version, Result: result})
		if err = platform.AtomicWritePrivate(body, resultPath); err != nil {
			return result, err
		}
	}
	return result, nil
}

// App preflight verifies this immutable supply-chain object path and its full
// inventory. Binding the path also invalidates indices across reviewed rebuilds
// of the same upstream version and across rollback, not just adapter changes.
func (p *Provider) runtimeIdentity() string {
	return digest([]byte(Version + "\n" + p.Distribution))
}

func syncSnapshot(root string, snap SourceSnapshot) error {
	if err := platform.EnsurePrivateDir(root); err != nil {
		return err
	}
	desired := make(map[string]bool, len(snap.Files))
	for _, file := range snap.Files {
		desired[file.Path] = true
		target := filepath.Join(root, file.Path)
		prior, err := readBounded(target, MaxFileBytes)
		if err == nil && digest(prior) == digest(file.Body) {
			continue
		}
		if err := platform.AtomicWritePrivate(file.Body, target); err != nil {
			return err
		}
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root || entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !desired[rel] {
			return os.Remove(path)
		}
		return nil
	})
}
func digest(body []byte) string { h := sha256.Sum256(body); return hex.EncodeToString(h[:]) }
func readBounded(path string, max int64) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > max {
		return nil, errors.New("invalid cache object")
	}
	body, err := io.ReadAll(io.LimitReader(f, max+1))
	if int64(len(body)) > max {
		return nil, errors.New("cache object exceeds budget")
	}
	return body, err
}
func fileDigest(path string) (string, error) {
	body, err := readBounded(path, 64*1024*1024)
	if err != nil {
		return "", err
	}
	return digest(body), nil
}
func openLock(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
func lock(ctx context.Context, path string) (func(), error) {
	file, err := openLock(path)
	if err != nil {
		return nil, err
	}
	for {
		if err = ctx.Err(); err != nil {
			file.Close()
			return nil, err
		}
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN); file.Close() }, nil
		}
		if err != unix.EWOULDBLOCK && err != unix.EAGAIN {
			file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}
func queueSlot(root string) (func(), error) {
	for i := range 8 {
		file, err := openLock(filepath.Join(root, fmt.Sprintf("queue-%d.lock", i)))
		if err != nil {
			return nil, err
		}
		if unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB) == nil {
			return func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN); file.Close() }, nil
		}
		file.Close()
	}
	return nil, errors.New("code intelligence queue full; use bounded direct inspection")
}

// All graph sidecars are part of the integrity boundary, not only wiring.json.
// A corrupt extraction cache must never be consumed during incremental rebuild.
func indexDigest(root string) (string, error) {
	h := sha256.New()
	var total int64
	files := 0
	hasWiring := false
	err := filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			if path == filepath.Join(root, "visualization") {
				return filepath.SkipDir
			}
			return nil
		}
		if !e.Type().IsRegular() {
			return errors.New("non-regular graph cache entry")
		}
		files++
		if files > 20000 {
			return errors.New("graph cache file budget exceeded")
		}
		body, err := readBounded(path, 64*1024*1024)
		if err != nil {
			return err
		}
		total += int64(len(body))
		if total > 128*1024*1024 {
			return errors.New("graph cache byte budget exceeded")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == filepath.Join(".graph", "wiring.json") {
			hasWiring = true
		}
		fmt.Fprintf(h, "%s\x00%s\n", rel, digest(body))
		return nil
	})
	if err != nil {
		return "", err
	}
	if !hasWiring {
		return "", errors.New("missing graph wiring")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
