package server

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type BackupManifest struct {
	FormatVersion int       `json:"format_version"`
	CreatedAt     time.Time `json:"created_at"`
	Authoritative []string  `json:"authoritative"`
	Rebuildable   []string  `json:"rebuildable"`
}

var backupRoots = []struct {
	archive    string
	selectPath func(Layout) string
}{
	{"config", func(l Layout) string { return l.ConfigDir }},
	{"data/context", func(l Layout) string { return l.ContextDir }},
	{"data/corpus", func(l Layout) string { return l.CorpusDir }},
	{"data/memory", func(l Layout) string { return l.MemoryDir }},
}

// Backup stores authoritative state and non-secret configuration. Qdrant and
// downloaded model data are rebuildable and deliberately excluded.
func Backup(layout Layout, destination string, now time.Time) error {
	if err := layout.Validate(); err != nil {
		return err
	}
	if !filepath.IsAbs(destination) {
		return errors.New("backup destination must be absolute")
	}
	if info, err := os.Lstat(destination); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing symlink backup destination")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), ".ivoai-backup-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	gzipWriter := gzip.NewWriter(temp)
	tarWriter := tar.NewWriter(gzipWriter)
	manifest := BackupManifest{FormatVersion: 1, CreatedAt: now.UTC(), Authoritative: []string{"config (secrets excluded)", "context catalog", "corpus metadata", "ai-memory persistent data"}, Rebuildable: []string{"Qdrant index", "embedding models"}}
	manifestData, _ := json.Marshal(manifest)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o600, Size: int64(len(manifestData)), ModTime: now.UTC(), Typeflag: tar.TypeReg}); err != nil {
		return closeBackup(temp, gzipWriter, tarWriter, err)
	}
	if _, err := tarWriter.Write(manifestData); err != nil {
		return closeBackup(temp, gzipWriter, tarWriter, err)
	}
	for _, root := range backupRoots {
		if err := addTree(tarWriter, root.selectPath(layout), root.archive); err != nil {
			return closeBackup(temp, gzipWriter, tarWriter, err)
		}
	}
	if err := closeBackup(temp, gzipWriter, tarWriter, nil); err != nil {
		return err
	}
	if info, err := os.Lstat(destination); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing symlink backup destination")
	}
	return os.Rename(tempName, destination)
}

func closeBackup(file *os.File, gzipWriter *gzip.Writer, tarWriter *tar.Writer, initial error) error {
	if err := tarWriter.Close(); initial == nil {
		initial = err
	}
	if err := gzipWriter.Close(); initial == nil {
		initial = err
	}
	if err := file.Sync(); initial == nil {
		initial = err
	}
	if err := file.Close(); initial == nil {
		initial = err
	}
	return initial
}

func secretConfigPath(rel string) bool {
	lower := strings.ToLower(filepath.ToSlash(rel))
	base := strings.ToLower(filepath.Base(lower))
	return lower == "secrets" || strings.HasPrefix(lower, "secrets/") || strings.Contains(base, "token") || strings.Contains(base, "credential") || strings.HasSuffix(base, ".key")
}

func restorableConfigPath(rel string) bool {
	clean := filepath.ToSlash(filepath.Clean(rel))
	if clean == "." {
		return true
	}
	switch clean {
	case "server.toml", "gateway.json", "connectors.json":
		return true
	default:
		return false
	}
}

func addTree(writer *tar.Writer, root, archiveRoot string) error {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink backup root %s", root)
	}
	return filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if archiveRoot == "config" && (secretConfigPath(rel) || !restorableConfigPath(rel)) {
			if info.IsDir() && rel != "." {
				return filepath.SkipDir
			}
			if rel != "." {
				return nil
			}
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to back up symlink %s", path)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return nil
		}
		name := archiveRoot
		if rel != "." {
			name += "/" + filepath.ToSlash(rel)
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = name
		header.Uid, header.Gid = 0, 0
		if info.IsDir() {
			header.Mode = 0o700
		} else {
			header.Mode = 0o600
		}
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(writer, file)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		}
		return nil
	})
}

