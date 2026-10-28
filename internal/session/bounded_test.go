package session

import (
	"context"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBoundedSnapshotsDoNotCreateAndPreserveNamespaces(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	s := Store{Root: root}
	snapshot, err := s.ListBounded(context.Background(), 4, 1<<20)
	if err != nil || len(snapshot.Sessions) != 0 {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("read created directory")
	}
	var ids []string
	for i := 0; i < 3; i++ {
		_, v := fixtureSession(t, root)
		if err := s.Create(v); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, v.SessionID)
	}
	for i, dir := range []string{s.nativeDir(), s.frontendDir()} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(root, ids[i]+".json"), filepath.Join(dir, ids[i]+".json")); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err = s.ListBounded(context.Background(), 4, 1<<20)
	if err != nil || len(snapshot.Sessions) != 3 || snapshot.Partial {
		t.Fatal(snapshot, err)
	}
	snapshot, err = s.ListBounded(context.Background(), 1, 1<<20)
	if err != nil || len(snapshot.Sessions) > 1 || !snapshot.Truncated {
		t.Fatal("cap lost", snapshot, err)
	}
	if _, err := s.GetBounded(ids[0], 1); err == nil {
		t.Fatal("oversized metadata admitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.ListBounded(ctx, 4, 1<<20); err == nil {
		t.Fatal("canceled read admitted")
	}
}
func TestBoundedSnapshotRejectsFIFOAndSymlinkWithoutOpenBlocking(t *testing.T) {
	s, v := fixtureSession(t, filepath.Join(t.TempDir(), "sessions"))
	if err := os.MkdirAll(s.Root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Root, v.SessionID+".json")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.GetBounded(v.SessionID, 4096); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO admitted")
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO open blocked")
	}
	os.Remove(path)
	if err := os.Symlink("/etc/passwd", path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetBounded(v.SessionID, 4096); err == nil {
		t.Fatal("symlink admitted")
	}
}
