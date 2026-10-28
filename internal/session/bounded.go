package session

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

// GetBounded uses the canonical namespace resolution and session validator with
// a smaller read budget. It never creates directories, locks, or metadata.
func (s Store) GetBounded(id string, maxBytes int64) (Session, error) {
	if ValidateID(id) != nil || maxBytes < 1 || maxBytes > maxStateBytes {
		return Session{}, errors.New("invalid bounded session request")
	}
	return s.readBounded(s.path(id), maxBytes)
}

type BoundedSnapshot struct {
	Sessions           []Session
	Truncated, Partial bool
}

// ListBounded scans at most limit+1 entries in each of the three existing
// namespaces and reads at most limit validated files. Truncated is conservative
// and the result is a subset, not the latest/global session count. Filesystem
// I/O and validation remain cooperative with ctx, not forcibly interruptible.
func (s Store) ListBounded(ctx context.Context, limit int, maxBytes int64) (BoundedSnapshot, error) {
	out := BoundedSnapshot{Sessions: []Session{}}
	if limit < 1 || limit > 128 || maxBytes < 1 || maxBytes > maxStateBytes {
		return out, errors.New("invalid bounded session request")
	}
	seen := map[string]bool{}
	for _, directory := range []string{s.Root, s.nativeDir(), s.frontendDir()} {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		info, err := os.Lstat(directory)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return out, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
			return out, errors.New("unsafe session directory")
		}
		fd, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return out, err
		}
		dir := os.NewFile(uintptr(fd), directory)
		if dir == nil {
			unix.Close(fd)
			return out, errors.New("session directory unavailable")
		}
		after, err := dir.Stat()
		if err != nil || !os.SameFile(info, after) {
			dir.Close()
			return out, errors.New("session directory changed")
		}
		entries, err := dir.ReadDir(limit + 1)
		dir.Close()
		if err != nil && err != io.EOF {
			return out, err
		}
		if len(entries) > limit {
			out.Truncated = true
			entries = entries[:limit]
		}
		for _, entry := range entries {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			id := strings.TrimSuffix(entry.Name(), ".json")
			if ValidateID(id) != nil || seen[id] {
				continue
			}
			seen[id] = true
			if len(out.Sessions) >= limit {
				out.Truncated = true
				continue
			}
			value, err := s.GetBounded(id, maxBytes)
			if err != nil {
				out.Partial = true
				continue
			}
			out.Sessions = append(out.Sessions, value)
		}
	}
	sort.Slice(out.Sessions, func(i, j int) bool { return out.Sessions[i].StartedAt.After(out.Sessions[j].StartedAt) })
	return out, nil
}
