package app

// Private opt-in overlay: real official CLI inference with owner revocation of
// one disposable Core receipt. No credential, provider response or plan is faked.
import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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
)

func TestLiveAdaptivePrimaryReceiptRevocation(t *testing.T) {
	if os.Getenv("IVOAI_ADAPTIVE_REVOKE_LIVE") != "1" {
		t.Skip("NOT_RUN: explicit official subscription receipt-revocation proof required")
	}
	binary, report := os.Getenv("IVOAI_LIVE_CODEX_PATH"), os.Getenv("IVOAI_ADAPTIVE_REVOKE_REPORT")
	if !filepath.IsAbs(binary) || primaryDirectActivationOutput(report) != nil {
		t.Fatal("reviewed official executable and new private report required")
	}
	type receipt struct {
		SchemaVersion       int                      `json:"schema_version"`
		Scope               string                   `json:"scope"`
		Outcome             string                   `json:"outcome"`
		Failure             string                   `json:"failure,omitempty"`
		WallMS              int64                    `json:"wall_ms"`
		OfficialStart       bool                     `json:"official_pid_and_turn_started_observed"`
		ReceiptRevoked      bool                     `json:"owner_revoked_active_readonly_receipt"`
		HostDenied          bool                     `json:"host_denied_adaptive_primary"`
		NoAnswerDelivered   bool                     `json:"no_final_response_delivered_to_frontend"`
		CallerCancelled     bool                     `json:"caller_cancelled_before_native_result"`
		NativeExitZero      bool                     `json:"native_exit_zero"`
		NativeCompletion    bool                     `json:"native_completion_event"`
		NativeResponseText  bool                     `json:"native_response_text_present"`
		ReceiptClosed       bool                     `json:"receipt_closed"`
		Attempts            int                      `json:"primary_attempts"`
		FailedAttempt       bool                     `json:"single_failed_attempt_denied_by_host"`
		DAG                 bool                     `json:"dag"`
		Workers             int                      `json:"workers"`
		NoReplay            bool                     `json:"no_replay"`
		NoAutomaticFailover bool                     `json:"no_automatic_failover"`
		ReplayHTTP          int                      `json:"duplicate_http_status"`
		NoDuplicateAttempt  bool                     `json:"duplicate_created_no_attempt"`
		FilesIntact         bool                     `json:"repository_data_files_intact"`
		Usage               *core.ReportedTokenUsage `json:"official_client_usage,omitempty"`
		GlobalEconomicGate  string                   `json:"global_economic_gate"`
	}
	row := receipt{SchemaVersion: 1, Scope: "private_real_official_managed_adaptive_Core_receipt_revocation_not_credential_revocation", Outcome: "FAIL", GlobalEconomicGate: "INCOMPLETE"}
	started := time.Now()
	defer func() {
		row.WallMS = time.Since(started).Milliseconds()
		file, err := os.OpenFile(report, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Error("bounded revocation receipt unavailable")
			return
		}
		err = json.NewEncoder(file).Encode(row)
		if file.Close() != nil || err != nil {
			t.Error("bounded revocation receipt write failed")
		}
	}()
	fail := func(code string) { t.Helper(); row.Failure = code; t.Fatal(code) }
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel() // Only cleanup after a terminal result/failure; never the proof trigger.
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
		fail("INITIAL_SOURCE_SNAPSHOT_FAILED")
	}
	if _, err := fingerprintAdaptivePair(ctx, func() adaptiveRuntime { _, p := adaptiveLiveRuntime(t, binary); return p }()); err != nil {
		fail("OFFICIAL_PAIR_NOT_MATCHED")
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
		row.Outcome, row.Failure = "NOT_RUN", "OBSERVED_OFFICIAL_MODEL_UNAVAILABLE"
		t.Skip("NOT_RUN: observed official model unavailable")
	}
	scope, err := prefetchNewDirectScope(binary, f.root, model.UpstreamModel, "read-only", environment)
	if err != nil {
		fail("OFFICIAL_SCOPE_UNAVAILABLE")
	}
	_, admission := prefetchCommonLiveAdmission(ctx, scope, prefetchSubscriptionHostPolicy{PermitFilteredEnvironment: true})
	if admission.State != "ADMITTED" {
		row.Outcome, row.Failure = "NOT_RUN", admission.Reason
		t.Skip("NOT_RUN: fresh official admission refused")
	}
	b, results := adaptiveInferenceBridge(t, f, environment, catalog)
	const message = "owner_receipt_revocation"
	const prompt = "Explique em português, em três parágrafos, por que autenticação e autorização precisam de verificações independentes em um backend de agentes. Responda conceitualmente, sem consultar ferramentas, sem alterar arquivos e sem criar plano ou workers."
	type response struct {
		answer string
		err    error
	}
	finished := make(chan response, 1)
	go func() {
		a, e := b.SubmitTurn(ctx, "fresh_frontend", message, prompt, model.ID, model.DefaultEffort)
		finished <- response{a, e}
	}()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	trigger := time.NewTimer(100 * time.Second)
	defer trigger.Stop()
	observedPID, receiptID := 0, ""
