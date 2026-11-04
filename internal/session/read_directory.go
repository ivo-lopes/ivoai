package session

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// Read paths validate the opened directory descriptor. They never initialize,
// repair modes, acquire a write lock, migrate metadata or follow a leaf link.
func openPrivateSessionReadDirectory(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "session-read-directory")
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || int(stat.Uid) != os.Geteuid() || stat.Mode&077 != 0 {
		file.Close()
		return nil, errors.New("unsafe session read directory")
	}
	return file, nil
}
