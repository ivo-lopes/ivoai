package platform

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const defaultPrivateFileLimit = 16 << 20

func EnsurePrivateDir(path string) error {
	if err := rejectSymlink(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create private directory %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure directory %s: %w", path, err)
	}
	return nil
}

func AtomicWritePrivate(path []byte, filename string) error {
	if err := EnsurePrivateDir(filepath.Dir(filename)); err != nil {
		return err
	}
	return atomicWriteFile(path, filename, 0o600, -1, -1)
}

// AtomicWriteFile replaces a managed regular file without following a leaf
// symlink. Both the payload and containing directory are synchronized so an
// update journal or snapshot cannot be acknowledged before its rename is
// durable.
func AtomicWriteFile(data []byte, filename string, mode fs.FileMode) error {
	dir := filepath.Dir(filename)
	if err := ensureManagedDir(dir); err != nil {
		return err
	}
	return atomicWriteFile(data, filename, mode, -1, -1)
}

func AtomicWriteFileOwned(data []byte, filename string, mode fs.FileMode, uid, gid int) error {
	dir := filepath.Dir(filename)
	if err := ensureManagedDir(dir); err != nil {
		return err
	}
	return atomicWriteFile(data, filename, mode, uid, gid)
}

func atomicWriteFile(data []byte, filename string, mode fs.FileMode, uid, gid int) error {
	dir := filepath.Dir(filename)
	if err := rejectSymlink(filename); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f, err := os.CreateTemp(dir, ".ivoai-write-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	// Populate and synchronize the private temporary file before changing
	// ownership or widening its final mode. This prevents another principal
	// from observing or modifying a partially written managed file.
	if uid >= 0 && gid >= 0 {
		if err := f.Chown(uid, gid); err != nil {
			f.Close()
			return err
		}
	}
	if err := f.Chmod(mode.Perm()); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, filename); err != nil {
		return fmt.Errorf("replace %s: %w", filename, err)
	}
	return SyncDir(dir)
}

func SyncDir(dir string) error {
	directory, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open containing directory: %w", err)
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return fmt.Errorf("sync containing directory: %w", syncErr)
	}
	return closeErr
}

func ensureManagedDir(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create managed directory %s: %w", path, err)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("managed parent is not a regular directory: %s", path)
	}
	return nil
}

// ReadRegularFile reads a bounded managed file without accepting a symlink,
// device, socket, or directory. Missing optional files remain distinguishable
// through os.ErrNotExist.
func ReadRegularFile(filename string, limit int64) ([]byte, error) {
	if limit <= 0 {
		limit = defaultPrivateFileLimit
	}
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing non-regular file %s", filename)
	}
	if info.Size() < 0 || info.Size() > limit {
		return nil, fmt.Errorf("managed file %s exceeds size limit", filename)
	}
	fd, err := unix.Open(filename, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), filename)
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return nil, fmt.Errorf("managed file changed during safe open: %s", filename)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("managed file %s exceeds size limit", filename)
	}
	return data, nil
}

func rejectSymlink(path string) error {
	i, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if i.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink path %s", path)
	}
	return nil
}
