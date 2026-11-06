package session

import (
	"errors"
	"os"
)

var (
	ErrOpenCodeManagedDeprecated = errors.New("OPENCODE_MANAGED_DEPRECATED: managed OpenCode installation, repair and execution were removed; use ivoai codex or another supported native executor; existing binaries, configuration, credentials and history are preserved")
	ErrOpenCodeMigrationRequired = errors.New("OPENCODE_MIGRATION_REQUIRED: retained OpenCode sessions are read-only history in this version; start a new native session with reviewed evidence or explicitly roll back the Core; saved tasks and checkpoints were not changed")
)

// NativeProvidersOnly remains source-compatible; every current execution uses
// native providers. Legacy metadata remains readable for history and rollback.
func NativeProvidersOnly(_ Session) bool { return true }

// CheckNativeProviders is a read-only compatibility gate. It must run before
// provider discovery or execution when reopening a native managed session.
func (s Store) CheckNativeProviders(value Session) error {
	if value.Frontend == "opencode" {
		return ErrOpenCodeMigrationRequired
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
	if value.Frontend == "opencode" {
		return ErrOpenCodeMigrationRequired
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
