package migration

import (
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
	"syscall"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"golang.org/x/sys/unix"
)

type Phase string

const (
	PhasePreparing   Phase = "preparing"
	PhasePrepared    Phase = "prepared"
	PhaseMigrating   Phase = "migrating"
	PhaseMigrated    Phase = "migrated"
	PhasePromoted    Phase = "promoted"
	PhaseVerifying   Phase = "verifying"
	PhaseCommitted   Phase = "committed"
	PhaseRollingBack Phase = "rolling_back"
	PhaseRolledBack  Phase = "rolled_back"
)

type FileSpec struct {
	Name       string
	Artifact   Artifact
	Path       string
	Root       string
	Optional   bool
	Executable bool
}

type Snapshot struct {
	Name       string   `json:"name"`
	Artifact   Artifact `json:"artifact"`
	File       string   `json:"file,omitempty"`
	Path       string   `json:"path"`
	Root       string   `json:"root"`
	Optional   bool     `json:"optional,omitempty"`
	Existed    bool     `json:"existed"`
	Mode       uint32   `json:"mode,omitempty"`
	UID        int      `json:"uid,omitempty"`
	GID        int      `json:"gid,omitempty"`
	Size       int64    `json:"size,omitempty"`
	SHA256     string   `json:"sha256,omitempty"`
	Executable bool     `json:"executable,omitempty"`
}

type Journal struct {
	FormatVersion int         `json:"format_version"`
	ID            string      `json:"transaction_id"`
	SourceVersion string      `json:"source_version"`
	TargetVersion string      `json:"target_version"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
	Phase         Phase       `json:"state"`
	SourceSchemas Schemas     `json:"source_schemas"`
	TargetSchemas Schemas     `json:"target_schemas"`
	SnapshotBytes int64       `json:"snapshot_bytes"`
	Snapshots     []Snapshot  `json:"snapshots"`
	Committed     []FileState `json:"committed_state,omitempty"`
}

type FileState struct {
	Name    string `json:"name"`
	Existed bool   `json:"existed"`
	Size    int64  `json:"size,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
}

type Manager struct {
	Root               string
	Files              []FileSpec
	AllowedRoots       []string
	Registry           Registry
	Now                func() time.Time
	Retention          int
	MaxSnapshotBytes   int64
	AvailableDiskBytes func(string) (uint64, error)
	CheckWritableRoot  func(string) error
}

type Transaction struct {
	manager Manager
	journal Journal
	lock    *os.File
}

var ErrRecoveryRequired = errors.New("an interrupted ivoai update requires recovery")

// NeedsRecovery validates the current journal without changing it. It is used
// by dry-run/preflight paths that must surface interruption but remain
// strictly read-only.
func (m Manager) NeedsRecovery() (bool, error) {
	journal, err := m.readJournal()
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !terminal(journal.Phase), nil
}

// PreflightSnapshot validates the managed-file boundary, permissions and
// bounded snapshot capacity without creating the update root or journal.
func (m Manager) PreflightSnapshot() (int64, error) {
	if err := m.validateRoot(); err != nil {
		return 0, err
	}
	if err := m.validateSpecs(); err != nil {
		return 0, err
	}
	return m.preflightSnapshotSpace()
}

