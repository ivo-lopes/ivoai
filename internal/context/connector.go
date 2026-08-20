package context

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const (
	defaultMaxDocumentBytes      int64 = 8 << 20
	defaultMaxConnectorBytes     int64 = 256 << 20
	defaultMaxConnectorDocuments       = 10_000
)

// Connector enumerates normalized documents. Implementations must treat source
// content as untrusted and must not follow symlinks.
type Connector interface {
	Name() string
	Documents(context.Context) ([]Document, error)
}

// FilesystemConnector ingests regular text files below a fixed root.
type FilesystemConnector struct {
	ConnectorName string
	Root          string
	MaxBytes      int64
	MaxTotalBytes int64
	MaxDocuments  int
}

func (c FilesystemConnector) Name() string {
	if c.ConnectorName != "" {
		return c.ConnectorName
	}
	return "filesystem"
}

func (c FilesystemConnector) Documents(ctx context.Context) ([]Document, error) {
	root, err := filepath.Abs(c.Root)
	if err != nil {
		return nil, err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect connector root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, errors.New("connector root is not a directory")
	}
	rootFile, err := openConnectorRoot(root)
	if err != nil {
		return nil, err
	}
	defer rootFile.Close()
	limit := c.MaxBytes
	if limit <= 0 {
		limit = defaultMaxDocumentBytes
	}
	var docs []Document
	var totalBytes int64
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if rel != "." && !SafeDocumentPath(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || !SafeDocumentPath(rel) {
			return nil
		}
		data, info, err := readRegularAt(rootFile, rel, limit)
		if err != nil {
			// A connector source can change while it is being scanned. Unsafe
			// replacements are skipped and can never escape the opened root.
			return nil
		}
		if !LooksTextual(data) {
			return nil
		}
		totalLimit := c.MaxTotalBytes
		if totalLimit <= 0 {
			totalLimit = defaultMaxConnectorBytes
		}
		documentLimit := c.MaxDocuments
		if documentLimit <= 0 {
			documentLimit = defaultMaxConnectorDocuments
		}
		totalBytes += int64(len(data))
		if totalBytes > totalLimit || len(docs) >= documentLimit {
			return errors.New("connector corpus exceeds document or byte quota")
		}
		content := strings.ToValidUTF8(string(data), "�")
		docs = append(docs, Document{
			ID: stableID(c.Name(), filepath.ToSlash(rel)), Source: c.Name(), Path: filepath.ToSlash(rel),
			Title: filepath.Base(rel), Content: content, ModifiedAt: info.ModTime().UTC(),
			IngestedAt: time.Now().UTC(), Metadata: map[string]string{"connector": c.Name()},
		})
		return nil
	})
	return docs, err
}

// GitConnector uses git ls-files to ingest only tracked files, then delegates
// content safety to the filesystem connector rules. User input is always argv.
type GitConnector struct {
	ConnectorName string
	Repository    string
	MaxBytes      int64
	GitBinary     string
	MaxTotalBytes int64
	MaxDocuments  int
}

func (c GitConnector) Name() string {
	if c.ConnectorName != "" {
		return c.ConnectorName
	}
	return "git"
}

