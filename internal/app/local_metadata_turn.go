package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/evidence"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
	"golang.org/x/sys/unix"
)

const localMetadataFlag = "IVOAI_EXPERIMENTAL_LOCAL_EVIDENCE_TURNS"

// prepareLocalMetadataTurn is a fixed read capability, not a primary executor
// or general question classifier. Before admission, unsupported input falls
// back. After admission every error fails closed and must never replay a CLI.
func (a *App) prepareLocalMetadataTurn(ctx context.Context, cfg config.Config, state config.State, store session.Store, id, cwd string, selectors []string, request core.ExecutorRequest) (preparedAutoTurn, bool, error) {
	if os.Getenv(localMetadataFlag) != "1" || !opencodebridge.SingleUserMessage(ctx) || !core.LocalVersionQuestion(request.Prompt) || request.Executor != "codex" || request.ExecutorSessionID != "" || request.FrontendSessionID == "" {
		return preparedAutoTurn{}, false, nil
	}
	root, err := filepath.EvalSymlinks(cwd)
	if err != nil || !filepath.IsAbs(cwd) || filepath.Clean(cwd) != root {
		return preparedAutoTurn{}, false, nil
	}
	rootInfo, err := os.Stat(root)
	if err != nil || !rootInfo.IsDir() {
		return preparedAutoTurn{}, false, nil
	}
	scope, err := conversationScopeID(cwd, cfg, selectors)
	if err != nil {
		return preparedAutoTurn{}, false, nil
	}
	initial, err := store.Get(id)
	if err != nil || !localMetadataSession(initial, request, cwd, scope) || !localMetadataNoCheckpoint(store, id) {
		return preparedAutoTurn{}, false, nil
	}
	currentConfig, cfgErr := a.Store.Load()
	currentState, stateErr := a.Store.LoadState()
	if cfgErr != nil || stateErr != nil || evidence.Digest(currentConfig) != evidence.Digest(cfg) || evidence.Digest(currentState) != evidence.Digest(state) {
		return preparedAutoTurn{}, false, nil
	}
	raw, literal, err := readLocalVersion(ctx, root, rootInfo)
	if err != nil {
		return preparedAutoTurn{}, false, nil
	}
	// Receipt acquisition is the boundary: subsequent revocation, stale source
	// or storage failure cannot enter the legacy execution path.
	receipt := localMetadataSessionDigest(initial)
	configDigest, stateDigest := evidence.Digest(cfg), evidence.Digest(state)
	sourceDigest := evidence.Digest(raw)
	authorize := func(checkCtx context.Context) error {
		if err := checkCtx.Err(); err != nil {
			return err
		}
		latest, err := store.Get(id)
		if err != nil || os.Getenv(localMetadataFlag) != "1" || !localMetadataSession(latest, request, cwd, scope) || localMetadataSessionDigest(latest) != receipt || !localMetadataNoCheckpoint(store, id) {
			return evidence.ErrDenied
		}
		latestConfig, cfgErr := a.Store.Load()
		latestState, stateErr := a.Store.LoadState()
		if cfgErr != nil || stateErr != nil || evidence.Digest(latestConfig) != configDigest || evidence.Digest(latestState) != stateDigest {
			return evidence.ErrDenied
		}
		return checkCtx.Err()
	}
	retrieval := evidence.Request{
		Owner: workingcontext.Ownership{SessionID: id}, Source: "tool_read", ReadOnly: true, BudgetBytes: core.LocalVersionMaxBytes,
		Scope: evidence.Scope{
			Organization: evidence.Digest(scope), Repository: evidence.Digest("local-launch-root", root),
			Worktree: evidence.Digest("local-worktree", root, scope), Permissions: evidence.Digest("fixed-read:VERSION", configDigest, stateDigest, receipt),
		},
		QueryDigest: evidence.Digest("VERSION:ascii-semver:v1", core.LocalVersionMaxBytes), Authorize: authorize,
		CurrentGeneration: func(checkCtx context.Context) (string, error) {
			latest, _, err := readLocalVersion(checkCtx, root, rootInfo)
			if err != nil || evidence.Digest(latest) != sourceDigest {
				return "", evidence.ErrChanged
			}
			return sourceDigest, nil
		},
	}
	gateway, gatewayErr := a.metadataEvidenceGateway()
	var ref evidence.Ref
	prepared := preparedAutoTurn{
		Runner: turnRunnerFunc(func(runCtx context.Context, _ core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
			result := core.ExecutorResult{ConfigurationSource: core.LocalMetadataSource}
			if gatewayErr != nil {
				return result, gatewayErr
			}
			packet, err := gateway.Retrieve(runCtx, retrieval, func(fetchCtx context.Context) (evidence.Document, error) {
				latest, _, err := readLocalVersion(fetchCtx, root, rootInfo)
				if err != nil || !bytes.Equal(latest, raw) {
					return evidence.Document{}, evidence.ErrChanged
				}
				return evidence.Document{Raw: latest, MediaType: "text/plain; charset=utf-8", Basis: evidence.Extracted, SourceComplete: true, Sensitivity: workingcontext.SensitivityRestricted}, nil
			})
			if err != nil {
				return result, localMetadataError(runCtx, err)
			}
			ref = packet.Ref
			exact, err := gateway.Recover(runCtx, retrieval, ref)
			if err != nil {
				return result, localMetadataError(runCtx, err)
			}
			if !bytes.Equal(exact, raw) {
				return result, evidence.ErrChanged
			}
			// Keep signed provenance and scope digests inside the host. This
			// opaque artifact ID identifies the retained exact source bytes;
			// native transports do not yet expose an artifact recovery API.
			text := "IVOAI Core local metadata (no model execution)\nSource: VERSION\nVersion: " + literal + "\nEvidence reference: " + ref.Result.Artifact.ID + "\n"
			return result, emit(text)
		}),
		Validate: func() error {
			if gatewayErr != nil {
				return gatewayErr
			}
			// autoTurnRunner buffers every byte until this second exact recovery
			// has rechecked the receipt, permissions, source and cancellation.
			exact, err := gateway.Recover(ctx, retrieval, ref)
			if err != nil {
				return localMetadataError(ctx, err)
			}
			if !bytes.Equal(exact, raw) {
				return evidence.ErrChanged
			}
			if err := authorize(ctx); err != nil {
				return err
			}
			_, err = store.Update(id, func(value *session.Session) error {
				if ctx.Err() != nil || localMetadataSessionDigest(*value) != receipt {
					return localMetadataError(ctx, evidence.ErrDenied)
				}
				value.ConfigurationSource = core.LocalMetadataSource
				value.EffectiveExecutor, value.EffectiveModel, value.EffectiveEffort = "", "", ""
				return session.AppendObservation(value, observability.Event{Category: observability.CategoryCapability, Operation: observability.OperationCapabilityResolve, Component: "tools", Capability: "evidence.local_metadata", State: observability.StateCompleted, TokenBasis: "unavailable"})
			})
			return localMetadataError(ctx, err)
		},
	}
	return prepared, true, nil
}

