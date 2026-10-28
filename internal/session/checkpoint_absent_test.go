package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestCheckpointAbsentNeverOpensNamespaceEntries(t *testing.T) {
	for _, location := range []string{"current", "legacy"} {
		for _, kind := range []string{"regular", "directory", "symlink", "dangling symlink", "FIFO"} {
			t.Run(location+"/"+kind, func(t *testing.T) {
				store := Store{Root: t.TempDir()}
				id, _ := NewID()
				if absent, err := store.CheckpointAbsent(id); !absent || err != nil {
					t.Fatalf("both absent: %v %v", absent, err)
				}
				path := filepath.Join(store.Root, "checkpoints", id+".json")
				if location == "legacy" {
					path = filepath.Join(store.Root, "runtime", id, "checkpoint.json")
				}
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				var err error
				switch kind {
				case "regular":
					err = os.WriteFile(path, []byte("not even JSON"), 0600)
				case "directory":
					err = os.Mkdir(path, 0700)
				case "symlink":
					err = os.Symlink(t.TempDir(), path)
				case "dangling symlink":
					err = os.Symlink(filepath.Join(t.TempDir(), "missing"), path)
				case "FIFO":
					err = unix.Mkfifo(path, 0600)
				}
				if err != nil {
					t.Fatal(err)
				}
				finished := make(chan bool, 1)
				go func() { absent, err := store.CheckpointAbsent(id); finished <- !absent && err == nil }()
				select {
				case denied := <-finished:
					if !denied {
						t.Fatal("entry was read or treated as absent")
					}
				case <-time.After(time.Second):
					t.Fatal("namespace inspection blocked on checkpoint contents")
				}
			})
		}
	}
}

func TestCheckpointAbsentRejectsInvalidIdentityAndInspectionErrors(t *testing.T) {
	store := Store{Root: t.TempDir()}
	if absent, err := store.CheckpointAbsent("../escape"); absent || err == nil {
		t.Fatal("invalid ID admitted")
	}
	id, _ := NewID()
	// ENOTDIR is deterministic for root/non-root CI and must not mean absence.
	if err := os.WriteFile(filepath.Join(store.Root, "checkpoints"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if absent, err := store.CheckpointAbsent(id); absent || err == nil {
		t.Fatalf("inspection error admitted: %v %v", absent, err)
	}
}
