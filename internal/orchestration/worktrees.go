package orchestration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var taskIdentity = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)

// Worktree is operational provenance only. Diffs and file content are never
// included in metadata. Writes are granted separately by the executor sandbox.
type Worktree struct {
	TaskID string `json:"task_id"`
	Branch string `json:"branch"`
	Path   string `json:"path"`
	Base   string `json:"base"`
	Commit string `json:"commit,omitempty"`
}

// Worktrees owns only paths and branches it created. Failure retains evidence;
// it never resets a user's checkout or force-removes an uncommitted worktree.
type Worktrees struct {
	mu                             sync.Mutex
	repository, root, base, prefix string
	owned                          map[string]Worktree
}

func NewWorktrees(ctx context.Context, repository, runtimeDir string) (*Worktrees, error) {
	if !filepath.IsAbs(repository) || !filepath.IsAbs(runtimeDir) {
		return nil, errors.New("WORKTREE_FAILED: absolute repository and private runtime required")
	}
	root, err := gitOutput(ctx, repository, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	if err = cleanTree(ctx, root); err != nil {
		return nil, err
	}
	base, err := gitOutput(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	var nonce [12]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	path, err := os.MkdirTemp(runtimeDir, "worktrees-")
	if err != nil {
		return nil, errors.New("WORKTREE_FAILED: private runtime unavailable")
	}
	return &Worktrees{repository: root, root: path, base: base, prefix: "ivoai/" + hex.EncodeToString(nonce[:]) + "/", owned: map[string]Worktree{}}, nil
}

func (m *Worktrees) Create(ctx context.Context, taskID string) (Worktree, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !taskIdentity.MatchString(taskID) {
		return Worktree{}, errors.New("WORKTREE_FAILED: invalid task identity")
	}
	if _, ok := m.owned[taskID]; ok {
		return Worktree{}, errors.New("WORKTREE_FAILED: task already owns a worktree")
	}
	w := Worktree{TaskID: taskID, Branch: m.prefix + taskID, Path: filepath.Join(m.root, taskID), Base: m.base}
	if _, err := gitOutput(ctx, m.repository, "worktree", "add", "-b", w.Branch, w.Path, w.Base); err != nil {
		return Worktree{}, err
	}
	m.owned[taskID] = w
	return w, nil
}

// Collect commits only changes within the approved path scope. A worker may
// not broaden its own scope, import another branch, or commit unexpected paths.
// Callers must finish/cancel the child before collection.
func (m *Worktrees) Collect(ctx context.Context, taskID string, allowed []string) (Worktree, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.owned[taskID]
	if !ok {
		return Worktree{}, errors.New("WORKTREE_FAILED: unknown owner")
	}
	if w.Commit != "" {
		return w, nil
	}
	head, err := gitOutput(ctx, w.Path, "rev-parse", "HEAD")
	if err != nil {
		return w, err
	}
	if head != w.Base {
		return w, errors.New("WORKTREE_FAILED: worker changed commit history")
	}
	tracked, err := gitOutput(ctx, w.Path, "diff", "--name-only", "-z", "HEAD", "--")
	if err != nil {
		return w, err
	}
	untracked, err := gitOutput(ctx, w.Path, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return w, err
	}
	paths := map[string]bool{}
	for _, list := range []string{tracked, untracked} {
		for _, path := range strings.Split(list, "\x00") {
			if path != "" {
				paths[path] = true
			}
		}
	}
	for path := range paths {
		if !approvedPath(path, allowed) {
			return w, errors.New("WORKTREE_FAILED: changed path outside approved scope")
		}
	}
	if len(paths) == 0 {
		w.Commit = w.Base
		m.owned[taskID] = w
		return w, nil
	}
	names := make([]string, 0, len(paths))
	for path := range paths {
		names = append(names, path)
	}
	sort.Strings(names)
	args := append([]string{"add", "--all", "--"}, names...)
	if _, err = gitOutput(ctx, w.Path, args...); err != nil {
		return w, err
	}
	if _, err = gitOutput(ctx, w.Path, "-c", "commit.gpgsign=false", "commit", "-m", "ivoai: collect task "+taskID); err != nil {
		return w, err
	}
	w.Commit, err = gitOutput(ctx, w.Path, "rev-parse", "HEAD")
	if err == nil {
		m.owned[taskID] = w
	}
	return w, err
}

func approvedPath(path string, allowed []string) bool {
	if path == "" || filepath.IsAbs(path) || strings.ContainsAny(path, "\n\r\x00") || strings.Contains(path, "\\") || filepath.ToSlash(filepath.Clean(path)) != path || path == ".." || strings.HasPrefix(path, "../") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == ".git" {
			return false
		}
	}
	for _, scope := range allowed {
		if scope == "" || scope == "." || filepath.IsAbs(scope) || strings.Contains(scope, "..") {
			continue
		}
		if path == scope || (strings.HasSuffix(scope, "/") && strings.HasPrefix(path, scope)) {
			return true
		}
	}
	return false
}

// Integrate builds the complete result in a separate worktree first. Conflicts
// never affect the primary checkout. The final fast-forward also refuses a
// dirty checkout or a changed primary HEAD.
func (m *Worktrees) Integrate(ctx context.Context, taskIDs []string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := cleanTree(ctx, m.repository); err != nil {
		return "", err
	}
	head, err := gitOutput(ctx, m.repository, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if head != m.base {
		return "", errors.New("INTEGRATION_CONFLICT: primary HEAD changed")
	}
	commits := []string{}
	seen := map[string]bool{}
	for _, id := range taskIDs {
		w, ok := m.owned[id]
		if !ok || w.Commit == "" || seen[id] {
			return "", errors.New("INTEGRATION_CONFLICT: uncollected or duplicate task")
		}
		seen[id] = true
		if w.Commit != w.Base {
			commits = append(commits, w.Commit)
		}
	}
	if len(commits) == 0 {
		return head, nil
	}
	path, err := os.MkdirTemp(m.root, "integration-")
	if err != nil {
		return "", err
	}
	if _, err = gitOutput(ctx, m.repository, "worktree", "add", "--detach", path, m.base); err != nil {
		return "", err
	}
	args := append([]string{"-c", "commit.gpgsign=false", "cherry-pick"}, commits...)
	if _, err = gitOutput(ctx, path, args...); err != nil {
		// Retain the conflicted integration tree and all worker branches. There
		// is no automatic conflict resolution or destructive rollback.
		return "", errors.New("INTEGRATION_CONFLICT: isolated integration requires review; worker evidence retained")
	}
	integrated, err := gitOutput(ctx, path, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if err = cleanTree(ctx, m.repository); err != nil {
		return "", err
	}
	head, err = gitOutput(ctx, m.repository, "rev-parse", "HEAD")
	if err != nil || head != m.base {
		return "", errors.New("INTEGRATION_CONFLICT: primary changed during integration")
	}
	if _, err = gitOutput(ctx, m.repository, "merge", "--ff-only", "--no-edit", integrated); err != nil {
		return "", err
	}
	if _, err = gitOutput(ctx, m.repository, "worktree", "remove", path); err != nil {
		return integrated, err
	}
	return integrated, nil
}

func (m *Worktrees) Cleanup(ctx context.Context, taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.owned[taskID]
	if !ok {
		return errors.New("WORKTREE_FAILED: unknown owner")
	}
	if w.Commit == "" {
		return errors.New("WORKTREE_FAILED: uncollected work retained")
	}
	if err := cleanTree(ctx, w.Path); err != nil {
		return err
	}
	if _, err := gitOutput(ctx, m.repository, "worktree", "remove", w.Path); err != nil {
		return err
	}
	// Keep the branch as a recovery reference. Branch garbage collection is not
	// a reason to risk deleting unintegrated worker evidence.
	delete(m.owned, taskID)
	return nil
}

func cleanTree(ctx context.Context, dir string) error {
	status, err := gitOutput(ctx, dir, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return err
	}
	if status != "" {
		return errors.New("WORKTREE_FAILED: checkout has uncommitted changes")
	}
	return nil
}

type gitBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *gitBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := (1 << 20) - b.Len()
	if len(p) > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.hooksPath=/dev/null"}, args...)...)
	cmd.Dir = dir
	// Do not let inherited GIT_DIR/GIT_WORK_TREE redirect an owned operation.
	for _, v := range os.Environ() {
		k, _, _ := strings.Cut(v, "=")
		if !strings.HasPrefix(k, "GIT_") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	var out gitBuffer
	cmd.Stdout = &out
	// Git errors can contain filenames, URLs or credential helpers. Return only
	// a fixed failure class, never raw stderr in public metadata.
	if err := cmd.Run(); err != nil {
		return "", errors.New("WORKTREE_FAILED: git operation failed")
	}
	if out.overflow {
		return "", errors.New("WORKTREE_FAILED: bounded git response exceeded")
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}