func (m Manager) Begin(ctx context.Context, sourceVersion, targetVersion string, sourceSchemas, targetSchemas Schemas) (*Transaction, error) {
	for artifact, target := range targetSchemas {
		source, ok := sourceSchemas[artifact]
		if !ok || source < 0 || target < source {
			return nil, fmt.Errorf("unsupported schema transition for %s: %d -> %d", artifact, source, target)
		}
	}
	lock, err := m.acquire()
	if err != nil {
		return nil, err
	}
	release := true
	defer func() {
		if release {
			releaseLock(lock)
		}
	}()
	if existing, err := m.readJournal(); err == nil && !terminal(existing.Phase) {
		return nil, fmt.Errorf("%w: transaction %s is %s", ErrRecoveryRequired, existing.ID, existing.Phase)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	snapshotBytes, err := m.PreflightSnapshot()
	if err != nil {
		return nil, err
	}
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	now := m.now()
	journal := Journal{FormatVersion: 1, ID: id, SourceVersion: sourceVersion, TargetVersion: targetVersion, CreatedAt: now, UpdatedAt: now, Phase: PhasePreparing, SourceSchemas: cloneSchemas(sourceSchemas), TargetSchemas: cloneSchemas(targetSchemas), SnapshotBytes: snapshotBytes}
	tx := &Transaction{manager: m, journal: journal, lock: lock}
	if err := platform.EnsurePrivateDir(tx.snapshotDir()); err != nil {
		return nil, err
	}
	if err := tx.persist(); err != nil {
		return nil, err
	}
	for index, spec := range m.sortedSpecs() {
		snapshot, snapshotErr := tx.snapshot(index, spec)
		if snapshotErr != nil {
			tx.journal.Phase = PhaseRolledBack
			_ = tx.persist()
			return nil, snapshotErr
		}
		tx.journal.Snapshots = append(tx.journal.Snapshots, snapshot)
		if err := tx.persist(); err != nil {
			return nil, err
		}
	}
	tx.journal.Phase = PhasePrepared
	if err := tx.persist(); err != nil {
		return nil, err
	}
	release = false
	return tx, nil
}

func (m Manager) Recover(ctx context.Context) (bool, error) {
	lock, err := m.acquire()
	if err != nil {
		return false, err
	}
	defer releaseLock(lock)
	journal, err := m.readJournal()
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if terminal(journal.Phase) {
		return false, nil
	}
	if len(m.Files) == 0 {
		m.Files = fileSpecsFromSnapshots(journal.Snapshots)
	}
	if err := m.validateSpecs(); err != nil {
		return false, err
	}
	tx := &Transaction{manager: m, journal: journal, lock: lock}
	if journal.Phase == PhasePreparing {
		tx.journal.Phase = PhaseRolledBack
		return true, tx.persist()
	}
	return true, tx.rollback(ctx, false)
}

func (m Manager) RollbackLast(ctx context.Context) (bool, error) {
	return m.rollbackLast(ctx, false)
}

func (m Manager) RollbackLastForce(ctx context.Context) (bool, error) {
	return m.rollbackLast(ctx, true)
}

func (m Manager) rollbackLast(ctx context.Context, force bool) (bool, error) {
	lock, err := m.acquire()
	if err != nil {
		return false, err
	}
	defer releaseLock(lock)
	journal, err := m.readJournal()
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if journal.Phase == PhaseRolledBack {
		return true, nil
	}
	if len(m.Files) == 0 {
		m.Files = fileSpecsFromSnapshots(journal.Snapshots)
	}
	if journal.Phase == PhaseCommitted && !force {
		if err := m.verifyCommittedState(journal); err != nil {
			return false, fmt.Errorf("managed files changed after update; refusing rollback without --force: %w", err)
		}
	}
	if err := m.validateSpecs(); err != nil {
		return false, err
	}
	tx := &Transaction{manager: m, journal: journal, lock: lock}
	return true, tx.rollback(ctx, false)
}

func (t *Transaction) ID() string { return t.journal.ID }

func (t *Transaction) MarkMigrating() error { return t.transition(PhaseMigrating) }
func (t *Transaction) MarkMigrated() error  { return t.transition(PhaseMigrated) }

func (t *Transaction) Apply(ctx context.Context) error {
	plan, err := t.manager.Registry.Resolve(t.journal.SourceSchemas, t.journal.TargetSchemas)
	if err != nil {
		return err
	}
	t.journal.Phase = PhaseMigrating
	if err := t.persist(); err != nil {
		return err
	}
	workspace := Workspace{Files: map[Artifact]string{}, NamedFiles: map[string]string{}}
	for _, spec := range t.manager.Files {
		workspace.NamedFiles[spec.Name] = spec.Path
		if _, present := workspace.Files[spec.Artifact]; !present {
			workspace.Files[spec.Artifact] = spec.Path
		}
	}
	if err := plan.Apply(ctx, workspace); err != nil {
		return err
	}
	t.journal.Phase = PhaseMigrated
	return t.persist()
}

// ApplyPrepared is called only by a checksum-verified candidate started by the
// lock-owning updater. The parent records phase transitions; the candidate
// contributes the migration registry shipped with the target release.
func (m Manager) ApplyPrepared(ctx context.Context, transactionID string) error {
	journal, err := m.readJournal()
	if err != nil {
		return err
	}
	if !validID(transactionID) || journal.ID != transactionID || journal.Phase != PhaseMigrating {
		return errors.New("update migration transaction is not prepared")
	}
	m.Files = fileSpecsFromSnapshots(journal.Snapshots)
	if len(m.AllowedRoots) == 0 {
		for _, spec := range m.Files {
			m.AllowedRoots = append(m.AllowedRoots, spec.Root)
		}
	}
	if err := m.validateSpecs(); err != nil {
		return err
	}
	plan, err := m.Registry.Resolve(journal.SourceSchemas, journal.TargetSchemas)
	if err != nil {
		return err
	}
	workspace := Workspace{Files: map[Artifact]string{}, NamedFiles: map[string]string{}}
	for _, spec := range m.Files {
		workspace.NamedFiles[spec.Name] = spec.Path
		if _, present := workspace.Files[spec.Artifact]; !present {
			workspace.Files[spec.Artifact] = spec.Path
		}
	}
	return plan.Apply(ctx, workspace)
}

func (t *Transaction) MarkPromoted() error  { return t.transition(PhasePromoted) }
func (t *Transaction) MarkVerifying() error { return t.transition(PhaseVerifying) }

func (t *Transaction) Commit() error {
	committed, err := t.manager.captureCurrentState()
	if err != nil {
		return err
	}
	t.journal.Committed = committed
	if err := t.persist(); err != nil {
		return err
	}
	// Retention is part of the transaction while the update lock and rollback
	// capability are still available. Once committed, cleanup failures must not
	// turn into an impossible attempt to roll back a closed transaction.
	if err := t.manager.prune(t.journal.ID); err != nil {
		return err
	}
	if err := t.transition(PhaseCommitted); err != nil {
		return err
	}
	releaseLock(t.lock)
	t.lock = nil
	return nil
}

func (m Manager) captureCurrentState() ([]FileState, error) {
	result := make([]FileState, 0, len(m.Files))
	for _, spec := range m.sortedSpecs() {
		state := FileState{Name: spec.Name}
		data, err := platform.ReadRegularFile(spec.Path, 512<<20)
		if errors.Is(err, os.ErrNotExist) && spec.Optional {
			result = append(result, state)
			continue
		}
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		state.Existed, state.Size, state.SHA256 = true, int64(len(data)), hex.EncodeToString(sum[:])
		result = append(result, state)
	}
	return result, nil
}

func (m Manager) verifyCommittedState(journal Journal) error {
	if len(journal.Committed) == 0 {
		return errors.New("transaction predates committed-state drift protection")
	}
	current, err := m.captureCurrentState()
	if err != nil {
		return err
	}
	if len(current) != len(journal.Committed) {
		return errors.New("managed file set changed")
	}
	for index := range current {
		if current[index] != journal.Committed[index] {
			return fmt.Errorf("managed file %s changed", current[index].Name)
		}
	}
	return nil
}

func (t *Transaction) Rollback(ctx context.Context) error {
	if t.lock == nil {
		return errors.New("update transaction is closed")
	}
	err := t.rollback(ctx, true)
	releaseLock(t.lock)
	t.lock = nil
	return err
}

func (t *Transaction) transition(phase Phase) error {
	t.journal.Phase = phase
	return t.persist()
}

func (t *Transaction) rollback(_ context.Context, persistPhase bool) error {
	if t.journal.Phase == PhaseRolledBack {
		return nil
	}
	t.journal.Phase = PhaseRollingBack
	if persistPhase {
		if err := t.persist(); err != nil {
			return err
		}
	} else if err := t.persist(); err != nil {
		return err
	}
	for index := len(t.journal.Snapshots) - 1; index >= 0; index-- {
		snapshot := t.journal.Snapshots[index]
		spec := FileSpec{Name: snapshot.Name, Artifact: snapshot.Artifact, Path: snapshot.Path, Root: snapshot.Root, Optional: snapshot.Optional, Executable: snapshot.Executable}
		if !t.manager.allowedRoot(spec.Root) {
			return fmt.Errorf("snapshot references disallowed managed root for %q", snapshot.Name)
		}
		if err := validateSpec(spec); err != nil {
			return err
		}
		if !snapshot.Existed {
			if info, err := os.Lstat(spec.Path); err == nil {
				if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
					return fmt.Errorf("refusing to remove non-regular restored path %s", spec.Path)
				}
				if err := os.Remove(spec.Path); err != nil {
					return err
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		data, err := platform.ReadRegularFile(filepath.Join(t.snapshotDir(), snapshot.File), 512<<20)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != snapshot.SHA256 || int64(len(data)) != snapshot.Size {
			return fmt.Errorf("snapshot checksum mismatch for %s", snapshot.Name)
		}
		mode := fs.FileMode(snapshot.Mode)
		if err := platform.AtomicWriteFileOwned(data, spec.Path, mode, snapshot.UID, snapshot.GID); err != nil {
			return err
		}
	}
	t.journal.Phase = PhaseRolledBack
	return t.persist()
}

func (t *Transaction) snapshot(index int, spec FileSpec) (Snapshot, error) {
	result := Snapshot{Name: spec.Name, Artifact: spec.Artifact, Path: filepath.Clean(spec.Path), Root: filepath.Clean(spec.Root), Optional: spec.Optional, Executable: spec.Executable}
	info, err := os.Lstat(spec.Path)
	if errors.Is(err, os.ErrNotExist) && spec.Optional {
		return result, nil
	}
	if err != nil {
		return Snapshot{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Snapshot{}, fmt.Errorf("managed update path must be a regular file: %s", spec.Path)
	}
	data, err := platform.ReadRegularFile(spec.Path, 512<<20)
	if err != nil {
		return Snapshot{}, err
	}
	name := fmt.Sprintf("%03d-%s.snapshot", index, spec.Name)
	if err := platform.AtomicWritePrivate(data, filepath.Join(t.snapshotDir(), name)); err != nil {
		return Snapshot{}, err
	}
	sum := sha256.Sum256(data)
	result.Existed, result.File, result.Mode, result.Size, result.SHA256 = true, name, uint32(info.Mode().Perm()), int64(len(data)), hex.EncodeToString(sum[:])
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		result.UID, result.GID = int(stat.Uid), int(stat.Gid)
	}
	return result, nil
}

func (t *Transaction) snapshotDir() string {
	return filepath.Join(t.manager.Root, "snapshots", t.journal.ID)
}

func (t *Transaction) persist() error {
	t.journal.UpdatedAt = t.manager.now()
	data, err := json.MarshalIndent(t.journal, "", "  ")
	if err != nil {
		return err
	}
	return platform.AtomicWritePrivate(append(data, '\n'), filepath.Join(t.manager.Root, "current.json"))
}

func (m Manager) readJournal() (Journal, error) {
	data, err := platform.ReadRegularFile(filepath.Join(m.Root, "current.json"), 4<<20)
	if err != nil {
		return Journal{}, err
	}
	var journal Journal
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&journal); err != nil {
		return Journal{}, fmt.Errorf("corrupted update journal: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Journal{}, errors.New("corrupted update journal has trailing data")
	}
	if journal.FormatVersion != 1 || journal.ID == "" || !validID(journal.ID) || !validPhase(journal.Phase) {
		return Journal{}, errors.New("corrupted update journal metadata")
	}
	if journal.SourceVersion == "" || journal.TargetVersion == "" || journal.CreatedAt.IsZero() || journal.UpdatedAt.IsZero() || journal.UpdatedAt.Before(journal.CreatedAt) {
		return Journal{}, errors.New("corrupted update journal version or timestamp metadata")
	}
	if err := validateJournalSchemas(journal.SourceSchemas, journal.TargetSchemas); err != nil {
		return Journal{}, err
	}
	if len(journal.Snapshots) > 1024 || journal.SnapshotBytes < 0 || journal.SnapshotBytes > 1<<30 {
		return Journal{}, errors.New("corrupted update journal snapshot bounds")
	}
	seen := map[string]bool{}
	for _, snapshot := range journal.Snapshots {
		if snapshot.Name == "" || strings.ContainsAny(snapshot.Name, `/\\`) || seen[snapshot.Name] || !validArtifact(snapshot.Artifact) || snapshot.Path == "" || snapshot.Root == "" || !filepath.IsAbs(snapshot.Path) || !filepath.IsAbs(snapshot.Root) {
			return Journal{}, errors.New("corrupted update journal snapshot metadata")
		}
		seen[snapshot.Name] = true
		if snapshot.Existed {
			if filepath.Base(snapshot.File) != snapshot.File || snapshot.File == "." || snapshot.Size < 0 || snapshot.Size > 512<<20 || snapshot.Mode == 0 || snapshot.Mode&^0o777 != 0 || snapshot.UID < 0 || snapshot.GID < 0 || len(snapshot.SHA256) != 64 {
				return Journal{}, errors.New("corrupted update journal snapshot payload")
			}
			if _, err := hex.DecodeString(snapshot.SHA256); err != nil {
				return Journal{}, errors.New("corrupted update journal snapshot checksum")
			}
		} else if snapshot.File != "" || snapshot.Size != 0 || snapshot.SHA256 != "" || snapshot.Mode != 0 || snapshot.Executable {
			return Journal{}, errors.New("corrupted absent snapshot metadata")
		}
	}
	if len(journal.Committed) > 0 {
		if len(journal.Committed) != len(journal.Snapshots) {
			return Journal{}, errors.New("corrupted committed-state metadata")
		}
		committedNames := map[string]bool{}
		for _, state := range journal.Committed {
			if state.Name == "" || committedNames[state.Name] || !seen[state.Name] || state.Size < 0 || state.Existed && len(state.SHA256) != 64 || !state.Existed && (state.Size != 0 || state.SHA256 != "") {
				return Journal{}, errors.New("corrupted committed-state metadata")
			}
			if state.Existed {
				if _, err := hex.DecodeString(state.SHA256); err != nil {
					return Journal{}, errors.New("corrupted committed-state checksum")
				}
			}
			committedNames[state.Name] = true
		}
	}
	return journal, nil
}

func validateJournalSchemas(source, target Schemas) error {
	if len(source) == 0 || len(target) == 0 {
		return errors.New("corrupted update journal schemas")
	}
	for artifact, targetSchema := range target {
		sourceSchema, ok := source[artifact]
		if !ok || !validArtifact(artifact) || sourceSchema < 0 || targetSchema < sourceSchema {
			return errors.New("corrupted update journal schemas")
		}
	}
	for artifact := range source {
		if !validArtifact(artifact) {
			return errors.New("corrupted update journal schemas")
		}
	}
	return nil
}

func validArtifact(artifact Artifact) bool {
	switch artifact {
	case ArtifactExecutable, ArtifactConfig, ArtifactState, ArtifactOwnership, ArtifactSecrets, ArtifactComponents, ArtifactSkillRegistry, ArtifactSupplyChain, ArtifactServer:
		return true
	default:
		return false
	}
}

func (m Manager) acquire() (*os.File, error) {
	if err := m.validateRoot(); err != nil {
		return nil, err
	}
	if err := platform.EnsurePrivateDir(m.Root); err != nil {
		return nil, err
	}
	path := filepath.Join(m.Root, "update.lock")
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("refusing symlink update lock")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("another ivoai update is in progress")
	}
	return file, nil
}

func (m Manager) validateRoot() error {
	if !filepath.IsAbs(m.Root) || filepath.Clean(m.Root) == "/" {
		return errors.New("update root must be an absolute non-root path")
	}
	if err := rejectSymlinkChain(filepath.Dir(m.Root)); err != nil {
		return err
	}
	if info, err := os.Lstat(m.Root); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("update root must be a regular directory")
		}
		if err := unix.Access(m.Root, unix.W_OK|unix.X_OK); err != nil {
			return fmt.Errorf("update root is not writable: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else {
		ancestor := filepath.Dir(m.Root)
		for {
			if info, statErr := os.Lstat(ancestor); statErr == nil {
				if !info.IsDir() || unix.Access(ancestor, unix.W_OK|unix.X_OK) != nil {
					return errors.New("update root parent is not writable")
				}
				break
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return statErr
			}
			parent := filepath.Dir(ancestor)
			if parent == ancestor {
				return errors.New("cannot find a writable update root ancestor")
			}
			ancestor = parent
		}
	}
	return nil
}

func releaseLock(file *os.File) {
	if file == nil {
		return
	}
	_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
	_ = file.Close()
}

func (m Manager) validateSpecs() error {
	seen := map[string]bool{}
	for _, spec := range m.Files {
		if spec.Name == "" || strings.ContainsAny(spec.Name, `/\\`) || seen[spec.Name] {
			return fmt.Errorf("invalid or duplicate update file name %q", spec.Name)
		}
		seen[spec.Name] = true
		if !m.allowedRoot(spec.Root) {
			return fmt.Errorf("managed update root is not allowlisted: %s", spec.Root)
		}
		if err := validateSpec(spec); err != nil {
			return err
		}
		if err := m.checkWritableRoot(spec.Root); err != nil {
			return fmt.Errorf("managed update root is not writable: %s: %w", spec.Root, err)
		}
	}
	return nil
}

func (m Manager) checkWritableRoot(root string) error {
	if m.CheckWritableRoot != nil {
		return m.CheckWritableRoot(root)
	}
	return unix.Access(root, unix.W_OK|unix.X_OK)
}

func (m Manager) allowedRoot(root string) bool {
	wanted := filepath.Clean(root)
	values := m.AllowedRoots
	if len(values) == 0 {
		values = make([]string, 0, len(m.Files))
		for _, spec := range m.Files {
			values = append(values, spec.Root)
		}
	}
	for _, value := range values {
		if filepath.Clean(value) == wanted {
			return true
		}
	}
	return false
}

func validateSpec(spec FileSpec) error {
	if !filepath.IsAbs(spec.Root) || !filepath.IsAbs(spec.Path) || filepath.Clean(spec.Root) == "/" {
		return fmt.Errorf("unsafe managed update path %s", spec.Path)
	}
	if err := rejectSymlinkChain(spec.Root); err != nil {
		return err
	}
	rel, err := filepath.Rel(filepath.Clean(spec.Root), filepath.Clean(spec.Path))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("managed update path escapes its root: %s", spec.Path)
	}
	rootInfo, err := os.Lstat(spec.Root)
	if err != nil {
		return err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return fmt.Errorf("managed update root is not a regular directory: %s", spec.Root)
	}
	current := spec.Root
	parts := strings.Split(rel, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("managed update path has unsafe parent %s", current)
		}
	}
	if info, err := os.Lstat(spec.Path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("managed update path is a symlink: %s", spec.Path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func rejectSymlinkChain(path string) error {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return fmt.Errorf("path is not absolute: %s", path)
	}
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
			return fmt.Errorf("managed path traverses symlink: %s", current)
		}
	}
	return nil
}