func localMetadataError(ctx context.Context, err error) error {
	// Evidence invalidation intentionally coarsens errors. The turn boundary
	// must still retain cancellation instead of recording a provider failure.
	if canceled := ctx.Err(); canceled != nil {
		return canceled
	}
	return err
}

func localMetadataNoCheckpoint(store session.Store, id string) bool {
	absent, err := store.CheckpointAbsent(id)
	return err == nil && absent
}

func localMetadataSession(v session.Session, request core.ExecutorRequest, cwd, scope string) bool {
	if v.State != session.StateRunning || v.Mode != session.ModeAuto || !v.Auto || v.Frontend != "codex" || v.Coordinator != "native" || (v.CurrentPhase != "intake" && v.CurrentPhase != "conversation") {
		return false
	}
	if v.WorkingDirectory != cwd || v.KnowledgeScopeID != scope || v.PrimaryExecutor != "codex" || v.CurrentPrimary != "codex" || v.InitialPlanner != "codex" {
		return false
	}
	// The official presentation thread is real frontend state, not a hidden
	// primary backend thread. Exactly that current Codex mapping is allowed.
	if v.FrontendSessionID != request.FrontendSessionID || len(v.FrontendSessions) != 1 || v.FrontendSessions["codex"] != request.FrontendSessionID || v.ExecutorSessionID != "" || len(v.ExecutorSessions) != 0 {
		return false
	}
	if v.Lineage != nil || v.RecoveryRequested || v.CheckpointAvailable || v.CheckpointUpdatedAt != nil || v.PlanID != "" || len(v.Tasks) != 0 || len(v.Workers) != 0 || len(v.Decisions) != 0 || len(v.ExecutorTrace) != 0 || v.FailoverCount != 0 || v.PrimaryRufloTaskID != "" {
		return false
	}
	if len(v.FrontendRequests) != 1 || len(v.TurnAttempts) != 1 || v.TurnState != "running" {
		return false
	}
	for key := range v.FrontendRequests {
		frontend, _, ok := strings.Cut(key, ":")
		if !ok || frontend != request.FrontendSessionID {
			return false
		}
	}
	var attempt opencodebridge.TurnAttempt
	return json.Unmarshal(v.TurnAttempts[0], &attempt) == nil && attempt.SessionID == v.SessionID && attempt.TurnID != "" && attempt.ID != "" && attempt.TurnID == v.ObservationTurnID && attempt.ID == v.ObservationAttemptID && attempt.State == "running" && attempt.EndedAt == nil && attempt.Frontend == "codex" && attempt.FrontendSessionID == request.FrontendSessionID && attempt.RequestedExecutor == "codex" && attempt.EffectiveExecutor == "codex" && attempt.Trace == nil && attempt.NativeSessionID == ""
}

