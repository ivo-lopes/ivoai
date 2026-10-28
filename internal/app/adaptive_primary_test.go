package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/codexresolver"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func newAdaptiveFixture(t *testing.T) *localMetadataFixture {
	t.Helper()
	f := newLocalMetadataFixture(t)
	t.Setenv(localMetadataFlag, "")
	t.Setenv(adaptivePrimaryFlag, "1")
	path := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(path, []byte("fixture runtime identity, never executed"), 0700); err != nil {
		t.Fatal(err)
	}
	sha, err := codexresolver.Fingerprint(path)
	if err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(filepath.Dir(path), "codex-code-mode-host")
	if err := os.WriteFile(host, []byte("disposable companion fixture, never executed"), 0700); err != nil {
		t.Fatal(err)
	}
	hostSHA, err := codexresolver.Fingerprint(host)
	if err != nil {
		t.Fatal(err)
	}
	// This private injected identity deliberately proves host admission behavior,
	// not the official executable. Production cannot set it through a flag/env.
	f.a.adaptiveRuntimeCheck = func(config.State, session.Session) (adaptiveRuntime, error) {
		return adaptiveRuntime{path, sha, host, hostSHA}, nil
	}
	f.state.Components["codex"] = config.ComponentState{Path: path, Version: "0.162.1", Installed: true}
	if err := f.a.Store.SaveState(f.state); err != nil {
		t.Fatal(err)
	}
	f.state, _ = f.a.Store.LoadState()
	f.update(t, func(v *session.Session) { v.CodexPath, v.CodexSHA256 = path, sha })
	return f
}

func adaptiveBridge(t *testing.T, f *localMetadataFixture, base core.ExecutorRunner, configure ...func(*opencodebridge.Options)) (*opencodebridge.Bridge, <-chan core.ExecutorResult) {
	return adaptiveBridgePath(t, f, base, "", configure...)
}

func adaptiveBridgePath(t *testing.T, f *localMetadataFixture, base core.ExecutorRunner, instructions string, configure ...func(*opencodebridge.Options)) (*opencodebridge.Bridge, <-chan core.ExecutorResult) {
	t.Helper()
	runtime := t.TempDir()
	if instructions == "" {
		instructions = filepath.Join(runtime, "instructions.md")
	}
	results := make(chan core.ExecutorResult, 4)
	catalog := opencodebridge.CatalogFromRegistry(routing.Registry{Providers: map[string]routing.ProviderCapability{"codex": {Authenticated: true, Models: []routing.ModelCapability{{Name: "fixture-observed-model", Source: routing.SourceRuntimeVerified, CapabilityTier: routing.TierStrong, SupportedEfforts: []string{"high"}, DefaultEffort: "high"}}}}})
	scoped := f.a.scopeAutoRunner(base, f.cfg, f.state, f.store, f.id, f.root, runtime, instructions, nil, nil, &autoTurnState{}, catalog, autoTurnOptions{FreshLocalEvidenceSession: true})
	options := opencodebridge.Options{Frontend: "codex", RequirePromptGate: true, Catalog: catalog,
		Runner: turnRunnerFunc(func(ctx context.Context, request core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
			result, err := scoped.Run(ctx, request, emit)
			results <- result
			return result, err
		}),
		Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() opencodebridge.Status { return opencodebridge.Status{Frontend: "codex"} },
		ClaimRequest: func(frontend, message string) (bool, error) {
			_, err := f.store.Update(f.id, func(v *session.Session) error {
				if v.FrontendRequests == nil {
					v.FrontendRequests = map[string]time.Time{}
				}
				v.FrontendRequests[frontend+":"+message] = time.Now().UTC()
				return nil
			})
			return err == nil, err
		}, Attempt: func(attempt opencodebridge.TurnAttempt) error {
			attempt.SessionID = f.id
			_, err := f.store.Update(f.id, func(v *session.Session) error {
				body, _ := json.Marshal(attempt)
				found := false
				for i, raw := range v.TurnAttempts {
					var prior opencodebridge.TurnAttempt
					if json.Unmarshal(raw, &prior) == nil && prior.ID == attempt.ID {
						v.TurnAttempts[i] = body
						found = true
						break
					}
				}
				if !found {
					v.TurnAttempts = append(v.TurnAttempts, body)
				}
				v.TurnState = attempt.State
				return appendTurnObservation(v, attempt)
			})
			return err
		}}
	for _, apply := range configure {
		apply(&options)
	}
	b, err := opencodebridge.Start(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close(context.Background()) })
	return b, results
}

