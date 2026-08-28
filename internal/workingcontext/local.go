package workingcontext

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

const artifactStoreSchema = 1

type Limits struct {
	MaxArtifactBytes int64
	MaxSessionBytes  int64
	MaxGlobalBytes   int64
	MaxSessionCount  int
	MaxGlobalCount   int
	DefaultTTL       time.Duration
	MaxTTL           time.Duration
}

func DefaultLimits() Limits {
	return Limits{MaxArtifactBytes: 16 << 20, MaxSessionBytes: 64 << 20, MaxGlobalBytes: 256 << 20, MaxSessionCount: 256, MaxGlobalCount: 2048, DefaultTTL: 24 * time.Hour, MaxTTL: 7 * 24 * time.Hour}
}

type LocalOptions struct {
	Limits Limits
	Now    func() time.Time
	ID     func() (string, error)
}

type LocalStore struct {
	root   string
	limits Limits
	now    func() time.Time
	id     func() (string, error)
	mu     sync.Mutex
}

type storedMetadata struct {
	Schema int         `json:"schema"`
	Ref    ArtifactRef `json:"ref"`
}

func NewLocalStore(root string, options LocalOptions) (*LocalStore, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) == string(filepath.Separator) {
		return nil, errors.New("artifact store root must be a specific absolute path")
	}
	limits := options.Limits
	if limits == (Limits{}) {
		limits = DefaultLimits()
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	id := options.ID
	if id == nil {
		id = randomArtifactID
	}
	store := &LocalStore{root: filepath.Clean(root), limits: limits, now: now, id: id}
	if err := store.ensureRoot(); err != nil {
		return nil, err
	}
	return store, nil
}

func (l Limits) validate() error {
	if l.MaxArtifactBytes < 1 || l.MaxSessionBytes < l.MaxArtifactBytes || l.MaxGlobalBytes < l.MaxSessionBytes || l.MaxSessionCount < 1 || l.MaxGlobalCount < l.MaxSessionCount || l.DefaultTTL <= 0 || l.MaxTTL < l.DefaultTTL {
		return errors.New("invalid artifact store limits")
	}
	return nil
}

func (s *LocalStore) Root() string { return s.root }

func (s *LocalStore) Put(ctx context.Context, request PutRequest, input io.Reader) (ArtifactRef, error) {
	if err := ctx.Err(); err != nil {
		return ArtifactRef{}, err
	}
	if input == nil || !request.Sensitivity.Persistable() || !validKind(request.Kind) || request.MediaType == "" || len(request.MediaType) > 128 || strings.ContainsAny(request.MediaType, "\x00\r\n\x1b") {
		return ArtifactRef{}, errors.New("artifact request is invalid or too sensitive for transient storage")
	}
	if err := request.Owner.Validate(); err != nil {
		return ArtifactRef{}, err
	}
	ttl := request.TTL
	if ttl == 0 {
		ttl = s.limits.DefaultTTL
	}
	if ttl < time.Minute || ttl > s.limits.MaxTTL {
		return ArtifactRef{}, errors.New("artifact TTL is outside the supported range")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureRoot(); err != nil {
		return ArtifactRef{}, err
	}
	usage, err := s.usageLocked()
	if err != nil {
		return ArtifactRef{}, err
	}
	if usage.globalCount >= s.limits.MaxGlobalCount || usage.sessionCount[request.Owner.SessionID] >= s.limits.MaxSessionCount {
		return ArtifactRef{}, errors.New("artifact count quota exceeded")
	}
	id, err := s.id()
	if err != nil || !opaqueIDPattern.MatchString(id) {
		return ArtifactRef{}, errors.New("generate opaque artifact identity")
	}
	objects := filepath.Join(s.root, "objects")
	staging, err := os.MkdirTemp(objects, ".staging-")
	if err != nil {
		return ArtifactRef{}, err
	}
	defer os.RemoveAll(staging)
	if err := os.Chmod(staging, 0o700); err != nil {
		return ArtifactRef{}, err
	}
	payloadPath := filepath.Join(staging, "payload")
	payload, err := os.OpenFile(payloadPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return ArtifactRef{}, err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(payload, hash), io.LimitReader(input, s.limits.MaxArtifactBytes+1))
	if syncErr := payload.Sync(); copyErr == nil {
		copyErr = syncErr
	}
	if closeErr := payload.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return ArtifactRef{}, copyErr
	}
	if written > s.limits.MaxArtifactBytes {
		return ArtifactRef{}, errors.New("artifact exceeds per-artifact size limit")
	}
	if usage.globalBytes+written > s.limits.MaxGlobalBytes || usage.sessionBytes[request.Owner.SessionID]+written > s.limits.MaxSessionBytes {
		return ArtifactRef{}, errors.New("artifact byte quota exceeded")
	}
	now := s.now().UTC()
	ref := ArtifactRef{ID: id, Kind: request.Kind, Size: written, SHA256: hex.EncodeToString(hash.Sum(nil)), MediaType: request.MediaType, CreatedAt: now, ExpiresAt: now.Add(ttl), Owner: request.Owner, Sensitivity: request.Sensitivity, Complete: !request.Truncated, Truncated: request.Truncated}
	if err := ref.Validate(); err != nil {
		return ArtifactRef{}, err
	}
	metadata, err := json.Marshal(storedMetadata{Schema: artifactStoreSchema, Ref: ref})
	if err != nil {
		return ArtifactRef{}, err
	}
	if err := platform.AtomicWritePrivate(metadata, filepath.Join(staging, "metadata.json")); err != nil {
		return ArtifactRef{}, err
	}
	if err := platform.SyncDir(staging); err != nil {
		return ArtifactRef{}, err
	}
	final, err := s.artifactDir(id)
	if err != nil {
		return ArtifactRef{}, err
	}
	if err := os.Rename(staging, final); err != nil {
		return ArtifactRef{}, err
	}
	if err := platform.SyncDir(objects); err != nil {
		return ArtifactRef{}, err
	}
	return ref, nil
}

