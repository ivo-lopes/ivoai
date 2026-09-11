package skillcatalog

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path/filepath"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/skills"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

//go:embed all:native
var nativeFS embed.FS

// NativeIDs is the release-approved pack, deliberately excluding historical
// intake such as awesome-gpt-image-2. Availability never implies authorization.
func NativeIDs() []string {
	return []string{"anthropic-cybersecurity-skills", "caveman-skills", "codex-security-skills", "hallmark", "i-have-adhd", "impeccable", "marketing-skills", "mattpocock-skills", "ponytail", "reverse-skill", "superpowers", "taste-skill", "ui-ux-pro-max"}
}

// NativeQuarantine records only reviewed metadata, never failed content or a
// raw transport/parser error. Quarantined entries cannot enter the Skill Gate.
func NativeQuarantine(id string) ([]skills.Entry, error) {
	local, err := NativeSource(id)
	if err != nil {
		return nil, err
	}
	var entries []skills.Entry
	for _, c := range local.Source.Classifications {
		for _, body := range local.Source.Skills {
			if body.Path != c.Path {
				continue
			}
			entry := c.entry(local.Source, body, body.Path)
			entry.ArtifactID = id
			entry.Lifecycle = skills.LifecycleQuarantined
			entry.QuarantineReason = "native_materialization_failed"
			entry.Provenance.Integrity.Verified = false
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func NativeSource(id string) (LocalSource, error) {
	known := false
	for _, candidate := range NativeIDs() {
		if id == candidate {
			known = true
		}
	}
	if !known {
		return LocalSource{}, errors.New("source is not in the approved native pack")
	}
	catalog, err := Load()
	if err != nil {
		return LocalSource{}, err
	}
	source, ok := catalog.Source(id)
	if !ok {
		return LocalSource{}, errors.New("native source lacks reviewed classification")
	}
	files, err := fs.Sub(nativeFS, "native/"+id)
	if err != nil {
		return LocalSource{}, err
	}
	local := LocalSource{Source: source, Files: files}
	err = fs.WalkDir(files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			local.Paths = append(local.Paths, path)
		}
		return nil
	})
	return local, err
}

// NativeClassifier reads only the IVOAI-owned overlay in a verified immutable
// object. The upstream SKILL body is never parsed as policy or classification.
type NativeClassifier struct{}

func (NativeClassifier) Classify(ctx context.Context, source supplychain.ResolvedSource, root string) ([]skills.Entry, error) {
	if _, err := NativeSource(source.ID); err != nil {
		return nil, err
	}
	data, err := platform.ReadRegularFile(filepath.Join(root, "IVOAI-CATALOG.json"), maxCatalogBytes)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var catalog Catalog
	if err := decoder.Decode(&catalog); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing native catalog data")
	}
	if err := catalog.Validate(); err != nil {
		return nil, err
	}
	if len(catalog.Sources) != 1 {
		return nil, errors.New("native artifact must contain exactly one source")
	}
	return (Classifier{Catalog: catalog}).Classify(ctx, source, root)
}
