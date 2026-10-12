package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"golang.org/x/sys/unix"
)

// All managed config writes share this lock. External editors do not cooperate;
// profile transactions additionally reject changes detected before promotion.
func (s *Store) lockConfig() (func(), error) {
	if err := platform.EnsurePrivateDir(filepath.Dir(s.Paths.Config)); err != nil {
		return nil, err
	}
	fd, err := unix.Open(s.Paths.Config+".lock", unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	var stat unix.Stat_t
	if err = unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
		unix.Close(fd)
		return nil, errors.New("unsafe configuration lock")
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		unix.Close(fd)
		return nil, errors.New("configuration update already in progress; retry")
	}
	return func() { _ = unix.Flock(fd, unix.LOCK_UN); _ = unix.Close(fd) }, nil
}

func configSnapshot(path string) ([]byte, bool, error) {
	body, err := platform.ReadRegularFile(path, 4<<20)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return body, err == nil, err
}

func (s *Store) updateAutomationConfig(mutate func(*Config) error) error {
	unlock, err := s.lockConfig()
	if err != nil {
		return err
	}
	defer unlock()
	before, existed, err := configSnapshot(s.Paths.Config)
	if err != nil {
		return err
	}
	c, err := s.Load()
	if err != nil {
		return err
	}
	original := c
	if err := mutate(&c); err != nil {
		return err
	}
	if reflect.DeepEqual(original, c) {
		return nil
	}
	return s.saveConfig(c, func() error {
		after, exists, err := configSnapshot(s.Paths.Config)
		if err != nil {
			return err
		}
		if exists != existed || !bytes.Equal(before, after) {
			return errors.New("configuration changed during profile transaction; preview again")
		}
		return nil
	})
}
