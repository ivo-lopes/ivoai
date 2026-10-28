package workingcontext

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProtectedNamespaceCountsTowardExistingQuotasAndGC(t *testing.T) {
	now := time.Now().UTC()
	limits := DefaultLimits()
	limits.MaxSessionCount, limits.MaxGlobalCount = 2, 2
	store, err := NewLocalStore(filepath.Join(t.TempDir(), "artifacts"), LocalOptions{Limits: limits, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	owner := testOwner("1", "")
	put := PutRequest{Kind: ArtifactStdout, MediaType: "text/plain", Owner: owner, Sensitivity: SensitivityRestricted}
	legacy, err := store.Put(context.Background(), put, strings.NewReader("legacy"))
	if err != nil || !strings.HasPrefix(legacy.ID, "artifact_") {
		t.Fatal("legacy namespace changed", err)
	}
	put.ProtectedRecovery = true
	protected, err := store.Put(context.Background(), put, strings.NewReader("snapshot"))
	if err != nil || !ProtectedArtifactID(protected.ID) {
		t.Fatal("protected namespace missing", err)
	}
	if protected.Kind != legacy.Kind {
		t.Fatal("metadata kind/schema was changed")
	}
	if _, err := store.Put(context.Background(), put, strings.NewReader("over quota")); err == nil {
		t.Fatal("protected objects escaped count quota")
	}
	if body, _, err := store.ReadRange(context.Background(), owner, protected.ID, 1, 3); err != nil || string(body) != "nap" {
		t.Fatal("host artifact range changed", err)
	}
	now = now.Add(limits.DefaultTTL + time.Second)
	if removed, err := store.GC(context.Background()); err != nil || removed != 2 {
		t.Fatalf("GC missed a namespace: removed=%d err=%v", removed, err)
	}
	if _, err := store.Put(context.Background(), put, strings.NewReader("after GC")); err != nil {
		t.Fatal("GC did not restore quota", err)
	}
}

func TestProtectedIDCannotBeSelectedByIDGeneratorWithoutHostPolicy(t *testing.T) {
	store, err := NewLocalStore(filepath.Join(t.TempDir(), "artifacts"), LocalOptions{ID: func() (string, error) { return "evidence_" + strings.Repeat("a", 32), nil }})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Put(context.Background(), PutRequest{Kind: ArtifactStdout, MediaType: "text/plain", Owner: testOwner("1", ""), Sensitivity: SensitivityInternal}, strings.NewReader("unmarked"))
	if err == nil {
		t.Fatal("protected ID was emitted without host policy")
	}
}
