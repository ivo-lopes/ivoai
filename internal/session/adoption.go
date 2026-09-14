package session

import (
	"encoding/hex"
	"errors"
	"path/filepath"
	"time"
)

type NativeAdoption struct {
	Provider    string    `json:"provider"`
	ThreadID    string    `json:"thread_id"`
	StateScope  string    `json:"state_scope"`
	Directory   string    `json:"directory"`
	ConfirmedAt time.Time `json:"confirmed_at"`
}

type ModeTransition struct {
	From        Mode      `json:"from"`
	To          Mode      `json:"to"`
	ConfirmedAt time.Time `json:"confirmed_at"`
}

func mapsCodexThread(v Session, thread string) bool {
	return v.FrontendSessions["codex"] == thread || (v.Frontend == "codex" && v.FrontendSessionID == thread) || (v.Mode == ModeDirect && v.PrimaryExecutor == "codex" && v.ExecutorSessionID == thread) || (v.NativeAdoption != nil && v.NativeAdoption.ThreadID == thread)
}

// FindCodexNative resolves an exact identity, never the most recent session.
func (s Store) FindCodexNative(thread, cwd string) (Session, error) {
	if !ValidNativeUUID(thread) {
		return Session{}, errors.New("INVALID_NATIVE_SESSION")
	}
	values, err := s.List()
	if err != nil {
		return Session{}, err
	}
	var found Session
	for _, v := range values {
		if !mapsCodexThread(v, thread) {
			continue
		}
		if found.SessionID != "" {
			return Session{}, errors.New("AMBIGUOUS_NATIVE_MAPPING")
		}
		if v.WorkingDirectory != cwd {
			return Session{}, errors.New("NATIVE_SESSION_SCOPE_MISMATCH")
		}
		found = v
	}
	if found.SessionID == "" {
		return Session{}, errors.New("NATIVE_SESSION_NOT_MANAGED")
	}
	return found, nil
}

func validateAdoption(v *NativeAdoption) error {
	if v == nil {
		return nil
	}
	_, err := hex.DecodeString(v.StateScope)
	if v.Provider != "codex" || !ValidNativeUUID(v.ThreadID) || len(v.StateScope) != 64 || err != nil || !filepath.IsAbs(v.Directory) || !safeText(v.Directory, 4096) || v.ConfirmedAt.IsZero() {
		return errors.New("INVALID_NATIVE_ADOPTION")
	}
	return nil
}

// AdoptCodex associates exactly one discovered conversation with the existing
// session domain. Callers must verify native metadata before confirmation.
// Holding the store lock across lookup/create prevents concurrent duplicates.
func (s Store) AdoptCodex(thread, cwd, scope string, confirmed bool) (Session, error) {
	if !confirmed {
		return Session{}, errors.New("SESSION_ADOPTION_REQUIRED")
	}
	adoption := NativeAdoption{Provider: "codex", ThreadID: thread, Directory: cwd, StateScope: scope, ConfirmedAt: time.Now().UTC()}
	if err := validateAdoption(&adoption); err != nil {
		return Session{}, err
	}
	var result Session
	err := s.withLock(func() error {
		values, err := s.List()
		if err != nil {
			return err
		}
		for _, v := range values {
			if !mapsCodexThread(v, thread) {
				continue
			}
			if result.SessionID != "" {
				return errors.New("AMBIGUOUS_NATIVE_MAPPING")
			}
			if v.WorkingDirectory != cwd || (v.NativeStoreScope != "" && v.NativeStoreScope != scope) || (v.NativeAdoption != nil && v.NativeAdoption.StateScope != scope) {
				return errors.New("NATIVE_SESSION_SCOPE_MISMATCH")
			}
			result = v
		}
		if result.SessionID != "" {
			// Idempotent adoption never resets an existing mode, checkpoint,
			// worker state or provider mapping. Mode switching is separate.
			if result.NativeAdoption != nil {
				return nil
			}
			lease, err := s.Acquire(result.SessionID)
			if err != nil {
				return err
			}
			defer lease.Close()
			if nativeSessionLive(result) {
				return errors.New("SESSION_ALREADY_ACTIVE")
			}
			result.NativeAdoption = &adoption
			result.NativeStoreScope = scope
			result.UpdatedAt = time.Now().UTC()
			return s.write(result)
		}
		id, err := NewID()
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		result = Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: ModeAuto, Auto: true, Coordinator: "native", PrimaryExecutor: "codex", InitialPlanner: "codex", CurrentPrimary: "codex", WorkingDirectory: cwd, PrimaryModel: UnknownModel(), State: StateCompleted, FrontendState: StateCompleted, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected", NativeAdoption: &adoption}
		result.SetFrontend("codex", thread)
		result.MaxWorkers = MaxNativeWorkers
		result.NativeStoreScope = scope
		result.SwarmID, result.PrimaryLifecycleID = "native_"+id, "native_"+id+"_"+id
		return s.write(result)
	})
	return result, err
}

