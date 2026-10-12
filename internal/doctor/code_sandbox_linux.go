//go:build linux && (amd64 || arm64)

package doctor

import (
	"golang.org/x/sys/unix"
	"os"
	"unsafe"
)

func codeSandboxPrerequisites() sandboxPrerequisites {
	result := sandboxPrerequisites{Unprivileged: os.Geteuid() != 0}
	abi, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno == 0 {
		result.LandlockABI = int(abi)
	}
	action := uint32(unix.SECCOMP_RET_ERRNO)
	_, _, errno = unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_GET_ACTION_AVAIL, 0, uintptr(unsafe.Pointer(&action)))
	result.Seccomp = errno == 0
	return result
}
