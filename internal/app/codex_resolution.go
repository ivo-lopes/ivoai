package app

import (
	"context"
	"fmt"
	"os"

	"github.com/ivo-lopes/ivoai/internal/codexresolver"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func (a *App) resolveCodex(ctx context.Context, state config.State) (config.State, codexresolver.Resolution, error) {
	if a.CodexResolution != nil {
		return a.CodexResolution(ctx, state)
	}
	r, err := (codexresolver.Resolver{Runner: a.Runner, PATH: os.Getenv("PATH"), Managed: state.Components["codex"], Host: state.Components["codex-code-mode-host"]}).Resolve(ctx)
	if a.Err != nil {
		fmt.Fprintln(a.Err, r.Summary())
		if r.VersionDrift || err != nil {
			fmt.Fprintln(a.Err, r.Reason)
		}
	}
	return r.Apply(state), r, err
}

func pinCodex(value *session.Session, r codexresolver.Resolution) {
	value.CodexPath = r.Effective.RealPath
	value.CodexVersion = r.Effective.Version
	value.CodexSHA256 = r.Effective.SHA256
	value.CodexManaged = r.Effective.Ownership == "managed"
}

func pinnedCodexState(state config.State, value session.Session) (config.State, error) {
	if value.CodexPath == "" {
		return state, fmt.Errorf("CODEX_SESSION_RESOLUTION_MISSING: start a new session")
	}
	hash, err := codexresolver.Fingerprint(value.CodexPath)
	if err != nil || hash != value.CodexSHA256 {
		return state, fmt.Errorf("CODEX_SESSION_EXECUTABLE_CHANGED: start a new session")
	}
	return (codexresolver.Resolution{Effective: codexresolver.Candidate{Path: value.CodexPath, RealPath: value.CodexPath, Version: value.CodexVersion, Ownership: map[bool]string{true: "managed", false: "user/system"}[value.CodexManaged]}}).Apply(state), nil
}