func TestAdaptivePrimaryPreparationFailureDoesNotExecuteOrReplay(t *testing.T) {
	f := newAdaptiveFixture(t)
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parent, []byte("private fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	var calls, alternate atomic.Int32
	base := turnRunnerFunc(func(context.Context, core.ExecutorRequest, func(string) error) (core.ExecutorResult, error) {
		calls.Add(1)
		return core.ExecutorResult{}, nil
	})
	b, results := adaptiveBridgePath(t, f, base, filepath.Join(parent, "instructions"), func(o *opencodebridge.Options) {
		o.SelectAlternate = func(context.Context, string, []string) (string, error) { alternate.Add(1); return "claude", nil }
	})
	messages := []map[string]string{{"role": "user", "content": "Hello"}}
	status, _ := metadataHTTP(t, b, messages)
	result := <-results
	v, _ := f.store.Get(f.id)
	if status == 200 || !result.Policy.NoReplay || !result.Policy.NoAutomaticFailover || calls.Load() != 0 || alternate.Load() != 0 || v.PrimaryTurn == nil || v.PrimaryTurn.Mode != core.PrimaryClosed {
		t.Fatal("post-admission preparation failure escaped policy")
	}
	if status, _ := metadataHTTP(t, b, messages); status != 409 {
		t.Fatal("failed admitted operation was replayed")
	}
}

func TestAdaptiveClosedReceiptDoesNotPoisonNextTurn(t *testing.T) {
	for _, firstDAG := range []bool{false, true} {
		t.Run(map[bool]string{false: "read_only", true: "completed_dag"}[firstDAG], func(t *testing.T) {
			f := newAdaptiveFixture(t)
			var calls atomic.Int32
			base := turnRunnerFunc(func(ctx context.Context, request core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
				count := calls.Add(1)
				v, _ := f.store.Get(f.id)
				if count == 1 && firstDAG {
					if err := f.store.EnterPrimaryDAG(f.id, v.PrimaryTurn.Identity()); err != nil {
						return core.ExecutorResult{}, err
					}
				}
				if count == 2 && v.PrimaryTurn != nil {
					return core.ExecutorResult{}, errors.New("closed receipt survived into next turn")
				}
				if count == 2 || firstDAG {
					_, err := f.store.Update(f.id, func(v *session.Session) error {
						v.PlanID = "plan_0123456789abcdef0123456789abcdef"
						if v.PrimaryTurn != nil {
							v.PrimaryTurn.PlanID = v.PlanID
						}
						v.CurrentPhase = "synthesizing"
						v.Tasks = []session.TaskMetadata{{ID: "inspect", Role: "research", Tier: "LIGHT", State: session.StateCompleted, ExecutionMode: "primary", Model: session.UnknownModel()}}
						return nil
					})
					if err != nil {
						return core.ExecutorResult{}, err
					}
				}
				return core.ExecutorResult{ExecutorSessionID: "thread_first"}, emit("answer")
			})
			b, results := adaptiveBridge(t, f, base)
			if status, _ := metadataHTTP(t, b, []map[string]string{{"role": "user", "content": "First question"}}); status != 200 {
				t.Fatal("first admitted turn failed")
			}
			if result := <-results; !result.Policy.NoReplay {
				t.Fatal("first receipt lost policy")
			}
			if _, err := b.SubmitTurn(context.Background(), "fresh_frontend", "second_message", "Follow-up question", "auto", ""); err != nil {
				t.Fatal("closed receipt blocked next ordinary turn", err)
			}
			if result := <-results; result.Policy.NoReplay {
				t.Fatal("later legacy turn inherited first receipt")
			}
			v, _ := f.store.Get(f.id)
			if v.PrimaryTurn != nil || len(v.FrontendRequests) != 2 || calls.Load() != 2 {
				t.Fatal("retirement erased history or replayed execution")
			}
		})
	}
}

func TestAdaptivePrimaryAnswersWithoutDAGAndNeverReplays(t *testing.T) {
	for _, prompt := range []string{"Olá", "Explain this idea briefly", "この考えを説明してください", "Bitte beantworte diese Frage"} {
		t.Run(prompt, func(t *testing.T) {
			f := newAdaptiveFixture(t)
			var calls atomic.Int32
			base := turnRunnerFunc(func(ctx context.Context, request core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
				calls.Add(1)
				v, _ := f.store.Get(f.id)
				if !session.PrimaryTurnActive(v) || v.PrimaryTurn.Mode != core.PrimaryReadOnly || v.PlanID != "" {
					return core.ExecutorResult{}, errors.New("primary executed before host read-only admission")
				}
				return core.ExecutorResult{Model: "fixture-observed-model", ConfigurationSource: "official_thread_configuration", Trace: &core.ExecutionTrace{UsageStatus: "reported", Usage: &core.ReportedTokenUsage{InputTokens: 17, OutputTokens: 3, Basis: "client_reported", Source: "codex_exec_turn_completed", Scope: "cli_invocation"}}}, emit("bounded primary answer")
			})
			b, results := adaptiveBridge(t, f, base)
			messages := []map[string]string{{"role": "user", "content": prompt}}
			status, body := metadataHTTP(t, b, messages)
			result := <-results
			if status != 200 || !strings.Contains(body, "bounded primary answer") || calls.Load() != 1 || !result.Policy.NoReplay || !result.Policy.NoAutomaticFailover || result.Trace.Usage.InputTokens != 17 || result.ConfigurationSource == core.LocalMetadataSource {
				t.Fatalf("incorrect primary completion: %d %s", status, body)
			}
			v, _ := f.store.Get(f.id)
			if v.PrimaryTurn.Mode != core.PrimaryClosed || v.PlanID != "" || len(v.Tasks) != 0 || len(v.Workers) != 0 || v.CheckpointAvailable {
				t.Fatal("simple completion fabricated a DAG or retained an open capability")
			}
			status, _ = metadataHTTP(t, b, messages)
			if status != 409 || calls.Load() != 1 {
				t.Fatal("admitted answer was replayed or reexecuted")
			}
		})
	}
}

func TestAdaptivePrimaryRevalidatesBeforeFinalAndPlanCannotFallBack(t *testing.T) {
	for _, change := range []string{"flag revoked", "config changed", "runtime changed", "turn changed", "session stopped", "plan requested", "plan incomplete"} {
		t.Run(change, func(t *testing.T) {
			f := newAdaptiveFixture(t)
			base := turnRunnerFunc(func(ctx context.Context, request core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
				if err := emit("unaccepted final"); err != nil {
					return core.ExecutorResult{}, err
				}
				switch change {
				case "flag revoked":
					t.Setenv(adaptivePrimaryFlag, "")
				case "config changed":
					cfg := f.cfg
					cfg.Orchestration.Auto.ParallelWrites = !cfg.Orchestration.Auto.ParallelWrites
					if err := f.a.Store.Save(cfg); err != nil {
						t.Fatal(err)
					}
				case "runtime changed":
					if err := os.WriteFile(f.state.Components["codex"].Path, []byte("changed"), 0700); err != nil {
						t.Fatal(err)
					}
				case "turn changed":
					f.update(t, func(v *session.Session) { v.ObservationAttemptID = "other" })
				case "session stopped":
					f.update(t, func(v *session.Session) { v.State = session.StateStopping })
				case "plan requested", "plan incomplete":
					current, _ := f.store.Get(f.id)
					if err := f.store.EnterPrimaryDAG(f.id, current.PrimaryTurn.Identity()); err != nil {
						t.Fatal(err)
					}
					if change == "plan incomplete" {
						f.update(t, func(v *session.Session) {
							v.PlanID = "plan_0123456789abcdef0123456789abcdef"
							v.PrimaryTurn.PlanID = v.PlanID
							v.CurrentPhase = "synthesizing"
							v.Tasks = []session.TaskMetadata{{ID: "inspect", Role: "research", Tier: "LIGHT", State: session.StatePrimary, ExecutionMode: "primary", Model: session.UnknownModel()}}
						})
					}
				}
				return core.ExecutorResult{}, nil
			})
			b, results := adaptiveBridge(t, f, base)
			status, body := metadataHTTP(t, b, []map[string]string{{"role": "user", "content": "A simple question"}})
			result := <-results
			if status == http.StatusOK || strings.Contains(body, "unaccepted final") || !result.Policy.NoReplay {
				t.Fatal("revoked or incomplete receipt emitted final")
			}
		})
	}
}

func TestAdaptivePrimaryFlagOffRetainsDAGGate(t *testing.T) {
	f := newAdaptiveFixture(t)
	t.Setenv(adaptivePrimaryFlag, "")
	b, results := adaptiveBridge(t, f, turnRunnerFunc(func(_ context.Context, _ core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
		return core.ExecutorResult{}, emit("UNACCEPTED_PRIMARY_SENTINEL")
	}))
	status, body := metadataHTTP(t, b, []map[string]string{{"role": "user", "content": "Hello"}})
	result := <-results
	if status == 200 || strings.Contains(body, "UNACCEPTED_PRIMARY_SENTINEL") || result.Policy.NoReplay {
		t.Fatal("disabled feature changed legacy admission")
	}
	v, _ := f.store.Get(f.id)
	if v.PrimaryTurn != nil {
		t.Fatal("disabled feature persisted receipt")
	}
}

func TestAdaptivePrimaryQuotaChangeNeverStartsAlternate(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "sse"}[stream], func(t *testing.T) { adaptiveQuotaChange(t, stream) })
	}
}

