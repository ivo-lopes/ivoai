package components

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type nodeZeroReader struct{}

func (nodeZeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestNodeArchiveAcceptsReviewedExecutableSize(t *testing.T) {
	// Node 26.11.1 Linux amd64's reviewed binary is 150047368 bytes.
	const reviewedSize int64 = 150047368
	archive := filepath.Join(t.TempDir(), "node.tgz")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewWriterLevel(f, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "node-release/bin/node", Size: reviewedSize, Mode: 0755, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(tw, nodeZeroReader{}, reviewedSize); err != nil {
		t.Fatal(err)
	}
	for _, closer := range []io.Closer{tw, gz, f} {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	destination := t.TempDir()
	if err := extractNodeArchive(archive, destination); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(destination, "bin/node"))
	if err != nil || info.Size() != reviewedSize || info.Mode().Perm() != 0700 {
		t.Fatalf("extracted executable: info=%v err=%v", info, err)
	}
}

func TestNodeArchiveKeepsOtherEntryAndExecutableBounds(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int64
	}{{"lib/oversized", 129 << 20}, {"bin/node", 257 << 20}} {
		t.Run(tc.name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "node.tgz")
			f, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			gz := gzip.NewWriter(f)
			tw := tar.NewWriter(gz)
			if err := tw.WriteHeader(&tar.Header{Name: "node-release/" + tc.name, Size: tc.size, Mode: 0644, Typeflag: tar.TypeReg}); err != nil {
				t.Fatal(err)
			}
			// No payload is needed: the declared size must be rejected before reading it.
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			err = extractNodeArchive(archive, t.TempDir())
			if err == nil || !strings.Contains(err.Error(), "invalid Node archive entry size") {
				t.Fatalf("oversized entry accepted: %v", err)
			}
		})
	}
}
