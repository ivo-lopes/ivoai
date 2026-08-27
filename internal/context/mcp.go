package context

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/ivo-lopes/ivoai/internal/core"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCPHandler exposes only read-only context tools. Connector mutation and
// ingestion are deliberately absent from the agent-facing surface.
type MCPHandler struct{ Service core.ContextBackend }

func (h MCPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32600, Message: "POST required"}})
		return
	}
	var request rpcRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.JSONRPC != "2.0" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: request.ID, Error: &rpcError{Code: -32600, Message: "invalid JSON-RPC request"}})
		return
	}
	result, err := h.dispatch(r, request)
	response := rpcResponse{JSONRPC: "2.0", ID: request.ID, Result: result}
	if err != nil {
		response.Result = nil
		response.Error = &rpcError{Code: -32602, Message: err.Error()}
	}
	_ = json.NewEncoder(w).Encode(response)
}

func (h MCPHandler) dispatch(r *http.Request, request rpcRequest) (any, error) {
	if h.Service == nil {
		return nil, errors.New("context service unavailable")
	}
	if request.Method == "initialize" {
		return map[string]any{"protocolVersion": "2025-03-26", "serverInfo": map[string]string{"name": "ivoai-context", "version": "1"}, "capabilities": map[string]any{"tools": map[string]any{}}}, nil
	}
	if request.Method == "tools/list" {
		return map[string]any{"tools": toolDefinitions()}, nil
	}
	if request.Method != "tools/call" {
		return nil, errors.New("method not found")
	}
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return nil, errors.New("invalid tool parameters")
	}
	var output any
	switch params.Name {
	case "context_search":
		var args struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, errors.New("invalid search arguments")
		}
		results, err := h.Service.Search(r.Context(), args.Query, args.Limit)
		if err != nil {
			return nil, err
		}
		output = map[string]any{"untrusted": true, "results": results}
	case "context_get_document":
		var args struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil || args.ID == "" {
			return nil, errors.New("document id is required")
		}
		doc, found, err := h.Service.GetDocument(args.ID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, errors.New("document not found")
		}
		output = map[string]any{"untrusted": true, "document": doc}
	case "context_recent":
		var args struct {
			Limit int `json:"limit"`
		}
		_ = json.Unmarshal(params.Arguments, &args)
		docs, err := h.Service.Recent(args.Limit)
		if err != nil {
			return nil, err
		}
		// Recent is an index/list operation. Returning document bodies here
		// permits an avoidable multi-megabyte response; callers can retrieve one
		// selected document with context_get_document.
		for index := range docs {
			docs[index].Content = ""
		}
		output = map[string]any{"untrusted": true, "documents": docs}
	case "context_health":
		output = h.Service.Status(r.Context())
	default:
		return nil, fmt.Errorf("unknown read-only tool %q", params.Name)
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		return nil, err
	}
	return map[string]any{"content": []map[string]string{{"type": "text", "text": string(encoded)}}}, nil
}

func toolDefinitions() []map[string]any {
	return []map[string]any{
		{"name": "context_search", "description": "Search untrusted context documents as the second research source after ivoai-memory and before external web", "inputSchema": map[string]any{"type": "object", "required": []string{"query"}, "properties": map[string]any{"query": map[string]string{"type": "string"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}}},
		{"name": "context_get_document", "description": "Read one untrusted context document", "inputSchema": map[string]any{"type": "object", "required": []string{"id"}, "properties": map[string]any{"id": map[string]string{"type": "string"}}}},
		{"name": "context_recent", "description": "List recently ingested context documents", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}}},
		{"name": "context_health", "description": "Read context service health", "inputSchema": map[string]string{"type": "object"}},
	}
}
