package session

import (
	"errors"
	"os"
)

var (
	ErrOpenCodeManagedDeprecated = errors.New("OPENCODE_MANAGED_DEPRECATED: OpenCode is unavailable in native Codex/Claude managed sessions; use a supported native executor or an explicit legacy ivoai opencode command")
	ErrOpenCodeMigrationRequired = errors.New("OPENCODE_MIGRATION_REQUIRED: retained OpenCode execution requires an explicit legacy frontend or a new native session with reviewed evidence; saved tasks and checkpoints were not changed")
)

// NativeProvidersOnly distinguishes the explicit legacy presentation path from
// native managed sessions. This is host session metadata, never model input.
func NativeProvidersOnly(value Session) bool { return value.Frontend != "opencode" }

// CheckNativeProviders is a read-only compatibility gate. It must run before
// provider discovery or execution when reopening a native managed session.
func (s Store) CheckNativeProviders(value Session) error {
	if !NativeProvidersOnly(value) {
		return nil
	}
	if err := CheckNativeProviderSnapshot(value, nil); err != nil {
		return err
	}
	checkpoint, err := s.LoadCheckpoint(value.SessionID)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("NATIVE_CHECKPOINT_UNAVAILABLE: retained checkpoint could not be safely inspected")
	}
	return CheckNativeProviderSnapshot(value, &checkpoint)
}

// CheckNativeProviderSnapshot also serves the recovery path that already loaded
// the canonical bounded checkpoint. No stored plan is rerouted or rewritten.
func CheckNativeProviderSnapshot(value Session, checkpoint *Checkpoint) error {
	if !NativeProvidersOnly(value) {
		return nil
	}
	if value.PrimaryExecutor == "opencode" || value.CurrentPrimary == "opencode" {
		return ErrOpenCodeMigrationRequired
	}
	for _, task := range value.Tasks {
		if task.Executor == "opencode" {
			return ErrOpenCodeMigrationRequired
		}
	}
	for _, worker := range value.Workers {
		if worker.Executor == "opencode" {
			return ErrOpenCodeMigrationRequired
		}
	}
	if checkpoint != nil && checkpoint.Recovery != nil {
		for _, task := range checkpoint.Recovery.Plan.Tasks {
			if task.Executor == "opencode" || task.PreferredExecutor == "opencode" || task.Profile.Provider == "opencode" {
				return ErrOpenCodeMigrationRequired
			}
		}
	}
	return nil
}
