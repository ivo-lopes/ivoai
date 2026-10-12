package codeintel

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestBoundedCacheOpenRejectsFIFOWithoutWriter(t *testing.T) {
	if path := os.Getenv("IVOAI_TEST_FIFO_PATH"); path != "" {
		if _, err := readBounded(path, 1024); err == nil {
			t.Fatal("accepted FIFO as cache data")
		}
		return
	}
	path := filepath.Join(t.TempDir(), "fifo")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	// No writer exists. A blocking open can never succeed, rather than merely
	// racing a slow machine. The subprocess timeout only bounds a broken test.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBoundedCacheOpenRejectsFIFOWithoutWriter$")
	cmd.Env = append(os.Environ(), "IVOAI_TEST_FIFO_PATH="+path)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatal("cache FIFO open blocked without a writer")
	}
	if err != nil {
		t.Fatalf("FIFO rejection failed: %v %s", err, output)
	}
}

func TestSourceOpenRejectsFIFOAfterRegularObservation(t *testing.T) {
	if path := os.Getenv("IVOAI_TEST_SOURCE_FIFO"); path != "" {
		root, err := os.OpenRoot(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		// Simulate the exact validation-to-open transition deterministically.
		if info, err := root.Lstat("source.go"); err != nil || !info.Mode().IsRegular() {
			t.Fatal("missing initial regular file")
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mkfifo(path, 0600); err != nil {
			t.Fatal(err)
		}
		if file, err := openSource(root, "source.go"); err == nil {
			file.Close()
			t.Fatal("accepted replacement FIFO")
		}
		return
	}
	path := filepath.Join(t.TempDir(), "source.go")
	if err := os.WriteFile(path, []byte("package fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSourceOpenRejectsFIFOAfterRegularObservation$")
	cmd.Env = append(os.Environ(), "IVOAI_TEST_SOURCE_FIFO="+path)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatal("source FIFO replacement blocked without writer")
	}
	if err != nil {
		t.Fatalf("source FIFO rejection failed: %v %s", err, output)
	}
}

func TestSourceOpenPreservesNonblockingDescriptorAndRejectsSymlinks(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "source.go"), []byte("package fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	file, err := openSource(root, "source.go")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_NONBLOCK == 0 {
		t.Fatalf("nonblocking invariant absent: %v %v", flags, err)
	}
	if err := os.Symlink("source.go", filepath.Join(directory, "link.go")); err != nil {
		t.Fatal(err)
	}
	if file, err := openSource(root, "link.go"); err == nil {
		file.Close()
		t.Fatal("accepted final symlink")
	}
}