func (m Manager) preflightSnapshotSpace() (int64, error) {
	limit := m.MaxSnapshotBytes
	if limit <= 0 {
		limit = 1 << 30
	}
	var total int64
	for _, spec := range m.Files {
		info, err := os.Lstat(spec.Path)
		if errors.Is(err, os.ErrNotExist) && spec.Optional {
			continue
		}
		if err != nil {
			return 0, err
		}
		if info.Size() < 0 || total > limit-info.Size() {
			return 0, fmt.Errorf("managed update snapshot exceeds the %d byte limit", limit)
		}
		total += info.Size()
	}
	diskPath := m.Root
	for {
		if _, err := os.Stat(diskPath); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
		parent := filepath.Dir(diskPath)
		if parent == diskPath {
			return 0, errors.New("cannot find an existing update root ancestor")
		}
		diskPath = parent
	}
	available, err := m.availableDiskBytes(diskPath)
	if err != nil {
		return 0, fmt.Errorf("check update snapshot disk space: %w", err)
	}
	const journalAndCopyReserve = uint64(64 << 20)
	required := uint64(total) + journalAndCopyReserve
	if available < required {
		return 0, fmt.Errorf("insufficient disk space for update snapshot: need at least %d bytes, have %d", required, available)
	}
	return total, nil
}

