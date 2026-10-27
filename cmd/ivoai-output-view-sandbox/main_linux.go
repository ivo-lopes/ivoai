//go:build linux && amd64

// This dedicated, host-owned helper is never an agent tool or application entry
// point. Run only within the minimal bubblewrap layout built by rtkview.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"

	"github.com/ivo-lopes/ivoai/internal/codesandbox"
	"github.com/ivo-lopes/ivoai/internal/rtkview"
	"golang.org/x/sys/unix"
)

func main() {
	if !run() {
		os.Exit(1) // Fixed, silent failure: no stderr or input may escape.
	}
}

func run() bool {
	if len(os.Args) != 3 || os.Getenv("HOME") != "/private" || os.Getenv("RTK_TELEMETRY_DISABLED") != "1" {
		return false
	}
	args := []string{"--version"}
	if os.Args[1] == "pipe" {
		filter := os.Args[2]
		if filter != "git-status" && filter != "git-log" && filter != "grep" {
			return false
		}
		args = []string{"pipe", "--filter", filter}
	} else if os.Args[1] != "version" || os.Args[2] != "" {
		return false
	}
	file, err := os.Open("/view/rtk")
	if err != nil {
		return false
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<20 {
		file.Close()
		return false
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(file, (16<<20)+1))
	file.Close()
	if err != nil || n != info.Size() || hex.EncodeToString(hash.Sum(nil)) != rtkview.BinarySHA256 {
		return false
	}
	if os.Args[1] == "pipe" {
		// A sealed RAM file bounds stdin even if this helper is accidentally
		// called with a different host pipe. No evidence is written to disk.
		fd, err := unix.MemfdCreate("ivoai-output-view", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
		if err != nil {
			return false
		}
		input := os.NewFile(uintptr(fd), "bounded-stdin")
		n, err := io.Copy(input, io.LimitReader(os.Stdin, (1<<20)+1))
		if err != nil || n > 1<<20 {
			input.Close()
			return false
		}
		if _, err = input.Seek(0, io.SeekStart); err != nil {
			input.Close()
			return false
		}
		if _, err = unix.FcntlInt(uintptr(fd), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); err != nil || unix.Dup3(fd, 0, 0) != nil {
			input.Close()
			return false
		}
		input.Close()
	}
	// Hard budgets are inherited by RTK. File writes are prohibited by the
	// read-only mount and the second Landlock boundary (no write grants).
	for _, limit := range []struct {
		resource int
		value    unix.Rlimit
	}{
		{unix.RLIMIT_CPU, unix.Rlimit{Cur: 2, Max: 2}},
		{unix.RLIMIT_AS, unix.Rlimit{Cur: 512 << 20, Max: 512 << 20}},
		{unix.RLIMIT_FSIZE, unix.Rlimit{}},
		{unix.RLIMIT_NOFILE, unix.Rlimit{Cur: 32, Max: 32}},
		{unix.RLIMIT_CORE, unix.Rlimit{}},
	} {
		if unix.Setrlimit(limit.resource, &limit.value) != nil {
			return false
		}
	}
	return codesandbox.Exec("/view/rtk", args, []string{"/private", "/tmp", "/dev/null", "/dev/urandom"}, nil) == nil
}
