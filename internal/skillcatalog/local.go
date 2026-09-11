package skillcatalog

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"reflect"
	"sort"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

// LocalSource presents a release-owned, declarative snapshot through the same
// supply-chain interfaces as network discovery. Files are explicitly reviewed;
// no directory walk, executable, hook, or network lookup happens here.
type LocalSource struct {
	Source Source
	Files  fs.FS
	Paths  []string
}

func (s LocalSource) archive(ctx context.Context) ([]byte, error) {
	if s.Files == nil || len(s.Paths) == 0 || len(s.Paths) > 256 {
		return nil, errors.New("invalid local capability file inventory")
	}
	paths := append([]string(nil), s.Paths...)
	sort.Strings(paths)
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tw := tar.NewWriter(gz)
	total := 0
	for i, name := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !fs.ValidPath(name) || name == "." || i > 0 && name == paths[i-1] {
			return nil, errors.New("unsafe or duplicate local capability path")
		}
		base := name[strings.LastIndex(name, "/")+1:]
		if !strings.HasSuffix(name, ".md") && base != "LICENSE" && base != "NOTICE" && base != "LICENSE.txt" {
			return nil, errors.New("local capability contains non-declarative content")
		}
		for parent := name; strings.Contains(parent, "/"); {
			parent = parent[:strings.LastIndex(parent, "/")]
			info, err := fs.Lstat(s.Files, parent)
			if err != nil {
				return nil, err
			}
			if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
				return nil, errors.New("unsafe local capability parent")
			}
		}
		info, err := fs.Lstat(s.Files, name)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode()&0111 != 0 || info.Size() > maxSkillDocument {
			return nil, errors.New("invalid local capability file type or size")
		}
		file, err := s.Files.Open(name)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(file, maxSkillDocument+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		total += len(body)
		if len(body) > maxSkillDocument || total > 8<<20 {
			return nil, errors.New("local capability exceeds bounded size")
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(body); err != nil {
			return nil, err
		}
	}
	// Preserve the reviewed overlay with the immutable object so rollback
	// does not reinterpret revision A using a future revision B classifier.
	metadata, err := json.Marshal(Catalog{Schema: SchemaVersion, Sources: []Source{s.Source}})
	if err != nil {
		return nil, err
	}
	if len(metadata) > maxCatalogBytes {
		return nil, errors.New("local catalog metadata exceeds limit")
	}
	if err := tw.WriteHeader(&tar.Header{Name: "IVOAI-CATALOG.json", Mode: 0600, Size: int64(len(metadata)), Typeflag: tar.TypeReg}); err != nil {
		return nil, err
	}
	if _, err := tw.Write(metadata); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func (s LocalSource) Resolve(ctx context.Context, ref supplychain.Reference) (supplychain.ResolvedSource, error) {
	if ref.ID != s.Source.ID || ref.Source != s.Source.Upstream.Repository || ref.Kind != supplychain.KindSkill || ref.Version != "" && ref.Version != s.Source.Provenance.Revision {
		return supplychain.ResolvedSource{}, errors.New("local capability reference mismatch")
	}
	archive, err := s.archive(ctx)
	if err != nil {
		return supplychain.ResolvedSource{}, err
	}
	digest := sha256.Sum256(archive)
	source := supplychain.ResolvedSource{ID: s.Source.ID, Kind: supplychain.KindSkill, Source: s.Source.Upstream.Repository, Revision: s.Source.Provenance.Revision, LogicalVersion: s.Source.Provenance.Revision, DefaultBranch: s.Source.Upstream.DefaultBranch, License: s.Source.Upstream.License, Integrity: supplychain.Integrity{Algorithm: "sha256", Digest: hex.EncodeToString(digest[:]), SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "commit_pinned_local_digest"}}
	return source, source.Validate()
}

func (s LocalSource) Fetch(ctx context.Context, requested supplychain.ResolvedSource) (io.ReadCloser, error) {
	resolved, err := s.Resolve(ctx, supplychain.Reference{ID: requested.ID, Kind: requested.Kind, Source: requested.Source, Version: requested.Revision})
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(resolved, requested) {
		return nil, errors.New("local capability identity mismatch")
	}
	archive, err := s.archive(ctx)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(archive)
	if hex.EncodeToString(digest[:]) != requested.Integrity.Digest {
		return nil, errors.New("local capability changed during fetch")
	}
	return io.NopCloser(bytes.NewReader(archive)), nil
}
