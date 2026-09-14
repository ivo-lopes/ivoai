package app

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ivo-lopes/ivoai/internal/session"
)

type resumeSessionKey struct{}

// One frontend may visit several native conversations, but only one logical
// session owns its primary at a time. Call Select only while no turn is active.
type conversationBinding struct {
	mu            sync.Mutex
	active        atomic.Value
	store         session.Store
	frontend, cwd string
	lease         *session.Lease
	visited       []string
	initial       session.Session
}

func newConversationBinding(store session.Store, value session.Session, lease *session.Lease) *conversationBinding {
	b := &conversationBinding{store: store, frontend: value.Frontend, cwd: value.WorkingDirectory, lease: lease, initial: value, visited: []string{value.SessionID}}
	b.active.Store(value.SessionID)
	return b
}

func (b *conversationBinding) ID() string { return b.active.Load().(string) }

func (b *conversationBinding) Available(thread string) bool {
	v, err := b.store.FindFrontend(b.frontend, thread, b.cwd)
	return err == nil && v.Mode == session.ModeAuto && v.Coordinator == "native"
}

func (b *conversationBinding) Select(thread string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	current, err := b.store.Get(b.ID())
	if err != nil {
		return err
	}
	if current.FrontendSessionID == thread {
		return nil
	}
	target, err := b.store.FindFrontend(b.frontend, thread, b.cwd)
	if err != nil && err.Error() != "NATIVE_SESSION_NOT_MANAGED" {
		return err
	}
	if err != nil && current.FrontendSessionID == "" {
		_, err := b.store.Update(current.SessionID, func(v *session.Session) error { v.SetFrontend(b.frontend, thread); return nil })
		return err
	}
	if err != nil {
		if err.Error() != "NATIVE_SESSION_NOT_MANAGED" {
			return err
		}
		id, err := session.NewID()
		if err != nil {
			return err
		}
		target = session.Session{SessionID: id, Frontend: b.frontend, FrontendSessionID: thread,
			Mode: session.ModeAuto, Auto: true, Coordinator: "native", State: session.StateStarting,
			PrimaryExecutor: b.initial.PrimaryExecutor, InitialPlanner: b.initial.InitialPlanner, CurrentPrimary: b.initial.CurrentPrimary,
			WorkingDirectory: b.cwd, PrimaryModel: session.UnknownModel(), MaxWorkers: b.initial.MaxWorkers,
			ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected"}
		target.SwarmID, target.PrimaryLifecycleID = "native_"+id, "native_"+id+"_"+id
		target.NativeStoreScope = b.initial.NativeStoreScope
		target.Observability = nil
		target.StartedAt, target.UpdatedAt = time.Now().UTC(), time.Now().UTC()
		if err := b.store.Create(target); err != nil {
			return err
		}
	}
	if len(b.visited) >= 128 {
		return errors.New("SESSION_SWITCH_LIMIT")
	}
	if target.Mode != session.ModeAuto || target.Coordinator != "native" {
		return errors.New("SESSION_MODE_MISMATCH")
	}
	next, err := b.store.Acquire(target.SessionID)
	if err != nil {
		return err
	}
	if _, err = b.store.Reopen(target.SessionID, b.cwd, session.ModeAuto); err != nil {
		_ = next.Close()
		return err
	}
	_, err = b.store.Update(target.SessionID, func(v *session.Session) error {
		v.SetFrontend(b.frontend, thread)
		v.PrimaryPID, v.PrimaryProcessStart = current.PrimaryPID, current.PrimaryProcessStart
		v.FrontendPID, v.FrontendProcessStart = current.FrontendPID, current.FrontendProcessStart
		v.State = session.StateRunning
		return nil
	})
	if err != nil {
		_ = next.Close()
		return err
	}
	_, err = b.store.Update(current.SessionID, func(v *session.Session) error {
		v.PrimaryPID, v.FrontendPID = 0, 0
		v.PrimaryProcessStart, v.FrontendProcessStart = "", ""
		v.State, v.CurrentPhase = session.StateCompleted, "detached"
		return nil
	})
	if err != nil {
		_, _ = b.store.Update(target.SessionID, func(v *session.Session) error {
			v.PrimaryPID, v.FrontendPID = 0, 0
			v.PrimaryProcessStart, v.FrontendProcessStart = "", ""
			v.State = session.StateFailed
			return nil
		})
		_ = next.Close()
		return err
	}
	_ = b.lease.Close()
	b.lease = next
	b.active.Store(target.SessionID)
	b.visited = append(b.visited, target.SessionID)
	return nil
}

func (b *conversationBinding) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	_ = b.lease.Close()
}
