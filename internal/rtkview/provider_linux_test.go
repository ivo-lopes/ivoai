//go:build linux && amd64

package rtkview

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRejectsInvalidRuntimeAndArgumentsWithoutExecution(t *testing.T) {
	p := New("/nonexistent", "/nonexistent")
	for _, filter := range []string{"git-status", "git-diff", "$(touch /tmp/unwanted)", ""} {
		if _, err := p.Transform(context.Background(), filter, []byte("input")); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("%q: %v", filter, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Transform(ctx, "grep", nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := p.Transform(context.Background(), "grep", make([]byte, maxBytes+1)); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}

func TestSnapshotAndBoundedCapture(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	if err := os.WriteFile(source, []byte("snapshot"), 0500); err != nil {
		t.Fatal(err)
	}
	if err := snapshot(source, target); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(target)
	if string(body) != "snapshot" {
		t.Fatal("copy changed bytes")
	}
	if err := snapshot(source, target); err == nil {
		t.Fatal("overwrote snapshot")
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(source, link); err != nil {
		t.Fatal(err)
	}
	if regularExecutable(link) || snapshot(link, filepath.Join(root, "linked")) == nil {
		t.Fatal("symlink admitted")
	}
	buffer := &limitedBuffer{max: 3}
	if _, err := buffer.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if _, err := buffer.Write([]byte("d")); err == nil || buffer.String() != "abc" || !buffer.overflow {
		t.Fatal("capture exceeded bound")
	}
}

func TestRealPinnedOSSView(t *testing.T) {
	binary, helper := os.Getenv("IVOAI_TEST_RTK_OSS"), os.Getenv("IVOAI_TEST_RTK_HELPER")
	if binary == "" || helper == "" {
		t.Skip("NOT_RUN: real pinned RTK OSS binary and static helper explicitly required")
	}
	p := New(binary, helper)
	raw := []byte(strings.Repeat("src/example.go:12: referenced_symbol\n", 50))
	view, err := p.Transform(context.Background(), "grep", raw)
	if err != nil {
		t.Fatalf("real OSS view unavailable (fixed diagnostic): %v", err)
	}
	if len(view) == 0 || len(view) >= len(raw) || !bytes.Contains(view, []byte("example.go")) {
		t.Fatal("real view did not reduce this controlled inventory fixture")
	}
	t.Logf("real_fixture raw_bytes=%d view_bytes=%d; output-only measurement, no session-token claim", len(raw), len(view))
	// Wrong bytes are checked inside the same containment and never executed.
	if _, err := New(helper, helper).Transform(context.Background(), "grep", raw); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("wrong binary fingerprint admitted: %v", err)
	}
	// Exercise the strict helper independently of the caller's input guard.
	for _, input := range []struct {
		filter string
		raw    []byte
	}{
		{"grep", make([]byte, maxBytes+1)},
		{"git-diff", raw},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := p.run(ctx, binary, "pipe", input.filter, input.raw)
		cancel()
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("helper admitted unbounded input/filter: %v", err)
		}
	}
}
