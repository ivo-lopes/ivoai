// Package codeintel implements bounded, scoped structural code intelligence.
package codeintel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const MaxFileBytes = 1_000_000
const MaxSourceBytes = 128 * 1024 * 1024
const MaxFiles = 20000

type Source struct {
	Path string
	Body []byte
}
type SourceSnapshot struct {
	Digest string
	Files  []Source
}

// Identity binds caches to the exact worktree and the host-authorized knowledge
// and permission scopes. It never infers an organization from repository text.
func Identity(root, scope, permissions string) (string, error) {
	if !filepath.IsAbs(root) || scope == "" || permissions == "" || len(scope) > 4096 || len(permissions) > 4096 {
		return "", errors.New("missing trusted code scope")
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(canonical)
	if err != nil || !st.IsDir() {
		return "", errors.New("invalid repository root")
	}
	body, _ := json.Marshal([]string{"ivoai-code-v1", canonical, scope, permissions})
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:]), nil
}

// Snapshot reads bounded exact bytes through os.Root, which prevents filesystem
// traversal even if a path is exchanged concurrently. No repository code runs.
// Only host-enumerated files belong here; secrets and project configuration must
// be excluded by the caller's inventory policy.
func Snapshot(root string, files []string) (SourceSnapshot, error) {
	var result SourceSnapshot
	if len(files) > MaxFiles {
		return result, errors.New("code inventory exceeds file budget")
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return result, err
	}
	defer dir.Close()
	names := slices.Clone(files)
	slices.Sort(names)
	hash := sha256.New()
	total := 0
	previous := ""
	for _, name := range names {
		if name == "" || name == previous || !filepath.IsLocal(name) || filepath.Clean(name) != name || strings.ContainsAny(name, "\x00\r\n") {
			return SourceSnapshot{}, errors.New("invalid code source path")
		}
		previous = name
		info, err := dir.Lstat(name)
		if err != nil || !info.Mode().IsRegular() || info.Size() > MaxFileBytes {
			return SourceSnapshot{}, errors.New("code source is not a bounded regular file")
		}
		file, err := dir.Open(name)
		if err != nil {
			return SourceSnapshot{}, errors.New("code source escaped scope or disappeared")
		}
		actual, statErr := file.Stat()
		if statErr != nil || !actual.Mode().IsRegular() {
			file.Close()
			return SourceSnapshot{}, errors.New("code source changed type")
		}
		body, readErr := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
		file.Close()
		if readErr != nil || len(body) > MaxFileBytes {
			return SourceSnapshot{}, errors.New("code source exceeds budget")
		}
		total += len(body)
		if total > MaxSourceBytes {
			return SourceSnapshot{}, errors.New("code inventory exceeds byte budget")
		}
		sum := sha256.Sum256(body)
		hash.Write([]byte(name))
		hash.Write([]byte{0})
		hash.Write(sum[:])
		result.Files = append(result.Files, Source{Path: name, Body: body})
	}
	result.Digest = hex.EncodeToString(hash.Sum(nil))
	return result, nil
}