waiting:
	for {
		select {
		case <-finished:
			fail("TURN_ENDED_BEFORE_OWNER_REVOCATION")
		case <-ctx.Done():
			fail("CASE_DEADLINE_BEFORE_OWNER_REVOCATION")
		case <-trigger.C:
			fail("OFFICIAL_TURN_START_NOT_OBSERVED")
		case <-tick.C:
			v, getErr := f.store.Get(f.id)
			var trace core.ExecutionTrace
			if getErr != nil || json.Unmarshal(v.ExecutorTrace, &trace) != nil || trace.Executor != "codex" || trace.Executable != binary || trace.PID <= 0 || trace.Events <= 0 || trace.LastEvent != "turn.started" || trace.Completion || trace.FinalResponse || !session.PrimaryTurnActive(v) || v.PrimaryTurn.Mode != core.PrimaryReadOnly {
				continue
			}
			observedPID, receiptID = trace.PID, v.PrimaryTurn.ID
			_, updateErr := f.store.Update(f.id, func(owned *session.Session) error {
				if !session.PrimaryTurnActive(*owned) || owned.PrimaryTurn.Mode != core.PrimaryReadOnly || owned.PrimaryTurn.ID != receiptID || owned.PrimaryTurn.TurnID != owned.ObservationTurnID || owned.PrimaryTurn.AttemptID != owned.ObservationAttemptID || owned.PlanID != "" || len(owned.Workers) != 0 || len(owned.Tasks) != 0 {
					return errors.New("revocation target no longer matches active owned receipt")
				}
				owned.PrimaryTurn.Mode = core.PrimaryClosed
				return nil
			})
			if updateErr != nil {
				fail("OWNED_READONLY_RECEIPT_REVOCATION_FAILED")
			}
			row.OfficialStart, row.ReceiptRevoked = true, true
			break waiting
		}
	}
	// Do not cancel to manufacture a rejection. The official invocation must
	// settle naturally; the existing final validation must reject its receipt.
	var out response
	select {
	case out = <-finished:
	case <-ctx.Done():
		fail("NATIVE_TURN_DID_NOT_SETTLE_AFTER_REVOCATION")
	}
	row.HostDenied = out.err != nil && strings.HasPrefix(out.err.Error(), "ADAPTIVE_PRIMARY_DENIED:")
	row.NoAnswerDelivered = out.answer == ""
	var result core.ExecutorResult
	select {
	case result = <-results:
	case <-ctx.Done():
		fail("REAL_EXECUTOR_RESULT_UNAVAILABLE")
	}
	row.NoReplay, row.NoAutomaticFailover = result.Policy.NoReplay, result.Policy.NoAutomaticFailover
	if result.Trace != nil {
		trace := result.Trace
		if trace.PID != observedPID || trace.Executor != "codex" || trace.Executable != binary || trace.Events <= 0 {
			fail("OFFICIAL_FINAL_TRACE_IDENTITY_MISMATCH")
		}
		row.NativeExitZero = trace.ExitCode == 0 && trace.Signal == "" && trace.Failure == ""
		row.NativeCompletion, row.NativeResponseText, row.Usage = trace.Completion, trace.FinalResponse, trace.Usage
	}
	v, err := f.store.Get(f.id)
	if err != nil {
		fail("FINAL_OWNED_SESSION_UNAVAILABLE")
	}
	row.Attempts, row.DAG, row.Workers = len(v.TurnAttempts), v.PlanID != "", len(v.Workers)
	row.ReceiptClosed = v.PrimaryTurn != nil && v.PrimaryTurn.ID == receiptID && v.PrimaryTurn.Mode == core.PrimaryClosed
	if len(v.TurnAttempts) == 1 {
		var attempt opencodebridge.TurnAttempt
		row.FailedAttempt = json.Unmarshal(v.TurnAttempts[0], &attempt) == nil && attempt.State == "failed" && attempt.Failure == "ADAPTIVE_PRIMARY_DENIED" && attempt.EndedAt != nil && attempt.EffectiveExecutor == "codex" && attempt.Trace != nil && attempt.Trace.PID == observedPID
	}
	proofCtx, proofCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer proofCancel()
	row.ReplayHTTP = adaptiveFailureDuplicate(proofCtx, b, message, prompt, model.ID, model.DefaultEffort)
	latest, getErr := f.store.Get(f.id)
	row.NoDuplicateAttempt = getErr == nil && len(latest.TurnAttempts) == 1 && len(latest.FrontendRequests) == 1 && latest.PrimaryTurn != nil && latest.PrimaryTurn.ID == receiptID && latest.PrimaryTurn.Mode == core.PrimaryClosed && latest.PlanID == "" && len(latest.Workers) == 0
	after, snapshotErr := adaptiveChangeSnapshot(f.root)
	row.FilesIntact = snapshotErr == nil && reflect.DeepEqual(before, after)
	if !row.OfficialStart || !row.ReceiptRevoked || !row.HostDenied || !row.NoAnswerDelivered || !row.NativeExitZero || !row.NativeCompletion || !row.ReceiptClosed || !row.FailedAttempt || row.Attempts != 1 || row.Workers != 0 || row.DAG || !row.NoReplay || !row.NoAutomaticFailover || row.ReplayHTTP != http.StatusConflict || !row.NoDuplicateAttempt || !row.FilesIntact {
		fail("CORE_RECEIPT_REVOCATION_CONTRACT_FAILED")
	}
	row.Outcome = "PASS"
}
