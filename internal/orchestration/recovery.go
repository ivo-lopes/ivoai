package orchestration

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func RecoveryRepositoryIdentity(ctx context.Context, directory string) (string, string, error) {
	root, err := gitOutput(ctx, directory, "rev-parse", "--path-format=absolute", "--git-common-dir")
	head := ""
	if err != nil {
		root = directory
	} else {
		head, err = gitOutput(ctx, directory, "rev-parse", "HEAD")
		if err != nil {
			return "", "", err
		}
	}
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(physical)
	if err != nil {
		return "", "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", "", errors.New("REPOSITORY_IDENTITY_UNAVAILABLE")
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", physical, stat.Dev, stat.Ino)))
	return fmt.Sprintf("repo_%x", digest[:]), head, nil
}

// RestoreWorktrees adopts only verified, collected work owned by this session.
// It performs no checkout/reset/merge and refuses all ambiguous modifications.
func RestoreWorktrees(ctx context.Context, repository, allowedRoot, base string, owned []Worktree) (*Worktrees, error) {
	if len(owned) == 0 {
		return nil, nil
	}
	root, err := gitOutput(ctx, repository, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	head, err := gitOutput(ctx, root, "rev-parse", "HEAD")
	if err != nil || head != base {
		return nil, errors.New("RECOVERY_REPOSITORY_CHANGED")
	}
	if err := cleanTree(ctx, root); err != nil {
		return nil, err
	}
	physical, err := filepath.EvalSymlinks(repository)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, physical)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, errors.New("RECOVERY_SCOPE_MISMATCH")
	}
	workRoot := filepath.Dir(owned[0].Path)
	if filepath.Dir(workRoot) != filepath.Clean(allowedRoot) || !strings.HasPrefix(filepath.Base(workRoot), "worktrees-") {
		return nil, errors.New("RECOVERY_WORKTREE_NOT_OWNED")
	}
	manager := &Worktrees{repository: root, root: workRoot, base: base, workingSubdirectory: rel, owned: map[string]Worktree{}}
	identity, _, err := RecoveryRepositoryIdentity(ctx, root)
	if err != nil {
		return nil, err
	}
	for _, w := range owned {
		if !taskIdentity.MatchString(w.TaskID) || filepath.Dir(w.Path) != workRoot || filepath.Base(w.Path) != w.TaskID || w.Commit == "" || !strings.HasPrefix(w.Branch, "ivoai/") {
			return nil, errors.New("AMBIGUOUS_PREVIOUS_EXECUTION")
		}
		prefix := strings.TrimSuffix(w.Branch, w.TaskID)
		if prefix == w.Branch || manager.prefix != "" && manager.prefix != prefix {
			return nil, errors.New("RECOVERY_WORKTREE_NOT_OWNED")
		}
		manager.prefix = prefix
		physical, err := filepath.EvalSymlinks(w.Path)
		if err != nil || physical != filepath.Clean(w.Path) {
			return nil, errors.New("RECOVERY_WORKTREE_UNSAFE")
		}
		branch, err := gitOutput(ctx, w.Path, "symbolic-ref", "--short", "HEAD")
		if err != nil || branch != w.Branch {
			return nil, errors.New("RECOVERY_BRANCH_CHANGED")
		}
		commit, err := gitOutput(ctx, w.Path, "rev-parse", "HEAD")
		if err != nil || commit != w.Commit {
			return nil, errors.New("RECOVERY_COMMIT_CHANGED")
		}
		if err := cleanTree(ctx, w.Path); err != nil {
			return nil, err
		}
		workIdentity, _, err := RecoveryRepositoryIdentity(ctx, w.Path)
		if err != nil || workIdentity != identity {
			return nil, errors.New("RECOVERY_WORKTREE_REPOSITORY_MISMATCH")
		}
		manager.owned[w.TaskID] = w
	}
	return manager, nil
}
