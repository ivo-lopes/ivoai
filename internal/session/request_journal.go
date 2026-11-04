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
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

var ErrClaimHistoryIncomplete = errors.New("REQUEST_CLAIM_HISTORY_INCOMPLETE: retained legacy claims reached their old truncation limit; inspect the preserved conversation and start a new managed conversation rather than replaying uncertain requests")

type frontendRequestClaim struct {
	SchemaVersion  int       `json:"schema_version"`
	OwnerSessionID string    `json:"owner_session_id"`
	ScopeDigest    string    `json:"scope_digest"`
	CreatedAt      time.Time `json:"created_at"`
}

func journalDigest(parts ...string) string {
	data, _ := json.Marshal(parts)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func privateRecordExists(fd int, name string) (bool, error) {
	var info unix.Stat_t
	err := unix.Fstatat(fd, name, &info, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	return err == nil, err
}

func readPrivateJournalRecord(fd int, name string, maxBytes int64) ([]byte, error) {
	fileFD, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fileFD), "private-journal-record")
	defer f.Close()
	var stat unix.Stat_t
	info, err := f.Stat()
	if err != nil || unix.Fstat(fileFD, &stat) != nil || int(stat.Uid) != os.Geteuid() || !info.Mode().IsRegular() || info.Mode().Perm()&077 != 0 || info.Size() > maxBytes {
		return nil, errors.New("unsafe private journal record")
	}
	body, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil || int64(len(body)) > maxBytes {
		return nil, errors.New("private journal record exceeds bound")
	}
	return body, nil
}

// New records become visible only after their bytes are synced. Linkat makes
// no-overwrite promotion atomic; a successful caller also observes directory
// durability before execution. Stale staging files grant no authority.
func writePrivateJournalRecord(ctx context.Context, fd int, name string, data []byte) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	id, err := NewID()
	if err != nil {
		return err
	}
	staging := ".staging-" + id
	fileFD, err := unix.Openat(fd, staging, unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	defer unix.Unlinkat(fd, staging, 0)
	f := os.NewFile(uintptr(fileFD), "private-journal-staging")
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := unix.Linkat(fd, staging, fd, name, 0); err != nil {
		return err
	}
	return unix.Fsync(fd)
}

func requestScope(v Session, frontend string) string {
	return journalDigest("ivoai-request-scope-v1", v.Frontend, v.WorkingDirectory, v.KnowledgeScopeID, frontend)
}

func sameRequestScope(a, b Session) bool {
	return a.Mode == ModeAuto && a.Frontend == b.Frontend && a.WorkingDirectory == b.WorkingDirectory && a.KnowledgeScopeID == b.KnowledgeScopeID
}

func ownsLegacyFrontend(v Session, frontend string) bool {
	if v.FrontendSessionID == frontend {
		return true
	}
	for key := range v.FrontendRequests {
		if strings.HasPrefix(key, frontend+":") {
			return true
		}
	}
	return false
}

