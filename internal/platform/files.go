package platform

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

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
	dir := filepath.Dir(filename)
	if err := EnsurePrivateDir(dir); err != nil {
		return err
	}
	if err := rejectSymlink(filename); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	f, err := os.CreateTemp(dir, ".ivoai-write-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(path); err != nil {
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
	return os.Chmod(filename, 0o600)
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
