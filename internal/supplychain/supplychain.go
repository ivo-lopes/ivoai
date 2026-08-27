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
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ivo-lopes/ivoai/internal/observability"
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
	if !safeID(r.ID) || !validKind(r.Kind) || !validHTTPS(r.Source) || !immutableRevision(r.Revision) || !safeBoundedText(r.LogicalVersion, 128) || !safeBoundedText(r.DefaultBranch, 256) {
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
	if !sort.StringsAreSorted(r.Executables) {
		return errors.New("resolved source executable paths must be deterministically ordered")
	}
	previous := ""
	for _, value := range r.Executables {
		if value == previous {
			return errors.New("resolved source declares a duplicate executable path")
		}
		if _, err := safeArchivePath(value); err != nil {
			return errors.New("resolved source declares an unsafe executable path")
		}
		if r.Kind == KindSkill {
			return errors.New("skills cannot declare executable files during staging")
		}
		previous = value
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
	Health     Validator
	Observe    func(observability.Event)
}

// Activation binds an artifact pointer transition to an authoritative
// external index such as the skill Registry. Apply and Validate run only
// after the immutable object and pointer are valid. Rollback must restore the
// external index when either validation or the supply-chain journal commit
// fails. All callbacks must be deterministic and idempotent.
type Activation struct {
	Apply    func() error
	Validate func() error
	Rollback func() error
}

type Pipeline struct {
	Manager    Manager
	Discoverer Discoverer
	Fetcher    Fetcher
}

type Staged struct {
	TransactionID  string         `json:"transaction_id"`
	Source         ResolvedSource `json:"source"`
	ObjectPath     string         `json:"object_path"`
	ManifestDigest string         `json:"manifest_digest"`
}

type Pointer struct {
	Schema    int       `json:"schema"`
	ID        string    `json:"id"`
	Active    string    `json:"active,omitempty"`
	Previous  string    `json:"previous,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type transaction struct {
	Schema         int            `json:"schema"`
	ID             string         `json:"id"`
	Source         ResolvedSource `json:"source"`
	State          string         `json:"state"`
	Staging        string         `json:"staging"`
	Object         string         `json:"object,omitempty"`
	ManifestDigest string         `json:"manifest_digest,omitempty"`
	Previous       string         `json:"previous,omitempty"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type manifestEntry struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

type objectManifest struct {
	Schema  int             `json:"schema"`
	Entries []manifestEntry `json:"entries"`
}

type storedProvenance struct {
	Schema         int            `json:"schema"`
	Source         ResolvedSource `json:"source"`
	ManifestDigest string         `json:"manifest_digest"`
}

func (p Pipeline) Prepare(ctx context.Context, reference Reference) (staged Staged, resultErr error) {
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
	if resolved.ID != reference.ID || resolved.Kind != reference.Kind || reference.Source != "" && resolved.Source != reference.Source || reference.Version != "" && reference.Version != resolved.Revision && reference.Version != resolved.LogicalVersion {
		return Staged{}, errors.New("resolved source does not match requested artifact")
	}
	p.Manager.emit(observability.Event{Category: observability.CategorySupplyChain, Operation: observability.OperationSupplyResolve, State: observability.StateCompleted, ArtifactID: resolved.ID, Revision: resolved.Revision, RoutingReason: observability.ReasonImmutableRevision, TrustLevel: resolved.Integrity.TrustLevel})
	archive, err := p.Fetcher.Fetch(ctx, resolved)
	if err != nil {
		return Staged{}, fmt.Errorf("fetch resolved artifact: %w", err)
	}
	defer archive.Close()
	return p.Manager.StageArchive(ctx, resolved, archive)
}

func (m Manager) StageArchive(ctx context.Context, source ResolvedSource, archive io.Reader) (staged Staged, resultErr error) {
	defer func() {
		state, reason := observability.StateStaged, observability.ReasonIntegrityVerified
		if resultErr != nil {
			state, reason = observability.StateFailed, observability.ReasonValidationFailed
		}
		m.emit(observability.Event{Category: observability.CategorySupplyChain, Operation: observability.OperationSupplyStage, State: state, ArtifactID: source.ID, Revision: source.Revision, RoutingReason: reason, TrustLevel: source.Integrity.TrustLevel})
	}()
	if err := source.Validate(); err != nil {
		return Staged{}, err
	}
	if m.Structural == nil || m.Policy == nil || m.Health == nil {
		return Staged{}, errors.New("supply-chain structural, policy, and health validators are required")
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
	if err := m.Structural.Validate(ctx, source, content); err != nil {
		return Staged{}, fmt.Errorf("staged structural validation: %w", err)
	}
	if err := m.Policy.Validate(ctx, source, content); err != nil {
		return Staged{}, fmt.Errorf("staged policy validation: %w", err)
	}
	manifest, manifestDigest, err := createObjectManifest(content, limits)
	if err != nil {
		return Staged{}, err
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Staged{}, err
	}
	if err := platform.AtomicWritePrivate(append(manifestData, '\n'), filepath.Join(content, ".ivoai-manifest.json")); err != nil {
		return Staged{}, err
	}
	provenance, err := json.MarshalIndent(storedProvenance{Schema: SchemaVersion, Source: source, ManifestDigest: manifestDigest}, "", "  ")
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
		if _, err := validateStoredObject(object, &source, manifestDigest); err != nil {
			return Staged{}, err
		}
		cleanup = false
		_ = os.RemoveAll(staging)
		tx.State, tx.Object, tx.ManifestDigest, tx.UpdatedAt = "staged", object, manifestDigest, m.now()
		if err := m.saveTransaction(tx); err != nil {
			return Staged{}, err
		}
		return Staged{TransactionID: id, Source: source, ObjectPath: object, ManifestDigest: manifestDigest}, nil
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
	tx.State, tx.Object, tx.ManifestDigest, tx.UpdatedAt = "staged", object, manifestDigest, m.now()
	if err := m.saveTransaction(tx); err != nil {
		return Staged{}, err
	}
	return Staged{TransactionID: id, Source: source, ObjectPath: object, ManifestDigest: manifestDigest}, nil
}

func (m Manager) Promote(staged Staged) error {
	return m.PromoteWithActivation(staged, Activation{})
}

func (m Manager) PromoteWithActivation(staged Staged, activation Activation) (resultErr error) {
	defer func() {
		state, reason := observability.StatePromoted, observability.ReasonIntegrityVerified
		if resultErr != nil {
			state, reason = observability.StateFailed, observability.ReasonValidationFailed
		}
		m.emit(observability.Event{Category: observability.CategorySupplyChain, Operation: observability.OperationSupplyPromote, State: state, ArtifactID: staged.Source.ID, Revision: staged.Source.Revision, RoutingReason: reason, TrustLevel: staged.Source.Integrity.TrustLevel})
	}()
	if err := m.ensureRoot(); err != nil {
		return err
	}
	if m.Health == nil {
		return errors.New("supply-chain health validator is required")
	}
	if err := staged.Source.Validate(); err != nil {
		return err
	}
	want := m.objectPath(staged.Source.ID, staged.Source.Revision)
	if filepath.Clean(staged.ObjectPath) != want || !safeTransactionID(staged.TransactionID) || !immutableRevision(staged.ManifestDigest) {
		return errors.New("staged artifact does not belong to this supply-chain root")
	}
	tx, err := m.loadTransaction(staged.TransactionID)
	if err != nil {
		return err
	}
	if tx.Object != want || tx.ManifestDigest != staged.ManifestDigest || !reflect.DeepEqual(tx.Source, staged.Source) {
		return errors.New("staged artifact does not match its transaction journal")
	}
	if tx.State == "committed" {
		pointer, err := m.loadPointer(staged.Source.ID)
		if err == nil && pointer.Active == staged.Source.Revision {
			return nil
		}
		return errors.New("committed supply-chain transaction does not match active pointer")
	}
	if tx.State != "staged" {
		return errors.New("staged artifact transaction is not promotable")
	}
	if _, err := validateStoredObject(want, &staged.Source, staged.ManifestDigest); err != nil {
		return err
	}
	pointer, err := m.loadPointer(staged.Source.ID)
	pointerExisted := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if pointer.Active == staged.Source.Revision {
		tx.State, tx.UpdatedAt = "committed", m.now()
		return m.saveTransaction(tx)
	}
	previousPointer := pointer
	tx.State, tx.Previous, tx.UpdatedAt = "promoting", pointer.Active, m.now()
	if err := m.saveTransaction(tx); err != nil {
		return err
	}
	pointer.Schema, pointer.ID, pointer.Previous, pointer.Active, pointer.UpdatedAt = SchemaVersion, staged.Source.ID, pointer.Active, staged.Source.Revision, m.now()
	if err := m.savePointer(pointer); err != nil {
		return err
	}
	if err := m.Health.Validate(context.Background(), staged.Source, want); err != nil {
		rollbackErr := m.restorePromotion(staged.Source.ID, previousPointer, pointerExisted)
		tx.State, tx.UpdatedAt = "rolled_back", m.now()
		journalErr := m.saveTransaction(tx)
		return errors.Join(fmt.Errorf("post-promotion health validation: %w", err), rollbackErr, journalErr)
	}
	if _, err := validateStoredObject(want, &staged.Source, staged.ManifestDigest); err != nil {
		rollbackErr := m.restorePromotion(staged.Source.ID, previousPointer, pointerExisted)
		tx.State, tx.UpdatedAt = "rolled_back", m.now()
		journalErr := m.saveTransaction(tx)
		return errors.Join(fmt.Errorf("post-promotion integrity validation: %w", err), rollbackErr, journalErr)
	}
	if activation.Apply != nil {
		if err := activation.Apply(); err != nil {
			activationErr := callActivation(activation.Rollback)
			rollbackErr := m.restorePromotion(staged.Source.ID, previousPointer, pointerExisted)
			tx.State, tx.UpdatedAt = "rolled_back", m.now()
			journalErr := m.saveTransaction(tx)
			return errors.Join(fmt.Errorf("activate promoted artifact: %w", err), activationErr, rollbackErr, journalErr)
		}
	}
	if activation.Validate != nil {
		if err := activation.Validate(); err != nil {
			activationErr := callActivation(activation.Rollback)
			rollbackErr := m.restorePromotion(staged.Source.ID, previousPointer, pointerExisted)
			tx.State, tx.UpdatedAt = "rolled_back", m.now()
			journalErr := m.saveTransaction(tx)
			return errors.Join(fmt.Errorf("validate promoted activation: %w", err), activationErr, rollbackErr, journalErr)
		}
	}
	tx.State, tx.UpdatedAt = "committed", m.now()
	if err := m.saveTransaction(tx); err != nil {
		activationErr := callActivation(activation.Rollback)
		rollbackErr := m.restorePromotion(staged.Source.ID, previousPointer, pointerExisted)
		tx.State, tx.UpdatedAt = "rolled_back", m.now()
		journalErr := m.saveTransaction(tx)
		return errors.Join(err, activationErr, rollbackErr, journalErr)
	}
	return nil
}

func (m Manager) Rollback(id string) (bool, error) {
	return m.RollbackWithActivation(id, Activation{})
}

func (m Manager) RollbackWithActivation(id string, activation Activation) (rolled bool, resultErr error) {
	defer func() {
		state, reason := observability.StateRolledBack, observability.ReasonRollbackComplete
		if resultErr != nil {
			state, reason = observability.StateFailed, observability.ReasonValidationFailed
		}
		m.emit(observability.Event{Category: observability.CategorySupplyChain, Operation: observability.OperationSupplyRollback, State: state, ArtifactID: id, RoutingReason: reason})
	}()
	if err := m.ensureRoot(); err != nil {
		return false, err
	}
	if m.Health == nil {
		return false, errors.New("supply-chain health validator is required")
	}
	pointer, err := m.loadPointer(id)
	if errors.Is(err, os.ErrNotExist) || pointer.Previous == "" {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	previousSource, err := validateStoredObject(m.objectPath(id, pointer.Previous), nil, "")
	if err != nil || previousSource.ID != id || previousSource.Revision != pointer.Previous {
		if err == nil {
			err = errors.New("previous artifact provenance does not match rollback pointer")
		}
		return false, err
	}
	current := pointer
	pointer.Active, pointer.Previous, pointer.UpdatedAt = pointer.Previous, "", m.now()
	if err := m.savePointer(pointer); err != nil {
		return false, err
	}
	if err := m.Health.Validate(context.Background(), previousSource, m.objectPath(id, pointer.Active)); err != nil {
		return false, errors.Join(fmt.Errorf("post-rollback health validation: %w", err), m.savePointer(current))
	}
	if _, err := validateStoredObject(m.objectPath(id, pointer.Active), &previousSource, ""); err != nil {
		return false, errors.Join(fmt.Errorf("post-rollback integrity validation: %w", err), m.savePointer(current))
	}
	if activation.Apply != nil {
		if err := activation.Apply(); err != nil {
			return false, errors.Join(fmt.Errorf("activate rolled-back artifact: %w", err), callActivation(activation.Rollback), m.savePointer(current))
		}
	}
	if activation.Validate != nil {
		if err := activation.Validate(); err != nil {
			return false, errors.Join(fmt.Errorf("validate rolled-back activation: %w", err), callActivation(activation.Rollback), m.savePointer(current))
		}
	}
	return true, nil
}

func callActivation(callback func() error) error {
	if callback == nil {
		return nil
	}
	return callback()
}

func (m Manager) Active(id string) (ResolvedSource, string, error) {
	if err := validateManagedRoot(m.Root); err != nil {
		return ResolvedSource{}, "", err
	}
	if err := validateManagedSubdirectory(m.Root, "state"); err != nil {
		return ResolvedSource{}, "", err
	}
	pointer, err := m.loadPointer(id)
	if err != nil {
		return ResolvedSource{}, "", err
	}
	if pointer.Active == "" {
		return ResolvedSource{}, "", os.ErrNotExist
	}
	path := m.objectPath(id, pointer.Active)
	source, err := validateStoredObject(path, nil, "")
	if err != nil || source.ID != id || source.Revision != pointer.Active {
		return ResolvedSource{}, "", errors.New("active artifact provenance is invalid")
	}
	return source, path, nil
}

func (m Manager) Recover() (int, error) {
	return m.RecoverWithActivation(nil)
}

// RecoverWithActivation restores interrupted pointer transitions and then
// asks the caller to reconcile its authoritative external index for each
// affected artifact. The callback receives only a canonical artifact ID.
func (m Manager) RecoverWithActivation(reconcile func(string) error) (int, error) {
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
		if tx.State == "promoting" {
			pointer, pointerErr := m.loadPointer(tx.Source.ID)
			if pointerErr == nil && pointer.Active == tx.Source.Revision {
				previous := Pointer{Schema: SchemaVersion, ID: tx.Source.ID, Active: tx.Previous, UpdatedAt: m.now()}
				if tx.Previous != "" {
					previous.Previous = ""
				}
				if err := m.restorePromotion(tx.Source.ID, previous, tx.Previous != ""); err != nil {
					return recovered, err
				}
			} else if pointerErr != nil && !errors.Is(pointerErr, os.ErrNotExist) {
				return recovered, pointerErr
			}
			tx.State, tx.UpdatedAt = "rolled_back", m.now()
			if err := m.saveTransaction(tx); err != nil {
				return recovered, err
			}
			if reconcile != nil {
				if err := reconcile(tx.Source.ID); err != nil {
					return recovered, fmt.Errorf("reconcile recovered artifact %s: %w", tx.Source.ID, err)
				}
			}
			recovered++
			continue
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
	} else if value.ArchiveBytes > defaults.ArchiveBytes {
		value.ArchiveBytes = defaults.ArchiveBytes
	}
	if value.ExpandedBytes <= 0 {
		value.ExpandedBytes = defaults.ExpandedBytes
	} else if value.ExpandedBytes > defaults.ExpandedBytes {
		value.ExpandedBytes = defaults.ExpandedBytes
	}
	if value.FileBytes <= 0 {
		value.FileBytes = defaults.FileBytes
	} else if value.FileBytes > defaults.FileBytes {
		value.FileBytes = defaults.FileBytes
	}
	if value.Files <= 0 {
		value.Files = defaults.Files
	} else if value.Files > defaults.Files {
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
	if pointer.Schema != SchemaVersion || !safeID(pointer.ID) || pointer.Active != "" && !immutableRevision(pointer.Active) || pointer.Previous != "" && !immutableRevision(pointer.Previous) || pointer.UpdatedAt.IsZero() {
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
	if err := strictJSON(data, &pointer); err != nil || pointer.Schema != SchemaVersion || pointer.ID != id || pointer.Active != "" && !immutableRevision(pointer.Active) || pointer.Previous != "" && !immutableRevision(pointer.Previous) || pointer.UpdatedAt.IsZero() {
		return Pointer{}, errors.New("invalid supply-chain active pointer")
	}
	return pointer, nil
}

func (m Manager) saveTransaction(tx transaction) error {
	if err := validateTransaction(m.Root, tx); err != nil || tx.UpdatedAt.IsZero() {
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
	if err := strictJSON(data, &tx); err != nil || tx.ID != id || validateTransaction(m.Root, tx) != nil || tx.UpdatedAt.IsZero() {
		return transaction{}, errors.New("invalid supply-chain transaction")
	}
	return tx, nil
}

func validateTransaction(root string, tx transaction) error {
	if tx.Schema != SchemaVersion || !safeTransactionID(tx.ID) || tx.Source.Validate() != nil || filepath.Clean(tx.Staging) != filepath.Join(root, "staging", tx.ID) {
		return errors.New("invalid supply-chain transaction")
	}
	switch tx.State {
	case "staging":
		if tx.Object != "" || tx.ManifestDigest != "" || tx.Previous != "" {
			return errors.New("invalid staging transaction metadata")
		}
	case "rolled_back":
		if tx.Object == "" && tx.ManifestDigest == "" && tx.Previous == "" {
			return nil
		}
		fallthrough
	case "staged", "promoting", "committed":
		if filepath.Clean(tx.Object) != filepath.Join(root, "objects", tx.Source.ID, tx.Source.Revision) || !immutableRevision(tx.ManifestDigest) || tx.Previous != "" && !immutableRevision(tx.Previous) {
			return errors.New("invalid resolved transaction metadata")
		}
	default:
		return errors.New("invalid transaction state")
	}
	return nil
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

func createObjectManifest(root string, limits Limits) (objectManifest, string, error) {
	manifest := objectManifest{Schema: SchemaVersion, Entries: []manifestEntry{}}
	var total int64
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}
		relative, err := filepath.Rel(root, current)
		if err != nil || !within(root, current) {
			return errors.New("staged object escaped its root")
		}
		relative = filepath.ToSlash(relative)
		if relative == ".ivoai-manifest.json" || relative == ".ivoai-provenance.json" {
			return nil
		}
		if strings.HasPrefix(relative, ".ivoai-") {
			return errors.New("staged object contains unexpected reserved metadata")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("staged object contains a symlink")
		}
		item := manifestEntry{Path: relative, Mode: uint32(info.Mode().Perm())}
		switch {
		case info.IsDir():
			item.Kind = "directory"
			if info.Mode().Perm() != 0o700 {
				return errors.New("staged object contains unsafe directory mode")
			}
		case info.Mode().IsRegular():
			item.Kind, item.Size = "file", info.Size()
			if info.Size() < 0 || info.Size() > limits.FileBytes || total+info.Size() > limits.ExpandedBytes {
				return errors.New("staged object exceeds manifest size limit")
			}
			if info.Mode().Perm() != 0o600 && info.Mode().Perm() != 0o700 {
				return errors.New("staged object contains unsafe file mode")
			}
			data, err := platform.ReadRegularFile(current, info.Size()+1)
			if err != nil {
				return err
			}
			digest := sha256.Sum256(data)
			item.SHA256 = hex.EncodeToString(digest[:])
			total += info.Size()
		default:
			return errors.New("staged object contains unsupported file type")
		}
		manifest.Entries = append(manifest.Entries, item)
		if len(manifest.Entries) > limits.Files {
			return errors.New("staged object exceeds manifest entry limit")
		}
		return nil
	})
	if err != nil {
		return objectManifest{}, "", err
	}
	sort.Slice(manifest.Entries, func(i, j int) bool { return manifest.Entries[i].Path < manifest.Entries[j].Path })
	data, err := json.Marshal(manifest)
	if err != nil {
		return objectManifest{}, "", err
	}
	digest := sha256.Sum256(data)
	return manifest, hex.EncodeToString(digest[:]), nil
}

func validateStoredObject(root string, want *ResolvedSource, wantManifestDigest string) (ResolvedSource, error) {
	if err := rejectSymlinkChain(root); err != nil {
		return ResolvedSource{}, err
	}
	if err := validateObjectDir(root); err != nil {
		return ResolvedSource{}, err
	}
	data, err := platform.ReadRegularFile(filepath.Join(root, ".ivoai-provenance.json"), 64<<10)
	if err != nil {
		return ResolvedSource{}, err
	}
	var provenance storedProvenance
	if err := strictJSON(data, &provenance); err != nil || provenance.Schema != SchemaVersion || provenance.Source.Validate() != nil || !immutableRevision(provenance.ManifestDigest) {
		return ResolvedSource{}, errors.New("immutable object provenance mismatch")
	}
	if want != nil && !reflect.DeepEqual(provenance.Source, *want) || wantManifestDigest != "" && provenance.ManifestDigest != wantManifestDigest {
		return ResolvedSource{}, errors.New("immutable object provenance mismatch")
	}
	manifestData, err := platform.ReadRegularFile(filepath.Join(root, ".ivoai-manifest.json"), 8<<20)
	if err != nil {
		return ResolvedSource{}, err
	}
	var storedManifest objectManifest
	if err := strictJSON(manifestData, &storedManifest); err != nil || storedManifest.Schema != SchemaVersion {
		return ResolvedSource{}, errors.New("immutable object manifest is invalid")
	}
	canonical, err := json.Marshal(storedManifest)
	if err != nil {
		return ResolvedSource{}, err
	}
	storedDigest := sha256.Sum256(canonical)
	if hex.EncodeToString(storedDigest[:]) != provenance.ManifestDigest {
		return ResolvedSource{}, errors.New("immutable object manifest digest mismatch")
	}
	actualManifest, actualDigest, err := createObjectManifest(root, DefaultLimits())
	if err != nil || actualDigest != provenance.ManifestDigest || !reflect.DeepEqual(actualManifest, storedManifest) {
		return ResolvedSource{}, errors.New("immutable object content integrity mismatch")
	}
	return provenance.Source, nil
}

func (m Manager) restorePromotion(id string, previous Pointer, existed bool) error {
	if existed {
		previous.Schema, previous.ID, previous.UpdatedAt = SchemaVersion, id, m.now()
		return m.savePointer(previous)
	}
	path := m.pointerPath(id)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("refusing unsafe supply-chain pointer rollback")
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return platform.SyncDir(filepath.Dir(path))
}

func (m Manager) emit(event observability.Event) {
	if m.Observe == nil {
		return
	}
	normalized, err := observability.Normalize(event)
	if err == nil {
		m.Observe(normalized)
	}
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
	if len(value) != 35 || !strings.HasPrefix(value, "sc_") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sc_"))
	return err == nil
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
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.Fragment == "" && parsed.RawQuery == ""
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

func safeBoundedText(value string, limit int) bool {
	if len(value) > limit || !utf8.ValidString(value) || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\x1b\r\n") {
		return false
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
	if err := validateManagedRoot(root); errors.Is(err, os.ErrNotExist) {
		return []Pointer{}, nil
	} else if err != nil {
		return nil, err
	}
	stateRoot := filepath.Join(root, "state")
	stateInfo, err := os.Lstat(stateRoot)
	if errors.Is(err, os.ErrNotExist) {
		return []Pointer{}, nil
	}
	if err != nil {
		return nil, err
	}
	if stateInfo.Mode()&os.ModeSymlink != 0 || !stateInfo.IsDir() || stateInfo.Mode().Perm() != 0o700 {
		return nil, errors.New("supply-chain state root is unsafe")
	}
	entries, err := os.ReadDir(stateRoot)
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

func validateManagedRoot(root string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) == "/" {
		return errors.New("supply-chain root must be an absolute non-root path")
	}
	if err := rejectSymlinkChain(root); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return errors.New("supply-chain root is unsafe")
	}
	return nil
}

// ValidateRoot performs a read-only health check of the managed supply-chain
// layout. Missing subdirectories are valid before first use; existing paths
// must remain private directories and transaction metadata must be parseable.
func ValidateRoot(root string) error {
	if err := validateManagedRoot(root); err != nil {
		return err
	}
	manager := Manager{Root: root}
	for _, name := range []string{"staging", "objects", "state", "transactions"} {
		if err := validateManagedSubdirectory(root, name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	transactions := filepath.Join(root, "transactions")
	entries, err := os.ReadDir(transactions)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || !safeTransactionID(strings.TrimSuffix(entry.Name(), ".json")) {
			return errors.New("supply-chain transaction root contains an unexpected entry")
		}
		if _, err := manager.loadTransaction(strings.TrimSuffix(entry.Name(), ".json")); err != nil {
			return err
		}
	}
	_, err = ListPointers(root)
	return err
}

func validateManagedSubdirectory(root, name string) error {
	path := filepath.Join(root, name)
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("supply-chain %s root is unsafe", name)
	}
	return nil
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
