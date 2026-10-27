package opencodebridge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBridgeIntakeRejectsBeforeSelectionAndExecution(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "sse"}[stream], func(t *testing.T) {
			runner := &fakeRunner{}
			bridge, err := Start(Options{
				RequirePromptGate: true, Runner: runner, Status: func() Status { return Status{} },
				Select: func(context.Context, string) (string, error) {
					t.Error("selector ran before gate")
					return "codex", nil
				},
				ClaimRequest: func(string, string) (bool, error) { t.Error("claimed refused prompt"); return true, nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			defer bridge.Close(context.Background())
			body, _ := json.Marshal(map[string]any{"model": "auto", "stream": stream, "messages": []map[string]string{{"role": "user", "content": "Leia VERSION\x00"}}})
			req, _ := http.NewRequest(http.MethodPost, bridge.URL()+"/v1/chat/completions", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+bridge.Token())
			req.Header.Set("X-IVOAI-OpenCode-Session", "session_gate")
			req.Header.Set("X-IVOAI-OpenCode-Message", "message_gate")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if res.StatusCode != http.StatusOK || !strings.Contains(string(data), "waiting_for_refinement") {
				t.Fatalf("intake: %d %s", res.StatusCode, data)
			}
			runner.mu.Lock()
			count := len(runner.requests)
			runner.mu.Unlock()
			if count != 0 {
				t.Fatal("executor ran for insufficient prompt")
			}
		})
	}
}

func TestBridgeIntakeAcceptsInformalTextWithoutAcceptanceTemplate(t *testing.T) {
	for _, prompt := range []string{"Qual versão deste projeto?", "corrija isso"} {
		t.Run(prompt, func(t *testing.T) {
			runner := &fakeRunner{result: ExecutorResult{ExecutorSessionID: "thread_intake"}}
			bridge, err := Start(Options{RequirePromptGate: true, Runner: runner,
				Status: func() Status { return Status{} },
				Select: func(context.Context, string) (string, error) { return "codex", nil },
			})
			if err != nil {
				t.Fatal(err)
			}
			defer bridge.Close(context.Background())
			body, _ := json.Marshal(map[string]any{"model": "auto", "messages": []map[string]string{{"role": "user", "content": prompt}}})
			req, _ := http.NewRequest(http.MethodPost, bridge.URL()+"/turn", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+bridge.Token())
			req.Header.Set("X-IVOAI-Session", "session_intake")
			req.Header.Set("X-IVOAI-Message", "message_intake")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if res.StatusCode != http.StatusOK || strings.Contains(string(data), "waiting_for_refinement") {
				t.Fatalf("informal text rejected: %d %s", res.StatusCode, data)
			}
			runner.mu.Lock()
			defer runner.mu.Unlock()
			if len(runner.requests) != 1 || runner.requests[0].Prompt != prompt {
				t.Fatal("intake did not preserve the user's request")
			}
		})
	}
}
