package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type liveMCPTransport func(*http.Request) (*http.Response, error)

func (f liveMCPTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The official reference SDK consumes the unmodified wire response. Private
// evidence files capture tools/call results before SDK decoding; console output
// contains only framing, schema field names and error status.
func TestLiveP0MCPReference(t *testing.T) {
	if os.Getenv("IVOAI_LIVE_P0") != "1" {
		t.Skip("requires real Memory/Context")
	}
	a, err := New("p0-live-reference", strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := a.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	knowledge, err := a.prepareSessionKnowledge(ctx, cfg, nil, "codex", t.TempDir(), os.Environ(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer knowledge.close()
	if knowledge.router == nil {
		t.Fatal("no real knowledge sources configured")
	}
	for _, service := range []string{"memory", "context"} {
		t.Run(service, func(t *testing.T) {
			client := &http.Client{Transport: liveMCPTransport(func(request *http.Request) (*http.Response, error) {
				request = request.Clone(request.Context())
				request.Header.Set("Authorization", "Bearer "+knowledge.router.Token())
				response, err := http.DefaultTransport.RoundTrip(request)
				if err != nil {
					return nil, err
				}
				body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
				response.Body.Close()
				if err != nil {
					return nil, err
				}
				response.Body = io.NopCloser(bytes.NewReader(body))
				var envelope map[string]json.RawMessage
				if json.Unmarshal(body, &envelope) == nil {
					var result map[string]json.RawMessage
					if json.Unmarshal(envelope["result"], &result) == nil && result["content"] != nil {
						keys := []string{}
						for key := range result {
							keys = append(keys, key)
						}
						t.Logf("raw CallToolResult: Content-Type=%s protocol=%s keys=%v bytes=%d", response.Header.Get("Content-Type"), request.Header.Get("MCP-Protocol-Version"), keys, len(body))
						if dir := os.Getenv("IVOAI_LIVE_P0_EVIDENCE"); filepath.IsAbs(dir) {
							file, err := os.CreateTemp(dir, "mcp-"+service+"-*.json")
							if err == nil {
								_, err = file.Write(body)
								file.Close()
							}
							if err != nil {
								return nil, err
							}
						}
					}
				}
				return response, nil
			})}
			sdk := mcp.NewClient(&mcp.Implementation{Name: "ivoai-live-reference", Version: "1"}, nil)
			session, err := sdk.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: knowledge.router.BaseURL() + "/mcp/" + service, HTTPClient: client, DisableStandaloneSSE: true}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			listed, err := session.ListTools(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			available := map[string]bool{}
			for _, tool := range listed.Tools {
				available[tool.Name] = true
			}
			calls := []*mcp.CallToolParams{{Name: "context_health"}, {Name: "context_search", Arguments: map[string]any{"query": "Voicehub"}}}
			if service == "memory" {
				calls = []*mcp.CallToolParams{{Name: "memory_status"}, {Name: "memory_query", Arguments: map[string]any{"query": "Voicehub"}}, {Name: "memory_read_page", Arguments: map[string]any{"path": "shared/ivoai/auto-opencode-and-client-server-visibility-2026-09-04.md"}}}
			}
			for _, call := range calls {
				if !available[call.Name] {
					t.Errorf("%s unavailable", call.Name)
					continue
				}
				result, err := session.CallTool(ctx, call)
				if err != nil {
					t.Errorf("%s decoder failed: %v", call.Name, err)
					continue
				}
				t.Logf("%s reference_decode=PASS isError=%t content_blocks=%d", call.Name, result.IsError, len(result.Content))
				if result.IsError {
					t.Errorf("%s returned tool failure", call.Name)
				}
			}
		})
	}
}
