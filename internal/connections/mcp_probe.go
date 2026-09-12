package connections

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/externalmcp"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type probeTransport struct {
	token  string
	mu     sync.Mutex
	status int
	scheme string
}

func (t *probeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+t.token)
	response, err := http.DefaultTransport.RoundTrip(request)
	if response != nil {
		t.mu.Lock()
		t.status = response.StatusCode
		t.scheme = "none"
		fields := strings.Fields(response.Header.Get("WWW-Authenticate"))
		if len(fields) > 0 {
			switch strings.ToLower(fields[0]) {
			case "bearer", "basic", "digest":
				t.scheme = strings.ToLower(fields[0])
			default:
				t.scheme = "other"
			}
		}
		t.mu.Unlock()
	}
	return response, err
}

func (t *probeTransport) diagnostic() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return fmt.Sprintf("HTTP_STATUS=%d AUTH_CHALLENGE=%s", t.status, t.scheme)
}

func (r Registry) Test(ctx context.Context, entry config.MCPServer) (int, error) {
	tools, err := r.DiscoverTools(ctx, entry)
	return len(tools), err
}

// MCPToolCapability contains public tool metadata only. Annotation absence is
// not evidence of read-only behavior; worker projections fail closed then.
type MCPToolCapability struct {
	Name     string
	ReadOnly bool
	Metadata config.MCPTool
}

type ProbeError struct{ Health, Diagnostic string }

func (e *ProbeError) Error() string { return "external MCP " + e.Health + " (" + e.Diagnostic + ")" }
func (t *probeTransport) failure() error {
	t.mu.Lock()
	status := t.status
	t.mu.Unlock()
	health := "PROTOCOL_ERROR"
	switch status {
	case 0, 502, 503, 504:
		health = "UNREACHABLE"
	case 401:
		health = "AUTH_REQUIRED"
	case 403:
		health = "AUTH_FAILED"
	}
	return &ProbeError{Health: health, Diagnostic: t.diagnostic()}
}

func (r Registry) DiscoverTools(ctx context.Context, entry config.MCPServer) ([]MCPToolCapability, error) {
	headers, err := r.Headers(entry)
	if err != nil {
		return nil, &ProbeError{Health: "AUTH_REQUIRED", Diagnostic: "credential unavailable"}
	}
	gateway, err := externalmcp.Start([]externalmcp.Target{{Name: "probe", URL: entry.URL, Headers: headers}}, "full")
	if err != nil {
		return nil, err
	}
	defer gateway.Close()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "ivoai-mcp-test", Version: "1"}, nil)
	transport := &probeTransport{token: gateway.Token(), scheme: "none"}
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: gateway.URL(0), DisableStandaloneSSE: true, HTTPClient: &http.Client{Transport: transport}}, nil)
	if err != nil {
		return nil, transport.failure()
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, transport.failure()
	}
	if len(tools.Tools) > externalmcp.MaxInventoryTools || tools.NextCursor != "" {
		return nil, fmt.Errorf("external MCP bounded tool inventory is incomplete")
	}
	result := make([]MCPToolCapability, 0, len(tools.Tools))
	seen := map[string]bool{}
	for _, tool := range tools.Tools {
		if !externalmcp.ValidToolName(tool.Name) || seen[tool.Name] {
			return nil, fmt.Errorf("invalid or duplicate MCP tool identity")
		}
		seen[tool.Name] = true
		class := "UNKNOWN"
		if tool.Annotations != nil {
			if tool.Annotations.ReadOnlyHint {
				class = "READ_ONLY"
			} else if tool.Annotations.DestructiveHint != nil {
				class = "MUTATING"
			}
		}
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil || len(schema) > 64<<10 {
			return nil, fmt.Errorf("MCP schema exceeds inventory budget")
		}
		digest := sha256.Sum256(schema)
		description := platform.Redact(tool.Description)
		for _, values := range headers {
			for _, secret := range values {
				if secret != "" {
					description = strings.ReplaceAll(description, secret, "[REDACTED]")
				}
				if token := strings.TrimPrefix(secret, "Bearer "); token != "" {
					description = strings.ReplaceAll(description, token, "[REDACTED]")
				}
			}
		}
		description = strings.Map(func(r rune) rune {
			if r < 32 || r == 127 {
				return ' '
			}
			return r
		}, description)
		if len(description) > 256 {
			description = "Description exceeds bounded inventory display; inspect upstream documentation."
		}
		metadata := config.MCPTool{Name: tool.Name, Description: description, Classification: class, Provenance: "server_annotations_untrusted", SchemaSHA256: hex.EncodeToString(digest[:])}
		result = append(result, MCPToolCapability{Name: tool.Name, ReadOnly: class == "READ_ONLY", Metadata: metadata})
	}
	return result, nil
}