func (s Store) importLegacyRequestScope(ctx context.Context, fd int, owner Session, frontend, scope string) error {
	marker := []byte(`{"schema_version":1,"scope":"` + scope + `","legacy_import_complete":true}`)
	if body, err := readPrivateJournalRecord(fd, "legacy-imported.json", 4096); err == nil {
		if !bytes.Equal(body, marker) {
			return errors.New("request claim migration marker changed")
		}
		return nil
	} else if !errors.Is(err, unix.ENOENT) {
		return err
	}
	values, err := s.List()
	if err != nil {
		return err
	}
	// Preflight all legacy records before importing or moving any of them.
	// A prior truncated map cannot prove what it no longer contains.
	var leases []*Lease
	defer func() {
		for _, lease := range leases {
			_ = lease.Close()
		}
	}()
	for _, v := range values {
		if !sameRequestScope(v, owner) || !ownsLegacyFrontend(v, frontend) || v.RequestJournalVersion == 1 {
			continue
		}
		if len(v.FrontendRequests) >= 256 {
			return ErrClaimHistoryIncomplete
		}
		if v.SessionID != owner.SessionID {
			if ProcessMatches(v.PrimaryPID, v.PrimaryProcessStart) || ProcessMatches(v.FrontendPID, v.FrontendProcessStart) {
				return errors.New("SESSION_FRONTEND_ALREADY_ACTIVE")
			}
			lease, err := s.Acquire(v.SessionID)
			if err != nil {
				return err
			}
			leases = append(leases, lease)
		}
	}
	for _, v := range values {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !sameRequestScope(v, owner) || !ownsLegacyFrontend(v, frontend) {
			continue
		}
		for key, at := range v.FrontendRequests {
			front, message, ok := strings.Cut(key, ":")
			if !ok || front != frontend {
				continue
			}
			name := journalDigest("ivoai-request-v1", frontend, message) + ".json"
			exists, err := privateRecordExists(fd, name)
			if err != nil {
				return err
			}
			if !exists {
				body, _ := json.Marshal(frontendRequestClaim{1, v.SessionID, scope, at})
				if err := writePrivateJournalRecord(ctx, fd, name, body); err != nil {
					return err
				}
			}
		}
		for _, raw := range v.TurnAttempts {
			if err := s.appendTurnAttempt(ctx, v.SessionID, raw); err != nil {
				return err
			}
		}
		if v.RequestJournalVersion == 0 {
			v.RequestJournalVersion = 1
			v.LegacyAttemptAuditPartial = len(v.TurnAttempts) >= 32
			if err := s.write(v); err != nil {
				return err
			}
		}
	}
	return writePrivateJournalRecord(ctx, fd, "legacy-imported.json", marker)
}

// ClaimFrontendRequest is the only managed admission claim authority. Its
// permanent scope journal is O(1) after one legacy import; the 256-entry map is
// a compatibility/UI projection and no longer determines replay eligibility.
func (s Store) ClaimFrontendRequest(ctx context.Context, id, frontend, message string) (bool, error) {
	if !safeText(frontend, 128) || frontend == "" || strings.Contains(frontend, ":") || !safeText(message, 128) || message == "" {
		return false, errors.New("invalid frontend request identity")
	}
	claimed := false
	err := s.withLock(func() error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		v, err := s.Get(id)
		if err != nil {
			return err
		}
		if v.Mode != ModeAuto || !v.Active() {
			return errors.New("managed request owner is not active")
		}
		scope := requestScope(v, frontend)
		fd, err := s.privateJournalDirectory("request-claims", scope, true)
		if err != nil {
			return err
		}
		defer unix.Close(fd)
		name := journalDigest("ivoai-request-v1", frontend, message) + ".json"
		if exists, err := privateRecordExists(fd, name); err != nil || exists {
			return err
		}
		if err := s.importLegacyRequestScope(ctx, fd, v, frontend, scope); err != nil {
			return err
		}
		// The import may have moved this exact legacy claim into the journal.
		if exists, err := privateRecordExists(fd, name); err != nil || exists {
			return err
		}
		now := time.Now().UTC()
		body, _ := json.Marshal(frontendRequestClaim{1, id, scope, now})
		if err := writePrivateJournalRecord(ctx, fd, name, body); err != nil {
			return err
		}
		v, err = s.Get(id)
		if err != nil {
			return err
		}
		v.RequestJournalVersion = 1
		if v.FrontendRequests == nil {
			v.FrontendRequests = map[string]time.Time{}
		}
		if len(v.FrontendRequests) >= 256 {
			oldest := ""
			for key, at := range v.FrontendRequests {
				if oldest == "" || at.Before(v.FrontendRequests[oldest]) {
					oldest = key
				}
			}
			delete(v.FrontendRequests, oldest)
		}
		v.FrontendRequests[frontend+":"+message] = now
		v.UpdatedAt = now
		if err := s.write(v); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		claimed = true
		return nil
	})
	return claimed, err
}

type turnAttemptEnvelope struct {
	SchemaVersion int    `json:"schema_version"`
	SessionID     string `json:"session_id"`
	AttemptID     string `json:"attempt_id"`
	Raw           []byte `json:"raw"`
}

func attemptIdentity(id string, raw []byte) (string, error) {
	var v struct {
		ID        string `json:"attempt_id"`
		SessionID string `json:"ivoai_session_id"`
	}
	if len(raw) == 0 || len(raw) > 1<<20 || json.Unmarshal(raw, &v) != nil || v.ID == "" || !safeText(v.ID, 128) || v.SessionID != "" && v.SessionID != id {
		return "", errors.New("invalid turn attempt journal identity")
	}
	return v.ID, nil
}

