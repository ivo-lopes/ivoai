package workingcontext

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveProtectedArtifactsDoNotPoisonLegacyCoreRollback(t *testing.T) {
	binary := os.Getenv("IVOAI_LEGACY_ARTIFACT_PROBE")
	if binary == "" {
		t.Skip("explicit source-pinned old Core probe required; not a rollback PASS")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("absolute old Core probe path required")
	}
	root := filepath.Join(t.TempDir(), "artifacts")
	store, err := NewLocalStore(root, LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	owner := testOwner("1", "")
	put := PutRequest{Kind: ArtifactStdout, Owner: owner, MediaType: "text/plain", Sensitivity: SensitivityRestricted}
	legacy, err := store.Put(context.Background(), put, strings.NewReader("legacy bytes"))
	if err != nil {
		t.Fatal(err)
	}
	put.ProtectedRecovery = true
	protected, err := store.Put(context.Background(), put, strings.NewReader("protected bytes preserved"))
	if err != nil {
		t.Fatal(err)
	}
	fixture, _ := json.Marshal(map[string]string{"root": root, "session": owner.SessionID, "legacy": legacy.ID, "protected": protected.ID})
	input := filepath.Join(t.TempDir(), "fixture.json")
	if err := os.WriteFile(input, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run", "^TestLegacyProtectedNamespaceReaderProbe$", "-test.count=1", "-test.v")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir(), "TMPDIR=" + t.TempDir(), "IVOAI_LEGACY_ARTIFACT_FIXTURE=" + input}
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "--- PASS: TestLegacyProtectedNamespaceReaderProbe (") || strings.Contains(string(output), "--- SKIP:") {
		t.Fatalf("source-pinned legacy Core probe failed: %v", err)
	}
	if body, _, err := store.Read(context.Background(), owner, protected.ID); err != nil {
		t.Fatal("legacy rollback damaged protected object", err)
	} else {
		raw, _ := os.ReadFile(filepath.Join(root, "objects", protected.ID, "payload"))
		if string(raw) != "protected bytes preserved" {
			t.Fatal("protected bytes changed")
		}
		body.Close()
	}
	t.Log("LEGACY_CORE_ROLLBACK=PASS old_full_and_range_denied legacy_put_read_gc_worked protected_bytes_preserved; old_inventory_excludes_protected_disk")
}
