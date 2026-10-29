package codexfrontend

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/session"
)

// Identity and state must come from one snapshot. A zombie may be reaped
// between separate reads; a later ENOENT must not turn a dead observation alive.
// Unknown errors/malformed data remain alive so cleanup verification fails closed.
func nativeFixtureWriterAlive(identity string, readStat func() ([]byte, error)) bool {
	body, err := readStat()
	if err != nil {
		return !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ESRCH)
	}
	end := strings.LastIndexByte(string(body), ')')
	if end < 0 {
		return true
	}
	fields := strings.Fields(string(body[end+1:]))
	if len(fields) < 20 {
		return true
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return true
	}
	if identity == "" {
		return true
	}
	if fields[19] != identity {
		return false
	}
	return fields[0] != "Z" && fields[0] != "X"
}

func TestNativeFixtureWriterCannotReviveAfterReap(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "read ignored; exit 0")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir(), "LANG=C"}
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		input.Close()
		if !waited {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	identity := session.ProcessStart(cmd.Process.Pid)
	if identity == "" {
		t.Fatal("owned child identity unavailable")
	}
	input.Close()
	path := "/proc/" + strconv.Itoa(cmd.Process.Pid) + "/stat"
	zombie := false
	for i := 0; i < 100; i++ {
		body, err := os.ReadFile(path)
		if err == nil {
			end := strings.LastIndexByte(string(body), ')')
			if end >= 0 {
				fields := strings.Fields(string(body[end+1:]))
				if len(fields) >= 20 && fields[0] == "Z" {
					zombie = true
					break
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !zombie {
		t.Fatal("owned child did not reach controlled zombie state")
	}
	reads := 0
	readThenReap := func() ([]byte, error) {
		body, err := os.ReadFile(path)
		reads++
		if !waited {
			if err := cmd.Wait(); err != nil {
				t.Fatal("controlled owned child exit was not zero")
			}
			waited = true
		}
		return body, err
	}
	if nativeFixtureWriterAlive(identity, readThenReap) {
		t.Fatal("known terminated writer became alive after owned reap between observations")
	}
	t.Logf("owned zero-exit child remains terminated after reap; proc snapshots=%d", reads)
}

func TestNativeFixtureWriterSnapshotStates(t *testing.T) {
	snapshot := func(state, start string) []byte {
		fields := make([]string, 20)
		for i := range fields {
			fields[i] = "0"
		}
		fields[0] = state
		fields[19] = start
		return []byte("123 (synthetic ) fixture) " + strings.Join(fields, " "))
	}
	for _, tc := range []struct {
		name     string
		body     []byte
		err      error
		identity string
		alive    bool
	}{
		{name: "live_identity", body: snapshot("S", "123"), identity: "123", alive: true},
		{name: "zombie_identity", body: snapshot("Z", "123"), identity: "123", alive: false},
		{name: "dead_identity", body: snapshot("X", "123"), identity: "123", alive: false},
		{name: "other_generation", body: snapshot("R", "124"), identity: "123", alive: false},
		{name: "known_gone", err: os.ErrNotExist, identity: "123", alive: false},
		{name: "no_such_process", err: syscall.ESRCH, identity: "123", alive: false},
		{name: "permission_unknown", err: os.ErrPermission, identity: "123", alive: true},
		{name: "other_error_unknown", err: syscall.EIO, identity: "123", alive: true},
		{name: "malformed_unknown", body: []byte("unparseable"), identity: "123", alive: true},
		{name: "short_unknown", body: []byte("123 (fixture) Z"), identity: "123", alive: true},
		{name: "invalid_start_unknown", body: snapshot("Z", "invalid"), identity: "123", alive: true},
		{name: "missing_identity_unknown", body: snapshot("Z", "123"), identity: "", alive: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := nativeFixtureWriterAlive(tc.identity, func() ([]byte, error) { return tc.body, tc.err }); got != tc.alive {
				t.Fatalf("snapshot alive=%v want=%v", got, tc.alive)
			}
		})
	}
}
