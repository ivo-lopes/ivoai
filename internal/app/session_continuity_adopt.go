package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/session"
)

// ScopeAdoption is metadata only. Confirmation explicitly authorizes retaining
// the conversation's existing provider history under the displayed new scope.
type ScopeAdoption struct {
	SessionID        string   `json:"session_id"`
	PreviousScope    string   `json:"previous_scope"`
	ProposedScope    string   `json:"proposed_scope"`
	Provider         string   `json:"provider"`
	WorkingDirectory string   `json:"working_directory"`
	BackupPath       string   `json:"backup_path,omitempty"`
	SelectedSources  []string `json:"selected_sources,omitempty"`
	Applied          bool     `json:"applied"`
}

// A nil expected value authorizes the current selection (CLI --confirm).
// Interactive callers must supply the exact preview shown before confirmation.
func (result ScopeAdoption) matchesPreview(expected *ScopeAdoption) bool {
	return expected == nil || result.SessionID == expected.SessionID &&
		result.PreviousScope == expected.PreviousScope && result.ProposedScope == expected.ProposedScope &&
		result.Provider == expected.Provider && result.WorkingDirectory == expected.WorkingDirectory &&
		slices.Equal(result.SelectedSources, expected.SelectedSources)
}

func (a *App) SessionAdoptScope(ctx context.Context, id string, selectors []string, confirmed bool, expected *ScopeAdoption) (ScopeAdoption, error) {
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	lease, err := store.Acquire(id)
	if err != nil {
		return ScopeAdoption{}, err
	}
	defer lease.Close()
	v, err := store.Get(id)
	if err != nil {
		return ScopeAdoption{}, err
	}
	if v.Mode != session.ModeAuto || v.Coordinator != "native" || !v.Resumable() {
		return ScopeAdoption{}, errors.New("SCOPE_ADOPTION_REQUIRES_INACTIVE_MANAGED_CONVERSATION")
	}
	cfg, err := a.Store.Load()
	if err != nil {
		return ScopeAdoption{}, err
	}
	scope, err := conversationScopeID(v.WorkingDirectory, cfg, selectors)
	if err != nil {
		return ScopeAdoption{}, err
	}
	result := ScopeAdoption{SessionID: id, PreviousScope: v.KnowledgeScopeID, ProposedScope: scope, Provider: v.PrimaryExecutor, WorkingDirectory: v.WorkingDirectory}
	selected, err := conversationScopeSelection(cfg, selectors)
	if err != nil {
		return result, err
	}
	for _, group := range selected.Groups {
		for _, p := range group.Profiles {
			result.SelectedSources = append(result.SelectedSources, p.Alias+" ("+p.ID+")")
		}
	}
	for name, p := range cfg.MCP.Servers {
		if p.Enabled && (p.Kind == "memory" || p.Kind == "context") {
			result.SelectedSources = append(result.SelectedSources, "legacy:"+name)
		}
	}
	sort.Strings(result.SelectedSources)

	if !confirmed {
		return result, nil
	}
	if !result.matchesPreview(expected) {
		return result, errors.New("SCOPE_ADOPTION_PREVIEW_CHANGED")
	}
	if err := a.verifyScopeAccount(ctx, v); err != nil {
		return result, err
	}
	if v.KnowledgeScopeID == scope {
		result.Applied = true
		return result, nil
	}
	backup, err := saveScopeBackup(store.Root, v)
	if err != nil {
		return result, err
	}
	_, err = store.Update(id, func(current *session.Session) error {
		if current.KnowledgeScopeID != v.KnowledgeScopeID || current.ExecutorSessionID != v.ExecutorSessionID || !current.UpdatedAt.Equal(v.UpdatedAt) {
			return errors.New("SCOPE_ADOPTION_SESSION_CHANGED")
		}
		current.KnowledgeScopeID = scope
		return nil
	})
	result.BackupPath = backup
	result.Applied = err == nil
	return result, err
}

func saveScopeBackup(root string, v session.Session) (string, error) {
	directory := filepath.Join(root, "scope-backups")
	if err := platform.EnsurePrivateDir(directory); err != nil {
		return "", err
	}
	body, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	path := filepath.Join(directory, fmt.Sprintf("%s-%x.json", v.SessionID, sum[:16]))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		info, statErr := os.Lstat(path)
		if statErr != nil || info.Mode().Perm() != 0600 {
			return "", errors.New("UNSAFE_SCOPE_BACKUP")
		}
		previous, readErr := platform.ReadRegularFile(path, int64(len(body)+1))
		if readErr != nil || string(previous) != string(body) {
			return "", errors.New("UNSAFE_SCOPE_BACKUP")
		}
		return path, nil
	}
	if err != nil {
		return "", err
	}
	if _, err = file.Write(body); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	return path, nil
}

