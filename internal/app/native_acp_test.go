package app

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/evidence"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestAntigravityEntryRequiresExplicitFlagBeforeRuntimeOrStorage(t *testing.T) {
	t.Setenv(antigravityFlag, "")
	a := &App{Out: io.Discard, Err: io.Discard}
	if err := a.AntigravityRun(context.Background(), NativeRunOptions{Prompt: "Explique este projeto."}); err == nil {
		t.Fatal("global experimental admission escaped")
	}
}

func nativeGuardFixture(t *testing.T) (*nativeACPGuard, session.Store, string) {
	t.Helper()
	root := t.TempDir()
	paths := config.Paths{Config: filepath.Join(root, "config.toml"), State: filepath.Join(root, "state.toml"), SessionsDir: filepath.Join(root, "sessions")}
	a := &App{Store: config.NewStore(paths)}
	cfg, err := a.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	state, err := a.Store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	id, err := session.NewID()
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "reviewed-fixture")
	if err = os.WriteFile(binary, []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	binaryInfo, err := adaptiveFileInfo(binary)
	if err != nil {
		t.Fatal(err)
	}
	receipt := core.PrimaryTurnReceipt{ID: "receipt", TurnID: "turn", AttemptID: "attempt", FrontendSessionID: "frontend", Executor: "antigravity", Mode: core.PrimaryReadOnly, ConfigDigest: evidence.Digest(cfg), StateDigest: evidence.Digest(state), ScopeDigest: nativeACPScope("scope", root, binary, info)}
	now := time.Now().UTC()
	v := session.Session{SessionID: id, StartedAt: now, UpdatedAt: now, Mode: session.ModeAuto, Auto: true, Coordinator: "native", State: session.StateRunning, InitialPlanner: "antigravity", CurrentPrimary: "antigravity", PrimaryExecutor: "antigravity", Frontend: "antigravity", FrontendSessionID: "frontend", FrontendPID: os.Getpid(), FrontendProcessStart: session.ProcessStart(os.Getpid()), WorkingDirectory: root, KnowledgeScopeID: "scope", SwarmID: "native_" + id, MaxWorkers: 2, PrimaryModel: session.ModelInfo{Name: "unknown", Source: session.ModelUnknown}, ContextStatus: "disabled", MemoryStatus: "disabled", ServerStatus: "not-connected", PrimaryTurn: &receipt, TurnState: "running", ObservationTurnID: "turn", ObservationAttemptID: "attempt", CurrentPhase: string(core.PrimaryReadOnly)}
	store := session.Store{Root: paths.SessionsDir}
	if err = store.Create(v); err != nil {
		t.Fatal(err)
	}
	return &nativeACPGuard{a, store, id, root, "scope", binary, info, binaryInfo, receipt}, store, id
}

func TestAntigravityGuardBindsOwnershipAndConsumesReadOnlyReceipt(t *testing.T) {
	g, store, id := nativeGuardFixture(t)
	if _, err := g.check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := g.consume(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := g.check(context.Background()); err == nil {
		t.Fatal("consumed receipt revived")
	}
	v, err := store.Get(id)
	if err != nil || v.PrimaryTurn.Mode != core.PrimaryClosed || len(v.Tasks) != 0 || len(v.Workers) != 0 {
		t.Fatalf("state=%#v err=%v", v, err)
	}
}

func TestAntigravityGuardRejectsForeignAttemptAndRuntimeReplacement(t *testing.T) {
	for _, change := range []string{"attempt", "frontend", "runtime", "cancel"} {
		t.Run(change, func(t *testing.T) {
			g, store, id := nativeGuardFixture(t)
			if change == "runtime" {
				replacement := g.binary + ".replacement"
				if err := os.WriteFile(replacement, []byte("fixture"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacement, g.binary); err != nil {
					t.Fatal(err)
				}
			} else {
				_, err := store.Update(id, func(v *session.Session) error {
					switch change {
					case "attempt":
						v.ObservationAttemptID = "foreign"
					case "frontend":
						v.SetFrontend("codex", "foreign")
					case "cancel":
						v.State = session.StateStopping
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := g.check(context.Background()); err == nil {
				t.Fatal("stale authority accepted")
			}
		})
	}
}

func TestAntigravitySharedJournalAdmission(t *testing.T) {
	_, store, id := nativeGuardFixture(t)
	claimed, err := store.ClaimFrontendRequest(context.Background(), id, "frontend", "message")
	if err != nil || !claimed {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
	raw, err := json.Marshal(opencodebridge.TurnAttempt{ID: "attempt-new", TurnID: "turn-new", SessionID: id, Frontend: "antigravity", FrontendSessionID: "frontend", RequestedExecutor: "antigravity", EffectiveExecutor: "antigravity", StartedAt: time.Now().UTC(), State: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.AppendTurnAttempt(context.Background(), id, raw); err != nil {
		t.Fatal(err)
	}
}

func TestAntigravityWorkspaceCustomizationsCannotAcquireExecution(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "repo")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if err := nativeWorkspaceBoundary(nested); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".agents")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(`{"untrusted":"never executed"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := nativeWorkspaceBoundary(nested); err == nil {
		t.Fatal("ancestor executable configuration bypassed Core")
	}
}

func TestNativeManagedMappingPreservesAutomaticAndExplicitSelectionContract(t *testing.T) {
	for _, model := range []string{"", "gemini-3.8-flash-low"} {
		t.Run("requested_"+model, func(t *testing.T) {
			_, store, id := nativeGuardFixture(t)
			mapping := opencodebridge.Mapping{FrontendSessionID: "frontend", Executor: "antigravity", ExecutorSessionID: "native-owned", SelectionMode: "auto", RequestedModel: "auto", EffectiveModel: "gemini-4-argon-medium", CatalogRevision: "fixture-revision", ConfigurationSource: "core_owned_private_acp_profile"}
			if model != "" {
				mapping.EffectiveModel = model
			}
			if err := persistManagedNativeMapping(store, id, "frontend", "antigravity", "process-epoch", NativeRunOptions{Model: model}, mapping); err != nil {
				t.Fatal(err)
			}
			v, err := store.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			requested, mode := "auto", "auto"
			if model != "" {
				requested, mode = model, "explicit"
			}
			if v.RequestedModel != requested || v.SelectionMode != mode || v.EffectiveModel != mapping.EffectiveModel || v.PrimaryModel.Source != session.ModelRuntimeVerified || v.ExecutorSessions["antigravity:frontend"].RequestedModel != requested {
				t.Fatalf("mapping lost selection: %#v", v)
			}
		})
	}
}
