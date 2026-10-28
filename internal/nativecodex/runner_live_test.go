//go:build linux

package nativecodex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
)

func officialBinary(t *testing.T) string {
	t.Helper()
	binary := os.Getenv("IVOAI_LIVE_CODEX_PATH")
	if binary == "" {
		t.Skip("NOT_RUN: explicit official Codex 0.164.0 path; local provider only")
	}
	real, err := filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func TestLiveOfficialPrimaryEvidenceViewAndOwnedResume(t *testing.T) {
	runLiveEvidence(t, false)
}

func TestLiveOfficialPrimaryViewThenExplicitExact(t *testing.T) {
	runLiveEvidence(t, true)
}

func runLiveEvidence(t *testing.T, explicitExact bool) {
	binary := officialBinary(t)
	f := fixtureEvidence(t)
	options, request := runnerOptions(t, f, binary)
	request.Model = "gpt-6.1-sol"
	var requests, views, exactCalls, exactResponses atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
		r.Body.Close()
		if err != nil {
			w.WriteHeader(413)
			return
		}
		n := requests.Add(1)
		containsRaw := strings.Contains(string(raw), "PRIVATE_RAW_SENTINEL")
		if containsRaw && (!explicitExact || exactCalls.Load() == 0) {
			t.Error("original raw evidence reached model request")
		}
		if containsRaw {
			exactResponses.Add(1)
		}
		var body struct {
			Input []struct {
				Type   string          `json:"type"`
				Output json.RawMessage `json:"output"`
			} `json:"input"`
		}
		if json.Unmarshal(raw, &body) != nil {
			t.Error("invalid local provider request")
		}
		for _, item := range body.Input {
			if item.Type == "function_call_output" && strings.Contains(string(item.Output), "VIEW_ONLY") && strings.Contains(string(item.Output), f.binding.Ref.Result.Artifact.ID) {
				views.Add(1)
				break
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(v any) { data, _ := json.Marshal(v); _, _ = fmt.Fprintf(w, "data: %s\n\n", data) }
		responseID := fmt.Sprintf("resp_native_evidence_%d", n)
		emit(map[string]any{"type": "response.created", "response": map[string]any{"id": responseID}})
		var item any
		toolCall := n%2 == 1
		representation := "view"
		if explicitExact {
			toolCall = n < 3
			if n == 2 {
				representation = "exact"
				exactCalls.Add(1)
			}
		}
		if toolCall {
			arguments, _ := json.Marshal(map[string]string{"evidence_id": f.registry.IDs()[0], "representation": representation})
			item = map[string]any{"id": fmt.Sprintf("fc_evidence_%d", n), "type": "function_call", "name": toolName, "call_id": fmt.Sprintf("call_evidence_%d", n), "arguments": string(arguments)}
		} else {
			item = map[string]any{"id": fmt.Sprintf("answer_evidence_%d", n), "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "SYNTHETIC_PRIMARY_FINISHED"}}}
		}
		emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
		emit(map[string]any{"type": "response.completed", "response": map[string]any{"id": responseID, "status": "completed"}})
	}))
	defer server.Close()
	options.SyntheticProviderURL = server.URL + "/v1"
	runner := New(options)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	var first core.ExecutorResult
	turns := 2
	if explicitExact {
		turns = 1
	}
	for turn := 0; turn < turns; turn++ {
		if turn == 1 {
			request.ExecutorSessionID = first.ExecutorSessionID
		}
		text := ""
		result, err := runner.Run(ctx, request, func(part string) error { text += part; return nil })
		if err != nil || text != "SYNTHETIC_PRIMARY_FINISHED" || result.Trace == nil || !result.Trace.FinalResponse || !result.Trace.Completion || result.Trace.ExitCode != 0 || result.Trace.Version != clientVersion || result.Trace.PID == 0 {
			t.Fatalf("official primary turn %d: text=%q result=%+v error=%v", turn, text, result, err)
		}
		if turn == 0 {
			first = result
		} else if result.ExecutorSessionID != first.ExecutorSessionID {
			t.Fatal("owned resume changed native thread identity")
		}
		encoded, _ := json.Marshal(result.Trace)
		if strings.Contains(string(encoded), "PRIVATE_RAW") || strings.Contains(string(encoded), "VIEW_ONLY") || strings.Contains(string(encoded), f.binding.Ref.Result.Artifact.ID) || strings.Contains(string(encoded), `"usage"`) {
			t.Fatal("trace contains payload or fabricated usage")
		}
	}
	if explicitExact {
		if requests.Load() != 3 || views.Load() < 1 || f.viewCalls.Load() != 1 || exactCalls.Load() != 1 || exactResponses.Load() != 1 {
			t.Fatalf("explicit exact protocol counts: requests=%d views=%d transforms=%d exact_calls=%d raw_requests=%d", requests.Load(), views.Load(), f.viewCalls.Load(), exactCalls.Load(), exactResponses.Load())
		}
		t.Logf("official Codex %s binary_sha256=%s: view then explicit exact callback; original raw supplied only after exact request; three synthetic local requests, no inference", clientVersion, options.SHA256)
		return
	}
	if requests.Load() != 4 || views.Load() < 2 || f.viewCalls.Load() != 2 {
		t.Fatalf("unexpected protocol counts requests=%d requests_with_view=%d transforms=%d", requests.Load(), views.Load(), f.viewCalls.Load())
	}
	t.Logf("official Codex %s binary_sha256=%s: two turns/four synthetic local requests; raw never supplied, transformed view and opaque artifact ref supplied, actual owned thread resumed; no subscription inference or TUI claim", clientVersion, options.SHA256)
}

func TestLiveOfficialPrimaryPendingCallbackCancellation(t *testing.T) {
	binary := officialBinary(t)
	f := fixtureEvidence(t)
	entered := make(chan struct{})
	b := f.binding
	b.ViewProvider = viewFunc(func(ctx context.Context, _ string, _ []byte) ([]byte, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	var err error
	f.registry, err = NewRegistry(context.Background(), f.owner, []Binding{b})
	if err != nil {
		t.Fatal(err)
	}
	options, request := runnerOptions(t, f, binary)
	request.Model = "gpt-6.1-sol"
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, http.MaxBytesReader(w, r.Body, 2<<20))
		r.Body.Close()
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		args, _ := json.Marshal(map[string]string{"evidence_id": f.registry.IDs()[0], "representation": "view"})
		for _, event := range []any{map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_pending"}}, map[string]any{"type": "response.output_item.done", "item": map[string]any{"id": "fc_pending", "type": "function_call", "name": toolName, "call_id": "call_pending", "arguments": string(args)}}, map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_pending", "status": "completed"}}} {
			raw, _ := json.Marshal(event)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
		}
	}))
	defer server.Close()
	options.SyntheticProviderURL = server.URL + "/v1"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := New(options).Run(ctx, request, func(string) error { t.Error("pending callback emitted final"); return nil })
		done <- err
	}()
	select {
	case <-entered:
		cancel()
	case err := <-done:
		t.Fatalf("official runner stopped before callback: %v", err)
	case <-time.After(20 * time.Second):
		t.Fatal("official dynamic callback unavailable")
	}
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("official parent cancellation lost: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("official child did not stop")
	}
	if requests.Load() != 1 {
		t.Fatal("canceled callback reached another model request")
	}
}

func TestBinaryIdentityFixtureUsesStreamingDigest(t *testing.T) {
	// Keep the helper's supplied identity rule explicit without retaining any
	// credential/config contents. Official integration tests log this digest.
	binary := protocolBinary(t, "success")
	file, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		t.Fatal(err)
	}
	if err := verifyBinary(context.Background(), binary, hex.EncodeToString(h.Sum(nil))); err != nil {
		t.Fatal(err)
	}
}
