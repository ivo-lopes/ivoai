package workingcontext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/observability"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

const DefaultContextBudget = 8 << 10

type ProjectionInput struct {
	Owner         Ownership
	Raw           []byte
	MediaType     string
	Status        ResultStatus
	ExitCode      int
	Failure       error
	ContextBudget int
	TTL           time.Duration
}

type Projector struct {
	Store   ArtifactWriter
	Observe func(observability.Event)
}

func (p Projector) Project(ctx context.Context, input ProjectionInput) WorkerResult {
	status := input.Status
	if !validResultStatus(status) {
		status = ResultDegraded
	}
	budget := input.ContextBudget
	if budget <= 0 {
		budget = DefaultContextBudget
	}
	if budget > MaxSummaryBytes {
		budget = MaxSummaryBytes
	}
	mediaType := input.MediaType
	if mediaType == "" {
		mediaType = "text/plain; charset=utf-8"
	}
	if p.Store == nil {
		return p.storeUnavailable(input.Owner, status, errors.New("artifact store is unavailable"))
	}
	// Exact evidence is committed before any bounded representation is derived.
	ref, err := p.Store.Put(ctx, PutRequest{Kind: ArtifactWorkerOutput, MediaType: mediaType, Owner: input.Owner, Sensitivity: SensitivityRestricted, TTL: input.TTL}, bytes.NewReader(input.Raw))
	if err != nil {
		return p.storeUnavailable(input.Owner, status, err)
	}
	p.emit(input.Owner, observability.OperationArtifactStoreWrite, observability.StateCompleted, observability.ReasonArtifactStored, ref, 0, 1, false)
	resultRef := ResultRef{Artifact: ref, Role: EvidencePrimary}
	result := WorkerResult{Status: status, Evidence: []ResultRef{resultRef}}
	result.ImportantErrors = importantErrors(status, input.ExitCode, input.Failure, input.Raw)
	result.Findings = findings(input.Raw, resultRef)
	result.StateDelta = StateDelta{Proposed: []ProposedChange{{Kind: ChangeObservation, Target: "worker", Summary: statusSummary(status, input.ExitCode), Evidence: []ResultRef{resultRef}}}}
	result.Summary, result.Truncated = projectSummary(input.Raw, status, input.ExitCode, result.ImportantErrors, ref.ID, budget, mediaType)
	if err := result.Validate(); err != nil {
		return p.storeUnavailable(input.Owner, status, fmt.Errorf("project bounded worker result: %w", err))
	}
	p.emit(input.Owner, observability.OperationWorkerResultProjected, observability.StateCompleted, observability.ReasonResultProjected, ref, len(result.Findings), len(result.Evidence), result.Truncated)
	if result.Truncated {
		p.emit(input.Owner, observability.OperationWorkingContextBudget, observability.StateCompleted, observability.ReasonContextBudgetApplied, ref, len(result.Findings), len(result.Evidence), true)
	}
	return result
}

func (p Projector) storeUnavailable(owner Ownership, status ResultStatus, cause error) WorkerResult {
	message := boundedLine(platform.Redact(cause.Error()), MaxImportantErrorSize)
	result := WorkerResult{Status: status, Summary: "WorkingContext degraded: exact worker evidence could not be stored. Raw output was not injected into primary context.", ImportantErrors: []string{message}, Degraded: true, Truncated: true}
	p.emit(owner, observability.OperationWorkingContextDegraded, observability.StateDegraded, observability.ReasonStoreUnavailable, ArtifactRef{}, 0, 0, true)
	return result
}

