package skillcatalog

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

func localFixture(t *testing.T) (LocalSource, supplychain.Reference) {
	t.Helper()
	catalog, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	source, _ := catalog.Source("ponytail")
	return LocalSource{Source: source, Paths: []string{"skills/example/SKILL.md", "LICENSE"}, Files: fstest.MapFS{
		"skills/example/SKILL.md": &fstest.MapFile{Data: []byte("declarative fixture"), Mode: 0600},
		"LICENSE":                 &fstest.MapFile{Data: []byte("fixture license"), Mode: 0600},
	}}, supplychain.Reference{ID: source.ID, Kind: supplychain.KindSkill, Source: source.Upstream.Repository}
}

func TestLocalSourceDeterministicAndImmutable(t *testing.T) {
	source, ref := localFixture(t)
	a, err := source.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	source.Paths[0], source.Paths[1] = source.Paths[1], source.Paths[0]
	b, err := source.Resolve(context.Background(), ref)
	if err != nil || a.Integrity.Digest != b.Integrity.Digest {
		t.Fatal("non-deterministic archive", err)
	}
	r, err := source.Fetch(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	r.Close()
	if err != nil || len(data) == 0 {
		t.Fatal("empty archive", err)
	}
	source.Files.(fstest.MapFS)["LICENSE"].Data = []byte("changed")
	if _, err := source.Fetch(context.Background(), a); err == nil {
		t.Fatal("changed content accepted")
	}
}

func TestLocalSourceRejectsUnsafeInventory(t *testing.T) {
	for _, tc := range []struct {
		name  string
		paths []string
		mode  fs.FileMode
	}{
		{"traversal", []string{"../SKILL.md"}, 0600},
		{"script", []string{"setup.sh"}, 0600},
		{"executable", []string{"SKILL.md"}, 0700},
		{"symlink", []string{"SKILL.md"}, fs.ModeSymlink | 0600},
		{"duplicate", []string{"SKILL.md", "SKILL.md"}, 0600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, ref := localFixture(t)
			source.Paths = tc.paths
			source.Files = fstest.MapFS{tc.paths[0]: &fstest.MapFile{Data: []byte("fixture"), Mode: tc.mode}}
			if _, err := source.Resolve(context.Background(), ref); err == nil {
				t.Fatal("unsafe inventory accepted")
			}
		})
	}
}

func TestLocalSourceRejectsSymlinkParent(t *testing.T) {
	source, ref := localFixture(t)
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "SKILL.md"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "skills")); err != nil {
		t.Fatal(err)
	}
	source.Paths = []string{"skills/SKILL.md"}
	source.Files = os.DirFS(root)
	if _, err := source.Resolve(context.Background(), ref); err == nil {
		t.Fatal("symlink parent accepted")
	}
}

func TestLocalSourceRejectsDisguisedBinary(t *testing.T) {
	for _, body := range [][]byte{[]byte("\x7fELF\x00binary"), {0xff, 0xfe, 0xfd}} {
		source, ref := localFixture(t)
		source.Files = fstest.MapFS{"SKILL.md": &fstest.MapFile{Data: body, Mode: 0600}}
		source.Paths = []string{"SKILL.md"}
		if _, err := source.Resolve(context.Background(), ref); err == nil {
			t.Fatal("binary content accepted as declarative markdown")
		}
	}
}
