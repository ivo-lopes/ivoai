package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/core"
	"golang.org/x/sys/unix"
)

const maxCompletedPrimaryBytes = 2 << 20

// CompletedPrimaryPlanRef is a small, additive pointer into the existing
// SessionStore. Each immutable snapshot retains the preceding pointer, so a
// caller can page history without truncating it or expanding session metadata.
// The reference is metadata, never a grant to execute or recover a task.
type CompletedPrimaryPlanRef struct {
	SHA256    string `json:"sha256"`
	Bytes     int64  `json:"bytes"`
	ReceiptID string `json:"receipt_id"`
	PlanID    string `json:"plan_id"`
}

func (r CompletedPrimaryPlanRef) Validate() error {
	b, err := hex.DecodeString(r.SHA256)
	if err != nil || len(b) != sha256.Size || strings.ToLower(r.SHA256) != r.SHA256 || r.Bytes < 1 || r.Bytes > maxCompletedPrimaryBytes || !safeText(r.ReceiptID, 128) || r.ReceiptID == "" || !safeText(r.PlanID, 80) || r.PlanID == "" {
		return errors.New("invalid completed primary reference")
	}
	return nil
}

// Snapshot stores only the already bounded host journal and exact checkpoint;
// worker raw outputs remain referenced through their existing ResultRefs.
// It is durable session history, separate from transient WorkingContext TTLs.
type CompletedPrimaryPlanSnapshot struct {
	SchemaVersion int     `json:"schema_version"`
	Session       Session `json:"session"`
	Checkpoint    []byte  `json:"checkpoint,omitempty"`
}

func validateCompletedPrimarySnapshot(v CompletedPrimaryPlanSnapshot, id string, ref CompletedPrimaryPlanRef) error {
	r := v.Session.PrimaryTurn
	if v.SchemaVersion != 1 || v.Session.SessionID != id || v.Session.Mode != ModeAuto || !v.Session.Auto || v.Session.Coordinator != "native" || v.Session.Frontend != "codex" || validate(v.Session) != nil || r == nil || r.Validate() != nil || r.Mode != core.PrimaryClosed || r.ID != ref.ReceiptID || r.PlanID != ref.PlanID || v.Session.PlanID != ref.PlanID {
		return errors.New("completed primary history ownership mismatch")
	}
	if len(v.Checkpoint) > 0 {
		var checkpoint Checkpoint
		if len(v.Checkpoint) > maxCheckpointBytes || json.Unmarshal(v.Checkpoint, &checkpoint) != nil || validateCheckpoint(checkpoint) != nil {
			return errors.New("invalid archived checkpoint")
		}
	}
	return nil
}