func (m Manager) availableDiskBytes(path string) (uint64, error) {
	if m.AvailableDiskBytes != nil {
		return m.AvailableDiskBytes(path)
	}
	var stats unix.Statfs_t
	if err := unix.Statfs(path, &stats); err != nil {
		return 0, err
	}
	return stats.Bavail * uint64(stats.Bsize), nil
}

func (m Manager) sortedSpecs() []FileSpec {
	result := append([]FileSpec(nil), m.Files...)
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (m Manager) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

func (m Manager) prune(current string) error {
	retention := m.Retention
	if retention <= 0 {
		retention = 1
	}
	root := filepath.Join(m.Root, "snapshots")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	type candidate struct {
		name string
		mod  time.Time
	}
	var values []candidate
	for _, entry := range entries {
		if !entry.IsDir() || !validID(entry.Name()) || entry.Name() == current {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		values = append(values, candidate{entry.Name(), info.ModTime()})
	}
	sort.Slice(values, func(i, j int) bool { return values[i].mod.Before(values[j].mod) })
	remove := len(values) + 1 - retention // include the current snapshot.
	for _, value := range values {
		if remove <= 0 {
			break
		}
		path := filepath.Join(root, value.name)
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		remove--
	}
	return nil
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func validID(value string) bool {
	if len(value) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func terminal(phase Phase) bool { return phase == PhaseCommitted || phase == PhaseRolledBack }

func validPhase(phase Phase) bool {
	switch phase {
	case PhasePreparing, PhasePrepared, PhaseMigrating, PhaseMigrated, PhasePromoted, PhaseVerifying, PhaseCommitted, PhaseRollingBack, PhaseRolledBack:
		return true
	default:
		return false
	}
}

func fileSpecsFromSnapshots(snapshots []Snapshot) []FileSpec {
	result := make([]FileSpec, 0, len(snapshots))
	for _, snapshot := range snapshots {
		result = append(result, FileSpec{Name: snapshot.Name, Artifact: snapshot.Artifact, Path: snapshot.Path, Root: snapshot.Root, Optional: snapshot.Optional, Executable: snapshot.Executable})
	}
	return result
}