func (a *App) verifyScopeAccount(ctx context.Context, v session.Session) error {
	var err error
	if v.PrimaryExecutor != "codex" && v.PrimaryExecutor != "claude" {
		return errors.New("SCOPE_ADOPTION_PROVIDER_UNSUPPORTED")
	}
	var account string
	if a.ProviderAccountReference != nil {
		account, err = a.ProviderAccountReference(ctx, v.PrimaryExecutor)
	} else {
		state, loadErr := a.Store.LoadState()
		if loadErr != nil {
			return loadErr
		}
		account, err = quota.AccountReference(ctx, state.Components[string(providerComponent(v.PrimaryExecutor))].Path, v.PrimaryExecutor)
	}
	if err != nil || account == "" {
		return errors.New("SCOPE_ADOPTION_ACCOUNT_UNVERIFIED")
	}
	proven := false
	for _, mapping := range v.ExecutorSessions {
		if mapping.Executor != v.PrimaryExecutor {
			continue
		}
		if mapping.AuthReference != account {
			return errors.New("SCOPE_ADOPTION_ACCOUNT_MISMATCH")
		}
		if mapping.ExecutorSessionID != "" && mapping.ExecutorSessionID == v.ExecutorSessionID {
			proven = true
		}
	}
	if !proven {
		return errors.New("SCOPE_ADOPTION_NATIVE_MAPPING_UNVERIFIED")
	}
	return nil
}

// SessionRestoreScope restores only the organization binding from an immutable
// private backup, never old process IDs, grants, decisions or provider state.
func (a *App) SessionRestoreScope(ctx context.Context, id, backup string, confirmed bool, expected *ScopeAdoption) (ScopeAdoption, error) {
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	lease, err := store.Acquire(id)
	if err != nil {
		return ScopeAdoption{}, err
	}
	defer lease.Close()
	v, err := store.Get(id)
	if err != nil {
		return ScopeAdoption{}, err
	}
	if v.Mode != session.ModeAuto || v.Coordinator != "native" || !v.Resumable() {
		return ScopeAdoption{}, errors.New("SCOPE_ADOPTION_REQUIRES_INACTIVE_MANAGED_CONVERSATION")
	}
	directory := filepath.Join(store.Root, "scope-backups")
	if filepath.Dir(filepath.Clean(backup)) != directory {
		return ScopeAdoption{}, errors.New("INVALID_SCOPE_BACKUP")
	}
	if err := platform.EnsurePrivateDir(directory); err != nil {
		return ScopeAdoption{}, err
	}
	body, err := platform.ReadRegularFile(backup, 4<<20)
	if err != nil {
		return ScopeAdoption{}, err
	}
	info, err := os.Lstat(backup)
	if err != nil || info.Mode().Perm() != 0600 {
		return ScopeAdoption{}, errors.New("UNSAFE_SCOPE_BACKUP")
	}
	sum := sha256.Sum256(body)
	if filepath.Base(backup) != fmt.Sprintf("%s-%x.json", id, sum[:16]) {
		return ScopeAdoption{}, errors.New("INVALID_SCOPE_BACKUP_DIGEST")
	}
	var previous session.Session
	if json.Unmarshal(body, &previous) != nil || previous.SessionID != id || previous.WorkingDirectory != v.WorkingDirectory || previous.PrimaryExecutor != v.PrimaryExecutor || previous.ExecutorSessionID != v.ExecutorSessionID {
		return ScopeAdoption{}, errors.New("SCOPE_BACKUP_IDENTITY_MISMATCH")
	}
	if !safeStoredConversationScope(previous.KnowledgeScopeID) {
		return ScopeAdoption{}, errors.New("INVALID_SCOPE_BACKUP_SCOPE")
	}
	result := ScopeAdoption{SessionID: id, PreviousScope: v.KnowledgeScopeID, ProposedScope: previous.KnowledgeScopeID, Provider: v.PrimaryExecutor, WorkingDirectory: v.WorkingDirectory}
	if !confirmed {
		return result, nil
	}
	if !result.matchesPreview(expected) {
		return result, errors.New("SCOPE_ADOPTION_PREVIEW_CHANGED")
	}
	if err := a.verifyScopeAccount(ctx, v); err != nil {
		return result, err
	}
	if err := a.verifyScopeAccount(ctx, previous); err != nil {
		return result, err
	}
	result.BackupPath, err = saveScopeBackup(store.Root, v)
	if err != nil {
		return result, err
	}
	_, err = store.Update(id, func(current *session.Session) error {
		if current.KnowledgeScopeID != v.KnowledgeScopeID || current.ExecutorSessionID != v.ExecutorSessionID || !current.UpdatedAt.Equal(v.UpdatedAt) {
			return errors.New("SCOPE_ADOPTION_SESSION_CHANGED")
		}
		current.KnowledgeScopeID = previous.KnowledgeScopeID
		return nil
	})
	result.Applied = err == nil
	return result, err
}

// Scope IDs are opaque bounded identifiers, including the empty legacy value.
// Validate before preview; a digest proves bytes, not safe display content.
func safeStoredConversationScope(value string) bool {
	if len(value) > 96 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
