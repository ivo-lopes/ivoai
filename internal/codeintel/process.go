package codeintel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ivo-lopes/ivoai/internal/codesandbox"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

const maxProcessMemory = 512 * 1024 * 1024

// SandboxHelper runs only in a disposable internal subprocess before app/config
// initialization. Caller-owned path grants never come from an MCP tool argument.
func SandboxHelper(args []string) error {
	if len(args) != 5 {
		return errors.New("invalid code sandbox invocation")
	}
	for _, path := range args {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("invalid code sandbox path")
		}
	}
	node, distribution, source, cache, entry := args[0], args[1], args[2], args[3], args[4]
	if source == cache || filepath.Dir(source) != filepath.Dir(cache) || filepath.Dir(entry) != filepath.Dir(cache) {
		return errors.New("invalid code sandbox layout")
	}
	read := []string{distribution, source, entry, "/dev/null", "/dev/urandom"}
	for _, path := range []string{"/lib", "/lib64", "/usr/lib"} {
		if _, err := os.Stat(path); err == nil {
			read = append(read, path)
		}
	}
	return codesandbox.Exec(node, []string{"--openssl-config=/dev/null", "--max-old-space-size=256", entry}, read, []string{cache})
}

func (p *Provider) runProcess(ctx context.Context, r adapterRequest) (adapterResponse, error) {
	var response adapterResponse
	if !filepath.IsAbs(p.Executable) || !filepath.IsAbs(p.Node) || !filepath.IsAbs(p.Distribution) {
		return response, errors.New("managed Graft runtime unavailable")
	}
	entry := filepath.Join(filepath.Dir(r.Cache), "adapter.mjs")
	if err := platform.AtomicWritePrivate(adapter, entry); err != nil {
		return response, err
	}
	r.Distribution = p.Distribution
	input, err := json.Marshal(r)
	if err != nil || len(input) > 16384 {
		return response, errors.New("invalid structural request")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.Executable, "_code-sandbox", p.Node, p.Distribution, r.Root, r.Cache, entry)
	cmd.Dir = r.Root
	cmd.Env = []string{"PATH=/nonexistent", "HOME=" + r.Cache, "TMPDIR=" + r.Cache, "DO_NOT_TRACK=1", "GRAFT_NO_REFRESH=1", "GRAFT_NO_SEED=1", "GRAFT_DIR=" + r.Cache, "LC_ALL=C", "TZ=UTC"}
	cmd.Stdin = bytes.NewReader(input)
	output := &limitedBuffer{max: MaxResultBytes + 8192}
	stderr := &limitedBuffer{max: 4096}
	cmd.Stdout = output
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = time.Second
	if err = cmd.Start(); err != nil {
		return response, errors.New("managed structural process could not start")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var peak int64
	exceeded := false
	for {
		select {
		case err = <-done:
			if exceeded {
				return response, errors.New("structural process exceeded memory budget")
			}
			if ctx.Err() != nil {
				return response, ctx.Err()
			}
			if err != nil {
				return response, errors.New("managed structural process failed; inspect component health")
			}
			if json.Unmarshal(output.Bytes(), &response) != nil {
				return response, errors.New("invalid structural response")
			}
			if usage, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
				rss := usage.Maxrss * 1024
				if rss > peak {
					peak = rss
				}
				cpu := (usage.Utime.Sec+usage.Stime.Sec)*1000 + (usage.Utime.Usec+usage.Stime.Usec)/1000
				response.CPUMS = &cpu
			}
			response.PeakMemoryBytes = &peak
			return response, nil
		case <-ticker.C:
			memory := processRSS(cmd.Process.Pid)
			if memory > peak {
				peak = memory
			}
			if memory > maxProcessMemory {
				exceeded = true
				cancel()
			}
		}
	}
}
func processRSS(pid int) int64 {
	body, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) > 1 {
				n, _ := strconv.ParseInt(fields[1], 10, 64)
				return n * 1024
			}
		}
	}
	return 0
}
