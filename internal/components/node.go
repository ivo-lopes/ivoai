package components

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

func (i *Installer) ensureNode(ctx context.Context) (string, string, error) {
	node := ManagedNodePath(i.Store.Paths.DataDir)
	root := filepath.Dir(filepath.Dir(node))
	npmCLI := filepath.Join(root, "lib", "node_modules", "npm", "bin", "npm-cli.js")
	if regular(node) && regular(npmCLI) {
		return node, npmCLI, nil
	}
	asset := managedNodeAsset()
	archive, err := i.downloadVerified(ctx, asset)
	if err != nil {
		return "", "", err
	}
	defer os.Remove(archive)
	parent := filepath.Dir(root)
	if err := platform.EnsurePrivateDir(parent); err != nil {
		return "", "", err
	}
	temp, err := os.MkdirTemp(parent, ".node-extract-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(temp)
	if err := extractNodeArchive(archive, temp); err != nil {
		return "", "", err
	}
	if !regular(filepath.Join(temp, "bin", "node")) || !regular(filepath.Join(temp, "lib", "node_modules", "npm", "bin", "npm-cli.js")) {
		return "", "", errors.New("Node archive is missing runtime files")
	}
	old := root + ".previous"
	_ = os.RemoveAll(old)
	if _, err := os.Stat(root); err == nil {
		if err := os.Rename(root, old); err != nil {
			return "", "", err
		}
	}
	if err := os.Rename(temp, root); err != nil {
		_ = os.Rename(old, root)
		return "", "", err
	}
	_ = os.RemoveAll(old)
	return node, npmCLI, nil
}

func extractNodeArchive(archive, destination string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var total int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		clean := filepath.Clean(h.Name)
		parts := strings.Split(clean, string(filepath.Separator))
		if len(parts) < 2 || clean == "." || filepath.IsAbs(clean) {
			continue
		}
		rel := filepath.Join(parts[1:]...)
		if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe Node archive path %q", h.Name)
		}
		target := filepath.Join(destination, rel)
		if !strings.HasPrefix(target, destination+string(filepath.Separator)) {
			return fmt.Errorf("Node archive traversal %q", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			// Node 26's reviewed executable exceeds 128 MiB. Keep the
			// larger allowance exclusive to its canonical archive path;
			// auxiliary files and the aggregate extraction budget stay bounded.
			entryLimit := int64(128 << 20)
			if rel == filepath.Join("bin", "node") {
				entryLimit = 256 << 20
			}
			if h.Size < 0 || h.Size > entryLimit {
				return errors.New("invalid Node archive entry size")
			}
			total += h.Size
			if total > 768<<20 {
				return errors.New("Node archive exceeds extraction limit")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(file, tr, h.Size)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			if h.FileInfo().Mode()&0o111 != 0 {
				if err := os.Chmod(target, 0o700); err != nil {
					return err
				}
			}
		}
	}
}
func regular(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// ManagedNodePath resolves the pinned private Node executable used by managed runtimes.
// Callers must still check runtime availability before execution.
func ManagedNodePath(dataDir string) string {
	return filepath.Join(dataDir, "components", "node-"+managedNodeVersion, "bin", "node")
}

func managedNodeAsset() Asset {
	arch, checksum := "x64", "5848553868b402d3fabb5b09270649977780a390f832155721025a08b5104455"
	if runtime.GOARCH == "arm64" {
		arch, checksum = "arm64", "439e9c438acdf6a6ffe47418da709153d66c4bed812e30a473faacb107dd2791"
	}
	return Asset{URL: "https://nodejs.org/dist/v" + managedNodeVersion + "/node-v" + managedNodeVersion + "-linux-" + arch + ".tar.gz", SHA256: checksum}
}
