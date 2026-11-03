package app

// Opt-in official subscription integration. Cancellation and refusal use the
// managed transport; no executor, plan, permission request or result is faked.
import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/routing"
)

type adaptiveFailureObservation struct {
	Scenario            string                   `json:"scenario"`
	Outcome             string                   `json:"outcome"`
	Failure             string                   `json:"failure,omitempty"`
	WallMS              int64                    `json:"wall_ms"`
	OfficialStart       bool                     `json:"official_pid_and_stdout_event_observed_before_cancel"`
	ReadDenied          bool                     `json:"actual_native_read_approval_denied"`
	CancelRequested     bool                     `json:"caller_cancellation_requested"`
	CancelConfirmed     bool                     `json:"official_executor_cancellation_confirmed"`
	Reaped              bool                     `json:"final_trace_after_executor_wait"`
	ReceiptClosed       bool                     `json:"receipt_closed"`
	Attempts            int                      `json:"primary_attempts"`
	Workers             int                      `json:"workers"`
	DAG                 bool                     `json:"dag"`
	CompletedCodeReads  int                      `json:"completed_host_code_reads"`
	PendingPermissions  int                      `json:"remaining_permissions"`
	ReplayHTTP          int                      `json:"duplicate_http_status"`
	NoDuplicateAttempt  bool                     `json:"duplicate_created_no_attempt"`
	NoAutomaticFailover bool                     `json:"no_automatic_failover"`
	NoReplay            bool                     `json:"no_replay"`
	FilesIntact         bool                     `json:"repository_data_files_intact"`
	Usage               *core.ReportedTokenUsage `json:"official_client_usage,omitempty"`
}

// Only status is retained. Neither the prompt nor an executor response reaches
// the receipt; the local bearer capability never leaves the bridge origin.
func adaptiveFailureDuplicate(ctx context.Context, b *opencodebridge.Bridge, message, prompt, model, effort string) int {
	body, err := json.Marshal(map[string]any{"model": model, "reasoning_effort": effort, "messages": []map[string]string{{"role": "user", "content": prompt}}})
	if err != nil {
		return 0
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.URL()+"/turn", bytes.NewReader(body))
	if err != nil {
		return 0
	}
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-IVOAI-Session", "fresh_frontend")
	req.Header.Set("X-IVOAI-Message", message)
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	return response.StatusCode
}

