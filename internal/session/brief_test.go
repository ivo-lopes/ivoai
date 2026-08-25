package session

import (
	"path/filepath"
	"testing"
)

func TestSharedContextBriefIsPrivateAndSessionMetadataContainsOnlyHash(t *testing.T) {
	store := Store{Root: filepath.Join(t.TempDir(), "sessions")}
	id, _ := NewID()
	metadata, err := store.SaveBrief(id, SharedContextBrief{Objective: "Review scheduler", Facts: []string{"bounded fact"}, References: []string{"memory:page/1", "context:doc/2"}, MemoryStatus: "ready", ContextStatus: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	if !metadata.Performed || metadata.ReferenceCount != 2 || len(metadata.BriefHash) != 64 {
		t.Fatalf("metadata=%+v", metadata)
	}
	loaded, err := store.LoadBrief(id)
	if err != nil || loaded.Facts[0] != "bounded fact" {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
}