func (c GitConnector) Documents(ctx context.Context) ([]Document, error) {
	repo, err := filepath.Abs(c.Repository)
	if err != nil {
		return nil, err
	}
	repoInfo, err := os.Lstat(repo)
	if err != nil || repoInfo.Mode()&os.ModeSymlink != 0 || !repoInfo.IsDir() {
		return nil, errors.New("git connector root must be a non-symlink directory")
	}
	rootFile, err := openConnectorRoot(repo)
	if err != nil {
		return nil, err
	}
	defer rootFile.Close()
	git := c.GitBinary
	if git == "" {
		git = "git"
	}
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// A repository is untrusted input. In particular, Git may execute an
	// arbitrary core.fsmonitor program while answering ls-files. Override every
	// execution-capable repository setting and discard inherited Git controls.
	cmd := exec.CommandContext(cmdCtx, git,
		"--no-optional-locks",
		"-c", "core.fsmonitor=false",
		"-c", "core.hooksPath=/dev/null",
		"-c", "credential.helper=",
		"-C", repo, "ls-files", "-z", "--",
	)
	cmd.Env = sanitizedGitEnvironment()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("enumerate tracked files: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("enumerate tracked files: %w", err)
	}
	const maxGitFileListBytes = 16 << 20
	out, readErr := io.ReadAll(io.LimitReader(stdout, maxGitFileListBytes+1))
	if len(out) > maxGitFileListBytes {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, errors.New("git tracked-file list exceeds safety quota")
	}
	waitErr := cmd.Wait()
	if readErr != nil || waitErr != nil {
		return nil, fmt.Errorf("enumerate tracked files: %w", errors.Join(readErr, waitErr))
	}
	limit := c.MaxBytes
	if limit <= 0 {
		limit = defaultMaxDocumentBytes
	}
	now := time.Now().UTC()
	var docs []Document
	var totalBytes int64
	for _, raw := range strings.Split(string(out), "\x00") {
		if raw == "" || !SafeDocumentPath(raw) {
			continue
		}
		data, info, err := readRegularAt(rootFile, filepath.FromSlash(raw), limit)
		if err != nil {
			continue
		}
		if !LooksTextual(data) {
			continue
		}
		totalLimit := c.MaxTotalBytes
		if totalLimit <= 0 {
			totalLimit = defaultMaxConnectorBytes
		}
		documentLimit := c.MaxDocuments
		if documentLimit <= 0 {
			documentLimit = defaultMaxConnectorDocuments
		}
		totalBytes += int64(len(data))
		if totalBytes > totalLimit || len(docs) >= documentLimit {
			return nil, errors.New("connector corpus exceeds document or byte quota")
		}
		docs = append(docs, Document{
			ID: stableID(c.Name(), filepath.ToSlash(raw)), Source: c.Name(), Path: filepath.ToSlash(raw),
			Title: filepath.Base(raw), Content: strings.ToValidUTF8(string(data), "�"),
			ModifiedAt: info.ModTime().UTC(), IngestedAt: now,
			Metadata: map[string]string{"connector": c.Name(), "repository": filepath.Base(repo)},
		})
	}
	return docs, nil
}

func sanitizedGitEnvironment() []string {
	environment := make([]string, 0, len(os.Environ())+4)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
	)
}

func openConnectorRoot(root string) (*os.File, error) {
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open connector root securely: %w", err)
	}
	file := os.NewFile(uintptr(fd), root)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open connector root securely")
	}
	info, err := file.Stat()
	if err != nil || !info.IsDir() {
		file.Close()
		return nil, errors.New("connector root is not a directory")
	}
	return file, nil
}

// readRegularAt traverses every path component relative to an already opened
// root with O_NOFOLLOW. This closes both final-file and intermediate-directory
// symlink races while enforcing the size limit on the opened descriptor.
func readRegularAt(root *os.File, relative string, limit int64) ([]byte, os.FileInfo, error) {
	clean := filepath.Clean(relative)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, nil, errors.New("unsafe connector path")
	}
	parts := strings.Split(clean, string(filepath.Separator))
	current := int(root.Fd())
	opened := make([]int, 0, len(parts))
	defer func() {
		for index := len(opened) - 1; index >= 0; index-- {
			_ = unix.Close(opened[index])
		}
	}()
	for index, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, nil, errors.New("unsafe connector path component")
		}
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
		if index != len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		fd, err := unix.Openat(current, part, flags, 0)
		if err != nil {
			return nil, nil, err
		}
		opened = append(opened, fd)
		current = fd
	}
	file := os.NewFile(uintptr(current), clean)
	if file == nil {
		return nil, nil, errors.New("open connector document")
	}
	// Transfer ownership of the final descriptor to os.File; the descriptor
	// cleanup above retains ownership only of intermediate directories.
	opened = opened[:len(opened)-1]
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, nil, errors.New("connector document is not a permitted regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) > limit {
		return nil, nil, errors.New("connector document exceeds size limit")
	}
	return data, info, nil
}

func stableID(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:16])
}
