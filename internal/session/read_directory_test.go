package session

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
)

type readFileState struct {
	Mode  fs.FileMode
	Size  int64
	Ctime syscall.Timespec
	Body  string
}

func readTreeState(t *testing.T, root string) map[string]readFileState {
	t.Helper()
	out := map[string]readFileState{}
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		i, err := d.Info()
		if err != nil {
			return err
		}
		x := readFileState{Mode: i.Mode(), Size: i.Size(), Ctime: i.Sys().(*syscall.Stat_t).Ctim}
		if i.Mode().IsRegular() {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			x.Body = string(b)
		}
		out[path] = x
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}
func TestSessionListReadsNeitherCreateNorChangeMetadata(t *testing.T) {
	for _, which := range []string{"List", "ListBounded", "Active", "FindFrontend"} {
		t.Run(which, func(t *testing.T) {
			parent := t.TempDir()
			s := Store{Root: filepath.Join(parent, "missing")}
			before := readTreeState(t, parent)
			switch which {
			case "List":
				v, e := s.List()
				if e != nil || len(v) != 0 {
					t.Fatal(e)
				}
			case "ListBounded":
				v, e := s.ListBounded(context.Background(), 5, 1<<20)
				if e != nil || len(v.Sessions) != 0 {
					t.Fatal(e)
				}
			case "Active":
				v, e := s.Active()
				if e != nil || len(v) != 0 {
					t.Fatal(e)
				}
			case "FindFrontend":
				_, _ = s.FindFrontend("codex", "unknown", parent)
			}
			if !reflect.DeepEqual(before, readTreeState(t, parent)) {
				t.Fatal("missing-root read changed parent/tree/content/ctime")
			}
			s, v := requestJournalFixture(t)
			if ok, e := s.ClaimFrontendRequest(context.Background(), v.SessionID, v.FrontendSessionID, "owned"); e != nil || !ok {
				t.Fatal(e)
			}
			before = readTreeState(t, s.Root)
			switch which {
			case "List":
				values, err := s.List()
				if err != nil || len(values) != 1 {
					t.Fatal("owned list failed", err)
				}
			case "ListBounded":
				values, err := s.ListBounded(context.Background(), 5, 1<<20)
				if err != nil || len(values.Sessions) != 1 {
					t.Fatal("owned bounded list failed", err)
				}
			case "Active":
				values, err := s.Active()
				if err != nil || len(values) != 1 {
					t.Fatal("owned active list failed", err)
				}
			case "FindFrontend":
				value, err := s.FindFrontend(v.Frontend, v.FrontendSessionID, v.WorkingDirectory)
				if err != nil || value.SessionID != v.SessionID {
					t.Fatal("owned frontend lookup failed", err)
				}
			}
			if !reflect.DeepEqual(before, readTreeState(t, s.Root)) {
				t.Fatal("owned read changed tree/content/ctime")
			}
		})
	}
}
func TestSessionListRejectsUnsafeRootsWithoutRepair(t *testing.T) {
	for _, kind := range []string{"mode", "symlink", "file", "namespace_mode", "namespace_symlink"} {
		t.Run(kind, func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "sessions")
			target := t.TempDir()
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "mode":
				if err := os.Chmod(root, 0755); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				_ = os.Remove(root)
				if err := os.Symlink(target, root); err != nil {
					t.Fatal(err)
				}
			case "file":
				_ = os.Remove(root)
				if err := os.WriteFile(root, []byte("inert"), 0600); err != nil {
					t.Fatal(err)
				}
			case "namespace_mode":
				if err := os.Mkdir(filepath.Join(root, requestJournalDirectory), 0755); err != nil {
					t.Fatal(err)
				}
			case "namespace_symlink":
				if err := os.Symlink(target, filepath.Join(root, requestJournalDirectory)); err != nil {
					t.Fatal(err)
				}
			}
			before, other := readTreeState(t, parent), readTreeState(t, target)
			s := Store{Root: root}
			if _, err := s.List(); err == nil || errors.Is(err, fs.ErrNotExist) {
				t.Fatal("unsafe list root accepted", err)
			}
			if _, err := s.ListBounded(context.Background(), 5, 1<<20); err == nil || errors.Is(err, fs.ErrNotExist) {
				t.Fatal("unsafe bounded root accepted", err)
			}
			if !reflect.DeepEqual(before, readTreeState(t, parent)) || !reflect.DeepEqual(other, readTreeState(t, target)) {
				t.Fatal("unsafe read repaired modes/followed link/changed ctime")
			}
		})
	}
}
