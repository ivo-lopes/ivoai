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
	root := filepath.Join(i.Store.Paths.DataDir, "components", "node-"+managedNodeVersion)
	node := filepath.Join(root, "bin", "node")
	npmCLI := filepath.Join(root, "lib", "node_modules", "npm", "bin", "npm-cli.js")
	if regular(node) && regular(npmCLI) {
		return node, npmCLI, nil
	}
	arch, checksum := "x64", "a2e703725d8683be86bb5da967bf8272f4518bdaf10f21389e2b2c9eaeae8c8a"
	if runtime.GOARCH == "arm64" {
		arch, checksum = "arm64", "d415eeea90a2fdb60c66dd386b258acbfc4d1fa4720a8df5dea7369fbdbcddee"
	}
	asset := Asset{URL: "https://nodejs.org/dist/v" + managedNodeVersion + "/node-v" + managedNodeVersion + "-linux-" + arch + ".tar.gz", SHA256: checksum}
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
			if h.Size < 0 || h.Size > 128<<20 {
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
