// Package supplychain provides the shared, non-executing staging and atomic
// promotion foundation for IVOAI-managed external components and skills.
package supplychain

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"golang.org/x/sys/unix"
)

const SchemaVersion = 1

type ArtifactKind string

const (
	KindSkill     ArtifactKind = "skill"
	KindComponent ArtifactKind = "component"
	KindHelper    ArtifactKind = "helper"
)

type Reference struct {
	ID      string
	Kind    ArtifactKind
	Source  string
	Version string
}

type Integrity struct {
	Algorithm         string `json:"algorithm"`
	Digest            string `json:"digest"`
	SignatureStatus   string `json:"signature_status"`
	AttestationStatus string `json:"attestation_status"`
	TrustLevel        string `json:"trust_level"`
}

type ResolvedSource struct {
	ID             string       `json:"id"`
	Kind           ArtifactKind `json:"kind"`
	Source         string       `json:"source"`
	Revision       string       `json:"revision"`
	LogicalVersion string       `json:"logical_version,omitempty"`
	DefaultBranch  string       `json:"default_branch,omitempty"`
	Executables    []string     `json:"executables,omitempty"`
	Integrity      Integrity    `json:"integrity"`
}

func (r ResolvedSource) Validate() error {
	if !safeID(r.ID) || !validKind(r.Kind) || !validHTTPS(r.Source) || !immutableRevision(r.Revision) || len(r.LogicalVersion) > 128 || len(r.DefaultBranch) > 256 {
		return errors.New("source did not resolve to safe immutable metadata")
	}
	if r.Integrity.Algorithm != "sha256" || len(r.Integrity.Digest) != 64 {
		return errors.New("resolved source requires sha256 integrity")
	}
	if _, err := hex.DecodeString(r.Integrity.Digest); err != nil {
		return errors.New("resolved source has invalid integrity digest")
	}
	for _, value := range []string{r.Integrity.SignatureStatus, r.Integrity.AttestationStatus, r.Integrity.TrustLevel} {
		if !safeStatus(value) {
			return errors.New("resolved source has invalid trust metadata")
		}
	}
	if len(r.Executables) > 64 {
		return errors.New("resolved source declares too many executables")
	}
	for _, value := range r.Executables {
		if _, err := safeArchivePath(value); err != nil {
			return errors.New("resolved source declares an unsafe executable path")
		}
		if r.Kind == KindSkill {
			return errors.New("skills cannot declare executable files during staging")
		}
	}
	return nil
}

type Discoverer interface {
	Resolve(context.Context, Reference) (ResolvedSource, error)
}

type Fetcher interface {
	Fetch(context.Context, ResolvedSource) (io.ReadCloser, error)
}

type Validator interface {
	Validate(context.Context, ResolvedSource, string) error
}

type ValidatorFunc func(context.Context, ResolvedSource, string) error

func (f ValidatorFunc) Validate(ctx context.Context, source ResolvedSource, root string) error {
	return f(ctx, source, root)
}

type Limits struct {
	ArchiveBytes  int64
	ExpandedBytes int64
	FileBytes     int64
	Files         int
}

func DefaultLimits() Limits {
	return Limits{ArchiveBytes: 64 << 20, ExpandedBytes: 256 << 20, FileBytes: 32 << 20, Files: 4096}
}

type Manager struct {
	Root       string
	Limits     Limits
	Now        func() time.Time
	Structural Validator
	Policy     Validator
}

type Pipeline struct {
	Manager    Manager
	Discoverer Discoverer
	Fetcher    Fetcher
}

type Staged struct {
	TransactionID string         `json:"transaction_id"`
	Source        ResolvedSource `json:"source"`
	ObjectPath    string         `json:"object_path"`
}