func openAttemptDirectory(fd int, attempt string, create bool) (int, error) {
	name := journalDigest("ivoai-turn-attempt-v1", attempt)
	if create {
		if err := unix.Mkdirat(fd, name, 0700); err != nil && !errors.Is(err, unix.EEXIST) {
			return -1, err
		}
		if err := unix.Fsync(fd); err != nil {
			return -1, err
		}
	}
	next, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	var info unix.Stat_t
	if unix.Fstat(next, &info) != nil || info.Mode&077 != 0 || int(info.Uid) != os.Geteuid() {
		unix.Close(next)
		return -1, errors.New("unsafe attempt journal directory")
	}
	return next, nil
}

func (s Store) appendTurnAttempt(ctx context.Context, id string, raw []byte) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	attempt, err := attemptIdentity(id, raw)
	if err != nil {
		return err
	}
	parent, err := s.privateJournalDirectory("turn-attempts", id, true)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	fd, err := openAttemptDirectory(parent, attempt, true)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	body, _ := json.Marshal(turnAttemptEnvelope{1, id, attempt, raw})
	digest := sha256.Sum256(body)
	name := hex.EncodeToString(digest[:]) + ".json"
	if old, err := readPrivateJournalRecord(fd, name, 2<<20); err == nil {
		if !bytes.Equal(old, body) {
			return errors.New("turn attempt journal changed")
		}
		return nil
	} else if !errors.Is(err, unix.ENOENT) {
		return err
	}
	if err := writePrivateJournalRecord(ctx, fd, name, body); err != nil && !errors.Is(err, unix.EEXIST) {
		return err
	}
	stored, err := readPrivateJournalRecord(fd, name, 2<<20)
	if err != nil || !bytes.Equal(stored, body) {
		return errors.New("turn attempt journal promotion failed")
	}
	return ctx.Err()
}

// AppendTurnAttempt persists exact callback bytes before the bounded metadata
// projection changes. It never invokes an executor or replays an operation.
func (s Store) AppendTurnAttempt(ctx context.Context, id string, raw []byte) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	return s.appendTurnAttempt(ctx, id, raw)
}

// ReadTurnAttemptEvents returns the immutable events for one explicit attempt.
// Files remain durable even after the 32-attempt UI projection advances.
func (s Store) ReadTurnAttemptEvents(ctx context.Context, id, attempt string) ([][]byte, error) {
	if attempt == "" || !safeText(attempt, 128) {
		return nil, errors.New("invalid turn attempt")
	}
	parent, err := s.privateJournalDirectory("turn-attempts", id, false)
	if err != nil {
		return nil, err
	}
	defer unix.Close(parent)
	fd, err := openAttemptDirectory(parent, attempt, false)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	dup, err := unix.Dup(fd)
	if err != nil {
		return nil, err
	}
	directory := os.NewFile(uintptr(dup), "turn-attempt-journal")
	entries, err := directory.ReadDir(65)
	directory.Close()
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > 64 {
		return nil, errors.New("turn attempt audit page exceeds bound")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	out := make([][]byte, 0, len(entries))
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".staging-") {
			continue
		}
		if !strings.HasSuffix(name, ".json") || !validJournalHash(strings.TrimSuffix(name, ".json")) {
			return nil, errors.New("invalid turn attempt audit entry")
		}
		body, err := readPrivateJournalRecord(fd, name, 2<<20)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(body)
		var envelope turnAttemptEnvelope
		if hex.EncodeToString(digest[:])+".json" != name || json.Unmarshal(body, &envelope) != nil || envelope.SchemaVersion != 1 || envelope.SessionID != id || envelope.AttemptID != attempt {
			return nil, errors.New("turn attempt audit integrity mismatch")
		}
		if got, err := attemptIdentity(id, envelope.Raw); err != nil || got != attempt {
			return nil, errors.New("turn attempt audit owner mismatch")
		}
		out = append(out, envelope.Raw)
	}
	return out, nil
}
