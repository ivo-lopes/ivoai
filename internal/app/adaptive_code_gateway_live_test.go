//go:build linux

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

func TestLiveAdaptiveCodeGatewayCachedReceiptRevocation(t *testing.T) {
	if os.Getenv("IVOAI_ADAPTIVE_CODE_GATEWAY_REVOKE_LIVE") != "1" {
		t.Skip("NOT_RUN: explicit official cached-read revocation integration required")
	}
	binary, report := os.Getenv("IVOAI_LIVE_CODEX_PATH"), os.Getenv("IVOAI_ADAPTIVE_CODE_GATEWAY_REVOKE_REPORT")
	if !filepath.IsAbs(binary) || primaryDirectActivationOutput(report) != nil {
		t.Fatal("reviewed official executable and new private report required")
	}
	row := struct {
		Scope               string                     `json:"scope"`
		Outcome             string                     `json:"outcome"`
		Failure             string                     `json:"failure,omitempty"`
		WallMS              int64                      `json:"wall_ms"`
		GuardedTurns        int                        `json:"fresh_admitted_official_turns"`
		GatewayResolves     int                        `json:"gateway_resolves"`
		GatewayHits         int                        `json:"gateway_hits"`
		ProviderInspections int                        `json:"provider_inspections"`
		ExactLiveRecovery   bool                       `json:"exact_protected_recovery_before_revocation"`
		ExactSHA            string                     `json:"exact_original_typed_result_sha256,omitempty"`
		RecoveryRevoked     bool                       `json:"recovery_denied_after_owner_revocation"`
		OwnerRevoked        bool                       `json:"owner_revoked_active_cached_receipt"`
		NoCallerCancel      bool                       `json:"no_caller_cancellation_as_trigger"`
		NativeExitZero      bool                       `json:"official_native_exit_zero"`
		NativeCompletion    bool                       `json:"official_native_completion"`
		HostDenied          bool                       `json:"host_denied_final_response"`
		NoReplay            bool                       `json:"no_replay"`
		NoFailover          bool                       `json:"no_automatic_failover"`
		DuplicateDenied     bool                       `json:"duplicate_created_no_attempt"`
		Attempts            int                        `json:"attempts"`
		FilesIntact         bool                       `json:"repository_files_intact"`
		Usage               []*core.ReportedTokenUsage `json:"official_client_invocation_usage,omitempty"`
		Economy             string                     `json:"global_economic_gate"`
	}{Scope: "real_official_managed_native_code_cached_receipt_revocation_not_credential_revocation", Outcome: "FAIL", NoCallerCancel: true, Economy: "INCOMPLETE"}
	started := time.Now()
	defer func() {
		row.WallMS = time.Since(started).Milliseconds()
		file, err := os.OpenFile(report, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Error("revocation receipt unavailable")
			return
		}
		err = json.NewEncoder(file).Encode(row)
		if file.Close() != nil || err != nil {
			t.Error("revocation receipt write failed")
		}
	}()
	fail := func(code string) { t.Helper(); row.Failure = code; t.Fatal(code) }
	t.Setenv("IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	f := adaptiveInferenceFixture(t, binary)
	f.cfg.OpenCode.PermissionMode = "full"
	f.cfg.Orchestration.Auto.AutomaticFailover = false
	f.cfg.Orchestration.Auto.ProviderPreference = "codex"
	if f.a.Store.Save(f.cfg) != nil {
		fail("PRIVATE_CONFIG_UNAVAILABLE")
	}
	f.cfg, _ = f.a.Store.Load()
	before, err := adaptiveChangeSnapshot(f.root)
	if err != nil {
		fail("SOURCE_SNAPSHOT_UNAVAILABLE")
	}
	environment := prefetchSubscriptionEnvironment()
	catalog := opencodebridge.CatalogFromRegistry((routing.Discoverer{CodexPath: binary}).Discover(ctx)).RestrictedTo("codex")
	model, available := catalog.StrongPrimary("codex")
	for _, entry := range catalog.Entries() {
		if entry.Executor == "codex" && entry.UpstreamModel == "gpt-6.1-sol" && entry.ModelSource == string(routing.SourceRuntimeVerified) {
			if selected, valid := catalog.Resolve(entry.ID, "low"); valid && selected.Effort == "low" {
				model, available = entry, true
				model.DefaultEffort = "low"
			}
		}
	}
	if !available || model.ModelSource != string(routing.SourceRuntimeVerified) {
		row.Outcome, row.Failure = "NOT_RUN", "OBSERVED_MODEL_UNAVAILABLE"
		t.Skip("NOT_RUN: observed official model unavailable")
	}
	guard := func() {
		t.Helper()
		scope, err := prefetchNewDirectScope(binary, f.root, model.UpstreamModel, "read-only", environment)
		if err != nil {
			fail("OFFICIAL_SCOPE_UNAVAILABLE")
		}
		_, admission := prefetchCommonLiveAdmission(ctx, scope, prefetchSubscriptionHostPolicy{PermitFilteredEnvironment: true})
		if admission.State != "ADMITTED" {
			if row.GuardedTurns == 0 {
				row.Outcome, row.Failure = "NOT_RUN", admission.Reason
				t.Skip("NOT_RUN: existing official admission refused")
			}
			fail("SUBSEQUENT_OFFICIAL_ADMISSION_REFUSED")
		}
		row.GuardedTurns++
	}
	bridge, results := adaptiveInferenceBridge(t, f, environment, catalog)
	const prompt = "Chame exatamente uma vez a ferramenta ivoai-code code_inspect com os argumentos {\"operation\":\"ask\",\"query\":\"Authenticate\",\"limit\":1,\"budget_bytes\":1024}. Não use shell, não altere arquivos e não crie plano ou workers. Depois explique em três parágrafos a condição de autenticação retornada pela ferramenta."
	guard()
	first, err := adaptiveSubmitReadApproved(ctx, bridge, "cache_first", prompt, model.ID, model.DefaultEffort)
	if err != nil || !strings.Contains(first, "Authenticate") {
		fail("FIRST_OFFICIAL_CODE_TURN_FAILED")
	}
	firstResult := <-results
	if firstResult.Trace == nil || firstResult.Trace.ExitCode != 0 || !firstResult.Trace.Completion || !firstResult.Trace.FinalResponse {
		fail("FIRST_OFFICIAL_NATIVE_TRACE_INCOMPLETE")
	}
	if firstResult.Trace.Usage != nil {
		row.Usage = append(row.Usage, firstResult.Trace.Usage)
	}
	guard()
	type response struct {
		answer string
		err    error
	}
	done := make(chan response, 1)
	go func() {
		answer, err := adaptiveSubmitReadApproved(ctx, bridge, "cache_revoked", prompt, model.ID, model.DefaultEffort)
		done <- response{answer, err}
	}()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	var owner workingcontext.Ownership
	var artifact string
waiting:
	for {
		select {
		case <-done:
			fail("CACHED_TURN_ENDED_BEFORE_OWNER_REVOCATION")
		case <-ctx.Done():
			fail("CACHE_HIT_REVOCATION_DEADLINE")
		case <-tick.C:
			v, err := f.store.Get(f.id)
			if err != nil || !session.PrimaryTurnActive(v) || v.PrimaryTurn.Mode != core.PrimaryReadOnly || len(v.TurnAttempts) != 2 {
				continue
			}
			var hit string
			for _, event := range adaptiveGatewayEvents(v) {
				if event.AttemptID == v.ObservationAttemptID && event.CacheHit != nil && *event.CacheHit {
					hit = event.ArtifactID
				}
			}
			if hit == "" {
				continue
			}
			owner = workingcontext.Ownership{SessionID: f.id, TaskID: "primary_" + v.PrimaryTurn.ID}
			artifact = hit
			raw, ref, err := f.a.recoverProtectedEvidence(ctx, owner, artifact)
			var code core.CodeResult
			if err != nil || ref.Owner != owner || json.Unmarshal(raw, &code) != nil || !code.Fresh || !code.Untrusted || !strings.Contains(code.Text, "Authenticate") || !strings.Contains(code.Text, "fixture-allowed") {
				fail("ACTUAL_CACHED_TYPED_RESULT_NOT_RECOVERABLE")
			}
			encoded, err := json.Marshal(code)
			if err != nil || !bytes.Equal(encoded, raw) {
				fail("ACTUAL_CACHED_TYPED_RESULT_CHANGED_BYTES")
			}
			digest := sha256.Sum256(raw)
			row.ExactSHA = hex.EncodeToString(digest[:])
			row.ExactLiveRecovery = true
			expected := v.PrimaryTurn.ID
			_, err = f.store.Update(f.id, func(current *session.Session) error {
				if !session.PrimaryTurnActive(*current) || current.PrimaryTurn.ID != expected || current.PrimaryTurn.Mode != core.PrimaryReadOnly {
					return errors.New("owned cached receipt no longer active")
				}
				current.PrimaryTurn.Mode = core.PrimaryClosed
				return nil
			})
			if err != nil {
				fail("OWNER_REVOCATION_WINDOW_MISSED")
			}
			row.OwnerRevoked = true
			break waiting
		}
	}
	out := response{}
	select {
	case out = <-done:
	case <-ctx.Done():
		fail("NATIVE_DID_NOT_SETTLE_NORMALLY_AFTER_REVOCATION")
	}
	row.HostDenied = out.answer == "" && out.err != nil && strings.HasPrefix(out.err.Error(), "ADAPTIVE_PRIMARY_DENIED:")
	var second core.ExecutorResult
	select {
	case second = <-results:
	case <-ctx.Done():
		fail("REAL_SECOND_EXECUTOR_RESULT_UNAVAILABLE")
	}
	row.NoReplay, row.NoFailover = second.Policy.NoReplay, second.Policy.NoAutomaticFailover
	if second.Trace != nil {
		row.NativeExitZero = second.Trace.Executor == "codex" && second.Trace.Executable == binary && second.Trace.PID > 0 && second.Trace.ExitCode == 0 && second.Trace.Signal == "" && second.Trace.Failure == ""
		row.NativeCompletion = second.Trace.Completion && second.Trace.FinalResponse
		if second.Trace.Usage != nil {
			row.Usage = append(row.Usage, second.Trace.Usage)
		}
	}
	if _, _, err := f.a.recoverProtectedEvidence(ctx, owner, artifact); err != nil {
		row.RecoveryRevoked = true
	}
	v, err := f.store.Get(f.id)
	if err != nil {
		fail("FINAL_SESSION_UNAVAILABLE")
	}
	row.Attempts = len(v.TurnAttempts)
	for _, event := range v.Observability {
		if event.State != "completed" {
			continue
		}
		if event.Operation == "code.inspect" {
			row.ProviderInspections++
		}
		if event.Capability == "evidence.code" {
			row.GatewayResolves++
			if event.CacheHit != nil && *event.CacheHit {
				row.GatewayHits++
			}
		}
	}
	_, duplicate := bridge.SubmitTurn(ctx, "fresh_frontend", "cache_revoked", prompt, model.ID, model.DefaultEffort)
	latest, latestErr := f.store.Get(f.id)
	row.DuplicateDenied = duplicate != nil && latestErr == nil && len(latest.TurnAttempts) == row.Attempts
	after, err := adaptiveChangeSnapshot(f.root)
	row.FilesIntact = err == nil && reflect.DeepEqual(before, after)
	if !row.ExactLiveRecovery || !row.RecoveryRevoked || !row.OwnerRevoked || !row.HostDenied || !row.NoReplay || !row.NoFailover || !row.DuplicateDenied || !row.FilesIntact || !row.NativeExitZero || !row.NativeCompletion || row.Attempts != 2 || row.GatewayResolves != 2 || row.GatewayHits != 1 || row.ProviderInspections != 1 || v.PlanID != "" || len(v.Workers) != 0 || len(v.Tasks) != 0 {
		fail("CACHED_NATIVE_REVOCATION_OR_NO_REPLAY_INVARIANT_FAILED")
	}
	row.Outcome = "PASS"
}
