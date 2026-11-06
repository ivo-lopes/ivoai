package servercmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/server"
	"golang.org/x/sys/unix"
)

type countingContextIngest struct{ calls int }

func (c *countingContextIngest) Ingest(context.Context, string) (int, error) {
	c.calls++
	return 1, nil
}
func TestContextLockApplies0640DespiteUmask077(t *testing.T) {
	root := t.TempDir()
	layout := server.DefaultLayout(root)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	old := unix.Umask(0077)
	defer unix.Umask(old)
	c := &countingContextIngest{}
	if _, err := ingestContextLocked(context.Background(), layout, c, "docs"); err != nil {
		t.Fatal(err)
	}
	var parent, lock unix.Stat_t
	if err := unix.Stat(layout.ContextDir, &parent); err != nil {
		t.Fatal(err)
	}
	if err := unix.Stat(filepath.Join(layout.ContextDir, "ingest.lock"), &lock); err != nil {
		t.Fatal(err)
	}
	if lock.Mode&0777 != 0640 || lock.Uid != parent.Uid || lock.Gid != parent.Gid || c.calls != 1 {
		t.Fatalf("lock identity mode=%o uid=%d gid=%d", lock.Mode&0777, lock.Uid, lock.Gid)
	}
}
func TestContextLockRejectsUnsafeInodeWithoutChangingIt(t *testing.T) {
	for _, kind := range []string{"hardlink", "nonempty", "world_writable", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			layout := server.DefaultLayout(t.TempDir())
			if err := layout.Ensure(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(layout.ContextDir, "ingest.lock")
			switch kind {
			case "hardlink":
				original := filepath.Join(layout.ContextDir, "private")
				if err := os.WriteFile(original, nil, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(original, path); err != nil {
					t.Fatal(err)
				}
			case "nonempty":
				if err := os.WriteFile(path, []byte("private-data"), 0600); err != nil {
					t.Fatal(err)
				}
			case "world_writable":
				if err := os.WriteFile(path, nil, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0666); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := unix.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			c := &countingContextIngest{}
			if _, err := ingestContextLocked(context.Background(), layout, c, "docs"); err == nil {
				t.Fatal("unsafe lock admitted")
			}
			after, _ := os.Lstat(path)
			if after.Mode() != before.Mode() || c.calls != 0 {
				t.Fatal("unsafe inode altered or ingestion ran")
			}
		})
	}
}
func TestContextRootFirstLockWorksForDifferentServiceUID(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root or an isolated user namespace with mapped service UID/GID; not counted as real cross-UID evidence")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	layout := server.DefaultLayout(root)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	// Each ancestor must permit traversing to the service-owned context directory.
	for p := filepath.Dir(layout.ContextDir); p != os.TempDir() && strings.HasPrefix(p, os.TempDir()+string(filepath.Separator)); p = filepath.Dir(p) {
		if err := os.Chmod(p, 0755); err != nil {
			t.Fatal(err)
		}

	}
	const uid, gid = 1234, 4567
	if err := os.Chown(layout.ContextDir, uid, gid); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(layout.ContextDir, os.ModeSetgid|0750); err != nil {
		t.Fatal(err)
	}
	old := unix.Umask(0077)
	c := &countingContextIngest{}
	_, err := ingestContextLocked(context.Background(), layout, c, "docs")
	unix.Umask(old)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestContextDifferentUIDHelper$", "-test.v")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "IVOAI_CONTEXT_UID_HELPER=" + layout.ContextDir}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: gid, NoSetGroups: true}}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("actual different-UID process could not acquire root-first lock: %v %s", err, out)
	}
	if !strings.Contains(string(out), "actual_uid=1234 actual_gid=4567") {
		t.Fatal("missing real UID/GID evidence", string(out))
	}
	t.Logf("root-first umask077 parent_uid=%d, child proof: %s", os.Geteuid(), out)
}
func TestContextDifferentUIDHelper(t *testing.T) {
	path := os.Getenv("IVOAI_CONTEXT_UID_HELPER")
	if path == "" {
		t.Skip("helper subprocess only")
	}
	if os.Geteuid() != 1234 || os.Getegid() != 4567 {
		t.Fatal("helper did not change OS credentials")
	}
	c := &countingContextIngest{}
	_, err := ingestContextLocked(context.Background(), server.Layout{ContextDir: path}, c, "docs")
	if err != nil || c.calls != 1 {
		t.Fatal("service UID could not acquire lock", err)
	}
	fmt.Printf("actual_uid=%d actual_gid=%d\n", os.Geteuid(), os.Getegid())
	if _, err := os.Stat(path); errors.Is(err, os.ErrPermission) {
		t.Fatal(err)
	}
}
