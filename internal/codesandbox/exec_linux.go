//go:build linux && (amd64 || arm64)

// Package codesandbox confines a disposable helper before replacing it with Node.
package codesandbox

import (
	"debug/elf"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Exec irreversibly confines the calling OS thread, then replaces the process.
// Call ONLY in a dedicated, trusted, unprivileged helper subprocess; on error exit immediately.
// args excludes argv[0]. Paths must be absolute, existing and caller-authorized.
// Directory grants include descendants (including nested mounts). Only the executable and its exact ELF loader are implicit. No HOME, runtime
// library directory, /proc, device or repository access is implicitly granted. The caller
// supplies a minimal environment and pipe/file stdio, never connected sockets.
// All other inherited descriptors are close-on-exec. No network or child processes
// are allowed; Node threads are allowed. This does not impose CPU/memory limits,
// hide metadata, or protect against kernel vulnerabilities.
func Exec(node string, args []string, readPaths []string, writePaths []string) error {
	runtime.LockOSThread() // Never unlock: restrictions are irreversible and thread-local.
	if os.Geteuid() == 0 {
		return fmt.Errorf("sandbox requires an unprivileged helper")
	}
	var caps [2]unix.CapUserData
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	if err := unix.Capset(&header, &caps[0]); err != nil {
		return fmt.Errorf("drop helper capabilities: %w", err)
	}
	if !filepath.IsAbs(node) {
		return fmt.Errorf("sandbox executable must be absolute")
	}
	abi, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 || abi < 3 {
		return fmt.Errorf("sandbox requires Landlock ABI >= 3 (ABI %d, errno %v)", abi, errno)
	}
	// ABI 3 handles every filesystem access bit through TRUNCATE. Deny device
	// ioctls as well on kernels which support that restriction.
	handled := uint64((1 << 15) - 1)
	if abi >= 5 {
		handled |= unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
	}
	attr := unix.LandlockRulesetAttr{Handled_access_fs: handled}
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), 8, 0)
	if errno != 0 {
		return fmt.Errorf("create Landlock ruleset: %w", errno)
	}
	defer unix.Close(int(fd))
	add := func(path string, access uint64) error {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("sandbox path must be absolute")
		}
		p, e := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
		if e != nil {
			return fmt.Errorf("open sandbox grant: %w", e)
		}
		defer unix.Close(p)
		var st unix.Stat_t
		if e = unix.Fstat(p, &st); e != nil {
			return e
		}
		if st.Mode&unix.S_IFMT != unix.S_IFDIR {
			access &= unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_TRUNCATE
		}
		rule := unix.LandlockPathBeneathAttr{Allowed_access: access, Parent_fd: int32(p)}
		_, _, e2 := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, fd, unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&rule)), 0, 0, 0)
		if e2 != 0 {
			return fmt.Errorf("add Landlock rule: %w", e2)
		}
		return nil
	}
	read := uint64(unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR)
	write := read | unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_TRUNCATE | unix.LANDLOCK_ACCESS_FS_REMOVE_DIR | unix.LANDLOCK_ACCESS_FS_REMOVE_FILE | unix.LANDLOCK_ACCESS_FS_MAKE_DIR | unix.LANDLOCK_ACCESS_FS_MAKE_REG | unix.LANDLOCK_ACCESS_FS_MAKE_SYM | unix.LANDLOCK_ACCESS_FS_REFER
	if err := add(node, read|unix.LANDLOCK_ACCESS_FS_EXECUTE); err != nil {
		return err
	}
	// Linux also checks EXECUTE on an ELF interpreter. Grant only the exact
	// loader recorded in the reviewed executable, never its containing directory.
	binary, err := elf.Open(node)
	if err != nil {
		return fmt.Errorf("read executable ELF: %w", err)
	}
	for _, segment := range binary.Progs {
		if segment.Type != elf.PT_INTERP {
			continue
		}
		data, e := io.ReadAll(io.LimitReader(segment.Open(), 4097))
		if e != nil || len(data) > 4096 {
			binary.Close()
			return fmt.Errorf("invalid ELF interpreter")
		}
		if e = add(strings.TrimRight(string(data), "\x00"), read|unix.LANDLOCK_ACCESS_FS_EXECUTE); e != nil {
			binary.Close()
			return e
		}
	}
	binary.Close()
	for _, p := range readPaths {
		if err := add(p, read); err != nil {
			return err
		}
	}
	for _, p := range writePaths {
		if err := add(p, write); err != nil {
			return err
		}
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("no_new_privs: %w", err)
	}
	if _, _, e := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0); e != 0 {
		return fmt.Errorf("restrict Landlock: %w", e)
	}
	if err := unix.CloseRange(3, ^uint(0), unix.CLOSE_RANGE_CLOEXEC); err != nil {
		return fmt.Errorf("close inherited descriptors: %w", err)
	}
	if err := restrictSyscalls(); err != nil {
		return err
	}
	return unix.Exec(node, append([]string{node}, args...), os.Environ())
}

