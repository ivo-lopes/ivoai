//go:build linux && amd64

// Package rtkview provides an experimental OSS-only, stdin-to-stdout view.
// It is not wired into the application and never executes an original command.
package rtkview

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const BinarySHA256 = "0b80810970ce2c79c92deaa474bbcf6f1cf01ac93baab0e273ee6cb4ef950d10"
const Version = "1.1.0"
const maxBytes = 1 << 20

var ErrUnavailable = errors.New("RTK OSS view unavailable")

// Provider requires a host-owned official OSS binary and the separately built
// ivoai-output-view-sandbox helper. Neither is installed or discovered on PATH.
// New limits each instance to one transformation and bypasses excess callers.
type Provider struct {
	binary, helper string
	busy           chan struct{}
}

func New(binary, helper string) *Provider {
	return &Provider{binary: binary, helper: helper, busy: make(chan struct{}, 1)}
}

func (p *Provider) Transform(ctx context.Context, filter string, raw []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p == nil || len(raw) > maxBytes || !allowedFilter(filter) {
		return nil, ErrUnavailable
	}
	select {
	case p.busy <- struct{}{}:
		defer func() { <-p.busy }()
	default:
		return nil, ErrUnavailable
	}
	if !regularExecutable(p.binary) || !regularExecutable(p.helper) || !regularExecutable("/usr/bin/bwrap") {
		return nil, ErrUnavailable
	}
	private, err := os.MkdirTemp("", "ivoai-rtk-view-")
	if err != nil {
		return nil, ErrUnavailable
	}
	defer os.RemoveAll(private)
	// Snapshot before any probe. The immutable, read-only sandbox mount is the
	// same snapshot hashed by the helper and executed for version and pipe.
	binary := filepath.Join(private, "rtk")
	if err := snapshot(p.binary, binary); err != nil {
		return nil, ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	version, err := p.run(bounded, binary, "version", "", nil)
	if err != nil || string(version) != "rtk "+Version+"\n" {
		if bounded.Err() != nil {
			return nil, bounded.Err()
		}
		return nil, ErrUnavailable
	}
	return p.run(bounded, binary, "pipe", filter, raw)
}

func (p *Provider) run(ctx context.Context, binary, mode, filter string, raw []byte) ([]byte, error) {
	args := []string{"--unshare-all", "--die-with-parent", "--new-session", "--clearenv",
		"--dir", "/view", "--dir", "/private", "--dir", "/tmp", "--dev", "/dev",
		"--ro-bind", p.helper, "/view/helper", "--ro-bind", binary, "/view/rtk",
		"--chdir", "/private", "--setenv", "HOME", "/private", "--setenv", "PATH", "/nonexistent",
		"--setenv", "XDG_CONFIG_HOME", "/private", "--setenv", "XDG_DATA_HOME", "/private", "--setenv", "XDG_CACHE_HOME", "/private",
		"--setenv", "TMPDIR", "/tmp", "--setenv", "LC_ALL", "C", "--setenv", "TZ", "UTC",
		"--setenv", "RTK_TELEMETRY_DISABLED", "1", "--setenv", "RTK_SUPPRESS_HOOK_WARNING", "1",
		"--setenv", "RTK_RECALL", "0", "--setenv", "RTK_TEE", "0", "--remount-ro", "/",
		"--", "/view/helper", mode, filter}
	cmd := exec.CommandContext(ctx, "/usr/bin/bwrap", args...)
	cmd.Env = []string{"PATH=/nonexistent", "LC_ALL=C"}
	cmd.Stdin = bytes.NewReader(raw)
	output := &limitedBuffer{max: maxBytes}
	stderr := &limitedBuffer{max: 4096}
	cmd.Stdout, cmd.Stderr = output, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 100 * time.Millisecond
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil || output.overflow || stderr.Len() != 0 || stderr.overflow {
		return nil, ErrUnavailable // Never expose or persist third-party stderr.
	}
	return bytes.Clone(output.Bytes()), nil
}

func allowedFilter(s string) bool { return s == "git-status" || s == "git-log" || s == "grep" }

func regularExecutable(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 && info.Mode()&0022 == 0
}

func snapshot(source, target string) error {
	before, err := os.Lstat(source)
	if err != nil || !before.Mode().IsRegular() || before.Size() > 16<<20 {
		return ErrUnavailable
	}
	// A path replaced after Lstat must neither follow a link nor block on a
	// FIFO. Validate the opened descriptor before copying any bytes.
	fd, err := unix.Open(source, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return ErrUnavailable
	}
	in := os.NewFile(uintptr(fd), source)
	defer in.Close()
	after, err := in.Stat()
	if err != nil || !after.Mode().IsRegular() || after.Size() != before.Size() || after.Size() > 16<<20 || !os.SameFile(before, after) {
		return ErrUnavailable
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0500)
	if err != nil {
		return ErrUnavailable
	}
	n, copyErr := io.Copy(out, io.LimitReader(in, (16<<20)+1))
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil || n > 16<<20 || n != before.Size() {
		return ErrUnavailable
	}
	return nil
}

type limitedBuffer struct {
	bytes.Buffer
	max      int
	overflow bool
	mu       sync.Mutex
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(p) > b.max-b.Len() {
		b.overflow = true
		return 0, ErrUnavailable
	}
	return b.Buffer.Write(p)
}