// Restore accepts only ivoai backup paths and regular files/directories. It
// rejects links, traversal, devices, oversized headers, and symlink parents.
func Restore(layout Layout, source string) error {
	if err := layout.Ensure(); err != nil {
		return err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("backup source must be a regular file")
	}
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("read backup compression: %w", err)
	}
	defer gzipReader.Close()
	stage, err := os.MkdirTemp(layout.DataDir, ".restore-stage-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	stageLayout := Layout{
		ConfigDir: filepath.Join(stage, "config"), ContextDir: filepath.Join(stage, "context"),
		CorpusDir: filepath.Join(stage, "corpus"), MemoryDir: filepath.Join(stage, "memory"),
	}
	reader := tar.NewReader(gzipReader)
	seenManifest := false
	firstEntry := true
	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read backup: %w", err)
		}
		if header.Size < 0 || header.Size > 1<<30 {
			return errors.New("backup entry exceeds size limit")
		}
		total += header.Size
		if total > 16<<30 {
			return errors.New("backup exceeds total size limit")
		}
		clean := filepath.Clean(filepath.FromSlash(header.Name))
		if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return errors.New("backup contains unsafe path")
		}
		if clean == "manifest.json" {
			if !firstEntry || seenManifest || header.Typeflag != tar.TypeReg {
				return errors.New("invalid backup manifest")
			}
			var manifest BackupManifest
			if err := json.NewDecoder(io.LimitReader(reader, 1<<20)).Decode(&manifest); err != nil || manifest.FormatVersion != 1 {
				return errors.New("unsupported backup manifest")
			}
			seenManifest = true
			firstEntry = false
			continue
		}
		if firstEntry {
			return errors.New("backup manifest must be the first entry")
		}
		firstEntry = false
		destination, ok := restoreDestination(stageLayout, clean)
		if !ok {
			return fmt.Errorf("backup contains unsupported path %q", header.Name)
		}
		if err := ensureNoSymlinkParents(destination, stageLayout); err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destination, 0o700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := restoreFile(reader, destination, header.Size); err != nil {
				return err
			}
		default:
			return fmt.Errorf("backup contains unsupported entry type for %q", header.Name)
		}
	}
	if !seenManifest {
		return errors.New("backup manifest is missing")
	}
	for _, roots := range []struct{ source, destination string }{
		{stageLayout.ConfigDir, layout.ConfigDir}, {stageLayout.ContextDir, layout.ContextDir},
		{stageLayout.CorpusDir, layout.CorpusDir}, {stageLayout.MemoryDir, layout.MemoryDir},
	} {
		if err := applyRestoredTree(roots.source, roots.destination, layout); err != nil {
			return err
		}
	}
	return nil
}

func applyRestoredTree(source, destination string, layout Layout) error {
	if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := destination
		if rel != "." {
			target = filepath.Join(destination, rel)
		}
		if err := ensureNoSymlinkParents(target, layout); err != nil {
			return err
		}
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !info.Mode().IsRegular() {
			return errors.New("staged restore contains unsupported file")
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		err = restoreFile(file, target, info.Size())
		closeErr := file.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}

func restoreDestination(layout Layout, clean string) (string, bool) {
	mappings := []struct{ prefix, root string }{{"config", layout.ConfigDir}, {"data/context", layout.ContextDir}, {"data/corpus", layout.CorpusDir}, {"data/memory", layout.MemoryDir}}
	for _, mapping := range mappings {
		if clean == mapping.prefix {
			return mapping.root, true
		}
		prefix := mapping.prefix + string(filepath.Separator)
		if strings.HasPrefix(clean, prefix) {
			rel := strings.TrimPrefix(clean, prefix)
			if mapping.prefix == "config" && (secretConfigPath(rel) || !restorableConfigPath(rel)) {
				return "", false
			}
			return filepath.Join(mapping.root, rel), true
		}
	}
	return "", false
}

func ensureNoSymlinkParents(destination string, layout Layout) error {
	allowed := []string{layout.ConfigDir, layout.ContextDir, layout.CorpusDir, layout.MemoryDir}
	var root string
	for _, candidate := range allowed {
		rel, err := filepath.Rel(candidate, destination)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			root = candidate
			break
		}
	}
	if root == "" {
		return errors.New("restore destination escapes managed directories")
	}
	rel, _ := filepath.Rel(root, destination)
	current := root
	parts := strings.Split(rel, string(filepath.Separator))
	for _, part := range parts {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("restore path contains symlink %s", current)
		}
	}
	return nil
}

func restoreFile(reader io.Reader, destination string, size int64) error {
	if info, err := os.Lstat(destination); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to restore over symlink")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), ".restore-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	written, err := io.CopyN(temp, reader, size)
	if err != nil || written != size {
		temp.Close()
		return errors.New("truncated backup entry")
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, destination)
}