// completedPrimaryDirectory walks only host-fixed and validated owner segments
// using directory FDs. Neither a swapped parent nor a leaf symlink is followed.
func (s Store) completedPrimaryDirectory(id string, create bool) (int, error) {
	if ValidateID(id) != nil {
		return -1, errors.New("invalid completed history owner")
	}
	fd, err := unix.Open(s.Root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	var rootInfo unix.Stat_t
	if unix.Fstat(fd, &rootInfo) != nil || rootInfo.Mode&077 != 0 || int(rootInfo.Uid) != os.Geteuid() {
		unix.Close(fd)
		return -1, errors.New("unsafe completed history root")
	}
	for _, name := range []string{"completed-primary", id} {
		if create {
			if err := unix.Mkdirat(fd, name, 0700); err != nil && !errors.Is(err, unix.EEXIST) {
				unix.Close(fd)
				return -1, err
			}
		}
		next, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if err != nil {
			return -1, err
		}
		fd = next
		var info unix.Stat_t
		if unix.Fstat(fd, &info) != nil || info.Mode&077 != 0 || int(info.Uid) != os.Geteuid() {
			unix.Close(fd)
			return -1, errors.New("unsafe completed history directory")
		}
	}
	return fd, nil
}

func readCompletedPrimaryAt(fd int, ref CompletedPrimaryPlanRef) ([]byte, error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	fileFD, err := unix.Openat(fd, ref.SHA256+".json", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fileFD), "completed-primary")
	defer f.Close()
	var stat unix.Stat_t
	if unix.Fstat(fileFD, &stat) != nil || int(stat.Uid) != os.Geteuid() {
		return nil, errors.New("unsafe completed history file owner")
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&077 != 0 || info.Size() != ref.Bytes {
		return nil, errors.New("unsafe completed history file")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxCompletedPrimaryBytes+1))
	if err != nil || int64(len(data)) != ref.Bytes {
		return nil, errors.New("invalid completed history size")
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != ref.SHA256 {
		return nil, errors.New("completed history digest mismatch")
	}
	return data, nil
}

// SaveCompletedPrimaryPlan persists and syncs the immutable snapshot before the
// caller may retire active DAG metadata. Repeating the same write is idempotent;
// existing different bytes are never overwritten. A failed metadata update can
// leave an unreferenced snapshot, not a missing/replayed completed operation.
func (s Store) SaveCompletedPrimaryPlan(ctx context.Context, snapshot CompletedPrimaryPlanSnapshot) (CompletedPrimaryPlanRef, error) {
	var ref CompletedPrimaryPlanRef
	if ctx.Err() != nil {
		return ref, ctx.Err()
	}
	r := snapshot.Session.PrimaryTurn
	if r == nil {
		return ref, errors.New("missing completed primary receipt")
	}
	data, err := json.Marshal(snapshot)
	if err != nil || len(data) > maxCompletedPrimaryBytes {
		return ref, errors.New("completed history exceeds bounded snapshot size")
	}
	digest := sha256.Sum256(data)
	ref = CompletedPrimaryPlanRef{hex.EncodeToString(digest[:]), int64(len(data)), r.ID, snapshot.Session.PlanID}
	if ref.Validate() != nil || validateCompletedPrimarySnapshot(snapshot, snapshot.Session.SessionID, ref) != nil {
		return CompletedPrimaryPlanRef{}, errors.New("invalid completed primary snapshot")
	}
	if previous := snapshot.Session.CompletedPrimaryPlan; previous != nil {
		if _, err := s.ReadCompletedPrimaryPlan(ctx, snapshot.Session.SessionID, *previous); err != nil {
			return CompletedPrimaryPlanRef{}, errors.New("previous completed history is unavailable")
		}
	}
	fd, err := s.completedPrimaryDirectory(snapshot.Session.SessionID, true)
	if err != nil {
		return CompletedPrimaryPlanRef{}, err
	}
	defer unix.Close(fd)
	if old, err := readCompletedPrimaryAt(fd, ref); err == nil {
		if !bytes.Equal(data, old) {
			return CompletedPrimaryPlanRef{}, errors.New("completed history identity changed")
		}
		return ref, nil
	} else if !errors.Is(err, unix.ENOENT) {
		return CompletedPrimaryPlanRef{}, err
	}
	id, err := NewID()
	if err != nil {
		return CompletedPrimaryPlanRef{}, err
	}
	staging := ".staging-" + id
	fileFD, err := unix.Openat(fd, staging, unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return CompletedPrimaryPlanRef{}, err
	}
	defer unix.Unlinkat(fd, staging, 0)
	f := os.NewFile(uintptr(fileFD), "completed-primary-staging")
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	if closeErr := f.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		return CompletedPrimaryPlanRef{}, writeErr
	}
	if ctx.Err() != nil {
		return CompletedPrimaryPlanRef{}, ctx.Err()
	}
	if err := unix.Linkat(fd, staging, fd, ref.SHA256+".json", 0); err != nil && !errors.Is(err, unix.EEXIST) {
		return CompletedPrimaryPlanRef{}, err
	}
	stored, err := readCompletedPrimaryAt(fd, ref)
	if err != nil || !bytes.Equal(data, stored) {
		return CompletedPrimaryPlanRef{}, errors.New("completed history promotion failed")
	}
	if err := unix.Fsync(fd); err != nil {
		return CompletedPrimaryPlanRef{}, err
	}
	return ref, nil
}

// ReadCompletedPrimaryPlan pages one caller-supplied owner-scoped reference.
// A digest copied from another session cannot cross the owner namespace.
func (s Store) ReadCompletedPrimaryPlan(ctx context.Context, id string, ref CompletedPrimaryPlanRef) (CompletedPrimaryPlanSnapshot, error) {
	var out CompletedPrimaryPlanSnapshot
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	fd, err := s.completedPrimaryDirectory(id, false)
	if err != nil {
		return out, err
	}
	defer unix.Close(fd)
	data, err := readCompletedPrimaryAt(fd, ref)
	if err != nil {
		return out, err
	}
	if json.Unmarshal(data, &out) != nil || validateCompletedPrimarySnapshot(out, id, ref) != nil {
		return CompletedPrimaryPlanSnapshot{}, errors.New("invalid completed primary history")
	}
	return out, ctx.Err()
}
