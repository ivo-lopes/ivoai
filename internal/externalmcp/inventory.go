package externalmcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type inventoryRequestKey struct{}

func writeDenied(w http.ResponseWriter, id json.RawMessage, reason string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": reason}}}})
}

// tools/list is finite JSON or finite SSE. No unauthorized schema is projected.
// Unknown encodings and oversized inventories fail closed, never pass through.
func filterInventory(response *http.Response, allowed map[string]bool) error {
	body, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	response.Body.Close()
	if err != nil || len(body) > 4<<20 {
		return errors.New("MCP inventory exceeds projection budget")
	}
	filter := func(data []byte) ([]byte, error) {
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(data, &envelope); err != nil {
			return nil, err
		}
		if envelope["result"] == nil {
			return data, nil
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(envelope["result"], &result); err != nil {
			return nil, err
		}
		var tools []json.RawMessage
		if err := json.Unmarshal(result["tools"], &tools); err != nil {
			return nil, err
		}
		if len(tools) > MaxInventoryTools {
			return nil, errors.New("MCP inventory exceeds tool limit")
		}
		selected := []json.RawMessage{}
		for _, tool := range tools {
			var identity struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(tool, &identity); err != nil {
				return nil, err
			}
			if allowed[identity.Name] {
				selected = append(selected, tool)
			}
		}
		result["tools"], _ = json.Marshal(selected)
		// A paginated inventory cannot certify the exact set.
		if cursor := result["nextCursor"]; len(cursor) != 0 && string(cursor) != `""` && string(cursor) != "null" {
			return nil, errors.New("incomplete MCP inventory")
		}
		envelope["result"], _ = json.Marshal(result)
		return json.Marshal(envelope)
	}
	if strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		body, err = filter(body)
	} else if strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		var output bytes.Buffer
		for _, event := range bytes.Split(bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n")), []byte("\n\n")) {
			if len(bytes.TrimSpace(event)) == 0 {
				continue
			}
			var data []byte
			for _, line := range bytes.Split(event, []byte("\n")) {
				if bytes.HasPrefix(line, []byte("data:")) {
					data = append(data, bytes.TrimPrefix(line[5:], []byte(" "))...)
					data = append(data, '\n')
				} else {
					output.Write(line)
					output.WriteByte('\n')
				}
			}
			if len(data) != 0 {
				var filtered []byte
				filtered, err = filter(data)
				if err != nil {
					break
				}
				output.WriteString("data: ")
				output.Write(filtered)
				output.WriteByte('\n')
			}
			output.WriteByte('\n')
		}
		body = output.Bytes()
	} else {
		err = errors.New("unsupported MCP inventory encoding")
	}
	if err != nil {
		return err
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	response.Header.Set("Content-Length", strconv.Itoa(len(body)))
	return nil
}
