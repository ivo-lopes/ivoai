package opencodebridge_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
)

// An implementation compiled against the old bridge names must satisfy the core
// interface without adapters or conversions, and vice versa.
type legacyTurnRunner struct{}

func (legacyTurnRunner) Run(_ context.Context, request opencodebridge.ExecutorRequest, emit func(string) error) (opencodebridge.ExecutorResult, error) {
	return core.ExecutorResult{ExecutorSessionID: request.ExecutorSessionID}, emit(request.Prompt)
}

type neutralTurnRunner struct{}

func (neutralTurnRunner) Run(ctx context.Context, request core.ExecutorRequest, emit func(string) error) (core.ExecutorResult, error) {
	return legacyTurnRunner{}.Run(ctx, request, emit)
}

var _ core.ExecutorRunner = legacyTurnRunner{}
var _ opencodebridge.ExecutorRunner = neutralTurnRunner{}

func TestTurnAliasesKeepRunnerCompatibility(t *testing.T) {
	request := core.ExecutorRequest{Prompt: "untrusted fixture text", ExecutorSessionID: "thread_fixture"}
	var legacy opencodebridge.ExecutorRequest = request
	var runner core.ExecutorRunner = legacyTurnRunner{}
	var bridgeRunner opencodebridge.ExecutorRunner = neutralTurnRunner{}
	for _, run := range []core.ExecutorRunner{runner, bridgeRunner} {
		text := ""
		result, err := run.Run(context.Background(), legacy, func(value string) error { text += value; return nil })
		if err != nil || text != request.Prompt || result.ExecutorSessionID != request.ExecutorSessionID {
			t.Fatal("legacy/core turn interface changed execution or emission")
		}
	}
}

func TestTurnFailureAliasesPreserveWrappedErrorIdentityAndClass(t *testing.T) {
	for _, tt := range []struct {
		name, class, message, expectedClass string
		code                                int
	}{
		{"completed exit code", "DAG_INCOMPLETE", "DAG_INCOMPLETE (exit 0)", "DAG_INCOMPLETE", 0},
		{"failed exit code", "executor_stream_incomplete", "executor_stream_incomplete (exit 1)", "executor_stream_incomplete", 1},
		{"unknown exit code", "CODEX_SESSION_EXECUTABLE_CHANGED", "CODEX_SESSION_EXECUTABLE_CHANGED", "CODEX_SESSION_EXECUTABLE_CHANGED", -1},
		{"empty class", "", "", "executor_failure", -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			original := &core.ExecutorFailure{Class: tt.class, ExitCode: tt.code}
			var oldName *opencodebridge.ExecutorFailure = original
			wrapped := fmt.Errorf("turn failed: %w", oldName)
			var neutral *core.ExecutorFailure
			var legacy *opencodebridge.ExecutorFailure
			if !errors.As(wrapped, &neutral) || !errors.As(wrapped, &legacy) || neutral != original || legacy != original {
				t.Fatal("errors.As no longer recovers the same failure through both names")
			}
			if original.Error() != tt.message || core.FailureClass(wrapped) != tt.expectedClass || opencodebridge.FailureClass(wrapped) != tt.expectedClass {
				t.Fatal("failure string or stable error classification changed")
			}
			fromLegacy := &opencodebridge.ExecutorFailure{Class: tt.class, ExitCode: tt.code}
			if !errors.As(fmt.Errorf("legacy: %w", fromLegacy), &neutral) || neutral != fromLegacy {
				t.Fatal("legacy construction no longer preserves core error identity")
			}
		})
	}
	for _, err := range []error{nil, errors.New("private provider diagnostic")} {
		if core.FailureClass(err) != "executor_failure" || opencodebridge.FailureClass(err) != "executor_failure" {
			t.Fatal("unknown failure fallback changed or exposed private error text")
		}
	}
}

func TestTurnAliasesPreserveLegacyJSON(t *testing.T) {
	// Fixed legacy documents include all fields and the historical spelling of
	// JSON keys. They detect schema drift, not merely equality between aliases.
	const requestJSON = `{"Executor":"codex","Model":"fixture","Effort":"low","SelectionMode":"explicit","CatalogRevision":"catalog_fixture","Prompt":"untrusted fixture text","FrontendSessionID":"frontend_fixture","ExecutorSessionID":"thread_fixture"}`
	const traceJSON = `{"configured_mcp":["mcp_servers.ivoai-code.url"],"executor":"codex","resolved_executable":"/fixture/codex","executor_version":"0.162.1","working_directory":"/fixture/repo","child_pid":42,"exit_code":1,"signal":"TERM","stdout_events":3,"last_valid_event":"item.completed","completion_event":false,"final_response_present":false,"partial_output_bytes":9,"stderr_bytes":4,"failure_class":"executor_cancelled","requested_model":"fixture","requested_reasoning":"low","effective_model":"fixture","effective_reasoning":"low","configuration_source":"official_thread","mcp_operations":[{"server":"ivoai-code","name":"code_inspect","status":"failed","failure_class":"cancelled","content_types":["text","resource"]}]}`
	const resultJSON = `{"AuthReference":"account_fixture","Trace":` + traceJSON + `,"ExecutorSessionID":"thread_fixture","CompressionUsed":false,"CompressionProvider":"direct","SelectionMode":"explicit","RequestedModel":"fixture","Model":"fixture","Effort":"low","CatalogRevision":"catalog_fixture","ConfigurationSource":"official_thread"}`
	const failureJSON = `{"Class":"DAG_INCOMPLETE","ExitCode":1}`
	const zeroTraceJSON = `{"executor":"","resolved_executable":"","executor_version":"","working_directory":"","child_pid":0,"exit_code":0,"stdout_events":0,"last_valid_event":"","completion_event":false,"final_response_present":false,"partial_output_bytes":0,"stderr_bytes":0}`
	const zeroToolJSON = `{"server":"","name":"","status":""}`
	const toolJSON = `{"server":"ivoai-code","name":"code_inspect","status":"failed","failure_class":"cancelled","content_types":["text","resource"]}`
	for _, tt := range []struct {
		name, document  string
		neutral, legacy any
	}{
		{"request", requestJSON, &core.ExecutorRequest{}, &opencodebridge.ExecutorRequest{}},
		{"result", resultJSON, &core.ExecutorResult{}, &opencodebridge.ExecutorResult{}},
		{"failure", failureJSON, &core.ExecutorFailure{}, &opencodebridge.ExecutorFailure{}},
		{"trace", traceJSON, &core.ExecutionTrace{}, &opencodebridge.ExecutionTrace{}},
		{"tool", toolJSON, &core.ToolTrace{}, &opencodebridge.ToolTrace{}},
		{"zero trace optional fields", zeroTraceJSON, &core.ExecutionTrace{}, &opencodebridge.ExecutionTrace{}},
		{"zero tool optional fields", zeroToolJSON, &core.ToolTrace{}, &opencodebridge.ToolTrace{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if reflect.TypeOf(tt.neutral) != reflect.TypeOf(tt.legacy) {
				t.Fatal("legacy name became a distinct type instead of an alias")
			}
			for _, value := range []any{tt.neutral, tt.legacy} {
				if err := json.Unmarshal([]byte(tt.document), value); err != nil {
					t.Fatal(err)
				}
				got, err := json.Marshal(value)
				if err != nil || string(got) != tt.document {
					t.Fatalf("legacy JSON changed: got=%s err=%v", got, err)
				}
			}
			if !reflect.DeepEqual(tt.neutral, tt.legacy) {
				t.Fatal("legacy JSON did not decode identically through both names")
			}
		})
	}
}