func TestLiveAdaptivePrimaryManagedFailure(t *testing.T) {
	if os.Getenv("IVOAI_ADAPTIVE_FAILURE_LIVE") != "1" {
		t.Skip("NOT_RUN: explicit official subscription failure integration required")
	}
	binary, report := os.Getenv("IVOAI_LIVE_CODEX_PATH"), os.Getenv("IVOAI_ADAPTIVE_FAILURE_REPORT")
	if !filepath.IsAbs(binary) || primaryDirectActivationOutput(report) != nil {
		t.Fatal("reviewed official executable and new private report required")
	}
	rows := []adaptiveFailureObservation{}
	defer func() {
		file, err := os.OpenFile(report, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Error("failure receipt unavailable")
			return
		}
		err = json.NewEncoder(file).Encode(map[string]any{"schema_version": 1, "scope": "real_official_managed_adaptive_failure", "global_economic_gate": "INCOMPLETE", "observations": rows})
		if file.Close() != nil || err != nil {
			t.Error("failure receipt write failed")
		}
	}()
	for _, scenario := range []string{"F_CANCEL", "F_DENY_READ"} {
		if selected := os.Getenv("IVOAI_ADAPTIVE_FAILURE_SCENARIOS"); selected != "" && !strings.Contains(","+selected+",", ","+scenario+",") {
			continue
		}
		t.Run(scenario, func(t *testing.T) {
			start := time.Now()
			row := adaptiveFailureObservation{Scenario: scenario, Outcome: "FAIL"}
			defer func() { row.WallMS = time.Since(start).Milliseconds(); rows = append(rows, row) }()
			ctx, stop := context.WithTimeout(context.Background(), 4*time.Minute)
			defer stop()
			f := adaptiveInferenceFixture(t, binary)
			f.cfg.Orchestration.Auto.AutomaticFailover = false
			f.cfg.Orchestration.Auto.ProviderPreference = "codex"
			f.cfg.OpenCode.PermissionMode = "full"
			if scenario == "F_DENY_READ" {
				f.cfg.OpenCode.PermissionMode = "interactive"
			}
			if f.a.Store.Save(f.cfg) != nil {
				row.Failure = "PRIVATE_CONFIG_FAILED"
				t.Fatal("private configuration unavailable")
			}
			f.cfg, _ = f.a.Store.Load()
			before, err := adaptiveChangeSnapshot(f.root)
			if err != nil {
				row.Failure = "INITIAL_SNAPSHOT_FAILED"
				t.Fatal("source snapshot unavailable")
			}
			environment := prefetchSubscriptionEnvironment()
			scope, err := prefetchNewDirectScope(binary, f.root, "gpt-6.1-sol", "read-only", environment)
			if err != nil {
				row.Failure = "SCOPE_UNAVAILABLE"
				t.Fatal("official scope unavailable")
			}
			_, admission := prefetchCommonLiveAdmission(ctx, scope, prefetchSubscriptionHostPolicy{PermitFilteredEnvironment: true})
			if admission.State != "ADMITTED" {
				row.Outcome, row.Failure = "NOT_RUN", admission.Reason
				t.Skip("NOT_RUN: existing official admission refused")
			}
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
			bridge, results := adaptiveInferenceBridge(t, f, environment, catalog)
			turnCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			message := "failure_" + scenario
			prompt := "Consulte code_inspect para explicar detalhadamente a autenticação em auth.go e a relação com os testes deste projeto. Trabalhe somente com leitura; preserve todos os arquivos e não crie plano nem workers."
			finished := make(chan error, 1)
			go func() {
				_, err := bridge.SubmitTurn(turnCtx, "fresh_frontend", message, prompt, model.ID, model.DefaultEffort)
				finished <- err
			}()
			ticker := time.NewTicker(25 * time.Millisecond)
			defer ticker.Stop()
			startLimit := time.NewTimer(100 * time.Second)
			defer startLimit.Stop()
			observedPID := 0
		waiting:
			for {
				select {
				case <-ctx.Done():
					row.Failure = "CASE_TIMEOUT_BEFORE_CANCELLATION"
					t.Fatal("case deadline is not a successful cancellation")
				case <-startLimit.C:
					row.Failure = "CANCELLATION_TRIGGER_NOT_OBSERVED"
					t.Fatal("official start or requested approval not observed")
				case <-finished:
					row.Failure = "TURN_ENDED_BEFORE_CONTROLLED_CANCELLATION"
					if scenario == "F_DENY_READ" {
						row.Outcome, row.Failure = "NOT_RUN", "ACTUAL_READ_APPROVAL_NOT_OBSERVED"
						t.Skip("NOT_RUN: native read approval was not observed")
					}
					t.Fatal("turn ended before controlled cancellation")
				case <-ticker.C:
					v, getErr := f.store.Get(f.id)
					var trace core.ExecutionTrace
					if getErr != nil || json.Unmarshal(v.ExecutorTrace, &trace) != nil || trace.Executor != "codex" || trace.Executable != binary || trace.PID <= 0 || trace.Events <= 0 || trace.Completion || trace.FinalResponse {
						continue
					}
					row.OfficialStart = true
					observedPID = trace.PID
					if scenario == "F_DENY_READ" {
						permissions, pendingErr := bridge.PendingDecisions(ctx)
						if pendingErr != nil {
							row.Failure = "NATIVE_PERMISSION_QUERY_FAILED"
							t.Fatal("native approval query failed")
						}
						if len(permissions) == 0 {
							continue
						}
						// Only the actual host Code Intelligence read is eligible.
						// Unexpected authority is refused and remains a failed case.
						for _, permission := range permissions {
							server, tool, valid := strings.Cut(permission.Description, " · ")
							if err := bridge.ReplyDecision(ctx, permission.ID, false); err != nil {
								row.Failure = "NATIVE_PERMISSION_REFUSAL_FAILED"
								t.Fatal("actual native approval could not be refused")
							}
							if !valid || server != nativeCodeMCP || !nativeCodeTool(tool) {
								row.Failure = "UNEXPECTED_PERMISSION_REFUSED"
								t.Fatal("unexpected permission refused")
							}
							row.ReadDenied = true
						}
						if !row.ReadDenied {
							continue
						}
					}
					row.CancelRequested = true
					cancel()
					break waiting
				}
			}
			// The HTTP client may return before the child exits. Only the real
			// runner's final trace and persisted ended attempt confirm cancellation.
			var result core.ExecutorResult
			select {
			case result = <-results:
			case <-time.After(45 * time.Second):
				row.Failure = "OFFICIAL_CANCELLATION_NOT_CONFIRMED"
				t.Fatal("no final official executor result after cancellation")
			}
			row.NoReplay, row.NoAutomaticFailover = result.Policy.NoReplay, result.Policy.NoAutomaticFailover
			if result.Trace != nil {
				row.CancelConfirmed = result.Trace.Executor == "codex" && result.Trace.Executable == binary && observedPID > 0 && result.Trace.PID == observedPID && result.Trace.Events > 0 && result.Trace.Failure == "executor_cancelled" && !result.Trace.Completion && !result.Trace.FinalResponse
				row.Reaped = row.CancelConfirmed // CLIRunner final trace is emitted only after cmd.Wait.
				row.Usage = result.Trace.Usage
			}
			// Result publication precedes the bridge's durable terminal attempt.
			settle := time.NewTimer(10 * time.Second)
			defer settle.Stop()
		settling:
			for {
				v, getErr := f.store.Get(f.id)
				if getErr == nil && len(v.TurnAttempts) == 1 {
					var attempt opencodebridge.TurnAttempt
					if json.Unmarshal(v.TurnAttempts[0], &attempt) == nil && attempt.EndedAt != nil && attempt.Failure == "executor_cancelled" && attempt.EffectiveExecutor == "codex" && attempt.Trace != nil && attempt.Trace.PID == observedPID && attempt.Trace.Failure == "executor_cancelled" {
						row.Attempts, row.Workers, row.DAG = len(v.TurnAttempts), len(v.Workers), v.PlanID != ""
						row.ReceiptClosed = v.PrimaryTurn != nil && v.PrimaryTurn.Mode == core.PrimaryClosed
						for _, event := range v.Observability {
							if event.Operation == observability.OperationCodeInspect && event.State == observability.StateCompleted {
								row.CompletedCodeReads++
							}
						}
						break settling
					}
				}
				select {
				case <-settle.C:
					row.Failure = "TERMINAL_ATTEMPT_NOT_PERSISTED"
					t.Fatal("cancelled attempt did not settle")
				case <-ticker.C:
				}
			}
			proofCtx, proofCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer proofCancel()
			row.ReplayHTTP = adaptiveFailureDuplicate(proofCtx, bridge, message, prompt, model.ID, model.DefaultEffort)
			v, getErr := f.store.Get(f.id)
			row.NoDuplicateAttempt = getErr == nil && len(v.TurnAttempts) == 1 && len(v.Workers) == 0 && v.PlanID == ""
			permissions, pendingErr := bridge.PendingDecisions(proofCtx)
			row.PendingPermissions = len(permissions)
			after, snapshotErr := adaptiveChangeSnapshot(f.root)
			row.FilesIntact = snapshotErr == nil && reflect.DeepEqual(before, after)
			if !row.OfficialStart || !row.CancelRequested || !row.CancelConfirmed || !row.Reaped || !row.ReceiptClosed || row.Attempts != 1 || row.Workers != 0 || row.DAG || !row.NoReplay || !row.NoAutomaticFailover || row.ReplayHTTP != http.StatusConflict || !row.NoDuplicateAttempt || pendingErr != nil || row.PendingPermissions != 0 || !row.FilesIntact || scenario == "F_DENY_READ" && (!row.ReadDenied || row.CompletedCodeReads != 0) {
				row.Failure = "CANCELLATION_OR_REFUSAL_CONTRACT_FAILED"
				t.Fatal("real cancellation/refusal or isolation contract failed")
			}
			row.Outcome = "PASS"
		})
	}
}