func (s *LocalStore) Stat(ctx context.Context, owner Ownership, id string) (ArtifactRef, error) {
	_, ref, err := s.readVerified(ctx, owner, id)
	return ref, err
}

func (s *LocalStore) Read(ctx context.Context, owner Ownership, id string) (io.ReadCloser, ArtifactRef, error) {
	body, ref, err := s.readVerified(ctx, owner, id)
	if err != nil {
		return nil, ArtifactRef{}, err
	}
	return io.NopCloser(bytes.NewReader(body)), ref, nil
}

func (s *LocalStore) ReadRange(ctx context.Context, owner Ownership, id string, offset, length int64) ([]byte, ArtifactRef, error) {
	if offset < 0 || length <= 0 || length > s.limits.MaxArtifactBytes || offset > int64(^uint64(0)>>1)-length {
		return nil, ArtifactRef{}, errors.New("artifact range is invalid")
	}
	body, ref, err := s.readVerified(ctx, owner, id)
	if err != nil {
		return nil, ArtifactRef{}, err
	}
	if offset > int64(len(body)) || length > int64(len(body))-offset {
		return nil, ArtifactRef{}, errors.New("artifact range exceeds payload")
	}
	return append([]byte(nil), body[offset:offset+length]...), ref, nil
}

func (s *LocalStore) readVerified(ctx context.Context, owner Ownership, id string) ([]byte, ArtifactRef, error) {
	if err := ctx.Err(); err != nil {
		return nil, ArtifactRef{}, err
	}
	if err := owner.Validate(); err != nil {
		return nil, ArtifactRef{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir, err := s.artifactDir(id)
	if err != nil {
		return nil, ArtifactRef{}, err
	}
	ref, err := s.loadMetadata(dir)
	if err != nil {
		return nil, ArtifactRef{}, err
	}
	if !ownerAllows(ref.Owner, owner) {
		return nil, ArtifactRef{}, errors.New("artifact access denied for this owner")
	}
	if !s.now().UTC().Before(ref.ExpiresAt) {
		return nil, ArtifactRef{}, errors.New("artifact expired")
	}
	body, err := platform.ReadRegularFile(filepath.Join(dir, "payload"), s.limits.MaxArtifactBytes)
	if err != nil {
		return nil, ArtifactRef{}, err
	}
	sum := sha256.Sum256(body)
	if int64(len(body)) != ref.Size || hex.EncodeToString(sum[:]) != ref.SHA256 {
		return nil, ArtifactRef{}, errors.New("artifact payload failed integrity validation")
	}
	return body, ref, nil
}

func (s *LocalStore) GC(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureRoot(); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(filepath.Join(s.root, "objects"))
	if err != nil {
		return 0, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	removed := 0
	var firstErr error
	now := s.now().UTC()
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		name := entry.Name()
		path := filepath.Join(s.root, "objects", name)
		info, infoErr := entry.Info()
		if infoErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if strings.HasPrefix(name, ".staging-") {
			if info.ModTime().Add(s.limits.DefaultTTL).Before(now) {
				if removeErr := os.RemoveAll(path); removeErr == nil {
					removed++
				} else if firstErr == nil {
					firstErr = removeErr
				}
			}
			continue
		}
		if !opaqueIDPattern.MatchString(name) {
			continue
		}
		ref, loadErr := s.loadMetadata(path)
		if loadErr != nil {
			if firstErr == nil {
				firstErr = loadErr
			}
			continue
		}
		if !now.Before(ref.ExpiresAt) {
			if removeErr := os.RemoveAll(path); removeErr == nil {
				removed++
			} else if firstErr == nil {
				firstErr = removeErr
			}
		}
	}
	if removed > 0 {
		_ = platform.SyncDir(filepath.Join(s.root, "objects"))
	}
	return removed, firstErr
}

type usage struct {
	globalBytes  int64
	globalCount  int
	sessionBytes map[string]int64
	sessionCount map[string]int
}

func (s *LocalStore) usageLocked() (usage, error) {
	result := usage{sessionBytes: map[string]int64{}, sessionCount: map[string]int{}}
	entries, err := os.ReadDir(filepath.Join(s.root, "objects"))
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !opaqueIDPattern.MatchString(entry.Name()) {
			continue
		}
		ref, err := s.loadMetadata(filepath.Join(s.root, "objects", entry.Name()))
		if err != nil {
			return result, fmt.Errorf("artifact quota inventory is unsafe: %w", err)
		}
		result.globalBytes += ref.Size
		result.globalCount++
		result.sessionBytes[ref.Owner.SessionID] += ref.Size
		result.sessionCount[ref.Owner.SessionID]++
	}
	return result, nil
}

func (s *LocalStore) loadMetadata(dir string) (ArtifactRef, error) {
	body, err := platform.ReadRegularFile(filepath.Join(dir, "metadata.json"), 64<<10)
	if err != nil {
		return ArtifactRef{}, err
	}
	var stored storedMetadata
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stored); err != nil || stored.Schema != artifactStoreSchema {
		return ArtifactRef{}, errors.New("artifact metadata is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ArtifactRef{}, errors.New("artifact metadata has trailing data")
	}
	if err := stored.Ref.Validate(); err != nil {
		return ArtifactRef{}, err
	}
	if filepath.Base(dir) != stored.Ref.ID {
		return ArtifactRef{}, errors.New("artifact metadata identity mismatch")
	}
	return stored.Ref, nil
}

func (s *LocalStore) artifactDir(id string) (string, error) {
	if !opaqueIDPattern.MatchString(id) {
		return "", errors.New("malformed artifact reference")
	}
	objects := filepath.Join(s.root, "objects")
	path := filepath.Join(objects, id)
	rel, err := filepath.Rel(objects, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("artifact path escaped managed root")
	}
	return path, nil
}

func (s *LocalStore) ensureRoot() error {
	if err := rejectSymlinkChain(s.root); err != nil {
		return err
	}
	if err := platform.EnsurePrivateDir(s.root); err != nil {
		return err
	}
	objects := filepath.Join(s.root, "objects")
	if err := platform.EnsurePrivateDir(objects); err != nil {
		return err
	}
	return rejectSymlinkChain(objects)
}

func rejectSymlinkChain(path string) error {
	path = filepath.Clean(path)
	volume := filepath.VolumeName(path)
	rest := strings.TrimPrefix(path, volume)
	current := volume + string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(rest, string(filepath.Separator)), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("artifact store path contains symlink: %s", current)
		}
	}
	return nil
}

func ownerAllows(stored, requester Ownership) bool {
	if stored.SessionID != requester.SessionID {
		return false
	}
	if requester.TaskID != "" && stored.TaskID != requester.TaskID {
		return false
	}
	if requester.WorkerID != "" && stored.WorkerID != requester.WorkerID {
		return false
	}
	return true
}

func randomArtifactID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "artifact_" + hex.EncodeToString(raw[:]), nil
}

var _ ArtifactStore = (*LocalStore)(nil)
