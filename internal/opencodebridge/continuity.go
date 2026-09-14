package opencodebridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"
)

// SubmitPrompt uses the pinned native asynchronous session API. It traverses
// the same IVOAI provider admission gate as composer input. It is not a worker
// or tool invocation, and the native conversation remains the history owner.
func (m *Managed) SubmitPrompt(ctx context.Context, id, directory, prompt string) error {
	if id != "" && !safeID(id) || len(prompt) == 0 || len(prompt) > 12<<10 {
		return errors.New("INVALID_CONTINUATION_INPUT")
	}
	if id == "" {
		created, err := m.continuationRequest(ctx, "/session?directory="+url.QueryEscape(directory), []byte("{}"))
		if err != nil {
			return err
		}
		var native struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(created, &native) != nil || !safeID(native.ID) {
			return errors.New("INVALID_NATIVE_SESSION_RESPONSE")
		}
		id = native.ID
		m.AttachArgs = append(m.AttachArgs, "--session", id)
	}
	body, err := json.Marshal(map[string]any{"parts": []map[string]string{{"type": "text", "text": prompt}}})
	if err != nil {
		return err
	}
	_, err = m.continuationRequest(ctx, "/session/"+url.PathEscape(id)+"/prompt_async?directory="+url.QueryEscape(directory), body)
	return err
}

func (m *Managed) continuationRequest(ctx context.Context, path string, body []byte) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, m.URL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.SetBasicAuth("ivoai", m.password)
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("NATIVE_CONTINUATION_SUBMISSION_FAILED")
	}
	defer response.Body.Close()
	result, err := io.ReadAll(io.LimitReader(response.Body, 8193))
	if err != nil || len(result) > 8192 {
		return nil, errors.New("INVALID_NATIVE_CONTINUATION_RESPONSE")
	}
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusOK {
		return nil, errors.New("NATIVE_CONTINUATION_SUBMISSION_REJECTED")
	}
	return result, nil
}
