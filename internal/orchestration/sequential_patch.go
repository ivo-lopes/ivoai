package orchestration

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

// SequentialPatch is the fallback when a Git worktree cannot be established.
// The official worker remains read-only. Only this owner applies a bounded,
// path-checked text patch. It does not initialize Git, reset a checkout, commit,
// copy source/credential trees, or accept partial/rejected hunks.
type SequentialPatch struct {
	directory string
	lease     chan struct{}
	mu        sync.Mutex
	failed    bool
}

func NewSequentialPatch(directory string) *SequentialPatch {
	return &SequentialPatch{directory: directory, lease: make(chan struct{}, 1)}
}

func (s *SequentialPatch) Acquire(ctx context.Context) (func(), error) {
	select {
	case s.lease <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	s.mu.Lock()
	failed := s.failed
	s.mu.Unlock()
	if failed {
		<-s.lease
		return nil, errors.New("INTEGRATION_CONFLICT: sequential patch scope requires review")
	}
	var once sync.Once
	return func() { once.Do(func() { <-s.lease }) }, nil
}

func (s *SequentialPatch) Apply(ctx context.Context, patch string, allowed []string) error {
	if patch == "" || len(patch) > 1<<20 || strings.ContainsAny(patch, "\x00\x1b") || platform.Redact(patch) != patch {
		return errors.New("WORKTREE_FAILED: empty, sensitive or oversized sequential patch")
	}
	if !strings.HasPrefix(patch, "diff --git ") || !strings.HasSuffix(patch, "\n") {
		return errors.New("WORKTREE_FAILED: return only a complete unified Git diff")
	}
	for _, line := range strings.Split(patch, "\n") {
		for _, prefix := range []string{"GIT binary patch", "Binary files ", "new file mode 120", "new file mode 160", "old mode ", "new mode ", "rename from ", "rename to ", "copy from ", "copy to "} {
			if strings.HasPrefix(line, prefix) {
				return errors.New("WORKTREE_FAILED: sequential fallback accepts regular text files only")
			}
		}
	}
	stat, err := s.gitApply(ctx, patch, "--numstat", "-z")
	if err != nil {
		return err
	}
	count := 0
	for _, item := range strings.Split(stat, "\x00") {
		if item == "" {
			continue
		}
		fields := strings.SplitN(item, "\t", 3)
		if len(fields) != 3 || fields[0] == "-" || fields[1] == "-" || !approvedPath(fields[2], allowed) {
			return errors.New("WORKTREE_FAILED: patch outside approved text-file scope")
		}
		count++
		if count > 128 {
			return errors.New("WORKTREE_FAILED: too many patch paths")
		}
		// Do not follow links into another workspace or provider store.
		path := s.directory
		for _, part := range strings.Split(fields[2], "/") {
			if strings.HasPrefix(part, ".") {
				return errors.New("WORKTREE_FAILED: hidden configuration paths are excluded from sequential fallback")
			}
			path = filepath.Join(path, part)
			info, err := os.Lstat(path)
			if err != nil && !os.IsNotExist(err) {
				return errors.New("WORKTREE_FAILED: patch path unavailable")
			}
			if info != nil && info.Mode()&os.ModeSymlink != 0 {
				return errors.New("WORKTREE_FAILED: symbolic patch path denied")
			}
		}
	}
	if count == 0 {
		return errors.New("WORKTREE_FAILED: patch contains no approved change")
	}
	if _, err := s.gitApply(ctx, patch, "--check", "--whitespace=nowarn"); err != nil {
		return err
	}
	if _, err := s.gitApply(ctx, patch, "--whitespace=nowarn"); err != nil {
		s.mu.Lock()
		s.failed = true
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *SequentialPatch) gitApply(ctx context.Context, patch string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.hooksPath=/dev/null", "apply"}, args...)...)
	cmd.Dir, cmd.Stdin = s.directory, strings.NewReader(patch)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", errors.New("INTEGRATION_CONFLICT: sequential patch did not apply cleanly; no forced resolution")
	}
	return out.String(), nil
}
