package app

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/skills"
)

// The release caller supplies a checksum-verified public binary. All mutable
// state is isolated; the menu and management commands execute that binary.
func TestNativeCapabilityPublishedArtifact(t *testing.T) {
	binary := os.Getenv("IVOAI_CAPABILITY_ARTIFACT")
	if binary == "" {
		t.Skip("provide a verified artifact")
	}
	a := nativeCapabilityTestApp(t)
	t.Setenv("NO_COLOR", "1")
	menu := exec.Command(binary)
	menu.Stdin = strings.NewReader("12\n1\n2\n2\n0\n0\n")
	output, err := menu.CombinedOutput()
	if err != nil {
		t.Fatalf("artifact TUI failed: %v", err)
	}
	if !bytes.Contains(output, []byte("Skills & Capabilities")) || bytes.Contains(output, []byte("# Ponytail")) {
		t.Fatal("invalid TUI metadata")
	}
	rows, err := a.NativeCapabilities(context.Background())
	if err != nil || len(rows) != 13 {
		t.Fatal("artifact pack unavailable", err)
	}
	for _, row := range rows {
		if row.Status != "ready" {
			t.Fatal("artifact source not ready", row.ID)
		}
	}
	cfg, err := a.Store.Load()
	if err != nil || cfg.Skills.ResolvedPonytail() != "off" {
		t.Fatal("artifact TUI preference did not persist", err)
	}
	store := skills.Store{Path: skills.RegistryPath(a.Store.Paths.StateDir)}
	registry, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	personal := registry.Entries[0]
	personal.ID, personal.ArtifactID = "personal-artifact-fixture", ""
	registry.Entries = append(registry.Entries, personal)
	if err := store.Save(registry); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"skills", "disable", "hallmark"}, {"skills", "pin", "ponytail"}, {"skills", "update"}, {"skills", "doctor"}} {
		if err := exec.Command(binary, args...).Run(); err != nil {
			t.Fatal("artifact management failed", args[1], err)
		}
	}
	registry, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range registry.Entries {
		if entry.ID == personal.ID && entry.ArtifactID == "" {
			found = true
		}
	}
	if !found {
		t.Fatal("personal entry overwritten")
	}
	info, err := os.Stat(filepath.Join(a.Store.Paths.StateDir, "skills", "registry.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private registry permissions changed", err)
	}
	t.Log("ARTIFACT_TUI=PASS NATIVE_SOURCES=13 PERSONAL_SKILL_PRESERVED=true PONYTAIL_POLICY=PASS")
}