func localMetadataSessionDigest(v session.Session) string {
	// Unrelated status rendering may append observational events. Everything
	// else, including claims, attempts, grants, models and quota state, is bound.
	v.UpdatedAt = v.StartedAt
	v.Observability = nil
	v.ObservationSequence = 0
	return evidence.Digest(v)
}

func readLocalVersion(ctx context.Context, root string, expectedRoot os.FileInfo) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil || canonical != root {
		return nil, "", evidence.ErrChanged
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, "", err
	}
	directory := os.NewFile(uintptr(fd), root)
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil || !os.SameFile(expectedRoot, info) {
		return nil, "", evidence.ErrChanged
	}
	fd, err = unix.Openat(int(directory.Fd()), "VERSION", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, "", err
	}
	file := os.NewFile(uintptr(fd), "VERSION")
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() > core.LocalVersionMaxBytes {
		return nil, "", evidence.ErrChanged
	}
	raw, err := io.ReadAll(io.LimitReader(file, core.LocalVersionMaxBytes+1))
	if err != nil {
		return nil, "", err
	}
	after, err := file.Stat()
	if err != nil || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, "", evidence.ErrChanged
	}
	literal, valid := core.LocalVersionLiteral(raw)
	if !valid {
		return nil, "", evidence.ErrChanged
	}
	return raw, literal, ctx.Err()
}

func (a *App) metadataEvidenceGateway() (*evidence.Gateway, error) {
	evidenceGatewayInitMu.Lock()
	defer evidenceGatewayInitMu.Unlock()
	if a.evidenceGateway == nil {
		artifacts, err := workingcontext.NewLocalStore(filepath.Join(a.Store.Paths.CacheDir, "working-context"), workingcontext.LocalOptions{})
		if err != nil {
			return nil, err
		}
		a.evidenceGateway, err = evidence.New(artifacts)
		if err != nil {
			return nil, err
		}
	}
	return a.evidenceGateway, nil
}
