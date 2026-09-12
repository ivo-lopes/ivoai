package opencodebridge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	responseText := "bridge ok"
	if os.Getenv("IVOAI_LIVE_OPENCODE_COPY") == "multiline" {
		responseText = "bridge ok\nsecond logical line\n\n```go\nfmt.Println(\"fixture\")\n```\n\n" + strings.TrimSpace(strings.Repeat("wrapped fixture ", 24))
		runner.text = responseText
	}
	catalog := CatalogFromRegistry(routing.Registry{Providers: map[string]routing.ProviderCapability{"codex": {Provider: "codex", Authenticated: true, Models: []routing.ModelCapability{{Name: "fixture-model", DisplayName: "Fixture model", SupportedEfforts: []string{"low", "high"}, DefaultEffort: "high", Source: routing.SourceRuntimeVerified}}}}})
	bridge, err := Start(Options{Runner: runner, Catalog: catalog, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status {
		return Status{Frontend: "opencode", PermissionMode: "full", KnowledgeMode: "restricted", ConfiguredCount: 2, ConnectedCount: 1, Servers: []ServerView{{Alias: "source-A", Purpose: "fixture", Enabled: true, Selected: true, Health: "healthy"}, {Alias: "source-B", Enabled: true, Health: "down"}}, Memory: "ready", Context: "ready", CodexAuth: "authenticated", ClaudeAuth: "not-configured"}
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	root := t.TempDir()
	keyboard := os.Getenv("IVOAI_LIVE_OPENCODE_KEYBOARD")
	if keyboard == "xterm" || os.Getenv("IVOAI_LIVE_OPENCODE_COPY") == "mouse" {
		t.Setenv("TERM", "xterm-256color")
		t.Setenv("TERM_PROGRAM", "xterm")
		t.Setenv("TERM_PROGRAM_VERSION", "398")
	}
	clipboardMode := os.Getenv("IVOAI_LIVE_OPENCODE_CLIPBOARD")
	clipboardPath := filepath.Join(root, "clipboard-fixture")
	var readDesktopClipboard func() []byte
	if clipboardMode == "desktop" {
		readDesktopClipboard = preserveDesktopClipboard(t, []byte(responseText))
	} else if clipboardMode != "" {
		if clipboardMode != "success" && clipboardMode != "failure" {
			t.Fatal("clipboard fixture must be success or failure")
		}
		bin := filepath.Join(root, "bin")
		if err := os.Mkdir(bin, 0700); err != nil {
			t.Fatal(err)
		}
		// Model the native X11 owner retaining inherited descriptors after the
		// launcher exits. The TUI process-group cleanup also reaps this fixture.
		body := "#!/bin/sh\n/usr/bin/cat > '" + clipboardPath + "'\n/usr/bin/sleep 30 &\n"
		if clipboardMode == "failure" {
			body = "#!/bin/sh\n/usr/bin/cat >/dev/null\nexit 1\n"
		}
		if err := os.WriteFile(filepath.Join(bin, "xclip"), []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		t.Setenv("WAYLAND_DISPLAY", "")
		t.Setenv("DISPLAY", ":42")
	}
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
			candidate := stripTerminalControls(text)
			if strings.Contains(expected, "\x1b") {
				candidate = text
			}
			if strings.Contains(candidate, expected) {
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
	if keyboard == "xterm" {
		wait(0, "\x1b[?4m")
		send("\x1b[>4;0m")
		wait(0, "\x1b[>4;2m")
	}
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
	if keyboard == "xterm" {
		send("\x1b[27;2;13~") // Negotiated xterm modifyOtherKeys.
	} else {
		send("\x1b[13;2u") // Kitty/CSI-u Shift+Enter, not Ctrl+J.
	}
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
			if clipboardMode != "" {
				// The executor request can precede the rendered response, especially
				// while a native build is using the host. Copy only after its text
				// has reached the frontend, not after a fixed scheduling delay.
				wait(0, "bridge ok")
				if os.Getenv("IVOAI_LIVE_OPENCODE_COPY") == "multiline" {
					wait(0, "wrapped fixture")
				}
				from := mark()
				mouseCopy := os.Getenv("IVOAI_LIVE_OPENCODE_COPY") == "mouse"
				if mouseCopy {
					mu.Lock()
					row, column, ok := fixtureTextPosition(output.String(), "bridge ok")
					mu.Unlock()
					if !ok {
						t.Fatal("fixture response position unavailable for native mouse selection")
					}
					t.Logf("native mouse selection fixture row=%d column=%d", row, column)
					send(fmt.Sprintf("\x1b[<0;%d;%dM", column, row))
					send(fmt.Sprintf("\x1b[<32;%d;%dM", column+9, row))
					send(fmt.Sprintf("\x1b[<0;%d;%dm", column+9, row))
				} else {
					send("\x18")
					send("y")
				}
				if clipboardMode == "failure" {
					if mouseCopy {
						wait(from, "Clipboard copy unconfirmed")
					} else {
						wait(from, "Failed to copy to clipboard")
					}
					mu.Lock()
					feedback := stripTerminalControls(output.String()[from:])
					falseSuccess := strings.Contains(feedback, "Message copied to clipboard") || strings.Contains(feedback, "Copied to clipboard")
					mu.Unlock()
					if falseSuccess {
						t.Fatal("failed backend produced a success toast")
					}
				} else {
					if readDesktopClipboard != nil {
						time.Sleep(time.Second)
						t.Logf("desktop fixture readback before feedback=%t", bytes.Equal(readDesktopClipboard(), []byte(responseText)))
					}
					if mouseCopy {
						wait(from, "Copied to clipboard")
					} else {
						wait(from, "Message copied to clipboard")
					}
					var body []byte
					var err error
					if readDesktopClipboard != nil {
						body = readDesktopClipboard()
					} else {
						body, err = os.ReadFile(clipboardPath)
					}
					if err != nil || string(body) != responseText {
						t.Logf("clipboard fixture bytes=%d expected_prefix=%t expected_suffix=%t", len(body), bytes.HasPrefix([]byte(responseText), body), bytes.HasSuffix([]byte(responseText), body))
						t.Fatal("native clipboard did not receive the logical response")
					}
				}
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("keyboard did not submit a prompt after resize and dialog close")
}

// Locate this ASCII fixture's last drawn run, not a general terminal renderer.
// All positions come from the owned PTY; no desktop screenshot is inspected.
func fixtureTextPosition(raw, text string) (int, int, bool) {
	index := strings.LastIndex(raw, text)
	if index < 0 {
		return 0, 0, false
	}
	pattern := regexp.MustCompile("\x1b\\[([0-9]+);([0-9]+)[Hf]")
	matches := pattern.FindAllStringSubmatchIndex(raw[:index], -1)
	if len(matches) == 0 {
		return 0, 0, false
	}
	last := matches[len(matches)-1]
	row, _ := strconv.Atoi(raw[last[2]:last[3]])
	column, _ := strconv.Atoi(raw[last[4]:last[5]])
	prefix := raw[last[1]:index]
	advance := func(text string) {
		for _, char := range stripTerminalControls(text) {
			switch char {
			case '\r':
				column = 1
			case '\n':
				row++
			case '\b':
				column--
			default:
				if char >= 32 {
					column++
				}
			}
		}
	}
	controls := regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]")
	offset := 0
	for _, bounds := range controls.FindAllStringIndex(prefix, -1) {
		advance(prefix[offset:bounds[0]])
		control := prefix[bounds[0]:bounds[1]]
		amount, err := strconv.Atoi(control[2 : len(control)-1])
		if err != nil || amount == 0 {
			amount = 1
		}
		switch control[len(control)-1] {
		case 'A':
			row -= amount
		case 'B', 'e':
			row += amount
		case 'C', 'a':
			column += amount
		case 'D':
			column -= amount
		case 'E':
			row += amount
			column = 1
		case 'F':
			row -= amount
			column = 1
		case 'G', '`':
			column = amount
		case 'd':
			row = amount
		}
		offset = bounds[1]
	}
	advance(prefix[offset:])
	return row, column, row > 0 && column > 0 && column+9 <= 160
}

// Explicit opt-in desktop smoke. Clipboard contents stay in process memory and
// are never logged or written to a fixture file. Rich clipboard formats are not
// replaced; a concurrent operator clipboard change is also left untouched.
func preserveDesktopClipboard(t *testing.T, fixture []byte) func() []byte {
	t.Helper()
	if os.Getenv("DISPLAY") == "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		t.Skip("desktop smoke requires an available X11 session")
	}
	path, err := exec.LookPath("xclip")
	if err != nil {
		t.Skip("desktop clipboard backend not installed")
	}
	read := func(target string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, path, "-selection", "clipboard", "-o", "-t", target)
		pipe, err := command.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := command.Start(); err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(pipe, (1<<20)+1))
		if len(body) > 1<<20 {
			cancel()
			readErr = fmt.Errorf("clipboard exceeds smoke limit")
		}
		waitErr := command.Wait()
		if readErr != nil {
			return nil, readErr
		}
		return body, waitErr
	}
	targets, err := read("TARGETS")
	if err != nil {
		t.Skip("desktop clipboard cannot be inspected safely")
	}
	for _, target := range strings.Fields(string(targets)) {
		switch target {
		case "TARGETS", "MULTIPLE", "TIMESTAMP", "SAVE_TARGETS", "UTF8_STRING", "TEXT", "STRING", "text/plain", "text/plain;charset=utf-8", "text/plain;charset=UTF-8":
		default:
			t.Skip("desktop clipboard has non-text formats; preserving operator state")
		}
	}
	previous, err := read("UTF8_STRING")
	if err != nil {
		t.Skip("desktop clipboard text cannot be preserved")
	}
	t.Cleanup(func() {
		current, err := read("UTF8_STRING")
		if err != nil || !bytes.Equal(current, fixture) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, path, "-selection", "clipboard", "-i")
		command.Stdin = bytes.NewReader(previous)
		if err := command.Run(); err != nil {
			t.Error("desktop clipboard restoration failed")
			return
		}
		if restored, err := read("UTF8_STRING"); err != nil || !bytes.Equal(restored, previous) {
			t.Error("desktop clipboard restoration could not be verified")
		}
	})
	return func() []byte {
		t.Helper()
		body, err := read("UTF8_STRING")
		if err != nil {
			t.Fatal("desktop clipboard readback failed")
		}
		return body
	}
}
