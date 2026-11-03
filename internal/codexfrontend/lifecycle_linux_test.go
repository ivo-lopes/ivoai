//go:build linux

package codexfrontend

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func shutdownFixture(t *testing.T, body string) (*Facade, string) {
	t.Helper()
	root := t.TempDir()
	binary := filepath.Join(root, "fixture-codex")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	catalog := opencodebridge.CatalogFromRegistry(routing.Registry{Providers: map[string]routing.ProviderCapability{"codex": {Provider: "codex", Authenticated: true, Models: []routing.ModelCapability{{Name: "fixture-strong", CapabilityTier: routing.TierStrong, SupportedEfforts: []string{"high"}, DefaultEffort: "high", Source: routing.SourceRuntimeVerified}}}}})
	bridge, err := opencodebridge.Start(opencodebridge.Options{Frontend: "codex", Catalog: catalog, Runner: &fixtureRunner{}, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() opencodebridge.Status { return opencodebridge.Status{Frontend: "codex"} }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close(context.Background()) })
	f, err := Start(context.Background(), Options{Binary: binary, Directory: root, RuntimeDir: root, SessionID: "shutdown", Bridge: bridge, Environment: []string{"PATH=/usr/bin:/bin", "HOME=" + root}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Close)
	return f, root
}

func waitShutdownFixtureFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("shutdown fixture did not reach its causal barrier")
}

func TestNativeIdleCloseUsesEOFAndReapsExitZero(t *testing.T) {
	f, root := shutdownFixture(t, "trap 'touch interrupted; exit 23' INT\ntouch ready\ncat >/dev/null\ntouch eof\n")
	waitShutdownFixtureFile(t, filepath.Join(root, "ready"))
	home := f.home
	f.Close()
	if f.ShutdownError() != nil || f.TurnError() != nil || !f.reaped || f.process.ProcessState == nil || !f.process.ProcessState.Success() {
		t.Fatal("ordinary idle close did not reap a successful EOF exit")
	}
	if _, err := os.Stat(filepath.Join(root, "eof")); err != nil {
		t.Fatal("native process did not observe ordinary stdin EOF")
	}
	if _, err := os.Stat(filepath.Join(root, "interrupted")); !os.IsNotExist(err) {
		t.Fatal("ordinary close interrupted the native process")
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatal("normally reaped temporary home was not cleaned")
	}
}

func TestNativeEOFReapsLeaderAfterStoppingRedirectedWriter(t *testing.T) {
	f, root := shutdownFixture(t, "(touch ready; while [ ! -f trigger ]; do sleep 0.01; done; touch leaked) >/dev/null 2>&1 &\nprintf '%s' \"$!\" > writer-pid.tmp\nmv writer-pid.tmp writer-pid\ncat >/dev/null\ntouch eof\n")
	waitShutdownFixtureFile(t, filepath.Join(root, "ready"))
	waitShutdownFixtureFile(t, filepath.Join(root, "writer-pid"))
	raw, err := os.ReadFile(filepath.Join(root, "writer-pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(raw))
	if err != nil || pid <= 0 {
		t.Fatal("fixture descendant identity unavailable")
	}
	start := session.ProcessStart(pid)
	if start == "" {
		t.Fatal("fixture descendant was not running before cleanup")
	}
	t.Cleanup(func() {
		if session.ProcessMatches(pid, start) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	f.Close()
	if f.ShutdownError() != nil || !f.reaped || !f.process.ProcessState.Success() {
		t.Fatal("normal EOF plus descendant cleanup did not retain leader exit zero")
	}
	if err := os.WriteFile(filepath.Join(root, "trigger"), []byte("release writer"), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if nativeFixtureWriterAlive(start, func() ([]byte, error) { return os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat") }) {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		break
	}
	if nativeFixtureWriterAlive(start, func() ([]byte, error) { return os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat") }) {
		t.Fatal("redirected descendant survived the normally exiting leader")
	}
	if _, err := os.Stat(filepath.Join(root, "leaked")); !os.IsNotExist(err) {
		t.Fatal("redirected descendant wrote after normal frontend cleanup")
	}
}

func TestNativeActiveCloseRecordsCancellationBeforeReturn(t *testing.T) {
	f, root := shutdownFixture(t, "touch ready\ncat >/dev/null\n")
	waitShutdownFixtureFile(t, filepath.Join(root, "ready"))
	ctx, cancel := context.WithCancel(context.Background())
	f.mu.Lock()
	f.active = &admittedTurn{thread: "fixture", cancel: cancel}
	f.mu.Unlock()
	f.Close()
	if ctx.Err() == nil || f.TurnError() == nil || f.TurnError().Error() != "TURN_CANCELLED" {
		t.Fatal("closing an active frontend became graceful turn success")
	}
	if f.ShutdownError() != nil {
		t.Fatal("active turn cancellation prevented normal native EOF shutdown")
	}
}

func TestNativeShutdownClosesHeldReaderAndPreservesUnreapedHome(t *testing.T) {
	for _, heldReap := range []bool{false, true} {
		t.Run(map[bool]string{false: "reader-held", true: "reap-held"}[heldReap], func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "native-home")
			if err := os.Mkdir(home, 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reader, writer := io.Pipe()
			defer writer.Close()
			defer reader.Close()
			f := &Facade{ctx: ctx, cancel: cancel, options: Options{RuntimeDir: root}, home: home, output: reader, done: make(chan error, 1)}
			readDone := make(chan struct{})
			go func() {
				_, _ = io.Copy(io.Discard, reader)
				close(readDone)
				if !heldReap {
					f.done <- nil
				}
			}()
			start := time.Now()
			f.Close()
			if time.Since(start) > 6*time.Second {
				t.Fatal("shutdown exceeded both fixed cleanup bounds")
			}
			select {
			case <-readDone:
			default:
				t.Fatal("held stdout reader still blocked native cleanup")
			}
			if err := f.ShutdownError(); err == nil || !strings.Contains(err.Error(), "TIMEOUT") {
				t.Fatal("forced or unreaped shutdown was not reported")
			}
			if _, err := os.Stat(home); err != nil {
				t.Fatal("home was removed without a proven normal owner reap")
			}
		})
	}
}