func projectSummary(raw []byte, status ResultStatus, exitCode int, important []string, artifactID string, budget int, mediaType string) (string, bool) {
	header := fmt.Sprintf("Worker status: %s (exit %d). Exact evidence: %s.", status, exitCode, artifactID)
	if !utf8.Valid(raw) || strings.HasPrefix(mediaType, "application/octet-stream") {
		return boundedLine(header+" Binary/non-UTF-8 output is available only through the evidence reference.", budget), len(raw) > 0
	}
	text := platform.Redact(string(raw))
	parts := []string{header}
	for _, critical := range important {
		parts = append(parts, "Critical: "+critical)
	}
	prefix := strings.Join(parts, "\n")
	remaining := budget - len(prefix) - 2
	if remaining > 0 && strings.TrimSpace(text) != "" {
		excerpt := text
		if len(excerpt) > remaining {
			excerpt = excerpt[:remaining]
		}
		prefix += "\n\nBounded worker excerpt:\n" + excerpt
	}
	if len(prefix) > budget {
		prefix = prefix[:budget]
	}
	return prefix, len(text) > maxInt(0, remaining)
}

func importantErrors(status ResultStatus, exitCode int, failure error, raw []byte) []string {
	values := []string{}
	if failure != nil {
		values = append(values, boundedLine(platform.Redact(failure.Error()), MaxImportantErrorSize))
	}
	if status == ResultFailed && failure == nil {
		values = append(values, fmt.Sprintf("worker failed with exit code %d", exitCode))
	}
	if status == ResultCancelled {
		values = append(values, "worker execution was cancelled")
	}
	if utf8.Valid(raw) {
		for _, line := range strings.Split(platform.Redact(string(raw)), "\n") {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "fail") || strings.Contains(lower, "error") || strings.Contains(lower, "panic") || strings.Contains(lower, "cancel") || strings.Contains(lower, "blocker") || strings.Contains(lower, "vulnerab") || strings.Contains(lower, "security") || strings.Contains(lower, "denied") {
				line = boundedLine(line, MaxImportantErrorSize)
				if line != "" && !containsString(values, line) {
					values = append(values, line)
				}
				if len(values) == MaxImportantErrors {
					break
				}
			}
		}
	}
	return values
}

func findings(raw []byte, evidence ResultRef) []Finding {
	if !utf8.Valid(raw) {
		return nil
	}
	result := []Finding{}
	for _, line := range strings.Split(platform.Redact(string(raw)), "\n") {
		lower := strings.ToLower(line)
		category, importance := "", ImportanceInfo
		switch {
		case strings.Contains(lower, "security") || strings.Contains(lower, "vulnerab"):
			category, importance = "security", ImportanceHigh
		case strings.Contains(lower, "test") && strings.Contains(lower, "fail"):
			category, importance = "test_failure", ImportanceHigh
		case strings.Contains(lower, "error") || strings.Contains(lower, "panic") || strings.Contains(lower, "blocker"):
			category, importance = "worker_error", ImportanceModerate
		}
		if category != "" {
			result = append(result, Finding{Category: category, Importance: importance, Summary: boundedLine(line, MaxFindingSummary), Evidence: []ResultRef{{Artifact: evidence.Artifact, Role: EvidenceFinding}}})
			if len(result) == MaxFindings {
				break
			}
		}
	}
	return result
}

func statusSummary(status ResultStatus, exitCode int) string {
	return fmt.Sprintf("Worker reported %s with exit code %d; this is advisory evidence and was not applied as authoritative state.", status, exitCode)
}

func boundedLine(value string, limit int) string {
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " "))
	if len(value) > limit {
		value = value[:limit]
	}
	return value
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (p Projector) emit(owner Ownership, operation observability.Operation, state observability.State, reason observability.Reason, ref ArtifactRef, findings, references int, truncated bool) {
	if p.Observe == nil {
		return
	}
	event, err := observability.Normalize(observability.Event{Category: observability.CategoryWorkingContext, Operation: operation, State: state, SessionID: owner.SessionID, TaskID: owner.TaskID, WorkerID: owner.WorkerID, Component: core.ComponentWorkingContext, ArtifactID: ref.ID, ArtifactBytes: ref.Size, FindingCount: findings, ReferenceCount: references, Truncated: truncated, RoutingReason: reason})
	if err == nil {
		p.Observe(event)
	}
}
