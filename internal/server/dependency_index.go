package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

type indexEntry struct {
	Path     string
	Mode     uint32
	UID, GID uint32
	SHA256   string
}

// The existing Core backup deliberately excludes the rebuildable index. An
// explicit dependency upgrade keeps a separate private stopped-state snapshot
// so a runtime rollback never points an older Qdrant at a forward index.
func copyDependencyIndex(ctx context.Context, source, destination string) (string, error) {
	var entries []indexEntry
	if err := os.Mkdir(destination, 0700); err != nil {
		return "", err
	}
	err := filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return errors.New("dependency index snapshot rejects links and special files")
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return errors.New("dependency index identity unavailable")
		}
		entry := indexEntry{Path: relative, Mode: uint32(info.Mode().Perm()), UID: stat.Uid, GID: stat.Gid}
		if info.IsDir() {
			if relative != "." {
				if err := os.Mkdir(target, 0700); err != nil {
					return err
				}
			}
		} else {
			fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
			if err != nil {
				return err
			}
			file := os.NewFile(uintptr(fd), path)
			defer file.Close()
			after, err := file.Stat()
			if err != nil || !os.SameFile(info, after) || !after.Mode().IsRegular() || stat.Nlink != 1 {
				return errors.New("dependency index changed during snapshot")
			}
			output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return err
			}
			digest := sha256.New()
			_, copyErr := io.Copy(io.MultiWriter(output, digest), file)
			if copyErr == nil {
				copyErr = output.Sync()
			}
			if closeErr := output.Close(); copyErr == nil {
				copyErr = closeErr
			}
			if copyErr != nil {
				return copyErr
			}
			entry.SHA256 = hex.EncodeToString(digest.Sum(nil))
		}
		if os.Geteuid() == 0 {
			if err := os.Chown(target, int(stat.Uid), int(stat.Gid)); err != nil {
				return err
			}
		} else if int(stat.Uid) != os.Geteuid() || int(stat.Gid) != os.Getegid() {
			return errors.New("index snapshot requires existing authority to preserve ownership")
		}
		if err := os.Chmod(target, info.Mode().Perm()); err != nil {
			return err
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(entries)
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}
