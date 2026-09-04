package opencodebridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeRunner struct {
	mu       sync.Mutex
	requests []ExecutorRequest
	result   ExecutorResult
	err      error
}

type blockingRunner struct {
	mu       sync.Mutex
	requests []ExecutorRequest
}

type concurrencyRunner struct {
	active atomic.Int32
	max    atomic.Int32
	calls  atomic.Int32
}

type partialFailureRunner struct{}

type nonStreamingQuotaRunner struct{ calls atomic.Int32 }

func (r *nonStreamingQuotaRunner) Run(ctx context.Context, _ ExecutorRequest, emit func(string) error) (ExecutorResult, error) {
	r.calls.Add(1)
	_ = emit("already produced output")
	<-ctx.Done()
	return ExecutorResult{ExecutorSessionID: "thread_partial"}, ctx.Err()
}

func (partialFailureRunner) Run(_ context.Context, _ ExecutorRequest, emit func(string) error) (ExecutorResult, error) {
	_ = emit("untrusted partial output")
	return ExecutorResult{ExecutorSessionID: "thread_failed"}, &ExecutorFailure{Class: "executor_stream_incomplete", ExitCode: -1}
}

func (f *concurrencyRunner) Run(ctx context.Context, request ExecutorRequest, emit func(string) error) (ExecutorResult, error) {
	f.calls.Add(1)
	active := f.active.Add(1)
	defer f.active.Add(-1)
	for {
		old := f.max.Load()
		if active <= old || f.max.CompareAndSwap(old, active) {
			break
		}
	}
	select {
	case <-time.After(50 * time.Millisecond):
	case <-ctx.Done():
		return ExecutorResult{}, ctx.Err()
	}
	_ = emit("ok")
	return ExecutorResult{ExecutorSessionID: "thread_fixture"}, nil
}

func (f *blockingRunner) Run(ctx context.Context, request ExecutorRequest, emit func(string) error) (ExecutorResult, error) {
	f.mu.Lock()
	f.requests = append(f.requests, request)
	f.mu.Unlock()
	if request.Executor == "codex" {
		<-ctx.Done()
		return ExecutorResult{}, ctx.Err()
	}
	_ = emit("continued")
	return ExecutorResult{ExecutorSessionID: "claude_fixture"}, nil
}

func (f *fakeRunner) Run(_ context.Context, request ExecutorRequest, emit func(string) error) (ExecutorResult, error) {
	f.mu.Lock()
	f.requests = append(f.requests, request)
	f.mu.Unlock()
	if f.err == nil {
		_ = emit("bridge ")
		_ = emit("ok")
	}
	return f.result, f.err
}

func TestBridgeStreamsAndPersistsExecutorMapping(t *testing.T) {
	runner := &fakeRunner{result: ExecutorResult{ExecutorSessionID: "thread_fixture"}}
	var mappings []Mapping
	bridge, err := Start(Options{
		Token: "fixture-capability", Runner: runner,
		Select: func(_ context.Context, previous string) (string, error) {
			if previous != "" {
				return previous, nil
			}
			return "codex", nil
		},
		Status:  func() Status { return Status{Version: "fixture", Frontend: "opencode"} },
		Mapping: func(value Mapping) error { mappings = append(mappings, value); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close(context.Background()) })

	for count := 0; count < 2; count++ {
		body := `{"model":"auto","stream":true,"messages":[{"role":"user","content":"do not expose credentials"}]}`
		request, _ := http.NewRequest(http.MethodPost, bridge.URL()+"/v1/chat/completions", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+bridge.Token())
		request.Header.Set("X-IVOAI-OpenCode-Session", "oc_fixture")
		request.Header.Set("X-IVOAI-OpenCode-Message", fmt.Sprintf("msg_%d", count))
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		payload, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK || !bytes.Contains(payload, []byte(`"content":"bridge "`)) || !bytes.Contains(payload, []byte(`"content":"ok"`)) || !bytes.Contains(payload, []byte("[DONE]")) {
			t.Fatalf("status=%d payload=%q", response.StatusCode, payload)
		}
	}
	if len(runner.requests) != 2 || runner.requests[0].ExecutorSessionID != "" || runner.requests[1].ExecutorSessionID != "thread_fixture" {
		t.Fatalf("requests=%+v", runner.requests)
	}
	if len(mappings) != 2 || mappings[1].FrontendSessionID != "oc_fixture" || mappings[1].Executor != "codex" {
		t.Fatalf("mappings=%+v", mappings)
	}
}

func TestBridgeRejectsMissingCapabilityAndInvalidSession(t *testing.T) {
	bridge, err := Start(Options{Token: "fixture-capability", Runner: &fakeRunner{}, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{} }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close(context.Background()) })
	response, err := http.Get(bridge.URL() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.StatusCode)
	}

	request, _ := http.NewRequest(http.MethodPost, bridge.URL()+"/v1/chat/completions", strings.NewReader(`{"model":"auto","messages":[{"role":"user","content":"hello"}]}`))
	request.Header.Set("Authorization", "Bearer "+bridge.Token())
	request.Header.Set("X-IVOAI-OpenCode-Session", "bad/value")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d", response.StatusCode)
	}
}

