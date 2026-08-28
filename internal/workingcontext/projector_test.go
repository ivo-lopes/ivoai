package workingcontext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/observability"
)

func TestProjectorPersistsExactEvidenceBeforeBoundedProjection(t *testing.T) {
	store, err := NewLocalStore(filepath.Join(t.TempDir(), "working-context"), LocalOptions{ID: sequentialIDs()})
	if err != nil {
		t.Fatal(err)
	}
	owner := testOwner("1", "worker_0123456789abcdef0123456789abcdef")
	raw := []byte(strings.Repeat("ordinary output\n", 200_000) + "TEST FAILED: expected 1 got 2\nsecurity blocker: denied\n")
	events := []observability.Event{}
	result := (Projector{Store: store, Observe: func(event observability.Event) { events = append(events, event) }}).Project(context.Background(), ProjectionInput{Owner: owner, Raw: raw, Status: ResultFailed, ExitCode: 2, Failure: errors.New("worker exited with status 2"), ContextBudget: 4096})
	if err := result.Validate(); err != nil {
		t.Fatal(err)
	}
	if result.Status != ResultFailed || !result.Truncated || len(result.Summary) > 4096 || len(result.Evidence) != 1 || len(result.Findings) < 2 {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Summary, "TEST FAILED") || !strings.Contains(result.Summary, "security blocker") {
		t.Fatalf("critical failure hidden: %q", result.Summary)
	}
	reader, _, err := store.Read(context.Background(), Ownership{SessionID: owner.SessionID}, result.Evidence[0].Artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || !bytes.Equal(recovered, raw) {
		t.Fatalf("exact evidence mismatch: bytes=%d err=%v", len(recovered), err)
	}
	encoded, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("TEST FAILED")) || bytes.Contains(encoded, raw[:128]) {
		t.Fatalf("raw result leaked to observability: %s", encoded)
	}
	if len(events) < 3 || events[0].Operation != observability.OperationArtifactStoreWrite {
		t.Fatalf("events=%+v", events)
	}
}

func TestProjectorPreservesCancellationAndBinaryWithoutInlineRaw(t *testing.T) {
	store, err := NewLocalStore(filepath.Join(t.TempDir(), "working-context"), LocalOptions{ID: sequentialIDs()})
	if err != nil {
		t.Fatal(err)
	}
	owner := testOwner("1", "")
	raw := []byte{0xff, 0xfe, 0x00, 0x01}
	result := (Projector{Store: store}).Project(context.Background(), ProjectionInput{Owner: owner, Raw: raw, MediaType: "application/octet-stream", Status: ResultCancelled, ExitCode: 130})
	if result.Status != ResultCancelled || !result.Truncated || !strings.Contains(result.Summary, "Binary/non-UTF-8") || !containsString(result.ImportantErrors, "worker execution was cancelled") {
		t.Fatalf("result=%+v", result)
	}
	if strings.Contains(result.Summary, string(raw)) {
		t.Fatal("binary raw output entered summary")
	}
}

func TestProjectorProviderNeutralSmallAndMalformedOutputs(t *testing.T) {
	for index, fixture := range []struct{ name, raw string }{
		{"codex", "review complete"}, {"claude", `{"type":"result","result":"review complete"}`}, {"future-executor", "not structured but preserved"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			store, err := NewLocalStore(filepath.Join(t.TempDir(), "working-context"), LocalOptions{ID: sequentialIDs()})
			if err != nil {
				t.Fatal(err)
			}
			owner := testOwner(fmtSuffix(index+1), "")
			result := (Projector{Store: store}).Project(context.Background(), ProjectionInput{Owner: owner, Raw: []byte(fixture.raw), Status: ResultCompleted})
			if err := result.Validate(); err != nil || len(result.Evidence) != 1 || !strings.Contains(result.Summary, fixture.raw) {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestProjectorStoreFailureIsExplicitAndNeverFallsBackToRaw(t *testing.T) {
	raw := "Authorization: Bearer must-not-enter-summary\n" + strings.Repeat("x", 1<<20)
	result := (Projector{}).Project(context.Background(), ProjectionInput{Owner: testOwner("1", ""), Raw: []byte(raw), Status: ResultCompleted})
	if result.Status != ResultDegraded || !result.Degraded || !result.Truncated || len(result.Evidence) != 0 || strings.Contains(result.Summary, "must-not-enter") || strings.Contains(result.Summary, strings.Repeat("x", 100)) {
		t.Fatalf("unsafe degradation: %+v", result)
	}
}

func TestProjectorMarksPartialEvidenceAsTruncated(t *testing.T) {
	store, err := NewLocalStore(filepath.Join(t.TempDir(), "working-context"), LocalOptions{ID: sequentialIDs()})
	if err != nil {
		t.Fatal(err)
	}
	result := (Projector{Store: store}).Project(context.Background(), ProjectionInput{Owner: testOwner("1", ""), Raw: []byte("captured prefix"), Status: ResultFailed, Truncated: true})
	if len(result.Evidence) != 1 || !result.Truncated || result.Evidence[0].Artifact.Complete || !result.Evidence[0].Artifact.Truncated {
		t.Fatalf("partial evidence state=%+v", result)
	}
}

func fmtSuffix(value int) string { return string(rune('0' + value)) }
