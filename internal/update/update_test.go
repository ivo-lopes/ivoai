package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCheckRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"v0.2.0","html_url":"https://example/release"}`))
	}))
	defer server.Close()
	release, available, err := (Checker{Client: server.Client(), Endpoint: server.URL}).Check(context.Background(), "v0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if !available || release.Version != "v0.2.0" {
		t.Fatalf("bad result %#v %t", release, available)
	}
}

func TestCheckDoesNotOfferDowngrade(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.1.0","html_url":"https://example/release"}`))
	}))
	defer server.Close()
	_, available, err := (Checker{Client: server.Client(), Endpoint: server.URL}).Check(context.Background(), "v0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if available {
		t.Fatal("older release offered as an update")
	}
}

func TestCheckRejectsOversizedAndTrailingReleaseMetadata(t *testing.T) {
	for name, body := range map[string]string{
		"oversized": strings.Repeat(" ", (1<<20)+1),
		"trailing":  `{"tag_name":"v0.2.0"} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			if _, _, err := (Checker{Client: server.Client(), Endpoint: server.URL}).Check(context.Background(), "v0.1.0"); err == nil {
				t.Fatal("unsafe release metadata was accepted")
			}
		})
	}
}

func TestApplyIsAtomicAndKeepsRollback(t *testing.T) {
	old := []byte("#!/bin/sh\necho 0.1.0\n")
	fresh := []byte("#!/bin/sh\necho 0.2.0\n")
	archive := updateArchive(t, fresh)
	sum := sha256.Sum256(archive)
	asset := "ivoai_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Base(r.URL.Path) == "checksums.txt" {
			fmt.Fprintf(w, "%x  %s\n", sum, asset)
			return
		}
		w.Write(archive)
	}))
	defer server.Close()
	dir := t.TempDir()
	executable := filepath.Join(dir, "ivoai")
	rollback := filepath.Join(dir, "state", "ivoai.previous")
	if err := os.WriteFile(executable, old, 0o700); err != nil {
		t.Fatal(err)
	}
	checker := Checker{Client: server.Client(), ReleaseBase: server.URL}
	if err := checker.Apply(context.Background(), Release{Version: "v0.2.0"}, executable, rollback); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(executable)
	previous, _ := os.ReadFile(rollback)
	if !bytes.Equal(got, fresh) || !bytes.Equal(previous, old) {
		t.Fatal("atomic update or rollback contents wrong")
	}
}

func TestRollbackRestoresPreviousAndRetainsNewerBinary(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "ivoai")
	rollback := filepath.Join(dir, "state", "ivoai.previous")
	current := []byte("#!/bin/sh\necho 0.2.0\n")
	previous := []byte("#!/bin/sh\necho 0.1.0\n")
	if err := os.WriteFile(executable, current, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(rollback), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rollback, previous, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := (Checker{}).Rollback(context.Background(), executable, rollback); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := os.ReadFile(rollback + ".newer")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, previous) || !bytes.Equal(retained, current) {
		t.Fatal("rollback did not restore the previous binary and retain the replaced one")
	}
}

func TestRollbackRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "ivoai")
	rollback := filepath.Join(dir, "previous")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\necho 0.2.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(executable, rollback); err != nil {
		t.Fatal(err)
	}
	if err := (Checker{}).Rollback(context.Background(), executable, rollback); err == nil {
		t.Fatal("symlink rollback binary was accepted")
	}
}

func TestPrepareRejectsInvalidChecksumWithoutChangingExecutable(t *testing.T) {
	archive := updateArchive(t, []byte("#!/bin/sh\necho v0.6.0\n"))
	asset := "ivoai_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Base(r.URL.Path) == "checksums.txt" {
			fmt.Fprintf(w, "%064d  %s\n", 0, asset)
			return
		}
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	dir := t.TempDir()
	executable := filepath.Join(dir, "ivoai")
	original := []byte("#!/bin/sh\necho v0.5.0\n")
	if err := os.WriteFile(executable, original, 0o755); err != nil {
		t.Fatal(err)
	}
	checker := Checker{Client: server.Client(), ReleaseBase: server.URL}
	if _, err := checker.Prepare(context.Background(), Release{Version: "v0.6.0"}, executable); err == nil {
		t.Fatal("invalid checksum was accepted")
	}
	after, err := os.ReadFile(executable)
	if err != nil || !bytes.Equal(after, original) {
		t.Fatalf("checksum failure changed executable: err=%v", err)
	}
}

func TestPrepareRejectsCandidateWithWrongVersion(t *testing.T) {
	archive := updateArchive(t, []byte("#!/bin/sh\necho v9.9.9\n"))
	asset := "ivoai_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Base(r.URL.Path) == "checksums.txt" {
			fmt.Fprintf(w, "%x  %s\n", sum, asset)
			return
		}
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	dir := t.TempDir()
	executable := filepath.Join(dir, "ivoai")
	original := []byte("#!/bin/sh\necho v0.5.0\n")
	if err := os.WriteFile(executable, original, 0o755); err != nil {
		t.Fatal(err)
	}
	checker := Checker{Client: server.Client(), ReleaseBase: server.URL}
	if _, err := checker.Prepare(context.Background(), Release{Version: "v0.6.0"}, executable); err == nil || !strings.Contains(err.Error(), "probe failed") {
		t.Fatalf("wrong-version candidate accepted: %v", err)
	}
	after, err := os.ReadFile(executable)
	if err != nil || !bytes.Equal(after, original) {
		t.Fatalf("candidate probe failure changed executable: err=%v", err)
	}
}

func updateArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var result bytes.Buffer
	gz := gzip.NewWriter(&result)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "ivoai", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}