func nativeSessionLive(v Session) bool {
	if ProcessMatches(v.PrimaryPID, v.PrimaryProcessStart) || ProcessMatches(v.FrontendPID, v.FrontendProcessStart) {
		return true
	}
	for _, worker := range v.Workers {
		if ProcessMatches(worker.PID, worker.ProcessStart) {
			return true
		}
	}
	return false
}

// SwitchCodexMode changes admission for future turns only. It does not create
// another native thread or replay work. The native ID was verified by caller.
func (s Store) SwitchCodexMode(id, thread, scope string, mode Mode, confirmed bool) (Session, error) {
	if !confirmed {
		return Session{}, errors.New("SESSION_MODE_CONFIRMATION_REQUIRED")
	}
	if mode != ModeAuto && mode != ModeDirect {
		return Session{}, errors.New("INVALID_SESSION_MODE")
	}
	if !ValidNativeUUID(thread) {
		return Session{}, errors.New("INVALID_NATIVE_SESSION")
	}
	if raw, err := hex.DecodeString(scope); err != nil || len(raw) != 32 {
		return Session{}, errors.New("INVALID_NATIVE_STORE_SCOPE")
	}
	lease, err := s.Acquire(id)
	if err != nil {
		return Session{}, err
	}
	defer lease.Close()
	return s.Update(id, func(v *Session) error {
		if v.PrimaryExecutor != "codex" {
			return errors.New("PROVIDER_HANDOFF_REQUIRED")
		}
		if nativeSessionLive(*v) {
			return errors.New("SESSION_ALREADY_ACTIVE")
		}
		mapped := v.FrontendSessions["codex"] == thread || (v.Frontend == "codex" && v.FrontendSessionID == thread) || (v.Mode == ModeDirect && v.ExecutorSessionID == thread)
		if !mapped {
			return errors.New("NATIVE_SESSION_MAPPING_MISMATCH")
		}
		if v.NativeStoreScope != "" && v.NativeStoreScope != scope {
			return errors.New("NATIVE_SESSION_SCOPE_MISMATCH")
		}
		v.NativeStoreScope = scope
		if v.Mode != mode {
			v.ModeTransitions = append(v.ModeTransitions, ModeTransition{From: v.Mode, To: mode, ConfirmedAt: time.Now().UTC()})
			if len(v.ModeTransitions) > 16 {
				v.ModeTransitions = v.ModeTransitions[len(v.ModeTransitions)-16:]
			}
		}
		v.Mode, v.Auto = mode, mode == ModeAuto
		v.SetFrontend("codex", thread)
		v.Coordinator = ""
		if mode == ModeAuto {
			v.Coordinator = "native"
			v.MaxWorkers = MaxNativeWorkers
			v.SwarmID = "native_" + id
			v.PrimaryLifecycleID = "native_" + id + "_" + id
		} else {
			v.MaxWorkers = 3
			v.ExecutorSessionID = thread
		}
		v.State, v.FrontendState = StateCompleted, StateCompleted
		v.CurrentPhase = "mode_selected_waiting_for_input"
		return nil
	})
}

type PortabilityClass string

const (
	SameNative       PortabilityClass = "SAME_NATIVE"
	SameIVOAI        PortabilityClass = "SAME_IVOAI_SESSION"
	ExplicitHandoff  PortabilityClass = "EXPLICIT_HANDOFF"
	ExplicitAdoption PortabilityClass = "EXPLICIT_ADOPTION"
	NotSupported     PortabilityClass = "NOT_SUPPORTED"
)

// ClassifyPortability distinguishes a presentation switch from provider change.
func ClassifyPortability(fromProvider, toProvider, fromFrontend, toFrontend string, managed bool) PortabilityClass {
	valid := func(p string) bool { return p == "codex" || p == "claude" || p == "opencode" }
	if !valid(fromProvider) || !valid(toProvider) {
		return NotSupported
	}
	if fromProvider != toProvider {
		return ExplicitHandoff
	}
	if !managed {
		return ExplicitAdoption
	}
	if fromFrontend != toFrontend {
		return SameIVOAI
	}
	return SameNative
}