type Pointer struct {
	Schema    int       `json:"schema"`
	ID        string    `json:"id"`
	Active    string    `json:"active,omitempty"`
	Previous  string    `json:"previous,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type transaction struct {
	Schema    int            `json:"schema"`
	ID        string         `json:"id"`
	Source    ResolvedSource `json:"source"`
	State     string         `json:"state"`
	Staging   string         `json:"staging"`
	Object    string         `json:"object,omitempty"`
	UpdatedAt time.Time      `json:"updated_at"`
}

func (p Pipeline) Prepare(ctx context.Context, reference Reference) (Staged, error) {
	if p.Discoverer == nil || p.Fetcher == nil {
		return Staged{}, errors.New("supply-chain discovery and fetch adapters are required")
	}
	resolved, err := p.Discoverer.Resolve(ctx, reference)
	if err != nil {
		return Staged{}, fmt.Errorf("resolve immutable source: %w", err)
	}
	if err := resolved.Validate(); err != nil {
		return Staged{}, err
	}
	archive, err := p.Fetcher.Fetch(ctx, resolved)
	if err != nil {
		return Staged{}, fmt.Errorf("fetch resolved artifact: %w", err)
	}
	defer archive.Close()
	return p.Manager.StageArchive(ctx, resolved, archive)
}

func (m Manager) StageArchive(ctx context.Context, source ResolvedSource, archive io.Reader) (Staged, error) {
	if err := source.Validate(); err != nil {
		return Staged{}, err
	}
	if err := m.ensureRoot(); err != nil {
		return Staged{}, err
	}
	id, err := randomID()
	if err != nil {
		return Staged{}, err
	}
	staging := filepath.Join(m.Root, "staging", id)
	if err := platform.EnsurePrivateDir(staging); err != nil {
		return Staged{}, err
	}
	tx := transaction{Schema: SchemaVersion, ID: id, Source: source, State: "staging", Staging: staging, UpdatedAt: m.now()}
	if err := m.saveTransaction(tx); err != nil {
		return Staged{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(staging)
		}
	}()
	limits := m.limits()
	compressed, err := io.ReadAll(io.LimitReader(archive, limits.ArchiveBytes+1))
	if err != nil {
		return Staged{}, err
	}
	if int64(len(compressed)) > limits.ArchiveBytes {
		return Staged{}, errors.New("artifact archive exceeds size limit")
	}
	sum := sha256.Sum256(compressed)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), source.Integrity.Digest) {
		return Staged{}, errors.New("artifact checksum mismatch")
	}
	content := filepath.Join(staging, "content")
	if err := platform.EnsurePrivateDir(content); err != nil {
		return Staged{}, err
	}
	if err := extractArchive(ctx, compressed, content, limits, source.Executables); err != nil {
		return Staged{}, err
	}
	if m.Structural != nil {
		if err := m.Structural.Validate(ctx, source, content); err != nil {
			return Staged{}, fmt.Errorf("staged structural validation: %w", err)
		}
	}
	if m.Policy != nil {
		if err := m.Policy.Validate(ctx, source, content); err != nil {
			return Staged{}, fmt.Errorf("staged policy validation: %w", err)
		}
	}
	provenance, err := json.MarshalIndent(source, "", "  ")
	if err != nil {
		return Staged{}, err
	}
	if err := platform.AtomicWritePrivate(append(provenance, '\n'), filepath.Join(content, ".ivoai-provenance.json")); err != nil {
		return Staged{}, err
	}
	object := m.objectPath(source.ID, source.Revision)
	if err := platform.EnsurePrivateDir(filepath.Dir(object)); err != nil {
		return Staged{}, err
	}
	if info, err := os.Lstat(object); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return Staged{}, errors.New("existing immutable object is unsafe")
		}
		if err := validateStoredProvenance(object, source); err != nil {
			return Staged{}, err
		}
		cleanup = false
		_ = os.RemoveAll(staging)
		tx.State, tx.Object, tx.UpdatedAt = "staged", object, m.now()
		if err := m.saveTransaction(tx); err != nil {
			return Staged{}, err
		}
		return Staged{TransactionID: id, Source: source, ObjectPath: object}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Staged{}, err
	}
	if err := os.Rename(content, object); err != nil {
		return Staged{}, fmt.Errorf("promote validated content to immutable object: %w", err)
	}
	if err := platform.SyncDir(filepath.Dir(object)); err != nil {
		return Staged{}, err
	}
	cleanup = false
	_ = os.RemoveAll(staging)
	tx.State, tx.Object, tx.UpdatedAt = "staged", object, m.now()
	if err := m.saveTransaction(tx); err != nil {
		return Staged{}, err
	}
	return Staged{TransactionID: id, Source: source, ObjectPath: object}, nil
}

func (m Manager) Promote(staged Staged) error {
	if err := m.ensureRoot(); err != nil {
		return err
	}
	if err := staged.Source.Validate(); err != nil {
		return err
	}
	want := m.objectPath(staged.Source.ID, staged.Source.Revision)
	if filepath.Clean(staged.ObjectPath) != want || !safeTransactionID(staged.TransactionID) {
		return errors.New("staged artifact does not belong to this supply-chain root")
	}
	if err := validateStoredProvenance(want, staged.Source); err != nil {
		return err
	}
	pointer, err := m.loadPointer(staged.Source.ID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if pointer.Active == staged.Source.Revision {
		return nil
	}
	pointer.Schema, pointer.ID, pointer.Previous, pointer.Active, pointer.UpdatedAt = SchemaVersion, staged.Source.ID, pointer.Active, staged.Source.Revision, m.now()
	if err := m.savePointer(pointer); err != nil {
		return err
	}
	tx, err := m.loadTransaction(staged.TransactionID)
	if err == nil {
		tx.State, tx.UpdatedAt = "promoted", m.now()
		return m.saveTransaction(tx)
	}
	return err
}

func (m Manager) Rollback(id string) (bool, error) {
	if err := m.ensureRoot(); err != nil {
		return false, err
	}
	pointer, err := m.loadPointer(id)
	if errors.Is(err, os.ErrNotExist) || pointer.Previous == "" {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := validateObjectDir(m.objectPath(id, pointer.Previous)); err != nil {
		return false, err
	}
	pointer.Active, pointer.Previous, pointer.UpdatedAt = pointer.Previous, "", m.now()
	return true, m.savePointer(pointer)
}

func (m Manager) Active(id string) (ResolvedSource, string, error) {
	pointer, err := m.loadPointer(id)
	if err != nil {
		return ResolvedSource{}, "", err
	}
	if pointer.Active == "" {
		return ResolvedSource{}, "", os.ErrNotExist
	}
	path := m.objectPath(id, pointer.Active)
	data, err := platform.ReadRegularFile(filepath.Join(path, ".ivoai-provenance.json"), 64<<10)
	if err != nil {
		return ResolvedSource{}, "", err
	}
	var source ResolvedSource
	if err := json.Unmarshal(data, &source); err != nil || source.Validate() != nil || source.ID != id || source.Revision != pointer.Active {
		return ResolvedSource{}, "", errors.New("active artifact provenance is invalid")
	}
	return source, path, nil
}

func (m Manager) Recover() (int, error) {
	if err := m.ensureRoot(); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(filepath.Join(m.Root, "transactions"))
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !safeTransactionID(id) {
			return recovered, errors.New("unsafe supply-chain transaction filename")
		}
		tx, err := m.loadTransaction(id)
		if err != nil {
			return recovered, err
		}
		if tx.State != "staging" {
			continue
		}
		want := filepath.Join(m.Root, "staging", id)
		if filepath.Clean(tx.Staging) != want {
			return recovered, errors.New("transaction staging path escaped managed root")
		}
		if err := os.RemoveAll(want); err != nil {
			return recovered, err
		}
		tx.State, tx.UpdatedAt = "rolled_back", m.now()
		if err := m.saveTransaction(tx); err != nil {
			return recovered, err
		}
		recovered++
	}
	return recovered, nil
}

func extractArchive(ctx context.Context, compressed []byte, destination string, limits Limits, executables []string) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return errors.New("artifact is not a valid gzip archive")
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	seen := map[string]bool{}
	allowedExecutables := map[string]bool{}
	for _, value := range executables {
		allowedExecutables[filepath.ToSlash(filepath.Clean(value))] = true
	}
	var total int64
	files := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read staged archive: %w", err)
		}
		name, err := safeArchivePath(header.Name)
		if err != nil {
			return err
		}
		if seen[name] || name == ".ivoai-provenance.json" || strings.HasPrefix(name, ".ivoai-") {
			return errors.New("archive contains duplicate or reserved path")
		}
		seen[name] = true
		files++
		if files > limits.Files {
			return errors.New("artifact archive exceeds file-count limit")
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		if !within(destination, target) {
			return errors.New("archive entry escapes staging root")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > limits.FileBytes || total+header.Size > limits.ExpandedBytes {
				return errors.New("artifact expanded content exceeds size limit")
			}
			executable := header.Mode&0o111 != 0
			if executable && !allowedExecutables[name] {
				return errors.New("archive contains unexpected executable file")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			mode := fs.FileMode(0o600)
			if executable {
				mode = 0o700
			}
			if err := writeArchiveFile(reader, target, header.Size, mode); err != nil {
				return err
			}
			total += header.Size
		case tar.TypeSymlink, tar.TypeLink:
			return errors.New("archive contains forbidden link")
		default:
			return errors.New("archive contains unsupported file type")
		}
	}
	return platform.SyncDir(destination)
}

func writeArchiveFile(reader io.Reader, target string, size int64, mode fs.FileMode) error {
	fd, err := unix.Open(target, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(mode.Perm()))
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), target)
	written, copyErr := io.CopyN(file, reader, size)
	if copyErr == nil && written != size {
		copyErr = io.ErrUnexpectedEOF
	}
	if copyErr == nil {
		copyErr = file.Sync()
	}
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func safeArchivePath(value string) (string, error) {
	if value == "" || !utf8.ValidString(value) || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") {
		return "", errors.New("archive contains unsafe path")
	}
	clean := filepath.ToSlash(filepath.Clean(value))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", errors.New("archive contains path traversal")
	}
	return clean, nil
}

func (m Manager) ensureRoot() error {
	if !filepath.IsAbs(m.Root) || filepath.Clean(m.Root) == "/" {
		return errors.New("supply-chain root must be an absolute non-root path")
	}
	if err := rejectSymlinkChain(filepath.Dir(m.Root)); err != nil {
		return err
	}
	for _, path := range []string{m.Root, filepath.Join(m.Root, "staging"), filepath.Join(m.Root, "objects"), filepath.Join(m.Root, "state"), filepath.Join(m.Root, "transactions")} {
		if err := platform.EnsurePrivateDir(path); err != nil {
			return err
		}
	}
	return nil
}

func (m Manager) limits() Limits {
	value := m.Limits
	defaults := DefaultLimits()
	if value.ArchiveBytes <= 0 {
		value.ArchiveBytes = defaults.ArchiveBytes
	}
	if value.ExpandedBytes <= 0 {
		value.ExpandedBytes = defaults.ExpandedBytes
	}
	if value.FileBytes <= 0 {
		value.FileBytes = defaults.FileBytes
	}
	if value.Files <= 0 {
		value.Files = defaults.Files
	}
	return value
}

func (m Manager) objectPath(id, revision string) string {
	return filepath.Join(m.Root, "objects", id, revision)
}
func (m Manager) pointerPath(id string) string { return filepath.Join(m.Root, "state", id+".json") }
func (m Manager) transactionPath(id string) string {
	return filepath.Join(m.Root, "transactions", id+".json")
}

func (m Manager) savePointer(pointer Pointer) error {
	if pointer.Schema != SchemaVersion || !safeID(pointer.ID) || pointer.Active != "" && !immutableRevision(pointer.Active) || pointer.Previous != "" && !immutableRevision(pointer.Previous) {
		return errors.New("invalid supply-chain active pointer")
	}
	data, err := json.MarshalIndent(pointer, "", "  ")
	if err != nil {
		return err
	}
	return platform.AtomicWritePrivate(append(data, '\n'), m.pointerPath(pointer.ID))
}

func (m Manager) loadPointer(id string) (Pointer, error) {
	if !safeID(id) {
		return Pointer{}, errors.New("invalid artifact ID")
	}
	data, err := platform.ReadRegularFile(m.pointerPath(id), 64<<10)
	if err != nil {
		return Pointer{}, err
	}
	var pointer Pointer
	if err := strictJSON(data, &pointer); err != nil || pointer.Schema != SchemaVersion || pointer.ID != id || pointer.Active != "" && !immutableRevision(pointer.Active) || pointer.Previous != "" && !immutableRevision(pointer.Previous) {
		return Pointer{}, errors.New("invalid supply-chain active pointer")
	}
	return pointer, nil
}

func (m Manager) saveTransaction(tx transaction) error {
	if tx.Schema != SchemaVersion || !safeTransactionID(tx.ID) || filepath.Clean(tx.Staging) != filepath.Join(m.Root, "staging", tx.ID) {
		return errors.New("invalid supply-chain transaction")
	}
	data, err := json.MarshalIndent(tx, "", "  ")
	if err != nil {
		return err
	}
	return platform.AtomicWritePrivate(append(data, '\n'), m.transactionPath(tx.ID))
}

func (m Manager) loadTransaction(id string) (transaction, error) {
	if !safeTransactionID(id) {
		return transaction{}, errors.New("invalid transaction ID")
	}
	data, err := platform.ReadRegularFile(m.transactionPath(id), 128<<10)
	if err != nil {
		return transaction{}, err
	}
	var tx transaction
	if err := strictJSON(data, &tx); err != nil || tx.Schema != SchemaVersion || tx.ID != id || tx.Source.Validate() != nil || filepath.Clean(tx.Staging) != filepath.Join(m.Root, "staging", id) {
		return transaction{}, errors.New("invalid supply-chain transaction")
	}
	return tx, nil
}

func strictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	return nil
}

func validateStoredProvenance(root string, want ResolvedSource) error {
	if err := validateObjectDir(root); err != nil {
		return err
	}
	data, err := platform.ReadRegularFile(filepath.Join(root, ".ivoai-provenance.json"), 64<<10)
	if err != nil {
		return err
	}
	var got ResolvedSource
	if err := strictJSON(data, &got); err != nil || got.Validate() != nil || got.ID != want.ID || got.Revision != want.Revision || !strings.EqualFold(got.Integrity.Digest, want.Integrity.Digest) {
		return errors.New("immutable object provenance mismatch")
	}
	return nil
}

func validateObjectDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("immutable object is not a regular directory")
	}
	return nil
}

func rejectSymlinkChain(path string) error {
	clean := filepath.Clean(path)
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(clean, string(filepath.Separator)), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("managed supply-chain path traverses symlink: %s", current)
		}
	}
	return nil
}

func within(root, path string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func randomID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return "sc_" + hex.EncodeToString(data), nil
}

func safeTransactionID(value string) bool {
	return len(value) == 35 && strings.HasPrefix(value, "sc_") && immutableRevision(strings.TrimPrefix(value, "sc_")+strings.Repeat("0", 8))
}

func safeID(value string) bool {
	if value == "" || len(value) > 128 || strings.Contains(value, "..") {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || strings.ContainsRune("._-", char)) {
			return false
		}
	}
	return true
}

func immutableRevision(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validKind(value ArtifactKind) bool {
	return value == KindSkill || value == KindComponent || value == KindHelper
}

func validHTTPS(value string) bool {
	if len(value) > 2048 || strings.ContainsAny(value, "\x00\x1b\r\n") {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.Fragment == ""
}

func safeStatus(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '_') {
			return false
		}
	}
	return true
}

func (m Manager) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

func ListPointers(root string) ([]Pointer, error) {
	entries, err := os.ReadDir(filepath.Join(root, "state"))
	if errors.Is(err, os.ErrNotExist) {
		return []Pointer{}, nil
	}
	if err != nil {
		return nil, err
	}
	manager := Manager{Root: root}
	var result []Pointer
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		pointer, err := manager.loadPointer(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		result = append(result, pointer)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// TransactionalPointerFiles returns only the small authoritative active-state
// pointers that belong in an IVOAI update snapshot. Immutable objects and
// staging journals remain outside blind update snapshots.
func TransactionalPointerFiles(root string) ([]string, error) {
	pointers, err := ListPointers(root)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(pointers))
	for _, pointer := range pointers {
		result = append(result, filepath.Join(root, "state", pointer.ID+".json"))
	}
	sort.Strings(result)
	return result, nil
}
