package session

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupRuntimePreservesExactCheckpointSnapshot(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint("legacy_", legacy), func(t *testing.T) {
			s, v := fixtureSession(t, t.TempDir())
			if err := s.Create(v); err != nil {
				t.Fatal(err)
			}
			runtime, err := s.RuntimeDir(v.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SaveCheckpoint(v.SessionID, Checkpoint{Objective: "exact durable checkpoint"}); err != nil {
				t.Fatal(err)
			}
			_, before, err := s.LoadCheckpointSnapshot(v.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			if legacy {
				if err := os.Rename(filepath.Join(s.Root, "checkpoints", v.SessionID+".json"), filepath.Join(runtime, "checkpoint.json")); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.CleanupRuntime(v.SessionID); err != nil {
				t.Fatal(err)
			}
			_, after, err := s.LoadCheckpointSnapshot(v.SessionID)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("cleanup changed exact archived checkpoint bytes", err)
			}
		})
	}
}
