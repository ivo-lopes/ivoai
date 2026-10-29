//go:build linux

package benchmarkfixtures

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLifecycleSyntheticChild(t *testing.T) {
	if os.Getenv("IVOAI_LIFECYCLE_TEST_CHILD") != "1" {
		t.Skip("synthetic helper; executed only as an explicitly spawned local child")
	}
	time.Sleep(400 * time.Millisecond)
}

func lifecycleFixture(t *testing.T) (*exec.Cmd, ClientIdentity, string, string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	command := exec.Command(binary, "-test.run=^TestLifecycleSyntheticChild$")
	command.Env = []string{"IVOAI_LIFECYCLE_TEST_CHILD=1"}
	identity := ClientIdentity{AttemptID: strings.Repeat("a", 32), Scenario: "implementation", Arm: "A", FixtureSHA256: strings.Repeat("b", 64), HarnessSHA256: strings.Repeat("c", 64), CompositionSHA256: strings.Repeat("d", 64), ClientSHA256: hex.EncodeToString(digest[:]), IsolationSHA256: strings.Repeat("e", 64)}
	private := t.TempDir()
	if err := os.Chmod(private, 0700); err != nil {
		t.Fatal(err)
	}
	return command, identity, filepath.Join(private, "start.json"), t.TempDir()
}

func TestClientLifecycleObservesActualChildAndReaps(t *testing.T) {
	command, identity, path, root := lifecycleFixture(t)
	value, err := RunObservedClient(command, identity, path, root)
	if err != nil || value == nil || value.PID != command.Process.Pid || value.StartTicks == 0 || value.StartedNS <= 0 || value.ReapedNS <= value.StartedNS || value.ExitCode != 0 || !command.ProcessState.Exited() {
		t.Fatalf("actual synthetic child observation failed: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var start ClientLifecycle
	if json.Unmarshal(body, &start) != nil || start.PID != value.PID || start.StartTicks != value.StartTicks || start.Identity != identity || start.ReapedNS != 0 || start.ExitCode != -1 {
		t.Fatal("start/reap identities differ")
	}
	t.Logf("CLIENT_LIFECYCLE_SOURCE_SHA256=%s; synthetic local process only, no provider", ClientLifecycleSHA256())
}

func TestClientLifecycleRejectsWorkspaceReceiptOverwriteAndUnknownIdentity(t *testing.T) {
	for _, kind := range []string{"workspace", "tmp", "existing", "symlink", "unknown", "public"} {
		t.Run(kind, func(t *testing.T) {
			command, identity, path, root := lifecycleFixture(t)
			switch kind {
			case "workspace":
				path = filepath.Join(root, "start.json")
			case "tmp":
				command.Env = append(command.Env, "TMPDIR="+filepath.Dir(path))
			case "existing":
				if err := os.WriteFile(path, []byte("preserved"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(filepath.Join(root, "untouched"), path); err != nil {
					t.Fatal(err)
				}
			case "unknown":
				identity.Scenario = "multiple_workers"
			case "public":
				if err := os.Chmod(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := RunObservedClient(command, identity, path, root); err == nil || command.Process != nil {
				t.Fatal("invalid observation launched child")
			}
			if kind == "existing" {
				body, _ := os.ReadFile(path)
				if string(body) != "preserved" {
					t.Fatal("existing receipt overwritten")
				}
			}
		})
	}
}

func TestClientLifecycleRejectsSubstitutedExecutableAndReaps(t *testing.T) {
	command, identity, path, root := lifecycleFixture(t)
	identity.ClientSHA256 = strings.Repeat("f", 64)
	if _, err := RunObservedClient(command, identity, path, root); err == nil || command.ProcessState == nil {
		t.Fatal("substituted executable admitted or unreaped")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid executable emitted receipt")
	}
}

func TestClientLifecycleCancellationReapsOnlyOwnedChild(t *testing.T) {
	command, identity, path, root := lifecycleFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command = exec.CommandContext(ctx, command.Path, command.Args[1:]...)
	command.Env = []string{"IVOAI_LIFECYCLE_TEST_CHILD=1"}
	ready := make(chan bool, 1)
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if _, err := os.Stat(path); err == nil {
					ready <- true
					cancel()
					return
				}
			case <-ctx.Done():
				ready <- false
				return
			}
		}
	}()
	value, err := RunObservedClient(command, identity, path, root)
	if !<-ready || err == nil || value == nil || command.ProcessState == nil || value.ExitCode == 0 {
		t.Fatal("cancelled synthetic client did not reap with failure")
	}
}
