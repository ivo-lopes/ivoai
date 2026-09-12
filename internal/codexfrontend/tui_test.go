package codexfrontend

import (
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
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"golang.org/x/sys/unix"
)

func TestLiveNativeTUI(t *testing.T) {
	binary := os.Getenv("IVOAI_LIVE_CODEX_PATH")
	if binary == "" || os.Getenv("IVOAI_LIVE_CODEX_TUI") != "1" {
		t.Skip("requires explicit native Codex PTY smoke")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
	defer cancel()
	runner := &fixtureRunner{decision: make(chan bool, 1)}
	var decided atomic.Bool
	catalog := opencodebridge.CatalogFromRegistry(routing.Registry{Providers: map[string]routing.ProviderCapability{"codex": {Provider: "codex", Authenticated: true, Models: []routing.ModelCapability{{Name: "fixture-strong", DisplayName: "Fixture strong", CapabilityTier: routing.TierStrong, SupportedEfforts: []string{"medium", "high"}, DefaultEffort: "high", Source: routing.SourceRuntimeVerified}}}}})
	bridge, err := opencodebridge.Start(opencodebridge.Options{Frontend: "codex", RequirePromptGate: true, PreferredExecutor: "codex", Catalog: catalog, Runner: runner, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() opencodebridge.Status { return opencodebridge.Status{Frontend: "codex"} }, NativePermissions: func() []opencodebridge.PermissionView {
		if runner.calls.Load() > 0 && !decided.Load() {
			return []opencodebridge.PermissionView{{ID: "plan_fixture", Description: "Approve fixture plan"}}
		}
		return nil
	}, ReplyNativePermission: func(_ context.Context, _ string, allow bool) error {
		decided.Store(true)
		runner.decision <- allow
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	root := t.TempDir()
	// An untrusted project cannot start its own MCP process before admission.
	if err := os.Mkdir(filepath.Join(root, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "unexpected-project-mcp")
	projectConfig := "[mcp_servers.untrusted_fixture]\ncommand = \"/bin/sh\"\nargs = [\"-c\", \"touch " + marker + "\"]\n"
	if err := os.WriteFile(filepath.Join(root, ".codex", "config.toml"), []byte(projectConfig), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := Start(ctx, Options{Binary: binary, Directory: root, RuntimeDir: root, SessionID: "native_tui_fixture", Bridge: bridge, Environment: os.Environ()})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
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
	if err := unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 45, Col: 140}); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, binary, f.Args()...)
	cmd.Env = f.Environment()
	cmd.Dir = root
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
	var mu, writeMu sync.Mutex
	var output strings.Builder
	write := func(value string) { writeMu.Lock(); _, _ = io.WriteString(master, value); writeMu.Unlock() }
	go func() {
		buffer := make([]byte, 32768)
		for {
			n, err := master.Read(buffer)
			if n > 0 {
				chunk := string(buffer[:n])
				mu.Lock()
				if output.Len() < 4<<20 {
					output.WriteString(chunk)
				}
				mu.Unlock()
				if strings.Contains(chunk, "\x1b[6n") {
					write("\x1b[1;1R")
				}
			}
			if err != nil {
				return
			}
		}
	}()
	controls := regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]")
	wait := func(expected string) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			text := controls.ReplaceAllString(output.String(), "")
			mu.Unlock()
			if strings.Contains(text, expected) {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		mu.Lock()
		text := controls.ReplaceAllString(output.String(), "")
		mu.Unlock()
		// This opt-in test contains only synthetic input and a private provider
		// with no account credentials. Bound the diagnostic; never use it live.
		if len(text) > 2000 {
			text = text[len(text)-2000:]
		}
		t.Logf("synthetic native startup: %q", text)
		t.Fatalf("native TUI state unavailable: %s", expected)
	}
	send := func(text string) { write(text); time.Sleep(250 * time.Millisecond) }
	wait("fixture-strong")
	send("/model")
	send("\r")
	wait("Select Model and Effort")
	send("\r")
	wait("Level for fixture-strong")
	send("\x1b[A")
	send("\r")
	send("corrija isso")
	send("\r")
	wait("PROMPT_INSUFFICIENT")
	if runner.calls.Load() != 0 {
		t.Fatal("native composer bypassed gate")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("project MCP executed before native admission")
	}
	send("Read VERSION and report the value. Acceptance: return only the version; do not modify files.")
	send("\r")
	wait("Approve fixture plan")
	send("\r")
	send("\r")
	wait("fixture synthesis")
	if !decided.Load() || runner.calls.Load() != 1 {
		t.Fatal("native approval or worker lifecycle failed")
	}
	if runner.effort.Load() != "medium" {
		t.Fatal("native reasoning selection did not reach the primary override")
	}
	t.Log(fmt.Sprintf("native composer gate=PASS approval=PASS synthesis=PASS worker_calls=%d", runner.calls.Load()))
}