func TestBridgeStatusIsBoundedMetadata(t *testing.T) {
	bridge, err := Start(Options{Token: "fixture-capability", Runner: &fakeRunner{}, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status {
		return Status{Version: "0.9.0", Servers: []ServerView{{ID: "srv_fixture", Alias: "company-a", Selected: true, Enabled: true, Health: "healthy"}}}
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bridge.Close(context.Background()) })
	request, _ := http.NewRequest(http.MethodGet, bridge.URL()+"/status", nil)
	request.Header.Set("Authorization", "Bearer "+bridge.Token())
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var status Status
	if json.NewDecoder(response.Body).Decode(&status) != nil || status.Servers[0].Alias != "company-a" || status.UpdatedAt.IsZero() {
		t.Fatalf("status=%+v", status)
	}
}

func TestBridgeNativeModelSelectionControlsOfficialExecutor(t *testing.T) {
	runner := &fakeRunner{result: ExecutorResult{ExecutorSessionID: "thread_explicit"}}
	selectCalls := 0
	catalog := newCatalog([]ModelSpec{{ID: "codex-fixture", Name: "Codex fixture", Mode: "explicit", Executor: "codex", UpstreamModel: "gpt-fixture", SupportedEfforts: []string{"low", "high"}}})
	bridge, err := Start(Options{
		Runner: runner, Catalog: catalog,
		Select: func(context.Context, string) (string, error) { selectCalls++; return "claude", nil },
		Status: func() Status { return Status{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())

	request, _ := http.NewRequest(http.MethodPost, bridge.URL()+"/v1/chat/completions", strings.NewReader(`{"model":"codex-fixture","reasoning_effort":"high","messages":[{"role":"user","content":"fixture"}]}`))
	request.Header.Set("Authorization", "Bearer "+bridge.Token())
	request.Header.Set("X-IVOAI-OpenCode-Session", "oc_explicit")
	request.Header.Set("X-IVOAI-OpenCode-Message", "msg_explicit")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || selectCalls != 0 || len(runner.requests) != 1 {
		t.Fatalf("status=%d select=%d requests=%+v", response.StatusCode, selectCalls, runner.requests)
	}
	got := runner.requests[0]
	if got.Executor != "codex" || got.Model != "gpt-fixture" || got.Effort != "high" || got.SelectionMode != "explicit" {
		t.Fatalf("explicit selection did not reach runner: %+v", got)
	}

	request, _ = http.NewRequest(http.MethodPost, bridge.URL()+"/v1/chat/completions", strings.NewReader(`{"model":"codex-fixture","reasoning_effort":"max","messages":[{"role":"user","content":"fixture"}]}`))
	request.Header.Set("Authorization", "Bearer "+bridge.Token())
	request.Header.Set("X-IVOAI-OpenCode-Session", "oc_explicit")
	request.Header.Set("X-IVOAI-OpenCode-Message", "msg_invalid_effort")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || len(runner.requests) != 1 {
		t.Fatalf("unsupported effort launched executor: status=%d requests=%+v", response.StatusCode, runner.requests)
	}
}

func TestBridgeModelEndpointMatchesManagedCatalog(t *testing.T) {
	catalog := newCatalog([]ModelSpec{{ID: "claude-default", Name: "Claude client default", Mode: "explicit", Executor: "claude", SupportedEfforts: []string{"high"}}})
	bridge, err := Start(Options{Runner: &fakeRunner{}, Catalog: catalog, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{} }})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	request, _ := http.NewRequest(http.MethodGet, bridge.URL()+"/v1/models", nil)
	request.Header.Set("Authorization", "Bearer "+bridge.Token())
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var value struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.NewDecoder(response.Body).Decode(&value) != nil || len(value.Data) != 2 || value.Data[0].ID != "auto" || value.Data[1].ID != "claude-default" {
		t.Fatalf("models endpoint drifted from catalog: %+v", value.Data)
	}
}

func TestScanJSONLinesIgnoresNonJSONAndBoundsTokens(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("noise\n{\"type\":\"ok\"}\n"))
	var values []map[string]any
	if err := ScanJSONLines(reader, func(value map[string]any) error { values = append(values, value); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0]["type"] != "ok" {
		t.Fatalf("values=%v", values)
	}
}

func TestScanJSONLinesRejectsCorruptionAfterProtocolStarts(t *testing.T) {
	value := "startup noise\n{\"type\":\"thread.started\"}\nnot-json\n"
	if err := ScanJSONLines(strings.NewReader(value), func(map[string]any) error { return nil }); err == nil {
		t.Fatal("malformed executor event stream was silently accepted")
	}
}

func TestExplicitSelectionFailsBeforeClaimOrLaunchWhenIneligible(t *testing.T) {
	runner := &fakeRunner{}
	claims := 0
	catalog := newCatalog([]ModelSpec{{ID: "codex-fixture", Name: "Codex fixture", Mode: "explicit", Executor: "codex", UpstreamModel: "gpt-fixture"}})
	bridge, err := Start(Options{Runner: runner, Catalog: catalog, Select: func(context.Context, string) (string, error) { return "claude", nil }, Status: func() Status { return Status{} }, AuthorizeSelection: func(context.Context, Selection) error { return errors.New("not authenticated") }, ClaimRequest: func(string, string) (bool, error) { claims++; return true, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	request, _ := http.NewRequest(http.MethodPost, bridge.URL()+"/v1/chat/completions", strings.NewReader(`{"model":"codex-fixture","messages":[{"role":"user","content":"fixture"}]}`))
	request.Header.Set("Authorization", "Bearer "+bridge.Token())
	request.Header.Set("X-IVOAI-OpenCode-Session", "oc_explicit")
	request.Header.Set("X-IVOAI-OpenCode-Message", "msg_explicit")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable || claims != 0 || len(runner.requests) != 0 || !bytes.Contains(payload, []byte("executor_selection_unavailable")) {
		t.Fatalf("selection was not fail-closed: status=%d claims=%d requests=%d payload=%s", response.StatusCode, claims, len(runner.requests), payload)
	}
}

func TestNonStreamingPartialOutputNeverTriggersDuplicateFailover(t *testing.T) {
	runner := &nonStreamingQuotaRunner{}
	bridge, err := Start(Options{Runner: runner, Select: func(_ context.Context, previous string) (string, error) {
		if previous == "codex" {
			return "claude", nil
		}
		return "codex", nil
	}, Monitor: func(context.Context, string) string { return "quota changed" }, Status: func() Status { return Status{} }})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	response := bridgeRequest(t, bridge, false)
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable || runner.calls.Load() != 1 {
		t.Fatalf("partial non-stream output was re-executed: status=%d calls=%d", response.StatusCode, runner.calls.Load())
	}
}

func TestExplicitModelChangeDoesNotResumeDifferentModelThread(t *testing.T) {
	runner := &fakeRunner{result: ExecutorResult{ExecutorSessionID: "thread_new"}}
	catalog := newCatalog([]ModelSpec{{ID: "codex-a", Name: "Codex A", Mode: "explicit", Executor: "codex", UpstreamModel: "model-a"}, {ID: "codex-b", Name: "Codex B", Mode: "explicit", Executor: "codex", UpstreamModel: "model-b"}})
	bridge, err := Start(Options{Runner: runner, Catalog: catalog, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{} }, LookupMapping: func(frontend string) []Mapping {
		return []Mapping{{FrontendSessionID: frontend, Executor: "codex", ExecutorSessionID: "thread_old", RequestedModel: "codex-a"}}
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	request, _ := http.NewRequest(http.MethodPost, bridge.URL()+"/v1/chat/completions", strings.NewReader(`{"model":"codex-b","messages":[{"role":"user","content":"fixture"}]}`))
	request.Header.Set("Authorization", "Bearer "+bridge.Token())
	request.Header.Set("X-IVOAI-OpenCode-Session", "oc_model_switch")
	request.Header.Set("X-IVOAI-OpenCode-Message", "msg_model_switch")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || len(runner.requests) != 1 || runner.requests[0].ExecutorSessionID != "" {
		t.Fatalf("model switch resumed stale thread: status=%d requests=%+v", response.StatusCode, runner.requests)
	}
}

func TestBridgeCloseIsBounded(t *testing.T) {
	bridge, err := Start(Options{Runner: &fakeRunner{}, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{} }})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := bridge.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestBridgeMessageIdempotencyAndSingleWriter(t *testing.T) {
	runner := &concurrencyRunner{}
	bridge, err := Start(Options{Runner: runner, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{} }})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	call := func(message string) int {
		body := `{"model":"auto","messages":[{"role":"user","content":"fixture"}]}`
		request, _ := http.NewRequest(http.MethodPost, bridge.URL()+"/v1/chat/completions", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+bridge.Token())
		request.Header.Set("X-IVOAI-OpenCode-Session", "oc_fixture")
		request.Header.Set("X-IVOAI-OpenCode-Message", message)
		response, requestErr := http.DefaultClient.Do(request)
		if requestErr != nil {
			t.Error(requestErr)
			return 0
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		return response.StatusCode
	}
	var wg sync.WaitGroup
	for _, message := range []string{"msg_one", "msg_two"} {
		wg.Add(1)
		go func(value string) {
			defer wg.Done()
			if status := call(value); status != http.StatusOK {
				t.Errorf("status=%d", status)
			}
		}(message)
	}
	wg.Wait()
	if status := call("msg_one"); status != http.StatusOK {
		t.Fatalf("idempotent replay status=%d", status)
	}
	if runner.calls.Load() != 2 || runner.max.Load() != 1 {
		t.Fatalf("calls=%d max-concurrency=%d", runner.calls.Load(), runner.max.Load())
	}
}

func TestBridgeStreamingFailureIsNotSuccessfulCompletion(t *testing.T) {
	runner := partialFailureRunner{}
	bridge, err := Start(Options{Runner: runner, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{} }})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	response := bridgeRequest(t, bridge, true)
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if !bytes.Contains(body, []byte(`"error"`)) || !bytes.Contains(body, []byte(`"code":"executor_stream_incomplete"`)) || !bytes.Contains(body, []byte("untrusted partial output")) || bytes.Contains(body, []byte("[DONE]")) || bytes.Contains(body, []byte(`"finish_reason":"stop"`)) {
		t.Fatalf("stream failure was presented as success: %s", body)
	}
}

func TestScanJSONLinesRejectsOversizedOutputAndDrains(t *testing.T) {
	for _, value := range []string{strings.Repeat("x", (1<<20)+1) + "\n", strings.Repeat("{\"type\":\"ok\"}\n", 600000)} {
		if err := ScanJSONLines(strings.NewReader(value), func(map[string]any) error { return nil }); err == nil {
			t.Fatal("oversized executor output accepted")
		}
	}
}

func TestBridgeRestoresPersistedMappingWithoutCredentialState(t *testing.T) {
	runner := &fakeRunner{result: ExecutorResult{ExecutorSessionID: "thread_next"}}
	bridge, err := Start(Options{
		Runner: runner, Select: func(_ context.Context, previous string) (string, error) { return previous, nil }, Status: func() Status { return Status{} },
		LookupMapping: func(frontendID string) []Mapping {
			return []Mapping{{FrontendSessionID: frontendID, Executor: "codex", ExecutorSessionID: "thread_previous"}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	response := bridgeRequest(t, bridge, false)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || len(runner.requests) != 1 || runner.requests[0].ExecutorSessionID != "thread_previous" {
		t.Fatalf("status=%d requests=%+v", response.StatusCode, runner.requests)
	}
}

func TestBridgeSerializesDifferentFrontendSessions(t *testing.T) {
	runner := &concurrencyRunner{}
	bridge, err := Start(Options{Runner: runner, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{} }})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	call := func(frontend, message string) {
		body := `{"model":"auto","messages":[{"role":"user","content":"fixture"}]}`
		request, _ := http.NewRequest(http.MethodPost, bridge.URL()+"/v1/chat/completions", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+bridge.Token())
		request.Header.Set("X-IVOAI-OpenCode-Session", frontend)
		request.Header.Set("X-IVOAI-OpenCode-Message", message)
		response, requestErr := http.DefaultClient.Do(request)
		if requestErr != nil {
			t.Error(requestErr)
			return
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}
	var wg sync.WaitGroup
	for index, frontend := range []string{"oc_one", "oc_two"} {
		wg.Add(1)
		go func(id string, number int) { defer wg.Done(); call(id, fmt.Sprintf("msg_%d", number)) }(frontend, index)
	}
	wg.Wait()
	if runner.calls.Load() != 2 || runner.max.Load() != 1 {
		t.Fatalf("calls=%d max=%d", runner.calls.Load(), runner.max.Load())
	}
}

func TestBridgeRetainsSeparateExecutorSessionsAcrossFailover(t *testing.T) {
	runner := &fakeRunner{}
	selectCount := 0
	bridge, err := Start(Options{
		Runner: runner,
		Select: func(_ context.Context, previous string) (string, error) {
			selectCount++
			if selectCount == 1 || selectCount == 3 {
				return "codex", nil
			}
			return "claude", nil
		},
		Status: func() Status { return Status{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	for index := 0; index < 3; index++ {
		if index == 0 || index == 2 {
			runner.result.ExecutorSessionID = "thread_codex"
		} else {
			runner.result.ExecutorSessionID = "thread_claude"
		}
		body := `{"model":"auto","messages":[{"role":"user","content":"fixture"}]}`
		request, _ := http.NewRequest(http.MethodPost, bridge.URL()+"/v1/chat/completions", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+bridge.Token())
		request.Header.Set("X-IVOAI-OpenCode-Session", "oc_fixture")
		request.Header.Set("X-IVOAI-OpenCode-Message", fmt.Sprintf("msg_%d", index))
		response, requestErr := http.DefaultClient.Do(request)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.requests) != 3 || runner.requests[2].Executor != "codex" || runner.requests[2].ExecutorSessionID != "thread_codex" {
		t.Fatalf("requests=%+v", runner.requests)
	}
}

func TestBridgePersistentClaimRefusesReplayAfterRestart(t *testing.T) {
	claims := map[string]bool{}
	claim := func(frontendID, messageID string) (bool, error) {
		key := frontendID + ":" + messageID
		if claims[key] {
			return false, nil
		}
		claims[key] = true
		return true, nil
	}
	runner := &fakeRunner{result: ExecutorResult{ExecutorSessionID: "thread_fixture"}}
	start := func() *Bridge {
		bridge, err := Start(Options{Runner: runner, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{} }, ClaimRequest: claim})
		if err != nil {
			t.Fatal(err)
		}
		return bridge
	}
	first := start()
	response := bridgeRequest(t, first, false)
	_ = response.Body.Close()
	_ = first.Close(context.Background())
	second := start()
	defer second.Close(context.Background())
	response = bridgeRequest(t, second, false)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusConflict || len(runner.requests) != 1 {
		t.Fatalf("status=%d requests=%d", response.StatusCode, len(runner.requests))
	}
}

func TestBridgeRejectsUnsupportedInputPartsInsteadOfDroppingThem(t *testing.T) {
	bridge, err := Start(Options{Runner: &fakeRunner{}, Select: func(context.Context, string) (string, error) { return "codex", nil }, Status: func() Status { return Status{} }})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	body := `{"model":"auto","messages":[{"role":"user","content":[{"type":"text","text":"inspect"},{"type":"image_url","image_url":{"url":"data:image/png;base64,fixture"}}]}]}`
	request, _ := http.NewRequest(http.MethodPost, bridge.URL()+"/v1/chat/completions", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+bridge.Token())
	request.Header.Set("X-IVOAI-OpenCode-Session", "oc_fixture")
	request.Header.Set("X-IVOAI-OpenCode-Message", "msg_fixture")
	response, requestErr := http.DefaultClient.Do(request)
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d", response.StatusCode)
	}
}

func TestBridgeQuotaFailoverCancelsOneExecutorAndContinues(t *testing.T) {
	runner := &blockingRunner{}
	var monitorCalls atomic.Int32
	bridge, err := Start(Options{
		Runner: runner,
		Select: func(_ context.Context, previous string) (string, error) {
			if previous == "codex" {
				return "claude", nil
			}
			return "codex", nil
		},
		Monitor: func(ctx context.Context, executor string) string {
			monitorCalls.Add(1)
			if executor == "codex" {
				return "fixture quota exhausted"
			}
			<-ctx.Done()
			return ""
		},
		FailoverHandoff: func(from, to, reason string) string { return from + " to " + to + ": " + reason },
		Status:          func() Status { return Status{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	response := bridgeRequest(t, bridge, false)
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("continued")) {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.requests) != 2 || runner.requests[0].Executor != "codex" || runner.requests[1].Executor != "claude" || !strings.Contains(runner.requests[1].Prompt, "fixture quota exhausted") || monitorCalls.Load() < 2 {
		t.Fatalf("requests=%+v monitor=%d", runner.requests, monitorCalls.Load())
	}
}

func TestBridgeQuotaAndExecutorErrorRaceDoesNotDeadlock(t *testing.T) {
	bridge, err := Start(Options{
		Runner: &fakeRunner{err: errors.New("fixture executor failure")},
		Select: func(_ context.Context, previous string) (string, error) {
			if previous == "codex" {
				return "claude", nil
			}
			return "codex", nil
		},
		Monitor: func(context.Context, string) string { return "fixture quota" },
		Status:  func() Status { return Status{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close(context.Background())
	done := make(chan int, 1)
	go func() {
		response := bridgeRequest(t, bridge, false)
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		done <- response.StatusCode
	}()
	select {
	case status := <-done:
		if status == http.StatusOK {
			t.Fatalf("unexpected success status=%d", status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("quota/error race deadlocked")
	}
}

func bridgeRequest(t *testing.T, bridge *Bridge, stream bool) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"model": "auto", "stream": stream, "messages": []map[string]string{{"role": "user", "content": "fixture"}}})
	request, _ := http.NewRequest(http.MethodPost, bridge.URL()+"/v1/chat/completions", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+bridge.Token())
	request.Header.Set("X-IVOAI-OpenCode-Session", "oc_fixture")
	request.Header.Set("X-IVOAI-OpenCode-Message", "msg_fixture")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