func adaptiveQuotaChange(t *testing.T, stream bool) {
	f := newAdaptiveFixture(t)
	started := make(chan struct{})
	var calls, alternate atomic.Int32
	base := turnRunnerFunc(func(ctx context.Context, request core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
		calls.Add(1)
		close(started)
		<-ctx.Done()
		return core.ExecutorResult{}, ctx.Err()
	})
	b, results := adaptiveBridge(t, f, base, func(o *opencodebridge.Options) {
		o.Monitor = func(ctx context.Context, executor string) string {
			select {
			case <-started:
				return "quota changed"
			case <-ctx.Done():
				return ""
			}
		}
		o.SelectAlternate = func(context.Context, string, []string) (string, error) { alternate.Add(1); return "claude", nil }
	})
	messages := []map[string]string{{"role": "user", "content": "Hello"}}
	status, body := adaptiveHTTP(t, b, messages, stream)
	result := <-results
	expected := http.StatusServiceUnavailable
	if stream {
		expected = http.StatusOK
	}
	if status != expected || !strings.Contains(body, "admitted_turn_quota_changed") || strings.Contains(body, "[DONE]") || calls.Load() != 1 || alternate.Load() != 0 || !result.Policy.NoAutomaticFailover {
		t.Fatalf("admitted turn escaped quota stop: %d calls=%d alternate=%d", status, calls.Load(), alternate.Load())
	}
	if status, _ := adaptiveHTTP(t, b, messages, stream); status != 409 {
		t.Fatal("quota-cancelled admitted turn replayed")
	}
}