func restrictSyscalls() error {
	arch := uint32(unix.AUDIT_ARCH_X86_64)
	if runtime.GOARCH == "arm64" {
		arch = unix.AUDIT_ARCH_AARCH64
	}
	ld := func(offset uint32) unix.SockFilter {
		return unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: offset}
	}
	eq := func(value uint32, yes, no uint8) unix.SockFilter {
		return unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: value, Jt: yes, Jf: no}
	}
	ret := func(value uint32) unix.SockFilter { return unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: value} }
	deny := uint32(unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM))
	f := []unix.SockFilter{ld(4), eq(arch, 1, 0), ret(unix.SECCOMP_RET_KILL_PROCESS), ld(0)}
	// Reject x32 syscall numbers instead of letting them bypass the native list.
	if runtime.GOARCH == "amd64" {
		f = append(f, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K, K: 0x40000000, Jf: 1}, ret(unix.SECCOMP_RET_KILL_PROCESS))
	}
	f = append(f, eq(unix.SYS_CLONE3, 0, 1), ret(unix.SECCOMP_RET_ERRNO|uint32(unix.ENOSYS)))
	// clone is permitted only for actual same-address-space, same-signal-handler
	// threads, with no namespace creation or process exit signal.
	f = append(f, eq(unix.SYS_CLONE, 0, 5), ld(16), unix.SockFilter{Code: unix.BPF_ALU | unix.BPF_AND | unix.BPF_K, K: uint32(unix.CLONE_THREAD|unix.CLONE_VM|unix.CLONE_SIGHAND) | 0xff | 0x7e020000}, eq(uint32(unix.CLONE_THREAD|unix.CLONE_VM|unix.CLONE_SIGHAND), 1, 0), ret(deny), ret(unix.SECCOMP_RET_ALLOW), ld(0))
	blocked := []uint32{unix.SYS_SOCKET, unix.SYS_SOCKETPAIR, unix.SYS_CONNECT, unix.SYS_BIND, unix.SYS_LISTEN, unix.SYS_ACCEPT, unix.SYS_ACCEPT4, unix.SYS_SENDTO, unix.SYS_SENDMSG, unix.SYS_SENDMMSG, unix.SYS_RECVFROM, unix.SYS_RECVMSG, unix.SYS_RECVMMSG, unix.SYS_IO_URING_SETUP, unix.SYS_IO_URING_ENTER, unix.SYS_IO_URING_REGISTER, unix.SYS_PTRACE, unix.SYS_PROCESS_VM_READV, unix.SYS_PROCESS_VM_WRITEV, unix.SYS_PIDFD_GETFD, unix.SYS_UNSHARE, unix.SYS_SETNS, unix.SYS_MOUNT, unix.SYS_UMOUNT2, unix.SYS_PIVOT_ROOT, unix.SYS_CHROOT, unix.SYS_OPEN_BY_HANDLE_AT, unix.SYS_BPF, unix.SYS_USERFAULTFD, unix.SYS_KILL, unix.SYS_PIDFD_SEND_SIGNAL, unix.SYS_UTIMENSAT, unix.SYS_FCHMOD, unix.SYS_FCHMODAT, unix.SYS_FCHMODAT2, unix.SYS_FCHOWN, unix.SYS_FCHOWNAT, unix.SYS_SETXATTR, unix.SYS_LSETXATTR, unix.SYS_FSETXATTR, unix.SYS_REMOVEXATTR, unix.SYS_LREMOVEXATTR, unix.SYS_FREMOVEXATTR}
	if runtime.GOARCH == "amd64" {
		blocked = append(blocked, 57, 58, 90, 92, 94, 132, 235, 261)
	} // fork, vfork, chmod, chown, lchown, utime, utimes, futimesat
	for _, nr := range blocked {
		f = append(f, eq(nr, 0, 1), ret(deny))
	}
	f = append(f, ret(unix.SECCOMP_RET_ALLOW))
	prog := unix.SockFprog{Len: uint16(len(f)), Filter: &f[0]}
	_, _, err := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, 0, uintptr(unsafe.Pointer(&prog)))
	if err != 0 {
		return fmt.Errorf("install seccomp: %w", err)
	}
	return nil
}
