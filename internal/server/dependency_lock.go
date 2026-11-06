package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"

	"golang.org/x/sys/unix"
)

type dependencyLockKey struct{}
type dependencyLockLease struct {
	path  string
	alive atomic.Bool
}

// One kernel lock covers setup, upgrade, rollback and their stopped-service
// transaction. The reentrant context capability is local to this package.
func WithDependencyMutationLock(ctx context.Context, l Layout, operation func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := l.Validate(); err != nil {
		return err
	}
	path := filepath.Clean(l.ConfigDir)
	if held, _ := ctx.Value(dependencyLockKey{}).(*dependencyLockLease); held != nil && held.path == path && held.alive.Load() {
		return operation(ctx)
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	dir, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	var parent unix.Stat_t
	if unix.Fstat(dir, &parent) != nil || int(parent.Uid) != os.Geteuid() || parent.Mode&0022 != 0 {
		return errors.New("dependency configuration root has unsafe ownership or permissions")
	}
	fd, err := unix.Openat(dir, ".dependency-upgrade.lock", unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 || stat.Size != 0 || int(stat.Uid) != os.Geteuid() || stat.Mode&0077 != 0 {
		return errors.New("unsafe dependency operation lock")
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.New("dependency operation already in progress; do not retry automatically")
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	lease := &dependencyLockLease{path: path}
	lease.alive.Store(true)
	defer lease.alive.Store(false)
	return operation(context.WithValue(ctx, dependencyLockKey{}, lease))
}