func adaptiveHTTP(t *testing.T, b *opencodebridge.Bridge, messages []map[string]string, stream bool) (int, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"model": "auto", "stream": stream, "messages": messages})
	r, _ := http.NewRequest(http.MethodPost, b.URL()+"/v1/chat/completions", bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+b.Token())
	r.Header.Set("X-IVOAI-OpenCode-Session", "fresh_frontend")
	r.Header.Set("X-IVOAI-OpenCode-Message", "first_message")
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(raw)
}

func TestAdaptivePrimaryBoundedFinalAndEmitFailureKeepPolicy(t *testing.T) {
	for _, kind := range []string{"oversize", "emit failure"} {
		t.Run(kind, func(t *testing.T) {
			f := newAdaptiveFixture(t)
			var calls atomic.Int32
			base := turnRunnerFunc(func(_ context.Context, _ core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
				calls.Add(1)
				text := "UNACCEPTED_EMIT_SENTINEL"
				if kind == "oversize" {
					text = strings.Repeat("x", (1<<20)+1)
				}
				return core.ExecutorResult{}, emit(text)
			})
			b, results := adaptiveBridge(t, f, base, func(o *opencodebridge.Options) {
				if kind == "emit failure" {
					original := o.Runner
					o.Runner = turnRunnerFunc(func(ctx context.Context, r core.ExecutorRequest, _ func(string) error) (core.ExecutorResult, error) {
						return original.Run(ctx, r, func(string) error { return errors.New("fixture emit failed") })
					})
				}
			})
			messages := []map[string]string{{"role": "user", "content": "Hello"}}
			status, body := metadataHTTP(t, b, messages)
			result := <-results
			if status == 200 || strings.Contains(body, "UNACCEPTED_EMIT_SENTINEL") || !result.Policy.NoReplay || calls.Load() != 1 {
				t.Fatal("bounded/failed final escaped execution policy")
			}
			v, _ := f.store.Get(f.id)
			if v.PrimaryTurn.Mode != core.PrimaryClosed {
				t.Fatal("failed final retained capability")
			}
			if status, _ := metadataHTTP(t, b, messages); status != 409 {
				t.Fatal("failed final replayed")
			}
		})
	}
}

func TestAdaptiveProductionAdmissionRefusesUnreviewedPair(t *testing.T) {
	f := newAdaptiveFixture(t)
	f.a.adaptiveRuntimeCheck = nil
	b, results := adaptiveBridge(t, f, turnRunnerFunc(func(_ context.Context, _ core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
		return core.ExecutorResult{}, emit("UNREVIEWED_PAIR_FINAL")
	}))
	status, body := metadataHTTP(t, b, []map[string]string{{"role": "user", "content": "Hello"}})
	result := <-results
	v, _ := f.store.Get(f.id)
	if status == 200 || strings.Contains(body, "UNREVIEWED_PAIR_FINAL") || v.PrimaryTurn != nil || result.Policy.NoReplay {
		t.Fatal("unreviewed pair entered production experimental policy")
	}
}
