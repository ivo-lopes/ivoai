package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/ivo-lopes/ivoai/internal/codexfrontend"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func (a *App) SessionNativeDiscover(ctx context.Context, id string) ([]codexfrontend.NativeThread, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	state, err := a.Store.LoadState()
	if err != nil {
		return nil, err
	}
	state, _, err = a.resolveCodex(ctx, state)
	if err != nil {
		return nil, err
	}
	native, err := codexfrontend.ResolveNativeState(os.Environ(), cwd)
	if err != nil {
		return nil, err
	}
	return codexfrontend.DiscoverNative(ctx, state.Components["codex"].Path, cwd, filepath.Join(a.Store.Paths.StateDir, "native-discovery"), native, id)
}

func (a *App) SessionAdoptCodex(ctx context.Context, id string, confirmed bool) (session.Session, error) {
	if !confirmed {
		return session.Session{}, errors.New("SESSION_ADOPTION_REQUIRED")
	}
	threads, err := a.SessionNativeDiscover(ctx, id)
	if err != nil {
		return session.Session{}, err
	}
	if len(threads) != 1 || threads[0].ID != id {
		return session.Session{}, errors.New("NATIVE_SESSION_NOT_AVAILABLE")
	}
	native, err := codexfrontend.ResolveNativeState(os.Environ(), threads[0].Directory)
	if err != nil {
		return session.Session{}, err
	}
	return (session.Store{Root: a.Store.Paths.SessionsDir}).AdoptCodex(id, threads[0].Directory, native.ScopeID(), confirmed)
}

func (a *App) SessionResumeMode(ctx context.Context, id, mode string, confirmed bool) error {
	return a.resumeCodexMode(ctx, id, mode, confirmed, nil)
}

func (a *App) resumeCodexMode(ctx context.Context, id, mode string, confirmed bool, args []string) error {
	if !confirmed {
		return errors.New("SESSION_MODE_CONFIRMATION_REQUIRED")
	}
	destination := session.Mode(mode)
	if destination == session.ModeOrchestrated {
		destination = session.ModeAuto
	}
	if destination != session.ModeAuto && destination != session.ModeDirect {
		return errors.New("INVALID_SESSION_MODE")
	}
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	value, err := store.Get(id)
	if err != nil {
		return err
	}
	if value.PrimaryExecutor != "codex" {
		return errors.New("PROVIDER_HANDOFF_REQUIRED")
	}
	thread := value.FrontendSessions["codex"]
	if value.Frontend == "codex" && value.FrontendSessionID != "" {
		thread = value.FrontendSessionID
	}
	if thread == "" {
		thread = value.ExecutorSessionID
	}
	state, err := a.Store.LoadState()
	if err != nil {
		return err
	}
	state, _, err = a.resolveCodex(ctx, state)
	if err != nil {
		return err
	}
	native, err := codexfrontend.ResolveNativeState(os.Environ(), value.WorkingDirectory)
	if err != nil {
		return err
	}
	if _, err = codexfrontend.DiscoverNative(ctx, state.Components["codex"].Path, value.WorkingDirectory, filepath.Join(a.Store.Paths.StateDir, "native-discovery"), native, thread); err != nil {
		if value.NativeStoreScope == "" && value.Mode == session.ModeAuto && err.Error() == "NATIVE_SESSION_NOT_AVAILABLE" {
			return errors.New("NATIVE_SESSION_NOT_PORTABLE: legacy provider store; resume in its original mode without --mode")
		}
		return err
	}
	if _, err = store.SwitchCodexMode(id, thread, native.ScopeID(), destination, confirmed); err != nil {
		return err
	}
	if destination == session.ModeDirect {
		return a.SessionStartWithKnowledge(context.WithValue(ctx, resumeSessionKey{}, id), "codex", session.ModeDirect, args, nil)
	}
	return a.SessionResume(ctx, id)
}
