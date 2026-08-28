package workingcontext

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLocalStoreExactRecoveryRangeModesAndOwnership(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache", "ivoai", "working-context")
	store, err := NewLocalStore(root, LocalOptions{ID: sequentialIDs()})
	if err != nil {
		t.Fatal(err)
	}
	owner := testOwner("1", "worker_0123456789abcdef0123456789abcdef")
	payload := []byte("byte-exact\x00binary\xffevidence")
	ref, err := store.Put(context.Background(), PutRequest{Kind: ArtifactWorkerOutput, MediaType: "application/octet-stream", Owner: owner, Sensitivity: SensitivityRestricted, TTL: time.Hour}, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	reader, readRef, err := store.Read(context.Background(), Ownership{SessionID: owner.SessionID}, ref.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || !bytes.Equal(got, payload) || readRef != ref {
		t.Fatalf("got=%q ref=%+v err=%v", got, readRef, err)
	}
	rangeBody, _, err := store.ReadRange(context.Background(), owner, ref.ID, 5, 7)
	if err != nil || !bytes.Equal(rangeBody, payload[5:12]) {
		t.Fatalf("range=%q err=%v", rangeBody, err)
	}
	if _, _, err := store.ReadRange(context.Background(), owner, ref.ID, -1, 1); err == nil {
		t.Fatal("negative range accepted")
	}
	if _, _, err := store.ReadRange(context.Background(), owner, ref.ID, int64(len(payload)), 2); err == nil {
		t.Fatal("overflow range accepted")
	}
	if _, _, err := store.Read(context.Background(), testOwner("2", ""), ref.ID); err == nil {
		t.Fatal("cross-session read accepted")
	}
	otherWorker := Ownership{SessionID: owner.SessionID, TaskID: owner.TaskID, WorkerID: "worker_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if _, _, err := store.Read(context.Background(), otherWorker, ref.ID); err == nil {
		t.Fatal("cross-worker read accepted")
	}
	if _, err := store.Stat(context.Background(), owner, "../../escape"); err == nil {
		t.Fatal("malformed reference accepted")
	}
	assertMode(t, root, 0o700)
	assertMode(t, filepath.Join(root, "objects", ref.ID), 0o700)
	assertMode(t, filepath.Join(root, "objects", ref.ID, "payload"), 0o600)
	assertMode(t, filepath.Join(root, "objects", ref.ID, "metadata.json"), 0o600)
}

func TestLocalStoreLimitsSecretsIntegrityAndUnmanagedPreservation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "working-context")
	limits := Limits{MaxArtifactBytes: 8, MaxSessionBytes: 12, MaxGlobalBytes: 16, MaxSessionCount: 2, MaxGlobalCount: 3, DefaultTTL: time.Hour, MaxTTL: 2 * time.Hour}
	store, err := NewLocalStore(root, LocalOptions{Limits: limits, ID: sequentialIDs()})
	if err != nil {
		t.Fatal(err)
	}
	ownerA := testOwner("1", "")
	ownerB := testOwner("2", "")
	put := func(owner Ownership, body string) (ArtifactRef, error) {
		return store.Put(context.Background(), PutRequest{Kind: ArtifactStdout, MediaType: "text/plain", Owner: owner, Sensitivity: SensitivityInternal}, strings.NewReader(body))
	}
	if _, err := put(ownerA, "123456789"); err == nil {
		t.Fatal("per-artifact limit ignored")
	}
	first, err := put(ownerA, "12345678")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := put(ownerA, "12345"); err == nil {
		t.Fatal("session byte quota ignored")
	}
	if _, err := put(ownerB, "abcdefgh"); err != nil {
		t.Fatal(err)
	}
	if _, err := put(testOwner("3", ""), "x"); err == nil {
		t.Fatal("global byte quota ignored")
	}
	if _, err := store.Put(context.Background(), PutRequest{Kind: ArtifactStdout, MediaType: "text/plain", Owner: ownerA, Sensitivity: SensitivitySecret}, strings.NewReader("secret")); err == nil {
		t.Fatal("secret-class artifact accepted")
	}
	unmanaged := filepath.Join(root, "objects", "user-not-managed")
	if err := os.MkdirAll(unmanaged, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unmanaged, "keep"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "objects", first.ID, "payload"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Read(context.Background(), ownerA, first.ID); err == nil {
		t.Fatal("corrupt payload accepted")
	}
	if _, err := store.GC(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(unmanaged, "keep")); err != nil {
		t.Fatal("unmanaged content was removed")
	}
}

func TestLocalStoreTTLGCInterruptedStagingAndCorruptMetadata(t *testing.T) {
	root := filepath.Join(t.TempDir(), "working-context")
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	store, err := NewLocalStore(root, LocalOptions{Now: func() time.Time { return now }, ID: sequentialIDs()})
	if err != nil {
		t.Fatal(err)
	}
	owner := testOwner("1", "")
	ref, err := store.Put(context.Background(), PutRequest{Kind: ArtifactTestLog, MediaType: "text/plain", Owner: owner, Sensitivity: SensitivityInternal, TTL: time.Hour}, strings.NewReader("test failed"))
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	if _, _, err := store.Read(context.Background(), owner, ref.ID); err == nil {
		t.Fatal("expired artifact read")
	}
	staging := filepath.Join(root, "objects", ".staging-interrupted")
	if err := os.Mkdir(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-48 * time.Hour)
	if err := os.Chtimes(staging, old, old); err != nil {
		t.Fatal(err)
	}
	removed, err := store.GC(context.Background())
	if err != nil || removed != 2 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}

	now = now.Add(time.Hour)
	ref, err = store.Put(context.Background(), PutRequest{Kind: ArtifactStdout, MediaType: "text/plain", Owner: owner, Sensitivity: SensitivityInternal}, strings.NewReader("valid"))
	if err != nil {
		t.Fatal(err)
	}
	metadata := filepath.Join(root, "objects", ref.ID, "metadata.json")
	if err := os.WriteFile(metadata, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	removed, err = store.GC(context.Background())
	if err == nil || removed != 0 {
		t.Fatalf("corrupt metadata was silently removed: removed=%d err=%v", removed, err)
	}
	if _, statErr := os.Stat(filepath.Dir(metadata)); statErr != nil {
		t.Fatal("corrupt artifact removed")
	}
}

func TestLocalStoreRejectsSymlinkChainAndConcurrentIDsRemainUnique(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	if err := os.Mkdir(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLocalStore(filepath.Join(link, "artifacts"), LocalOptions{}); err == nil {
		t.Fatal("symlink chain accepted")
	}

	store, err := NewLocalStore(filepath.Join(base, "safe"), LocalOptions{ID: sequentialIDs()})
	if err != nil {
		t.Fatal(err)
	}
	owner := testOwner("1", "")
	const count = 32
	refs := make(chan string, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ref, err := store.Put(context.Background(), PutRequest{Kind: ArtifactWorkerOutput, MediaType: "text/plain", Owner: owner, Sensitivity: SensitivityInternal}, strings.NewReader("concurrent"))
			if err != nil {
				errs <- err
				return
			}
			refs <- ref.ID
		}()
	}
	wg.Wait()
	close(refs)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for id := range refs {
		if seen[id] {
			t.Fatalf("duplicate ID %s", id)
		}
		seen[id] = true
	}
	if len(seen) != count {
		t.Fatalf("refs=%d", len(seen))
	}
}

func sequentialIDs() func() (string, error) {
	var mu sync.Mutex
	value := 0
	return func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		value++
		return fmt.Sprintf("artifact_%032x", value), nil
	}
}

func testOwner(suffix, worker string) Ownership {
	return Ownership{SessionID: "sess_" + strings.Repeat("0", 31) + suffix, TaskID: "task", WorkerID: worker}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("mode %s=%o want %o", path, info.Mode().Perm(), want)
	}
}
