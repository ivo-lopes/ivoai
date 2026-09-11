package opencodebridge

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/routing"
	"golang.org/x/sys/unix"
)

func TestLiveManagedOpenCodeResizeKeyboard(t *testing.T) {
	path, version := os.Getenv("IVOAI_LIVE_OPENCODE_PATH"), os.Getenv("IVOAI_LIVE_OPENCODE_VERSION")
	if path == "" || version == "" || os.Getenv("IVOAI_LIVE_OPENCODE_TUI") != "1" {
		t.Skip("requires pinned OpenCode PTY")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	runner := &fakeRunner{result: ExecutorResult{ExecutorSessionID: "thread_keyboard"}}
	catalog := CatalogFromRegistry(routing.Registry{Providers: map[string]routing.ProviderCapability{"codex": {Provider: "codex", Authenticated: true, Models: []routing.ModelCapability{{Name: "fixture-model", DisplayName: "Fixture model", SupportedEfforts: []string{"low", "high"}, DefaultEffort: "high", Source: routing.SourceRuntimeVerified}}}}})
	bridge, err := Start(Options{Runner: runner, Catalog: catalog, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status {
		return Status{Frontend: "opencode", PermissionMode: "full", KnowledgeMode: "restricted", ConfiguredCount: 2, ConnectedCount: 1, Servers: []ServerView{{Alias: "source-A", Purpose: "fixture", Enabled: true, Selected: true, Health: "healthy"}, {Alias: "source-B", Enabled: true, Health: "down"}}, Memory: "ready", Context: "ready", CodexAuth: "authenticated", ClaudeAuth: "not-configured"}
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	root := t.TempDir()
	m, err := StartManaged(ctx, ManagedOptions{OpenCodePath: path, Version: version, Directory: root, RuntimeDir: filepath.Join(root, "runtime"), StateDir: filepath.Join(root, "state"), Bridge: bridge, PermissionMode: "full"})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(context.Background())
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer slave.Close()
	resize := func(width int) {
		t.Helper()
		if err := unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 45, Col: uint16(width)}); err != nil {
			t.Fatal(err)
		}
	}
	resize(160)
	cmd := exec.Command(path, m.Args()...)
	cmd.Env = m.Env()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
	}()
	var mu sync.Mutex
	var output strings.Builder
	go func() {
		buffer := make([]byte, 32768)
		for {
			n, err := master.Read(buffer)
			if n > 0 {
				mu.Lock()
				if output.Len() < 4<<20 {
					output.Write(buffer[:n])
				}
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	mark := func() int { mu.Lock(); defer mu.Unlock(); return output.Len() }
	wait := func(from int, expected string) {
		t.Helper()
		until := time.Now().Add(12 * time.Second)
		for time.Now().Before(until) {
			mu.Lock()
			text := output.String()[from:]
			mu.Unlock()
			if strings.Contains(stripTerminalControls(text), expected) {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("missing keyboard/resize state %q (raw transcript not logged)", expected)
	}
	send := func(value string) {
		t.Helper()
		if _, err := io.WriteString(master, value); err != nil {
			t.Fatal(err)
		}
		time.Sleep(250 * time.Millisecond)
	}
	wait(0, "OpenCode frontend")
	for _, width := range []int{100, 60, 100, 160} {
		start := mark()
		resize(width)
		send("/ivoai")
		send("\r")
		wait(start, "Permissions:")
		wait(start, "source-A")
		send("\x1b")
		t.Logf("resize=%d panel visible; Escape returned", width)
	}
	start := mark()
	send("/models")
	send("\r")
	wait(start, "Select model")
	send("Fixture model")
	send("\r")
	wait(start, "variant")
	send("high")
	send("\r")
	send("safe keyboard fixture line one")
	send("\x1b[13;2u") // Kitty/CSI-u Shift+Enter, not Ctrl+J.
	runner.mu.Lock()
	beforeSubmit := len(runner.requests)
	runner.mu.Unlock()
	if beforeSubmit != 0 {
		t.Fatal("Shift+Enter submitted instead of inserting a newline")
	}
	send("line two")
	send("\r")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		runner.mu.Lock()
		count := len(runner.requests)
		var request ExecutorRequest
		if count > 0 {
			request = runner.requests[count-1]
		}
		runner.mu.Unlock()
		if count > 0 {
			if count != 1 || !strings.Contains(request.Prompt, "line one\nline two") {
				t.Fatal("multiline composer did not preserve one logical prompt")
			}
			if request.Model != "fixture-model" || request.Effort != "high" {
				t.Fatalf("selection not effective: %s", fmt.Sprint(request.Model, request.Effort))
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("keyboard did not submit a prompt after resize and dialog close")
}
