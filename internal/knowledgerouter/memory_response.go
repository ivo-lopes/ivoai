package knowledgerouter

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"strings"
	"unicode/utf8"
)

// Bounded duplicate-aware parsing avoids different readers choosing different
// values for an ID, error, tool result or nested machine-readable flag.
func uniqueJSON(body []byte) (any, error) {
	if len(body) == 0 || len(body) > maxMemoryPageBytes || !utf8.Valid(body) || bytes.ContainsRune(body, '\x00') {
		return nil, ErrMemoryPageResponse
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	nodes := 0
	var read func(int) (any, error)
	read = func(depth int) (any, error) {
		nodes++
		if depth > 32 || nodes > 8192 {
			return nil, ErrMemoryPageResponse
		}
		t, err := d.Token()
		if err != nil {
			return nil, ErrMemoryPageResponse
		}
		if delimiter, ok := t.(json.Delim); ok {
			switch delimiter {
			case '{':
				object := map[string]any{}
				for d.More() {
					key, err := d.Token()
					name, ok := key.(string)
					if err != nil || !ok {
						return nil, ErrMemoryPageResponse
					}
					if _, duplicate := object[name]; duplicate {
						return nil, ErrMemoryPageResponse
					}
					value, err := read(depth + 1)
					if err != nil {
						return nil, err
					}
					object[name] = value
				}
				end, err := d.Token()
				if err != nil || end != json.Delim('}') {
					return nil, ErrMemoryPageResponse
				}
				return object, nil
			case '[':
				array := []any{}
				for d.More() {
					value, err := read(depth + 1)
					if err != nil {
						return nil, err
					}
					array = append(array, value)
				}
				end, err := d.Token()
				if err != nil || end != json.Delim(']') {
					return nil, ErrMemoryPageResponse
				}
				return array, nil
			default:
				return nil, ErrMemoryPageResponse
			}
		}
		return t, nil
	}
	value, err := read(0)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, ErrMemoryPageResponse
	}
	return value, nil
}

func strictMemoryPageResponse(response upstreamResponse, expectedID string) (string, string, error) {
	if response.status != 200 || len(response.body) > maxMemoryPageBytes || len(response.header.Values(mcpSessionHeader)) != 0 || len(response.header.Values("Content-Range")) != 0 || len(response.header.Values("Content-Type")) != 1 {
		return "", "", ErrMemoryPageResponse
	}
	media, parameters, err := mime.ParseMediaType(response.header.Get("Content-Type"))
	if err != nil {
		return "", "", ErrMemoryPageResponse
	}
	for name, value := range parameters {
		if name != "charset" || !strings.EqualFold(value, "utf-8") {
			return "", "", ErrMemoryPageResponse
		}
	}
	payload := response.body
	switch media {
	case "application/json":
	case "text/event-stream":
		payload, err = singleMemoryEvent(payload)
		if err != nil {
			return "", "", ErrMemoryPageResponse
		}
	default:
		return "", "", ErrMemoryPageResponse
	}
	value, err := uniqueJSON(payload)
	envelope, ok := value.(map[string]any)
	if err != nil || !ok || len(envelope) != 3 || envelope["jsonrpc"] != "2.0" || envelope["id"] != expectedID {
		return "", "", ErrMemoryPageResponse
	}
	if _, hasError := envelope["error"]; hasError {
		return "", "", ErrMemoryPageResponse
	}
	result, ok := envelope["result"].(map[string]any)
	if !ok {
		return "", "", ErrMemoryPageResponse
	}
	for key := range result {
		if key != "content" && key != "structuredContent" && key != "isError" && key != "_meta" {
			return "", "", ErrMemoryPageResponse
		}
	}
	if flag, present := result["isError"]; present && flag != false {
		return "", "", ErrMemoryPageResponse
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) != 1 {
		return "", "", ErrMemoryPageResponse
	}
	text, ok := content[0].(map[string]any)
	if !ok || text["type"] != "text" {
		return "", "", ErrMemoryPageResponse
	}
	for key := range text {
		if key != "type" && key != "text" && key != "annotations" && key != "_meta" {
			return "", "", ErrMemoryPageResponse
		}
	}
	if body, ok := text["text"].(string); !ok || strings.TrimSpace(body) == "" || strings.ContainsAny(body, "\x00\x1b") {
		return "", "", ErrMemoryPageResponse
	}
	for _, name := range []string{"structuredContent", "_meta"} {
		if value, present := result[name]; present {
			if _, ok := value.(map[string]any); !ok {
				return "", "", ErrMemoryPageResponse
			}
		}
	}
	if hasIncompleteFlag(result) {
		return "", "", ErrMemoryPageResponse
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(payload, &raw) != nil {
		return "", "", ErrMemoryPageResponse
	}
	sum := sha256.Sum256(raw["result"])
	return hex.EncodeToString(sum[:]), media, nil
}

func hasIncompleteFlag(value any) bool {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if strings.EqualFold(key, "partial") || strings.EqualFold(key, "truncated") || strings.EqualFold(key, "isError") {
				if child != false {
					return true
				}
			}
			if hasIncompleteFlag(child) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if hasIncompleteFlag(child) {
				return true
			}
		}
	}
	return false
}

// Parse the entire finite response, not the first usable event. This slice
// accepts LF/CRLF message events and bounded comments/opaque event IDs, without
// resumption, server requests, notifications or implicit retry instructions.
func singleMemoryEvent(body []byte) ([]byte, error) {
	if !utf8.Valid(body) || len(body) > maxMemoryPageBytes || bytes.ContainsRune(body, '\x00') {
		return nil, ErrMemoryPageResponse
	}
	normalized := bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n"))
	if bytes.ContainsRune(normalized, '\r') || !bytes.HasSuffix(normalized, []byte("\n\n")) {
		return nil, ErrMemoryPageResponse
	}
	frames := bytes.Split(normalized, []byte("\n\n"))
	if len(frames) > 64 {
		return nil, ErrMemoryPageResponse
	}
	var payload []byte
	for _, frame := range frames {
		var data [][]byte
		seenEvent, seenID := false, false
		for _, line := range bytes.Split(frame, []byte("\n")) {
			if len(line) == 0 || line[0] == ':' {
				continue
			}
			name, value, found := bytes.Cut(line, []byte(":"))
			if !found {
				return nil, ErrMemoryPageResponse
			}
			value = bytes.TrimPrefix(value, []byte(" "))
			switch string(name) {
			case "data":
				data = append(data, value)
			case "event":
				if seenEvent || !bytes.Equal(value, []byte("message")) {
					return nil, ErrMemoryPageResponse
				}
				seenEvent = true
			case "id":
				if seenID || len(value) > 128 {
					return nil, ErrMemoryPageResponse
				}
				seenID = true
			default:
				return nil, ErrMemoryPageResponse
			}
		}
		if len(data) > 0 {
			if payload != nil {
				return nil, ErrMemoryPageResponse
			}
			payload = bytes.Join(data, []byte("\n"))
			if len(bytes.TrimSpace(payload)) == 0 {
				return nil, ErrMemoryPageResponse
			}
		} else if seenEvent || seenID {
			return nil, ErrMemoryPageResponse
		}
	}
	if payload == nil {
		return nil, ErrMemoryPageResponse
	}
	return payload, nil
}
