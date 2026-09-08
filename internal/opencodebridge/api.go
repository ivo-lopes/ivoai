package opencodebridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxAPIResponse = 8 << 20

// APIRequest invokes only documented, instance-local OpenCode operations. It
// never accepts an arbitrary destination or exposes the managed password.
// Session execution still belongs to IVOAI; these are frontend lifecycle APIs.
func (m *Managed) APIRequest(ctx context.Context, operation, sessionID string, query url.Values, payload any) (json.RawMessage, error) {
	method, path := "GET", ""
	switch operation {
	case "sessions":
		path = "/session"
	case "create":
		method, path = "POST", "/session"
	case "status":
		path = "/session/status"
	case "agents":
		path = "/agent"
	case "mcp":
		path = "/mcp"
	case "files":
		path = "/file"
	case "file-content":
		path = "/file/content"
	case "permissions":
		path = "/permission"
	case "permission-reply":
		if !safeID(sessionID) || sessionID == "." || sessionID == ".." {
			return nil, errors.New("invalid OpenCode permission identifier")
		}
		method, path = "POST", "/permission/"+sessionID+"/reply"
	case "get", "prompt", "prompt-async", "abort", "diff":
		if !safeID(sessionID) || sessionID == "." || sessionID == ".." {
			return nil, errors.New("invalid OpenCode session identifier")
		}
		path = "/session/" + sessionID
		switch operation {
		case "prompt":
			method, path = "POST", path+"/message"
		case "prompt-async":
			method, path = "POST", path+"/prompt_async"
		case "abort":
			method, path = "POST", path+"/abort"
		case "diff":
			path += "/diff"
		}
	default:
		return nil, errors.New("unsupported managed OpenCode operation")
	}
	for key := range query {
		if key != "path" && key != "limit" && key != "messageID" {
			return nil, errors.New("unsupported OpenCode query field")
		}
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	body, err := json.Marshal(payload)
	if err != nil || len(body) > maxAPIResponse {
		return nil, errors.New("invalid or oversized OpenCode API payload")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var input io.Reader
	if method != http.MethodGet {
		input = bytes.NewReader(body)
	}
	request, err := m.apiRequest(ctx, method, path, input)
	if err != nil {
		return nil, err
	}
	response, err := privateAPIClient().Do(request)
	if err != nil {
		return nil, errors.New("managed OpenCode API transport failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("managed OpenCode API HTTP %d", response.StatusCode)
	}
	if response.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return nil, errors.New("invalid OpenCode API response media type")
	}
	result, err := io.ReadAll(io.LimitReader(response.Body, maxAPIResponse+1))
	if err != nil || len(result) > maxAPIResponse || !json.Valid(result) {
		return nil, errors.New("invalid or oversized OpenCode API response")
	}
	return result, nil
}

func privateAPIClient() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (m *Managed) apiRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	endpoint, err := url.Parse(m.URL)
	if err != nil || endpoint.Scheme != "http" || endpoint.Hostname() != "127.0.0.1" || endpoint.User != nil || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("OpenCode API must be private loopback")
	}
	request, err := http.NewRequestWithContext(ctx, method, m.URL+path, body)
	if err != nil {
		return nil, errors.New("invalid OpenCode API request")
	}
	request.SetBasicAuth("ivoai", m.password)
	request.Header.Set("Content-Type", "application/json")
	return request, nil
}

// Events consumes bounded JSON SSE events until cancellation or stream closure.
// It does not persist payloads. The caller owns event interpretation and redaction.
func (m *Managed) Events(ctx context.Context, consume func(json.RawMessage) error) error {
	if consume == nil {
		return errors.New("OpenCode event consumer is required")
	}
	request, err := m.apiRequest(ctx, "GET", "/event", nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "text/event-stream")
	response, err := privateAPIClient().Do(request)
	if err != nil {
		return errors.New("managed OpenCode event transport failed")
	}
	defer response.Body.Close()
	media, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != 200 || media != "text/event-stream" {
		return errors.New("invalid OpenCode event response")
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var data []byte
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" && len(data) > 0 {
			if !json.Valid(data) {
				return errors.New("invalid OpenCode event JSON")
			}
			if err := consume(json.RawMessage(data)); err != nil {
				return err
			}
			data = nil
		} else if strings.HasPrefix(line, "data:") {
			if len(data) > 0 {
				data = append(data, '\n')
			}
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")...)
			if len(data) > 1<<20 {
				return errors.New("OpenCode event exceeded limit")
			}
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if scanner.Err() != nil {
		return errors.New("OpenCode event stream failed")
	}
	return io.ErrUnexpectedEOF
}
